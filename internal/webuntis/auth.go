package webuntis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/dgrieser/web-untis-cli/internal/config"
)

// RPCError is a JSON-RPC error.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message) }

// Known JSON-RPC error codes.
const (
	rpcNotAuthenticated = -8520
	rpcBadCredentials   = -8504
)

// RPC calls a JSON-RPC method on /WebUntis/jsonrpc.do (session cookie based).
func (c *Client) RPC(ctx context.Context, method string, params any, out any) error {
	if !c.hasSessionCookie() && method != "authenticate" {
		if err := c.Login(ctx); err != nil {
			return err
		}
	}
	err := c.rpc(ctx, method, params, out)
	var re *RPCError
	if errors.As(err, &re) && re.Code == rpcNotAuthenticated && method != "authenticate" {
		if err := c.Login(ctx); err != nil {
			return err
		}
		return c.rpc(ctx, method, params, out)
	}
	return err
}

func (c *Client) rpc(ctx context.Context, method string, params any, out any) error {
	if params == nil {
		params = map[string]any{}
	}
	reqBody, err := json.Marshal(map[string]any{"id": fmt.Sprint(time.Now().UnixNano()), "method": method, "params": params, "jsonrpc": "2.0"})
	if err != nil {
		return err
	}
	u := c.base() + "/WebUntis/jsonrpc.do?school=" + url.QueryEscape(c.Profile.School)
	resp, b, err := c.raw(ctx, http.MethodPost, u, bytes.NewReader(reqBody), map[string]string{"Content-Type": "application/json"})
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return &APIError{Status: resp.StatusCode, URL: u, Body: string(b)}
	}
	var r struct {
		Result json.RawMessage `json:"result"`
		Error  *RPCError       `json:"error"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return fmt.Errorf("decode JSON-RPC %s: %w", method, err)
	}
	if r.Error != nil {
		return r.Error
	}
	if out != nil {
		return json.Unmarshal(r.Result, out)
	}
	return nil
}

// AuthResult is returned by the JSON-RPC authenticate method.
type AuthResult struct {
	SessionID  string `json:"sessionId"`
	PersonType int    `json:"personType"`
	PersonID   int    `json:"personId"`
	KlasseID   int    `json:"klasseId"`
}

// Login authenticates with the stored credentials.
func (c *Client) Login(ctx context.Context) error {
	p := c.Profile
	password := p.Password
	if password == "" {
		password = os.Getenv("WEBUNTIS_PASSWORD")
	}
	if p.Username == "" || password == "" {
		return fmt.Errorf("%w: session expired and no stored password (run `webuntis login`)", ErrAuth)
	}
	c.debugf("logging in as %s at %s/%s", p.Username, p.Server, p.School)
	c.session.Token = ""
	c.setSchoolCookies("")

	var res AuthResult
	err := c.rpc(ctx, "authenticate", map[string]any{"user": p.Username, "password": password, "client": "webuntis-cli"}, &res)
	if err == nil && res.SessionID != "" {
		c.setSchoolCookies(res.SessionID)
		if _, terr := c.fetchToken(ctx); terr == nil {
			c.session.LoginAt = time.Now()
			return c.SaveSession()
		}
		c.debugf("JSON-RPC session not accepted by REST API, trying form login")
	}
	var re *RPCError
	if errors.As(err, &re) && re.Code == rpcBadCredentials {
		return fmt.Errorf("login failed: bad credentials for %q at school %q", p.Username, p.School)
	}
	if err != nil {
		c.debugf("JSON-RPC authenticate failed (%v), trying form login", err)
	}
	if ferr := c.formLogin(ctx, password); ferr != nil {
		if err != nil {
			return fmt.Errorf("login failed: %v; form login: %w", err, ferr)
		}
		return fmt.Errorf("login failed: %w", ferr)
	}
	c.session.LoginAt = time.Now()
	return c.SaveSession()
}

// formLogin emulates the classic web login form.
func (c *Client) formLogin(ctx context.Context, password string) error {
	form := url.Values{
		"school":     {c.Profile.School},
		"j_username": {c.Profile.Username},
		"j_password": {password},
		"token":      {""},
	}
	resp, b, err := c.raw(ctx, http.MethodPost, c.base()+"/WebUntis/j_spring_security_check", strings.NewReader(form.Encode()),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Accept": "application/json"})
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return &APIError{Status: resp.StatusCode, URL: "j_spring_security_check", Body: string(b)}
	}
	var r struct {
		State      string `json:"state"`
		LoginError string `json:"loginError"`
	}
	if json.Unmarshal(b, &r) == nil && (r.LoginError != "" || strings.EqualFold(r.State, "FAILED")) {
		return fmt.Errorf("form login rejected: %s %s", r.State, r.LoginError)
	}
	if strings.Contains(resp.Request.URL.String(), "login_error") {
		return errors.New("form login rejected (bad credentials, 2FA or SSO-only account)")
	}
	_, err = c.fetchToken(ctx)
	if err != nil {
		return errors.New("form login did not yield a valid session (2FA or SSO-only accounts are not supported)")
	}
	return nil
}

// Logout ends the server session and clears the local one.
func (c *Client) Logout(ctx context.Context) error {
	if c.hasSessionCookie() {
		_ = c.rpc(ctx, "logout", map[string]any{}, nil)
	}
	c.session.Token = ""
	c.session.Cookies = map[string][]config.Cookie{}
	return c.Profile.ClearSession()
}

// School is a search result of the public school search.
type School struct {
	Server      string `json:"server" yaml:"server"`
	LoginName   string `json:"loginName" yaml:"loginName"`
	DisplayName string `json:"displayName" yaml:"displayName"`
	Address     string `json:"address" yaml:"address"`
	SchoolID    int    `json:"schoolId" yaml:"schoolId"`
	TenantID    string `json:"tenantId" yaml:"tenantId"`
	ServerURL   string `json:"serverUrl" yaml:"serverUrl"`
}

// SearchSchools queries the public WebUntis school directory (no login needed).
func SearchSchools(ctx context.Context, query string) ([]School, error) {
	body, _ := json.Marshal(map[string]any{"id": "1", "method": "searchSchool", "params": []any{map[string]string{"search": query}}, "jsonrpc": "2.0"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://mobile.webuntis.com/ms/schoolquery2", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var r struct {
		Result struct {
			Schools []School `json:"schools"`
		} `json:"result"`
		Error *RPCError `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	if r.Error != nil {
		if r.Error.Code == -6003 {
			return nil, fmt.Errorf("too many results for %q, please be more specific", query)
		}
		return nil, r.Error
	}
	return r.Result.Schools, nil
}
