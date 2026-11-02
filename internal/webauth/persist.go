package webauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"
)

// ErrSignedOut means a restored session no longer reaches the signed-in
// Connect devices page: it expired or was signed out.
var ErrSignedOut = errors.New("saved Connect session is no longer signed in")

// savedFormat versions the exported session; Restore rejects other values.
const savedFormat = 1

// savedCookie is one cookie as the server set it, with the URL it came from,
// so that Restore applies the same domain and path rules again.
type savedCookie struct {
	URL      string        `json:"url"`
	Name     string        `json:"name"`
	Value    string        `json:"value"`
	Path     string        `json:"path,omitempty"`
	Domain   string        `json:"domain,omitempty"`
	Expires  time.Time     `json:"expires,omitzero"`
	Secure   bool          `json:"secure,omitempty"`
	HTTPOnly bool          `json:"http_only,omitempty"`
	SameSite http.SameSite `json:"same_site,omitempty"`
}

type savedSession struct {
	Format  int           `json:"format"`
	Account string        `json:"account"`
	Cookies []savedCookie `json:"cookies"`
}

// recordingJar is a cookiejar.Jar that also remembers the cookies it was
// given, because the standard jar cannot enumerate them for export.
type recordingJar struct {
	*cookiejar.Jar
	mu   sync.Mutex
	seen map[string]savedCookie
	now  func() time.Time
}

func newJar() *recordingJar {
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	return &recordingJar{Jar: jar, seen: map[string]savedCookie{}, now: time.Now}
}

func (j *recordingJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.Jar.SetCookies(u, cookies)
	j.mu.Lock()
	defer j.mu.Unlock()
	now := j.now()
	for _, c := range cookies {
		at := url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}
		key := strings.Join([]string{u.Host, strings.ToLower(c.Domain), c.Path, c.Name}, "\x00")
		expires := c.Expires
		if c.MaxAge > 0 {
			expires = now.Add(time.Duration(c.MaxAge) * time.Second)
		}
		if c.MaxAge < 0 || (!expires.IsZero() && !expires.After(now)) {
			delete(j.seen, key)
			continue
		}
		j.seen[key] = savedCookie{URL: at.String(), Name: c.Name, Value: c.Value, Path: c.Path, Domain: c.Domain,
			Expires: expires, Secure: c.Secure, HTTPOnly: c.HttpOnly, SameSite: c.SameSite}
	}
}

func (j *recordingJar) export() []savedCookie {
	j.mu.Lock()
	defer j.mu.Unlock()
	now := j.now()
	out := make([]savedCookie, 0, len(j.seen))
	for _, c := range j.seen {
		if c.Expires.IsZero() || c.Expires.After(now) {
			out = append(out, c)
		}
	}
	return out
}

// Account reports the Raspberry Pi ID email this session signed in with.
func (c *Client) Account() string { return c.account }

// Export serializes the signed-in session (its cookies and account) for the
// encrypted session store. The result is a bearer credential: callers must
// never log it or write it unencrypted.
func (c *Client) Export() ([]byte, error) {
	if c.stage != Authenticated || c.account == "" {
		return nil, errors.New("only a signed-in direct login session can be saved")
	}
	return json.Marshal(savedSession{Format: savedFormat, Account: c.account, Cookies: c.jar.export()})
}

// Restore loads Export output into a new client (from New). Cookies are
// accepted only for the reviewed HTTPS origins. The client is not signed in
// until Verify confirms that Connect still accepts the session.
func (c *Client) Restore(data []byte) error {
	if c.stage != "" || c.passwordSent || c.pending != nil || c.account != "" {
		return ErrFlow
	}
	var saved savedSession
	if err := json.Unmarshal(data, &saved); err != nil || saved.Format != savedFormat || saved.Account == "" {
		return errors.New("saved Connect session has an unsupported format")
	}
	for _, s := range saved.Cookies {
		u, err := url.Parse(s.URL)
		if err != nil || !c.permitted(u) || u.Scheme != "https" || u.RawQuery != "" {
			c.forget()
			return ErrBoundary
		}
		c.jar.SetCookies(u, []*http.Cookie{{Name: s.Name, Value: s.Value, Path: s.Path, Domain: s.Domain,
			Expires: s.Expires, Secure: s.Secure, HttpOnly: s.HTTPOnly, SameSite: s.SameSite}})
	}
	c.account = saved.Account
	return nil
}

// Verify reads the Connect devices page with the restored cookies, without
// following any redirect. Only a page carrying the signed-in sign-out form
// marks the client signed in; a redirect (to sign-in) means ErrSignedOut.
func (c *Client) Verify(ctx context.Context) error {
	if c.stage == Authenticated {
		return nil
	}
	if c.passwordSent || c.pending != nil {
		return ErrFlow
	}
	s := c.probe(ctx)
	defer s.Close()
	if _, err := s.devices(ctx); err != nil {
		if errors.Is(err, ErrBoundary) {
			return ErrSignedOut
		}
		return err
	}
	if s.signOut == "" {
		return ErrSignedOut
	}
	c.stage = Authenticated
	return nil
}

// Logout submits Connect's own sign-out form (DELETE /session through the
// Rails _method field, with that form's CSRF token) and forgets the local
// cookies. The response is not followed. It does not claim that Raspberry Pi
// ID or other copies of the cookies are invalidated by the service.
func (c *Client) Logout(ctx context.Context) error {
	if c.stage != Authenticated {
		return errors.New("only a signed-in session can be signed out")
	}
	s := c.probe(ctx)
	defer s.Close()
	if _, err := s.devices(ctx); err != nil {
		if errors.Is(err, ErrBoundary) {
			return ErrSignedOut
		}
		return err
	}
	if s.signOut == "" || len(s.signOut) > 4096 {
		return ErrSignedOut
	}
	body := url.Values{"_method": {"delete"}, "authenticity_token": {s.signOut}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.root+"/session", strings.NewReader(body))
	if err != nil {
		return ErrRequest
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", c.root)
	req.Header.Set("User-Agent", "pi-sh/experimental-direct-login")
	h := *c.http
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := h.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return requestFailure(err)
	}
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return fmt.Errorf("Connect sign-out was not accepted (HTTP %d)", resp.StatusCode)
	}
	c.forget()
	return nil
}

// forget drops every cookie and the signed-in state.
func (c *Client) forget() {
	c.jar = newJar()
	c.http.Jar = c.jar
	c.stage = ""
}

// probe is a request context for pages read outside device preparation.
func (c *Client) probe(ctx context.Context) *Connection {
	owned, cancel := context.WithCancel(ctx)
	return &Connection{client: c, ctx: owned, cancel: cancel}
}
