package webuntis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// Klassengeld (klassengeld.app) is a third party addin for class funds.
// It authenticates via WebUntis OAuth (SSO) and renders server side HTML;
// the data grids are embedded as JavaScript arrays which we extract.

const klassengeldHost = "klassengeld.app"

// KGProject is a collection ("Projekt") with its payment status.
type KGProject struct {
	Name   string            `json:"name" yaml:"name"`
	Due    string            `json:"due" yaml:"due"`
	Amount float64           `json:"amount" yaml:"amount"`
	Status string            `json:"status" yaml:"status"`
	Info   string            `json:"info,omitempty" yaml:"info,omitempty"`
	Extras string            `json:"extras,omitempty" yaml:"extras,omitempty"`
	Fields map[string]string `json:"fields,omitempty" yaml:"fields,omitempty"`
}

// KGTransaction is a booking on the student account.
type KGTransaction struct {
	Direction string   `json:"direction" yaml:"direction"` // in / out
	Date      string   `json:"date" yaml:"date"`
	Amount    float64  `json:"amount" yaml:"amount"`
	Type      string   `json:"type" yaml:"type"`
	Details   string   `json:"details" yaml:"details"`
	Extra     []string `json:"extra,omitempty" yaml:"extra,omitempty"`
}

// KGReservation is an amount reserved for a project.
type KGReservation struct {
	Name   string  `json:"name" yaml:"name"`
	Amount float64 `json:"amount" yaml:"amount"`
}

// KGStudent is one child with account balance, projects and transactions.
type KGStudent struct {
	Name         string          `json:"name" yaml:"name"`
	School       string          `json:"school" yaml:"school"`
	Balance      float64         `json:"balance" yaml:"balance"`
	AccountID    int             `json:"accountId" yaml:"accountId"`
	Projects     []KGProject     `json:"projects" yaml:"projects"`
	Reservations []KGReservation `json:"reservations,omitempty" yaml:"reservations,omitempty"`
	Transactions []KGTransaction `json:"transactions,omitempty" yaml:"transactions,omitempty"`
}

// Klassengeld is the overview of all children.
type Klassengeld struct {
	URL      string      `json:"url" yaml:"url"`
	Students []KGStudent `json:"students" yaml:"students"`
}

func (c *Client) kgGet(ctx context.Context, rawURL string) (string, *url.URL, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; webuntis-cli)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, err
	}
	c.debugf("GET %s -> %d (final %s)", redactURL(rawURL), resp.StatusCode, redactURL(resp.Request.URL.String()))
	if resp.StatusCode >= 400 {
		return "", resp.Request.URL, &APIError{Status: resp.StatusCode, URL: redactURL(rawURL), Body: string(b)}
	}
	return string(b), resp.Request.URL, nil
}

func kgIsLoginPage(body string, final *url.URL) bool {
	if final != nil && final.Host == klassengeldHost && strings.Contains(final.Path, "login") {
		return true
	}
	if final != nil && final.Host != klassengeldHost {
		return true
	}
	return strings.Contains(body, `name="password"`) && !strings.Contains(body, "show_saldo_info")
}

// Klassengeld logs into klassengeld.app via WebUntis SSO (if necessary)
// and returns balances, projects and (optionally) transactions.
func (c *Client) Klassengeld(ctx context.Context, app *PlatformApp, withTransactions bool) (*Klassengeld, error) {
	c.TrackHost(klassengeldHost)
	defer func() { _ = c.SaveSession() }()

	dash := "https://" + klassengeldHost + "/dashboard"
	body, final, err := c.kgGet(ctx, dash)
	if err != nil || kgIsLoginPage(body, final) {
		c.debugf("klassengeld: no valid session, starting WebUntis SSO")
		// Make sure the WebUntis session is alive (SSO relies on it).
		if _, err := c.Token(ctx); err != nil {
			return nil, err
		}
		if _, _, err = c.kgGet(ctx, app.RedirectURL); err != nil {
			return nil, fmt.Errorf("klassengeld SSO: %w", err)
		}
		body, final, err = c.kgGet(ctx, dash)
		if err != nil {
			return nil, err
		}
		if kgIsLoginPage(body, final) {
			return nil, errors.New("klassengeld SSO failed: still on login page (is the addin enabled for your account?)")
		}
	}
	kg := &Klassengeld{URL: dash}
	kg.Students, err = parseKGDashboard(body)
	if err != nil {
		return nil, err
	}
	if withTransactions {
		for i := range kg.Students {
			s := &kg.Students[i]
			if s.AccountID == 0 {
				continue
			}
			frag, _, err := c.kgGet(ctx, "https://"+klassengeldHost+"/ajax/saldo_account/"+strconv.Itoa(s.AccountID))
			if err != nil {
				return kg, fmt.Errorf("transactions of %s: %w", s.Name, err)
			}
			s.Reservations, s.Transactions = parseKGSaldo(frag)
		}
	}
	return kg, nil
}

var (
	kgHeaderRe  = regexp.MustCompile(`<strong[^>]*>\s*([^<]+?)\s*</strong>\s*-\s*([^<]+?)\s*</div>`)
	kgBalanceRe = regexp.MustCompile(`(?s)(-?[\d.]+,\d{2})\s*(?:&euro;|€)\s*</span>\s*</div>\s*<span>\s*Kontostand`)
	kgSaldoRe   = regexp.MustCompile(`show_saldo_info\((\d+)\)`)
	kgDSRe      = regexp.MustCompile(`dataSource:\s*([A-Za-z_$][\w$]*)`)
	kgCaptionRe = regexp.MustCompile(`caption:\s*"([^"]*)",\s*dataField:\s*"(fieldtxt\d+)"`)
	kgSortKeyRe = regexp.MustCompile(`^\{[^}]*\}`)
	kgTagRe     = regexp.MustCompile(`<[^>]*>`)
)

// kgGrid is an extracted DevExtreme grid: column captions + rows.
type kgGrid struct {
	captions map[string]string // fieldtxtN -> caption
	rows     []map[string]any
}

func extractKGGrids(page string) []kgGrid {
	var grids []kgGrid
	dsMatches := kgDSRe.FindAllStringSubmatchIndex(page, -1)
	for i, m := range dsMatches {
		name := page[m[2]:m[3]]
		end := len(page)
		if i+1 < len(dsMatches) {
			end = dsMatches[i+1][0]
		}
		g := kgGrid{captions: map[string]string{}}
		for _, cm := range kgCaptionRe.FindAllStringSubmatch(page[m[1]:end], -1) {
			g.captions[cm[2]] = cm[1]
		}
		if _, rest, ok := strings.Cut(page, "var "+name+" = "); ok {
			_ = json.NewDecoder(strings.NewReader(rest)).Decode(&g.rows)
		}
		grids = append(grids, g)
	}
	return grids
}

func kgText(v any) string {
	s, _ := v.(string)
	s = kgSortKeyRe.ReplaceAllString(s, "")
	if strings.Contains(s, "<") {
		if doc, err := goquery.NewDocumentFromReader(strings.NewReader(s)); err == nil {
			s = doc.Text()
		} else {
			s = kgTagRe.ReplaceAllString(s, " ")
		}
	}
	return strings.Join(strings.Fields(html.UnescapeString(s)), " ")
}

func parseEuro(s string) float64 {
	s = strings.TrimSpace(strings.NewReplacer("€", "", "&euro;", "", "EUR", "", " ", "", " ", "").Replace(s))
	if strings.Contains(s, ",") {
		s = strings.ReplaceAll(s, ".", "")
		s = strings.ReplaceAll(s, ",", ".")
	}
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func parseKGDashboard(page string) ([]KGStudent, error) {
	headers := kgHeaderRe.FindAllStringSubmatch(page, -1)
	balances := kgBalanceRe.FindAllStringSubmatch(page, -1)
	saldo := kgSaldoRe.FindAllStringSubmatch(page, -1)
	grids := extractKGGrids(page)
	if len(headers) == 0 && len(grids) == 0 {
		return nil, errors.New("klassengeld: could not parse dashboard (layout changed?)")
	}
	n := max(len(headers), len(grids))
	var out []KGStudent
	seenSaldo := map[string]bool{}
	var saldoIDs []int
	for _, s := range saldo {
		if !seenSaldo[s[1]] {
			seenSaldo[s[1]] = true
			id, _ := strconv.Atoi(s[1])
			saldoIDs = append(saldoIDs, id)
		}
	}
	for i := range n {
		var st KGStudent
		if i < len(headers) {
			st.Name = html.UnescapeString(headers[i][1])
			st.School = html.UnescapeString(headers[i][2])
		}
		if i < len(balances) {
			st.Balance = parseEuro(balances[i][1])
		}
		if i < len(saldoIDs) {
			st.AccountID = saldoIDs[i]
		}
		if i < len(grids) {
			g := grids[i]
			for _, row := range g.rows {
				p := KGProject{Fields: map[string]string{}}
				for k, cap := range g.captions {
					v := kgText(row[k])
					switch strings.ToLower(cap) {
					case "projekt":
						p.Name = v
					case "frist":
						p.Due = v
					case "betrag":
						p.Amount = parseEuro(v)
					case "zahlungsanforderung":
						p.Status = v
					case "weitere informationen":
						p.Info = v
					case "extras":
						p.Extras = v
					default:
						if v != "" {
							name := cap
							if name == "" {
								name = k
							}
							p.Fields[name] = v
						}
					}
				}
				if len(p.Fields) == 0 {
					p.Fields = nil
				}
				if p.Name != "" || p.Amount != 0 {
					st.Projects = append(st.Projects, p)
				}
			}
		}
		out = append(out, st)
	}
	return out, nil
}

func parseKGSaldo(frag string) ([]KGReservation, []KGTransaction) {
	var res []KGReservation
	if doc, err := goquery.NewDocumentFromReader(strings.NewReader(frag)); err == nil {
		doc.Find("table").First().Find("tr").Each(func(_ int, tr *goquery.Selection) {
			tds := tr.Find("td")
			if tds.Length() >= 2 {
				name := strings.TrimSpace(tds.Eq(0).Text())
				if name != "" {
					res = append(res, KGReservation{Name: strings.Join(strings.Fields(name), " "), Amount: parseEuro(tds.Eq(1).Text())})
				}
			}
		})
	}
	var txs []KGTransaction
	for _, g := range extractKGGrids(frag) {
		for _, row := range g.rows {
			icon, _ := row["field0"].(string)
			t := KGTransaction{
				Date:    kgText(row["fieldtxt1"]),
				Amount:  parseEuro(kgText(row["fieldtxt2"])),
				Type:    kgText(row["fieldtxt3"]),
				Details: kgText(row["fieldtxt4"]),
			}
			switch {
			case strings.Contains(icon, "arrow-right"), strings.Contains(icon, "plus"):
				t.Direction = "in"
			case strings.Contains(icon, "arrow-left"), strings.Contains(icon, "minus"):
				t.Direction = "out"
			}
			for i := 5; i < 10; i++ {
				if v := kgText(row["fieldtxt"+strconv.Itoa(i)]); v != "" {
					t.Extra = append(t.Extra, v)
				}
			}
			txs = append(txs, t)
		}
	}
	return res, txs
}

// KlassengeldCached wraps Klassengeld with a short-lived cache.
func (c *Client) KlassengeldCached(ctx context.Context, app *PlatformApp, withTransactions bool) (*Klassengeld, error) {
	key := fmt.Sprintf("%s|%s|klassengeld|%v", c.Profile.Server, c.Profile.Username, withTransactions)
	if b, ok := c.Cache.Get(key); ok {
		var kg Klassengeld
		if json.Unmarshal(b, &kg) == nil {
			return &kg, nil
		}
	}
	kg, err := c.Klassengeld(ctx, app, withTransactions)
	if err != nil {
		return nil, err
	}
	if b, err := json.Marshal(kg); err == nil {
		c.Cache.Put(key, b, 10*time.Minute)
	}
	return kg, nil
}
