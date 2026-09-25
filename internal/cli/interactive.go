package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/dgrieser/web-untis-cli/internal/config"
	"github.com/dgrieser/web-untis-cli/internal/webuntis"
)

const newProfileChoice = "\x00new"

// interactive reports whether we can show interactive forms.
func interactive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stderr.Fd()))
}

func (a *app) setupCmd() *cobra.Command {
	var noStore bool
	cmd := &cobra.Command{
		Use:     "setup",
		Aliases: []string{"init"},
		Short:   "Interactive setup: profile, school search, login, default student",
		Long: `Interactive setup wizard:

  1. choose an existing profile or create a new one
  2. search the school (name, city, login name or URL) with live results
  3. enter username and password (optionally not stored)
  4. choose the default student (parent accounts with several children)

Same as running "webuntis login" without arguments on a terminal.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !interactive() {
				return errors.New("setup needs a terminal; use `webuntis login <school> --user … --password-stdin` in scripts")
			}
			return a.runSetup(cmd.Context(), noStore)
		},
	}
	cmd.Flags().BoolVar(&noStore, "no-store-password", false, "do not store the password on disk")
	return cmd
}

// schoolSearcher caches school directory lookups while the user types.
type schoolSearcher struct {
	mu    sync.Mutex
	cache map[string][]huh.Option[webuntis.School]
}

func schoolLabel(s webuntis.School) string {
	label := s.DisplayName
	if s.Address != "" {
		label += " · " + s.Address
	}
	return label + "  (" + s.LoginName + ")"
}

func (ss *schoolSearcher) options(ctx context.Context, query string, current webuntis.School) []huh.Option[webuntis.School] {
	q := strings.TrimSpace(query)
	hint := func(msg string) []huh.Option[webuntis.School] {
		opts := []huh.Option[webuntis.School]{}
		if current.LoginName != "" {
			opts = append(opts, huh.NewOption(schoolLabel(current), current))
		}
		return append(opts, huh.NewOption(msg, webuntis.School{}))
	}
	if len([]rune(q)) < 3 {
		return hint("… mindestens 3 Zeichen eingeben")
	}
	ss.mu.Lock()
	if o, ok := ss.cache[q]; ok {
		ss.mu.Unlock()
		return o
	}
	ss.mu.Unlock()

	var opts []huh.Option[webuntis.School]
	if strings.Contains(q, "://") || strings.Contains(q, ".") {
		server, login, tenant, display, err := resolveSchool(ctx, q, "")
		if err != nil {
			return hint("⚠ " + err.Error())
		}
		s := webuntis.School{Server: server, LoginName: login, TenantID: tenant, DisplayName: firstNonEmpty(display, login)}
		opts = append(opts, huh.NewOption(schoolLabel(s), s))
	} else {
		res, err := webuntis.SearchSchools(ctx, q)
		if err != nil {
			return hint("⚠ " + err.Error())
		}
		if len(res) == 0 {
			return hint("… keine Schule gefunden")
		}
		for _, s := range res {
			opts = append(opts, huh.NewOption(schoolLabel(s), s))
		}
	}
	ss.mu.Lock()
	ss.cache[q] = opts
	ss.mu.Unlock()
	return opts
}

func notEmpty(what string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("%s fehlt", what)
		}
		return nil
	}
}

func (a *app) runSetup(ctx context.Context, noStore bool) error {
	theme := huh.ThemeCharm()
	profiles, _ := config.ListProfiles()

	// ---- 1. profile
	choice := a.profile
	newName := ""
	if choice == "" {
		choice = config.CurrentProfileName()
		if !slices.Contains(profiles, choice) {
			choice = newProfileChoice
		}
		if len(profiles) == 0 {
			newName = config.DefaultProfile
		}
	}
	if a.profile == "" && len(profiles) > 0 {
		var opts []huh.Option[string]
		for _, n := range profiles {
			label := n
			if p, err := config.Load(n); err == nil {
				label = fmt.Sprintf("%s — %s, %s", n, firstNonEmpty(p.SchoolDisplayName, p.School), p.Username)
			}
			opts = append(opts, huh.NewOption(label, n))
		}
		opts = append(opts, huh.NewOption("➕ Neues Profil anlegen", newProfileChoice))
		err := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().Title("Profil").
					Description("Ein Profil = eine Schule + ein Benutzerkonto").
					Options(opts...).Value(&choice),
			),
			huh.NewGroup(
				huh.NewInput().Title("Name des neuen Profils").Placeholder("z.B. liana, schule2").Value(&newName).
					Validate(func(s string) error {
						if err := config.ValidateProfileName(s); err != nil {
							return err
						}
						if slices.Contains(profiles, s) {
							return fmt.Errorf("Profil %q existiert bereits", s)
						}
						return nil
					}),
			).WithHideFunc(func() bool { return choice != newProfileChoice }),
		).WithTheme(theme).RunWithContext(ctx)
		if err != nil {
			return abortErr(err)
		}
	}
	name := choice
	if choice == newProfileChoice {
		name = newName
	}
	existing, _ := config.Load(name)

	// ---- 2. school + credentials
	var school webuntis.School
	query, user := "", ""
	if existing != nil {
		school = webuntis.School{Server: existing.Server, LoginName: existing.School, TenantID: existing.TenantID,
			DisplayName: firstNonEmpty(existing.SchoolDisplayName, existing.School)}
		query, user = existing.School, existing.Username
	}
	storePw := !noStore
	searcher := &schoolSearcher{cache: map[string][]huh.Option[webuntis.School]{}}
	password := ""

	for attempt := 1; ; attempt++ {
		groups := []*huh.Group{
			huh.NewGroup(
				huh.NewInput().Title("Schule suchen").
					Description("Name, Ort, Login-Name oder URL (z.B. https://ge-huellhorst.webuntis.com)").
					Placeholder("hüllhorst").Value(&query).Validate(notEmpty("Suchbegriff")),
				huh.NewSelect[webuntis.School]().Title("Schule").
					OptionsFunc(func() []huh.Option[webuntis.School] { return searcher.options(ctx, query, school) }, &query).
					Value(&school).Height(8).
					Validate(func(s webuntis.School) error {
						if s.LoginName == "" {
							return errors.New("bitte eine Schule auswählen")
						}
						return nil
					}),
			),
			huh.NewGroup(
				huh.NewInput().TitleFunc(func() string { return "Benutzername bei " + firstNonEmpty(school.DisplayName, school.LoginName) }, &school).
					Value(&user).Validate(notEmpty("Benutzername")),
				huh.NewInput().Title("Passwort").EchoMode(huh.EchoModePassword).Value(&password).Validate(notEmpty("Passwort")),
				huh.NewConfirm().Title("Passwort speichern?").
					Description("Erlaubt automatische Neuanmeldung, gespeichert mit Dateimodus 0600.").
					Affirmative("Ja").Negative("Nein").Value(&storePw),
			),
		}
		if attempt > 1 {
			groups = groups[1:] // school already chosen, only retry credentials
		}
		if err := huh.NewForm(groups...).WithTheme(theme).RunWithContext(ctx); err != nil {
			return abortErr(err)
		}
		p := &config.Profile{Name: name, Server: school.Server, School: school.LoginName, TenantID: school.TenantID,
			SchoolDisplayName: school.DisplayName, Username: strings.TrimSpace(user), Password: password}
		fmt.Fprintf(os.Stderr, "Anmelden bei %s …\n", firstNonEmpty(school.DisplayName, school.LoginName))
		ad, err := a.performLogin(ctx, p, existing, !storePw)
		if err != nil {
			fmt.Fprintln(os.Stderr, "✗", err)
			retry := true
			if ferr := huh.NewForm(huh.NewGroup(huh.NewConfirm().Title("Anmeldung fehlgeschlagen – nochmal versuchen?").
				Affirmative("Ja").Negative("Abbrechen").Value(&retry))).WithTheme(theme).RunWithContext(ctx); ferr != nil || !retry {
				return err
			}
			password = ""
			continue
		}

		// ---- 3. default student
		if len(ad.User.Students) > 1 {
			sel := p.Student
			var opts []huh.Option[string]
			for _, s := range ad.User.Students {
				opts = append(opts, huh.NewOption(s.DisplayName, fmt.Sprint(s.ID)))
			}
			if err := huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Title("Standard-Schüler").
				Description("Andere jederzeit mit --student <name>").Options(opts...).Value(&sel))).
				WithTheme(theme).RunWithContext(ctx); err == nil {
				for _, s := range ad.User.Students {
					if fmt.Sprint(s.ID) == sel {
						p.Student = s.DisplayName
					}
				}
				if err := p.Save(); err != nil {
					return err
				}
			}
		}
		return a.printLoginSummary(p, ad)
	}
}

func abortErr(err error) error {
	if errors.Is(err, huh.ErrUserAborted) {
		return errors.New("setup aborted")
	}
	return err
}
