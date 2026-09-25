// Package mailer forwards WebUntis messages to an SMTP server.
package mailer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wneessen/go-mail"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	gmhtml "github.com/yuin/goldmark/renderer/html"

	"github.com/dgrieser/web-untis-cli/internal/config"
	"github.com/dgrieser/web-untis-cli/internal/render"
	"github.com/dgrieser/web-untis-cli/internal/webuntis"
)

// Validate checks that the SMTP configuration is usable.
func Validate(c config.SMTPConfig) error {
	var missing []string
	if c.Host == "" {
		missing = append(missing, "host")
	}
	if c.From == "" {
		missing = append(missing, "from")
	}
	if len(c.To) == 0 {
		missing = append(missing, "to")
	}
	if len(missing) > 0 {
		return fmt.Errorf("SMTP configuration incomplete (missing: %s); run `webuntis config smtp --help`", strings.Join(missing, ", "))
	}
	switch strings.ToLower(c.Security) {
	case "", "starttls", "tls", "ssl", "none", "opportunistic":
	default:
		return fmt.Errorf("invalid SMTP security %q (starttls, tls, none, opportunistic)", c.Security)
	}
	return nil
}

// Build creates the e-mail for a message.
func Build(c config.SMTPConfig, school, webURL string, m *webuntis.MessageDetail, files []webuntis.DownloadedFile) (*mail.Msg, error) {
	msg := mail.NewMsg()
	senderName := "WebUntis"
	if m.Sender != nil && m.Sender.DisplayName != "" {
		senderName = m.Sender.DisplayName + " via WebUntis"
	}
	if err := msg.FromFormat(senderName, c.From); err != nil {
		if err := msg.From(c.From); err != nil {
			return nil, fmt.Errorf("invalid from address %q: %w", c.From, err)
		}
	}
	if err := msg.To(c.To...); err != nil {
		return nil, fmt.Errorf("invalid recipient: %w", err)
	}
	prefix := c.SubjectPrefix
	if prefix == "" {
		prefix = "[WebUntis]"
	}
	msg.Subject(strings.TrimSpace(prefix + " " + m.Subject))
	if t := m.Sent(); !t.IsZero() {
		msg.SetDateWithValue(t)
	} else {
		msg.SetDate()
	}
	msg.SetMessageIDWithValue(fmt.Sprintf("webuntis.%s.%s.%d@webuntis-cli", school, m.Folder, m.ID))
	msg.SetGenHeader(mail.Header("X-WebUntis-Message-Id"), fmt.Sprint(m.ID))
	msg.SetGenHeader(mail.Header("X-WebUntis-School"), school)
	msg.SetUserAgent("webuntis-cli")

	mdBody := MarkdownBody(m, webURL, files)
	msg.SetBodyString(mail.TypeTextPlain, PlainBody(m, webURL, files))
	if html, err := markdownToHTML(mdBody); err == nil {
		msg.AddAlternativeString(mail.TypeTextHTML, html)
	}
	for _, f := range files {
		if f.Data == nil {
			continue
		}
		var opts []mail.FileOption
		ct := f.ContentType
		if ct == "" || strings.HasPrefix(ct, "application/octet-stream") || strings.HasPrefix(ct, "binary/") {
			ct = mime.TypeByExtension(strings.ToLower(filepath.Ext(f.Name)))
		}
		if ct != "" {
			opts = append(opts, mail.WithFileContentType(mail.ContentType(ct)))
		}
		if err := msg.AttachReader(f.Name, bytes.NewReader(f.Data), opts...); err != nil {
			return nil, fmt.Errorf("attach %q: %w", f.Name, err)
		}
	}
	return msg, nil
}

func header(m *webuntis.MessageDetail) (from, to string) {
	if m.Sender != nil {
		from = m.Sender.DisplayName
	}
	var r []string
	for _, p := range m.AllRecipients() {
		r = append(r, p.DisplayName)
	}
	return from, strings.Join(r, ", ")
}

// PlainBody renders the text/plain part.
func PlainBody(m *webuntis.MessageDetail, webURL string, files []webuntis.DownloadedFile) string {
	var b strings.Builder
	from, to := header(m)
	fmt.Fprintf(&b, "Von:     %s\n", from)
	if to != "" {
		fmt.Fprintf(&b, "An:      %s\n", to)
	}
	fmt.Fprintf(&b, "Datum:   %s\n", m.Sent().Format("02.01.2006 15:04"))
	fmt.Fprintf(&b, "Betreff: %s\n", m.Subject)
	b.WriteString(strings.Repeat("-", 60) + "\n\n")
	content := m.Content
	if render.LooksLikeHTML(content) {
		content = render.StripTags(content)
	}
	b.WriteString(strings.TrimSpace(content) + "\n")
	writeLinks(&b, files, false)
	for _, h := range m.ReplyHistory {
		hf := ""
		if h.Sender != nil {
			hf = h.Sender.DisplayName
		}
		fmt.Fprintf(&b, "\n\n----- %s, %s -----\n", hf, h.Sent().Format("02.01.2006 15:04"))
		c := h.Content
		if render.LooksLikeHTML(c) {
			c = render.StripTags(c)
		}
		for _, line := range strings.Split(strings.TrimSpace(c), "\n") {
			b.WriteString("> " + line + "\n")
		}
	}
	if webURL != "" {
		fmt.Fprintf(&b, "\n--\nIn WebUntis öffnen: %s\n", webURL)
	}
	return b.String()
}

func writeLinks(b *strings.Builder, files []webuntis.DownloadedFile, md bool) {
	var links []webuntis.DownloadedFile
	for _, f := range files {
		if f.Link != "" {
			links = append(links, f)
		}
	}
	if len(links) == 0 {
		return
	}
	b.WriteString("\nLinks:\n")
	for _, f := range links {
		if md {
			fmt.Fprintf(b, "- [%s](%s)\n", render.Esc(f.Name), f.Link)
		} else {
			fmt.Fprintf(b, "- %s: %s\n", f.Name, f.Link)
		}
	}
}

// MarkdownBody renders the message as Markdown (used for the HTML part).
func MarkdownBody(m *webuntis.MessageDetail, webURL string, files []webuntis.DownloadedFile) string {
	var b strings.Builder
	from, to := header(m)
	fmt.Fprintf(&b, "**Von:** %s  \n", render.Esc(from))
	if to != "" {
		fmt.Fprintf(&b, "**An:** %s  \n", render.Esc(to))
	}
	fmt.Fprintf(&b, "**Datum:** %s\n\n---\n\n", m.Sent().Format("02.01.2006 15:04"))
	b.WriteString(render.Body(m.Content) + "\n")
	writeLinks(&b, files, true)
	for _, h := range m.ReplyHistory {
		hf := ""
		if h.Sender != nil {
			hf = h.Sender.DisplayName
		}
		fmt.Fprintf(&b, "\n---\n\n**%s**, %s\n\n", render.Esc(hf), h.Sent().Format("02.01.2006 15:04"))
		for _, line := range strings.Split(render.Body(h.Content), "\n") {
			b.WriteString("> " + line + "\n")
		}
	}
	if webURL != "" {
		fmt.Fprintf(&b, "\n---\n\n[In WebUntis öffnen](%s)\n", webURL)
	}
	return b.String()
}

func markdownToHTML(src string) (string, error) {
	gm := goldmark.New(goldmark.WithExtensions(extension.GFM, extension.Linkify), goldmark.WithRendererOptions(gmhtml.WithHardWraps()))
	var buf bytes.Buffer
	buf.WriteString(`<!doctype html><html><body style="font-family: -apple-system, Segoe UI, Roboto, Helvetica, Arial, sans-serif; font-size: 14px; line-height: 1.5">`)
	if err := gm.Convert([]byte(src), &buf); err != nil {
		return "", err
	}
	buf.WriteString("</body></html>")
	return buf.String(), nil
}

// Send delivers messages via SMTP.
func Send(ctx context.Context, c config.SMTPConfig, msgs ...*mail.Msg) error {
	if len(msgs) == 0 {
		return nil
	}
	password := c.Password
	if p := os.Getenv("WEBUNTIS_SMTP_PASSWORD"); p != "" {
		password = p
	}
	opts := []mail.Option{mail.WithTimeout(60 * time.Second)}
	port := c.Port
	switch strings.ToLower(c.Security) {
	case "tls", "ssl":
		opts = append(opts, mail.WithSSL())
		if port == 0 {
			port = 465
		}
	case "none":
		opts = append(opts, mail.WithTLSPolicy(mail.NoTLS))
		if port == 0 {
			port = 25
		}
	case "opportunistic":
		opts = append(opts, mail.WithTLSPolicy(mail.TLSOpportunistic))
		if port == 0 {
			port = 587
		}
	default: // starttls
		opts = append(opts, mail.WithTLSPolicy(mail.TLSMandatory))
		if port == 0 {
			port = 587
		}
	}
	opts = append(opts, mail.WithPort(port))
	if c.Username != "" {
		opts = append(opts, mail.WithSMTPAuth(mail.SMTPAuthAutoDiscover), mail.WithUsername(c.Username), mail.WithPassword(password))
	}
	client, err := mail.NewClient(c.Host, opts...)
	if err != nil {
		return err
	}
	if err := client.DialAndSendWithContext(ctx, msgs...); err != nil {
		return fmt.Errorf("SMTP %s:%d: %w", c.Host, port, err)
	}
	return nil
}

// State tracks which messages have been forwarded already.
type State struct {
	path      string
	Forwarded map[string]time.Time `json:"forwarded"` // key: folder/id
}

// LoadState reads the forward state file.
func LoadState(path string) (*State, error) {
	s := &State{path: path, Forwarded: map[string]time.Time{}}
	err := config.ReadJSON(path, s)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if s.Forwarded == nil {
		s.Forwarded = map[string]time.Time{}
	}
	return s, nil
}

// Key builds the state key of a message.
func Key(folder string, id int) string { return fmt.Sprintf("%s/%d", folder, id) }

// Has reports whether a message was forwarded.
func (s *State) Has(folder string, id int) bool {
	_, ok := s.Forwarded[Key(folder, id)]
	return ok
}

// Mark records a forwarded message.
func (s *State) Mark(folder string, id int) { s.Forwarded[Key(folder, id)] = time.Now() }

// Save writes the state file.
func (s *State) Save() error { return config.WriteJSON(s.path, s) }
