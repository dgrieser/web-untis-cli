package cli

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dgrieser/web-untis-cli/internal/render"
	"github.com/dgrieser/web-untis-cli/internal/views"
	"github.com/dgrieser/web-untis-cli/internal/webuntis"
)

func (a *app) addinsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "addins",
		Aliases: []string{"apps", "platform"},
		Short:   "Addins (platform applications) such as Klassengeld or custom links",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.api()
			if err != nil {
				return err
			}
			apps, err := c.PlatformApps(cmd.Context())
			if err != nil {
				return err
			}
			return a.emit(apps, func() string { return views.Addins(apps) }, nil)
		},
	}
	cmd.AddCommand(a.addinOpenCmd())
	kg := a.klassengeldCmd()
	kg.GroupID = ""
	cmd.AddCommand(kg)
	return cmd
}

func (a *app) addinOpenCmd() *cobra.Command {
	var download string
	var browser, text bool
	cmd := &cobra.Command{
		Use:   "open NAME",
		Short: "Show, download or open a custom link addin (e.g. \"Namen eingeben\")",
		Long: `Resolves the target of a link addin.

  --download FILE   save the target (e.g. a PDF) to FILE ("-" = auto name)
  --text            print the text of a PDF target (needs pdftotext)
  --browser         open the target in the default browser

SSO addins (Klassengeld, …) are opened in the browser via their WebUntis page.`,
		Example: `  webuntis addins open "Namen eingeben"
  webuntis addins open "Namen eingeben" --download -
  webuntis addins open "Namen eingeben" --text`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c, err := a.api()
			if err != nil {
				return err
			}
			app, err := c.PlatformApp(ctx, args[0])
			if err != nil {
				return err
			}
			target := app.LinkTarget()
			if app.Kind == "sso" {
				target = app.WebURL
			}
			if browser {
				return openBrowser(target)
			}
			if download == "" && !text {
				info := struct {
					*webuntis.PlatformApp
					Target string `json:"target" yaml:"target"`
				}{app, target}
				return a.emit(info, func() string {
					var d render.Doc
					d.H(2, "🧩 %s", render.Esc(app.Name))
					d.KV("Typ", app.Kind, "Ziel", target, "WebUntis", app.WebURL)
					if app.Kind == "link" {
						d.Pf("_Speichern: `webuntis addins open %q --download -` · Browser: `--browser`_", app.Name)
					}
					return d.String()
				}, nil)
			}
			if app.Kind == "sso" {
				return errors.New("download is only supported for link addins")
			}
			data, ct, err := c.Download(ctx, target, nil, false)
			if err != nil {
				return err
			}
			if text {
				if !strings.Contains(ct, "pdf") && !bytes.HasPrefix(data, []byte("%PDF")) {
					_, err := os.Stdout.Write(data)
					return err
				}
				pt, err := exec.LookPath("pdftotext")
				if err != nil {
					return errors.New("pdftotext not found (install poppler-utils) – use --download instead")
				}
				c := exec.CommandContext(ctx, pt, "-layout", "-", "-")
				c.Stdin = bytes.NewReader(data)
				c.Stdout = os.Stdout
				c.Stderr = os.Stderr
				return c.Run()
			}
			file := download
			if file == "-" {
				u, _ := url.Parse(target)
				file = safeFileName(path.Base(u.Path))
			}
			if err := os.WriteFile(file, data, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "saved %s (%s)\n", file, humanSize(len(data)))
			return nil
		},
	}
	cmd.Flags().StringVar(&download, "download", "", `save target to file ("-" = name from URL)`)
	cmd.Flags().BoolVar(&text, "text", false, "print text content (PDF via pdftotext)")
	cmd.Flags().BoolVar(&browser, "browser", false, "open in browser")
	return cmd
}

func openBrowser(u string) error {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", u)
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		c = exec.Command("xdg-open", u)
	}
	return c.Start()
}

func (a *app) klassengeldCmd() *cobra.Command {
	var tx bool
	cmd := &cobra.Command{
		Use:     "klassengeld",
		Aliases: []string{"kg", "classmoney"},
		Short:   "Klassengeld addin: balance, projects/payments and transactions",
		Long: `Reads klassengeld.app via WebUntis single sign-on (like clicking
"Applikation öffnen"). Shows account balance and payment status of all
projects per child; --transactions adds reserved amounts and bookings.`,
		Example: `  webuntis klassengeld
  webuntis klassengeld --transactions -o json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c, err := a.api()
			if err != nil {
				return err
			}
			app, err := c.PlatformApp(ctx, "Klassengeld")
			if err != nil {
				return err
			}
			kg, err := c.KlassengeldCached(ctx, app, tx)
			if err != nil {
				return err
			}
			return a.emit(kg, func() string { return views.Klassengeld(kg) }, nil)
		},
	}
	cmd.Flags().BoolVarP(&tx, "transactions", "T", false, "include reserved amounts and transactions")
	return cmd
}
