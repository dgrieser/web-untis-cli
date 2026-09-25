package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"

	"github.com/dgrieser/web-untis-cli/internal/config"
	"github.com/dgrieser/web-untis-cli/internal/mailer"
	"github.com/dgrieser/web-untis-cli/internal/render"
	"github.com/dgrieser/web-untis-cli/internal/webuntis"
)

// ---------------------------------------------------------------- login

func prompt(label string) (string, error) {
	fmt.Fprint(os.Stderr, label)
	r := bufio.NewReader(os.Stdin)
	s, err := r.ReadString('\n')
	if err != nil && s == "" {
		return "", err
	}
	return strings.TrimSpace(s), nil
}

func promptPassword(label string) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("no terminal for password prompt: use --password-stdin or WEBUNTIS_PASSWORD")
	}
	fmt.Fprint(os.Stderr, label)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return string(b), err
}

func readStdinSecret() (string, error) {
	b, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && b == "" {
		return "", err
	}
	return strings.TrimRight(b, "\r\n"), nil
}

// resolveSchool turns "https://x.webuntis.com/...", "x.webuntis.com",
// "x.webuntis.com?school=y" or a school name into server + login name.
func resolveSchool(ctx context.Context, input, schoolFlag string) (server, school, tenant, display string, err error) {
	in := strings.TrimSpace(input)
	if in != "" && !strings.Contains(in, "://") && strings.Contains(in, ".") {
		in = "https://" + in
	}
	if u, perr := url.Parse(in); perr == nil && u.Host != "" {
		server = u.Host
		school = firstNonEmpty(schoolFlag, u.Query().Get("school"))
		if school == "" {
			// New-style hosts: <school>.webuntis.com
			school = strings.Split(server, ".")[0]
		}
	} else {
		school = firstNonEmpty(schoolFlag, in)
	}
	if school == "" {
		return "", "", "", "", errors.New("school required")
	}
	// Verify / enrich via the public school search.
	res, serr := webuntis.SearchSchools(ctx, school)
	if serr == nil {
		for _, s := range res {
			if strings.EqualFold(s.LoginName, school) && (server == "" || strings.EqualFold(s.Server, server)) {
				return s.Server, s.LoginName, s.TenantID, s.DisplayName, nil
			}
		}
		if server == "" && len(res) == 1 {
			s := res[0]
			return s.Server, s.LoginName, s.TenantID, s.DisplayName, nil
		}
		if server == "" && len(res) > 1 {
			var names []string
			for _, s := range res {
				names = append(names, fmt.Sprintf("%s (%s, %s)", s.DisplayName, s.LoginName, s.Server))
			}
			return "", "", "", "", fmt.Errorf("several schools match %q, use --school <loginName> or the school URL:\n  %s", school, strings.Join(names, "\n  "))
		}
	}
	if server == "" {
		return "", "", "", "", fmt.Errorf("school %q not found (try `webuntis schools search <name>`)", school)
	}
	return server, school, "", "", nil
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if strings.TrimSpace(x) != "" {
			return strings.TrimSpace(x)
		}
	}
	return ""
}

func (a *app) loginCmd() *cobra.Command {
	var school, user string
	var pwStdin, noStore bool
	cmd := &cobra.Command{
		Use:   "login [SCHOOL-URL | SERVER | SCHOOL]",
		Short: "Log in and store school + credentials",
		Long: `Log in to WebUntis and store school, username and password in the profile
directory (default ~/.cache/webuntis-cli/<profile>/config.json, mode 0600).

Without arguments on a terminal, an interactive setup starts (same as
"webuntis setup"): pick/create a profile, search the school, enter
credentials, choose the default student.

The school can be given as URL (https://ge-huellhorst.webuntis.com/today),
host name, or school login name. Use --no-store-password to only keep the
session cookie (you will have to log in again when it expires).`,
		Example: `  webuntis login https://ge-huellhorst.webuntis.com
  webuntis login ge-huellhorst --user jane@example.com
  echo "$PW" | webuntis login ge-huellhorst -u jane --password-stdin
  webuntis login -p kid2 neilo.webuntis.com --school my-school`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := a.profileName()
			if err := config.ValidateProfileName(name); err != nil {
				return err
			}
			if len(args) == 0 && school == "" && user == "" && !pwStdin && interactive() {
				return a.runSetup(ctx, noStore)
			}
			existing, _ := config.Load(name)
			input := ""
			if len(args) > 0 {
				input = args[0]
			}
			if input == "" && school == "" && existing != nil {
				input, school = existing.Server, existing.School
			}
			if input == "" && school == "" {
				var err error
				if input, err = prompt("School (URL, server or name): "); err != nil {
					return err
				}
			}
			server, loginName, tenant, display, err := resolveSchool(ctx, input, school)
			if err != nil {
				return err
			}
			if user == "" && existing != nil && existing.School == loginName {
				user = existing.Username
			}
			if user == "" {
				if user, err = prompt("Username: "); err != nil {
					return err
				}
			}
			var password string
			switch {
			case pwStdin:
				password, err = readStdinSecret()
			case os.Getenv("WEBUNTIS_PASSWORD") != "":
				password = os.Getenv("WEBUNTIS_PASSWORD")
			default:
				password, err = promptPassword(fmt.Sprintf("Password for %s (school %s): ", user, loginName))
			}
			if err != nil {
				return err
			}
			p := &config.Profile{Name: name, Server: server, School: loginName, TenantID: tenant, SchoolDisplayName: display, Username: user, Password: password}
			ad, err := a.performLogin(ctx, p, existing, noStore)
			if err != nil {
				return err
			}
			return a.printLoginSummary(p, ad)
		},
	}
	cmd.Flags().StringVar(&school, "school", "", "school login name (if not part of the URL)")
	cmd.Flags().StringVarP(&user, "user", "u", "", "username")
	cmd.Flags().BoolVar(&pwStdin, "password-stdin", false, "read the password from stdin")
	cmd.Flags().BoolVar(&noStore, "no-store-password", false, "do not store the password on disk")
	return cmd
}

// performLogin logs in with p, stores the profile and makes it current
// (unless --profile was given explicitly).
func (a *app) performLogin(ctx context.Context, p, existing *config.Profile, noStore bool) (*webuntis.AppData, error) {
	if existing != nil {
		p.SMTP, p.Student, p.Timezone = existing.SMTP, existing.Student, existing.Timezone
	}
	_ = p.ClearSession()
	c, err := webuntis.New(p, webuntis.Options{Debug: a.debug, NoCache: true})
	if err != nil {
		return nil, err
	}
	if err := c.Login(ctx); err != nil {
		return nil, err
	}
	ad, err := c.AppData(ctx)
	if err != nil {
		return nil, err
	}
	if noStore {
		p.Password = ""
	}
	if err := p.Save(); err != nil {
		return nil, err
	}
	a.client = c
	if a.profile == "" || a.profile == p.Name {
		_ = config.SetCurrentProfile(p.Name)
	}
	return ad, nil
}

func (a *app) printLoginSummary(p *config.Profile, ad *webuntis.AppData) error {
	var d render.Doc
	d.H(2, "✅ Angemeldet")
	var kids []string
	for _, s := range ad.User.Students {
		kids = append(kids, render.Esc(s.DisplayName))
	}
	person := ""
	if ad.User.Person != nil {
		person = ad.User.Person.DisplayName
	}
	d.KV("Schule", render.Esc(firstNonEmpty(p.SchoolDisplayName, ad.Tenant.DisplayName, p.School)),
		"Server", p.Server, "Benutzer", "`"+ad.User.Name+"`", "Person", render.Esc(person),
		"Rollen", strings.Join(ad.User.Roles, ", "), "Schüler", strings.Join(kids, ", "),
		"Standard-Schüler", render.Esc(p.Student),
		"Schuljahr", ad.CurrentSchoolYear.Name, "Profil", p.Name, "Gespeichert in", p.Path())
	return a.renderer().Markdown(d.String())
}

func (a *app) logoutCmd() *cobra.Command {
	var forget bool
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "End the session (and optionally forget the profile)",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.api()
			if err != nil {
				return err
			}
			if err := c.Logout(cmd.Context()); err != nil {
				return err
			}
			a.client = nil
			if forget {
				if err := os.RemoveAll(c.Profile.Path()); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Profile %q removed.\n", c.Profile.Name)
				return nil
			}
			fmt.Fprintln(os.Stderr, "Logged out (credentials kept; use --forget to remove them).")
			return nil
		},
	}
	cmd.Flags().BoolVar(&forget, "forget", false, "also delete stored credentials, settings and cache of the profile")
	return cmd
}

type statusInfo struct {
	Profile   string             `json:"profile" yaml:"profile"`
	Server    string             `json:"server" yaml:"server"`
	School    string             `json:"school" yaml:"school"`
	Display   string             `json:"schoolDisplayName" yaml:"schoolDisplayName"`
	Username  string             `json:"username" yaml:"username"`
	Roles     []string           `json:"roles" yaml:"roles"`
	Students  []webuntis.Student `json:"students" yaml:"students"`
	Year      string             `json:"schoolYear" yaml:"schoolYear"`
	Password  bool               `json:"passwordStored" yaml:"passwordStored"`
	SMTP      bool               `json:"smtpConfigured" yaml:"smtpConfigured"`
	ConfigDir string             `json:"configDir" yaml:"configDir"`
}

func (a *app) statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Aliases: []string{"whoami"},
		Short:   "Show login status, school and students",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.api()
			if err != nil {
				return err
			}
			ad, err := c.AppData(cmd.Context())
			if err != nil {
				return err
			}
			students, _ := c.Students(cmd.Context())
			p := c.Profile
			info := statusInfo{Profile: p.Name, Server: p.Server, School: p.School, Display: firstNonEmpty(p.SchoolDisplayName, ad.Tenant.DisplayName),
				Username: ad.User.Name, Roles: ad.User.Roles, Students: students, Year: ad.CurrentSchoolYear.Name,
				Password: p.Password != "", SMTP: mailer.Validate(p.SMTP) == nil, ConfigDir: p.Path()}
			return a.emit(info, func() string {
				var d render.Doc
				d.H(2, "%s", render.Esc(info.Display))
				var s []string
				for _, st := range students {
					s = append(s, fmt.Sprintf("%s (%d)", render.Esc(st.Name), st.ID))
				}
				d.KV("Profil", info.Profile, "Server", info.Server, "Schule", info.School, "Benutzer", "`"+info.Username+"`",
					"Rollen", strings.Join(info.Roles, ", "), "Schüler", strings.Join(s, ", "), "Schuljahr", info.Year,
					"Passwort gespeichert", render.Check(info.Password)+map[bool]string{true: "", false: "nein"}[info.Password],
					"SMTP konfiguriert", render.Check(info.SMTP)+map[bool]string{true: "", false: "nein"}[info.SMTP],
					"Verzeichnis", info.ConfigDir)
				return d.String()
			}, nil)
		},
	}
}

func (a *app) studentsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "students",
		Short: "List the students (children) visible to this account",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.api()
			if err != nil {
				return err
			}
			st, err := c.Students(cmd.Context())
			if err != nil {
				return err
			}
			return a.emit(st, func() string {
				var rows [][]string
				for _, s := range st {
					def := ""
					if c.Profile.Student != "" && (fmt.Sprint(s.ID) == c.Profile.Student || strings.Contains(strings.ToLower(s.Name), strings.ToLower(c.Profile.Student))) {
						def = "✓"
					}
					rows = append(rows, []string{fmt.Sprint(s.ID), render.Esc(s.Name), def})
				}
				var d render.Doc
				d.H(2, "Schüler")
				d.Table([]string{"ID", "Name", "Standard"}, rows)
				d.P("_Auswahl mit `--student <name|id>` oder `webuntis config set student <name|id>`_")
				return d.String()
			}, nil)
		},
	}
}

func (a *app) schoolsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "schools", Short: "Search the public WebUntis school directory"}
	cmd.AddCommand(&cobra.Command{
		Use:     "search QUERY",
		Short:   "Search schools by name, city or login name",
		Example: "  webuntis schools search hüllhorst",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := webuntis.SearchSchools(cmd.Context(), strings.Join(args, " "))
			if err != nil {
				return err
			}
			return a.emit(res, func() string {
				var d render.Doc
				d.H(2, "Schulen (%d)", len(res))
				var rows [][]string
				for _, s := range res {
					rows = append(rows, []string{render.Esc(s.DisplayName), render.Esc(s.Address), s.LoginName, s.Server})
				}
				d.Table([]string{"Name", "Adresse", "Login-Name", "Server"}, rows)
				d.P("_Anmelden: `webuntis login <server> --school <login-name>`_")
				return d.String()
			}, nil)
		},
	})
	return cmd
}

// ---------------------------------------------------------------- config

func (a *app) configCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Show or change profile settings (SMTP, default student, time zone)"}
	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show the profile configuration (secrets masked)",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := a.loadProfile()
			if err != nil {
				return err
			}
			masked := *p
			if masked.Password != "" {
				masked.Password = "********"
			}
			if masked.SMTP.Password != "" {
				masked.SMTP.Password = "********"
			}
			if a.format == render.Pretty || a.format == render.Markdown {
				b, _ := json.MarshalIndent(masked, "", "  ")
				return a.renderer().Markdown("## Profil `" + p.Name + "`\n\n```json\n" + string(b) + "\n```\n")
			}
			return a.renderer().Data(masked)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "set KEY VALUE",
		Short: "Set a profile value: student, timezone",
		Example: `  webuntis config set student Liana
  webuntis config set timezone Europe/Berlin`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := a.loadProfile()
			if err != nil {
				return err
			}
			switch args[0] {
			case "student":
				p.Student = args[1]
			case "timezone", "tz":
				if _, err := time.LoadLocation(args[1]); err != nil {
					return err
				}
				p.Timezone = args[1]
			default:
				return fmt.Errorf("unknown key %q (student, timezone)", args[0])
			}
			return p.Save()
		},
	})
	cmd.AddCommand(a.smtpConfigCmd())
	return cmd
}

func (a *app) smtpConfigCmd() *cobra.Command {
	var host, user, from, security, prefix string
	var port int
	var to []string
	var pwStdin, pwPrompt, clear bool
	cmd := &cobra.Command{
		Use:   "smtp",
		Short: "Configure the SMTP server used by `messages forward` / `news forward`",
		Long: `Configure the SMTP server used by "webuntis messages forward" and
"webuntis news forward".

Without flags on a terminal, an interactive wizard starts (provider presets,
test mail). "webuntis config smtp test" sends a test mail.

Security modes: starttls (default, port 587), tls (implicit TLS, port 465),
opportunistic (STARTTLS if offered), none (plain, port 25).
The SMTP password can also be provided via $WEBUNTIS_SMTP_PASSWORD.`,
		Example: `  webuntis config smtp                 # interactive
  webuntis config smtp test            # send a test mail
  webuntis config smtp --host smtp.example.com --user me@example.com --password-prompt \
      --from me@example.com --to me@example.com
  webuntis config smtp --security tls --port 465`,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := a.loadProfile()
			if err != nil {
				return err
			}
			if clear {
				p.SMTP = config.SMTPConfig{}
				return p.Save()
			}
			f := cmd.Flags()
			if !anyChanged(f, "host", "port", "user", "from", "to", "security", "subject-prefix", "password-stdin", "password-prompt") && interactive() {
				return a.runSMTPSetup(cmd.Context(), p)
			}
			if f.Changed("host") {
				p.SMTP.Host = host
			}
			if f.Changed("port") {
				p.SMTP.Port = port
			}
			if f.Changed("user") {
				p.SMTP.Username = user
			}
			if f.Changed("from") {
				p.SMTP.From = from
			}
			if f.Changed("to") {
				p.SMTP.To = to
			}
			if f.Changed("security") {
				p.SMTP.Security = security
			}
			if f.Changed("subject-prefix") {
				p.SMTP.SubjectPrefix = prefix
			}
			switch {
			case pwStdin:
				if p.SMTP.Password, err = readStdinSecret(); err != nil {
					return err
				}
			case pwPrompt:
				if p.SMTP.Password, err = promptPassword("SMTP password: "); err != nil {
					return err
				}
			}
			if err := p.Save(); err != nil {
				return err
			}
			if err := mailer.Validate(p.SMTP); err != nil {
				fmt.Fprintln(os.Stderr, "Saved, but:", err)
				return nil
			}
			fmt.Fprintf(os.Stderr, "SMTP saved: %s → %s via %s\n", p.SMTP.From, strings.Join(p.SMTP.To, ", "), p.SMTP.Host)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&host, "host", "", "SMTP host")
	f.IntVar(&port, "port", 0, "SMTP port (default by security mode)")
	f.StringVar(&user, "user", "", "SMTP username (empty: no auth)")
	f.BoolVar(&pwStdin, "password-stdin", false, "read SMTP password from stdin")
	f.BoolVar(&pwPrompt, "password-prompt", false, "prompt for SMTP password")
	f.StringVar(&from, "from", "", "sender address")
	f.StringSliceVar(&to, "to", nil, "recipient address(es), comma separated")
	f.StringVar(&security, "security", "", "starttls, tls, opportunistic, none")
	f.StringVar(&prefix, "subject-prefix", "", `subject prefix (default "[WebUntis]")`)
	f.BoolVar(&clear, "clear", false, "remove SMTP configuration")
	cmd.AddCommand(&cobra.Command{
		Use:   "test",
		Short: "Send a test mail with the configured SMTP settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := a.loadProfile()
			if err != nil {
				return err
			}
			return sendTestMail(cmd.Context(), p)
		},
	})
	return cmd
}

func (a *app) profilesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profiles",
		Short: "List profiles (several schools / accounts)",
		RunE: func(cmd *cobra.Command, args []string) error {
			names, err := config.ListProfiles()
			if err != nil {
				return err
			}
			cur := config.CurrentProfileName()
			type row struct {
				Name    string `json:"name" yaml:"name"`
				Current bool   `json:"current" yaml:"current"`
				Server  string `json:"server" yaml:"server"`
				School  string `json:"school" yaml:"school"`
				User    string `json:"username" yaml:"username"`
			}
			var list []row
			for _, n := range names {
				p, err := config.Load(n)
				if err != nil {
					continue
				}
				list = append(list, row{n, n == cur, p.Server, p.School, p.Username})
			}
			return a.emit(list, func() string {
				var rows [][]string
				for _, r := range list {
					rows = append(rows, []string{r.Name, render.Check(r.Current), r.School, r.Server, render.Esc(r.User)})
				}
				var d render.Doc
				d.H(2, "Profile")
				if len(rows) == 0 {
					return d.Empty("Keine Profile. `webuntis login` legt eines an.").String()
				}
				d.Table([]string{"Name", "Aktiv", "Schule", "Server", "Benutzer"}, rows)
				d.P("_Wechseln: `webuntis profiles use <name>` · einmalig: `--profile <name>`_")
				return d.String()
			}, nil)
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "use NAME",
		Short: "Switch the active profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := config.Load(args[0]); err != nil {
				return fmt.Errorf("profile %q: %w", args[0], err)
			}
			return config.SetCurrentProfile(args[0])
		},
	})
	return cmd
}

func (a *app) cacheCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "cache", Short: "Manage the local response cache"}
	cmd.AddCommand(&cobra.Command{
		Use:   "clear",
		Short: "Delete all cached responses of the profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.api()
			if err != nil {
				return err
			}
			return c.Cache.Clear()
		},
	})
	return cmd
}

// ---------------------------------------------------------------- raw api

func (a *app) apiCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "api",
		Short: "Raw read-only access to the WebUntis APIs (for exploring)",
	}
	var params []string
	get := &cobra.Command{
		Use:   "get PATH",
		Short: "GET an API path with the current session and print the JSON",
		Example: `  webuntis api get /WebUntis/api/rest/view/v1/app/data
  webuntis api get /WebUntis/api/homeworks/lessons -q startDate=20260901 -q endDate=20260930
  webuntis api get rest/view/v1/messages`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.api()
			if err != nil {
				return err
			}
			path := args[0]
			if !strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "http") {
				path = "/WebUntis/api/" + strings.TrimPrefix(path, "api/")
			}
			if strings.HasPrefix(path, "http") && !strings.HasPrefix(path, c.Profile.BaseURL()+"/") {
				return errors.New("only URLs of the configured WebUntis server are allowed")
			}
			q := url.Values{}
			for _, kv := range params {
				k, v, _ := strings.Cut(kv, "=")
				q.Add(k, v)
			}
			b, err := c.Get(cmd.Context(), path, q, 0)
			if err != nil {
				return err
			}
			return printRawJSON(a, b)
		},
	}
	get.Flags().StringArrayVarP(&params, "query", "q", nil, "query parameter key=value (repeatable)")
	cmd.AddCommand(get)
	cmd.AddCommand(&cobra.Command{
		Use:   "rpc METHOD [PARAMS-JSON]",
		Short: "Call a read-only JSON-RPC method (get*) of the public WebUntis API",
		Example: `  webuntis api rpc getSubjects
  webuntis api rpc getTimetable '{"options":{"element":{"id":8685,"type":5},"startDate":20260921,"endDate":20260925}}'`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !strings.HasPrefix(args[0], "get") {
				return errors.New("only read-only get* methods are allowed")
			}
			c, err := a.api()
			if err != nil {
				return err
			}
			var params any = map[string]any{}
			if len(args) == 2 {
				if err := json.Unmarshal([]byte(args[1]), &params); err != nil {
					return fmt.Errorf("params: %w", err)
				}
			}
			var out json.RawMessage
			if err := c.RPC(cmd.Context(), args[0], params, &out); err != nil {
				return err
			}
			return printRawJSON(a, out)
		},
	})
	return cmd
}

func printRawJSON(a *app, b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		_, err := os.Stdout.Write(b)
		return err
	}
	r := a.renderer()
	if a.format == render.YAML {
		return r.Data(v)
	}
	r.Format = render.JSON
	return r.Data(v)
}

func anyChanged(f *pflag.FlagSet, names ...string) bool {
	return slices.ContainsFunc(names, f.Changed)
}
