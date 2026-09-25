package webuntis

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// PlatformApp is an addin ("platform application") shown in the navigation.
type PlatformApp struct {
	ID             int    `json:"id" yaml:"id"`
	Name           string `json:"name" yaml:"name"`
	Icon           string `json:"icon" yaml:"icon"`
	RedirectURL    string `json:"redirectUrl" yaml:"redirectUrl"`
	LogoutURL      string `json:"logoutUrl,omitempty" yaml:"logoutUrl,omitempty"`
	OpenInNewTab   bool   `json:"openInNewTab" yaml:"openInNewTab"`
	ReloadOnResume bool   `json:"reloadOnResume" yaml:"reloadOnResume"`
	// Kind is derived: "sso" for apps that authenticate via WebUntis OAuth,
	// "link" for plain custom links.
	Kind string `json:"kind" yaml:"kind"`
	// WebURL is the WebUntis page of the app.
	WebURL string `json:"webUrl" yaml:"webUrl"`
}

// PlatformApps lists the addins available to the user.
func (c *Client) PlatformApps(ctx context.Context) ([]PlatformApp, error) {
	var apps []PlatformApp
	if err := c.GetJSON(ctx, "/WebUntis/api/rest/view/v1/app/platform-application/menus", nil, time.Hour, &apps); err != nil {
		return nil, err
	}
	for i := range apps {
		a := &apps[i]
		// The web UI applies decodeURIComponent to the redirect URL.
		if dec, err := url.PathUnescape(a.RedirectURL); err == nil {
			a.RedirectURL = dec
		}
		a.Kind = "sso"
		if strings.Contains(a.Icon, "eigenerlink") || a.LogoutURL == "" {
			a.Kind = "link"
		}
		a.WebURL = c.base() + "/platform-application/" + url.PathEscape(a.Name)
	}
	return apps, nil
}

// PlatformApp finds an addin by (case-insensitive, partial) name or id.
func (c *Client) PlatformApp(ctx context.Context, selector string) (*PlatformApp, error) {
	apps, err := c.PlatformApps(ctx)
	if err != nil {
		return nil, err
	}
	sel := strings.ToLower(strings.TrimSpace(selector))
	var partial []PlatformApp
	var names []string
	for _, a := range apps {
		names = append(names, a.Name)
		if strings.ToLower(a.Name) == sel || fmt.Sprint(a.ID) == sel {
			a := a
			return &a, nil
		}
		if strings.Contains(strings.ToLower(a.Name), sel) {
			partial = append(partial, a)
		}
	}
	if len(partial) == 1 {
		return &partial[0], nil
	}
	return nil, fmt.Errorf("no addin matches %q (available: %s)", selector, strings.Join(names, ", "))
}

// LinkTarget returns the actual target of a custom link addin. Custom links
// get WebUntis' tenant_id/school parameters appended; they are stripped
// here to get a clean URL.
func (a PlatformApp) LinkTarget() string {
	u, err := url.Parse(a.RedirectURL)
	if err != nil {
		return a.RedirectURL
	}
	q := u.Query()
	q.Del("tenant_id")
	q.Del("school")
	u.RawQuery = q.Encode()
	return u.String()
}
