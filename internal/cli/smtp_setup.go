package cli

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/dgrieser/web-untis-cli/internal/config"
	"github.com/dgrieser/web-untis-cli/internal/mailer"
	"github.com/dgrieser/web-untis-cli/internal/secrets"
)

type smtpPreset struct {
	Name, Host string
	Port       int
	Security   string
	Hint       string
}

var smtpPresets = []smtpPreset{
	{"Gmail", "smtp.gmail.com", 587, "starttls", "Benötigt ein App-Passwort (Google-Konto → Sicherheit → App-Passwörter)."},
	{"Outlook.com / Hotmail", "smtp-mail.outlook.com", 587, "starttls", ""},
	{"Microsoft 365", "smtp.office365.com", 587, "starttls", "SMTP AUTH muss für das Postfach aktiviert sein."},
	{"iCloud", "smtp.mail.me.com", 587, "starttls", "Benötigt ein app-spezifisches Passwort."},
	{"GMX", "mail.gmx.net", 587, "starttls", "POP3/IMAP/SMTP-Zugriff in den GMX-Einstellungen aktivieren."},
	{"WEB.DE", "smtp.web.de", 587, "starttls", "POP3/IMAP/SMTP-Zugriff in den WEB.DE-Einstellungen aktivieren."},
	{"T-Online", "securesmtp.t-online.de", 465, "tls", "Nutzt das separate E-Mail-Passwort."},
	{"mailbox.org", "smtp.mailbox.org", 465, "tls", ""},
	{"Posteo", "posteo.de", 587, "starttls", ""},
}

const customPreset = "\x00custom"

func validAddress(s string) error {
	if _, err := mail.ParseAddress(strings.TrimSpace(s)); err != nil {
		return fmt.Errorf("ungültige E-Mail-Adresse: %q", s)
	}
	return nil
}

func splitAddresses(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == ' ' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// runSMTPSetup asks for SMTP settings interactively and saves them.
func (a *app) runSMTPSetup(ctx context.Context, p *config.Profile) error {
	theme := formTheme()
	cur := p.SMTP

	// ---- 1. provider preset
	preset := customPreset
	for _, pr := range smtpPresets {
		if cur.Host != "" && strings.EqualFold(pr.Host, cur.Host) {
			preset = pr.Host
		}
	}
	if cur.Host == "" {
		preset = smtpPresets[0].Host
	}
	opts := []huh.Option[string]{}
	for _, pr := range smtpPresets {
		opts = append(opts, huh.NewOption(fmt.Sprintf("%s (%s)", pr.Name, pr.Host), pr.Host))
	}
	opts = append(opts, huh.NewOption("Anderer Server …", customPreset))
	if err := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title("E-Mail-Anbieter").
			Description("Für die Weiterleitung von Mitteilungen und Nachrichten per E-Mail").
			Options(opts...).Value(&preset),
	)).WithTheme(theme).RunWithContext(ctx); err != nil {
		return abortErr(err)
	}
	host, port, security, hint := cur.Host, cur.Port, cur.Security, ""
	for _, pr := range smtpPresets {
		if pr.Host == preset {
			host, port, security, hint = pr.Host, pr.Port, pr.Security, pr.Hint
		}
	}
	if security == "" {
		security = "starttls"
	}
	// Only prefill a port that differs from the default of its security
	// mode; otherwise leave it empty so the default follows the selection.
	portStr := ""
	if port != 0 && port != defaultPort(security) {
		portStr = strconv.Itoa(port)
	}

	// ---- 2. server, account, addresses
	user, from := cur.Username, cur.From
	to := strings.Join(cur.To, ", ")
	prefix := cur.SubjectPrefix
	if prefix == "" {
		prefix = "[WebUntis]"
	}
	password := ""
	hasPassword := secrets.Where(p, secrets.SMTP, a.noKeyring) != secrets.SourceNone
	pwDesc := "Leer lassen, um das gespeicherte Passwort zu behalten."
	if !hasPassword {
		pwDesc = "Wird im Schlüsselbund des Systems gespeichert; alternativ $WEBUNTIS_SMTP_PASSWORD."
		if a.noKeyring {
			pwDesc = "Wird in config.json gespeichert (Dateimodus 0600); alternativ $WEBUNTIS_SMTP_PASSWORD."
		}
	}
	if hint != "" {
		pwDesc = hint + " " + pwDesc
	}
	var groups []*huh.Group
	if preset == customPreset {
		groups = append(groups, huh.NewGroup(
			huh.NewInput().Title("SMTP-Server").Placeholder("smtp.example.com").Value(&host).Validate(notEmpty("Server")),
			huh.NewSelect[string]().Title("Verschlüsselung").Options(
				huh.NewOption("STARTTLS (Port 587)", "starttls"),
				huh.NewOption("TLS/SSL (Port 465)", "tls"),
				huh.NewOption("STARTTLS wenn verfügbar", "opportunistic"),
				huh.NewOption("keine (Port 25, nur lokal!)", "none"),
			).Value(&security),
			huh.NewInput().Title("Port").
				DescriptionFunc(func() string {
					return fmt.Sprintf("Leer lassen für den Standard (%d)", defaultPort(security))
				}, &security).
				PlaceholderFunc(func() string { return strconv.Itoa(defaultPort(security)) }, &security).
				Value(&portStr).Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return nil
				}
				if n, err := strconv.Atoi(strings.TrimSpace(s)); err != nil || n < 1 || n > 65535 {
					return errors.New("ungültiger Port")
				}
				return nil
			}),
		))
	}
	groups = append(groups,
		huh.NewGroup(
			huh.NewInput().Title("Benutzername").Description("Meist die E-Mail-Adresse; leer = ohne Anmeldung").Value(&user),
			huh.NewInput().Title("Passwort").Description(pwDesc).EchoMode(huh.EchoModePassword).Value(&password),
		),
		huh.NewGroup(
			huh.NewInput().Title("Absender").Placeholder("ich@example.com").
				Description("Muss meist zum Konto passen").Value(&from).Validate(validAddress),
			huh.NewInput().Title("Empfänger").Description("Mehrere mit Komma trennen").Value(&to).
				Validate(func(s string) error {
					list := splitAddresses(s)
					if len(list) == 0 {
						return errors.New("mindestens ein Empfänger")
					}
					for _, a := range list {
						if err := validAddress(a); err != nil {
							return err
						}
					}
					return nil
				}),
			huh.NewInput().Title("Betreff-Präfix").Value(&prefix),
		),
	)
	if from == "" && strings.Contains(user, "@") {
		from = user
	}
	if err := huh.NewForm(groups...).WithTheme(theme).RunWithContext(ctx); err != nil {
		return abortErr(err)
	}
	if to == "" {
		to = from
	}
	if n, err := strconv.Atoi(strings.TrimSpace(portStr)); err == nil {
		port = n
	} else if preset == customPreset || port == 0 {
		port = defaultPort(security)
	}
	p.SMTP = config.SMTPConfig{
		Host: strings.TrimSpace(host), Port: port, Security: security,
		Username: strings.TrimSpace(user), Password: cur.Password, // file-stored only
		From: strings.TrimSpace(from), To: splitAddresses(to), SubjectPrefix: strings.TrimSpace(prefix),
	}
	switch {
	case p.SMTP.Username == "":
		if err := secrets.Delete(p, secrets.SMTP, a.noKeyring); err != nil {
			return err
		}
	case password != "":
		if err := secrets.Set(p, secrets.SMTP, password, a.noKeyring); err != nil {
			return err
		}
	}
	if err := mailer.Validate(p.SMTP); err != nil {
		return err
	}
	if err := p.Save(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "✓ SMTP gespeichert: %s → %s via %s:%d\n", p.SMTP.From, strings.Join(p.SMTP.To, ", "), p.SMTP.Host, p.SMTP.Port)

	// ---- 3. test mail
	test := true
	if err := huh.NewForm(huh.NewGroup(huh.NewConfirm().Title("Testmail senden?").
		Affirmative("Ja").Negative("Nein").Value(&test))).WithTheme(theme).RunWithContext(ctx); err != nil || !test {
		return nil
	}
	err := a.sendTestMail(ctx, p)
	if err == nil {
		fmt.Fprintln(os.Stderr, "Weiterleiten: webuntis messages forward --mark-only && webuntis messages forward")
		return nil
	}
	fmt.Fprintln(os.Stderr, "✗", err)
	retry := false
	if ferr := huh.NewForm(huh.NewGroup(huh.NewConfirm().Title("Einstellungen korrigieren?").
		Affirmative("Ja").Negative("Nein").Value(&retry))).WithTheme(theme).RunWithContext(ctx); ferr != nil || !retry {
		return err
	}
	return a.runSMTPSetup(ctx, p) // prefilled with the values just entered
}

func (a *app) sendTestMail(ctx context.Context, p *config.Profile) error {
	if err := mailer.Validate(p.SMTP); err != nil {
		return err
	}
	cfg, err := a.smtpConfig(p)
	if err != nil {
		return err
	}
	msg, err := mailer.BuildTest(cfg, firstNonEmpty(p.SchoolDisplayName, p.School))
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Sende Testmail an %s …\n", strings.Join(cfg.To, ", "))
	if err := mailer.Send(ctx, cfg, msg); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "✓ Testmail gesendet")
	return nil
}

// defaultPort returns the usual SMTP port for a security mode.
func defaultPort(security string) int {
	switch security {
	case "tls", "ssl":
		return 465
	case "none":
		return 25
	}
	return 587
}
