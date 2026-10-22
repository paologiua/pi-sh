// Package webauth investigates the normal Connect web login as an HTTP user
// agent. It neither imports browser credentials nor registers an OAuth client.
package webauth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/publicsuffix"
)

type Stage string

const (
	PasswordRequired Stage = "password_required"
	CodeRequired     Stage = "one_time_code_required"
	Authenticated    Stage = "connect_devices_page_observed"
)

var (
	ErrFlow     = errors.New("unrecognized web login step; no credentials submitted to that step")
	ErrRequest  = errors.New("web login HTTPS request failed")
	ErrRejected = errors.New("login was not accepted; no automatic retry")
	ErrBoundary = errors.New("web login origin or redirect boundary rejected")
)

type form struct {
	action       *url.URL
	method       string
	values       url.Values
	fields       map[string]string // type by name, never logged
	autocomplete map[string]string
	label        string
	invalid      bool
}

type page struct {
	at          *url.URL
	forms       []form
	deviceLinks bool
	loginForm   bool
}

// Client owns only an in-memory jar. No session serialization is exposed.
type Client struct {
	http                   *http.Client
	root                   string
	identity               string
	allowed                map[string]bool
	pending                *form
	stage                  Stage
	passwordSent, codeSent bool
}

func New() *Client {
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	c := &Client{root: "https://connect.raspberrypi.com", identity: "https://id.raspberrypi.com", allowed: map[string]bool{"https://connect.raspberrypi.com": true, "https://id.raspberrypi.com": true}}
	c.http = &http.Client{Jar: jar, Timeout: 30 * time.Second}
	c.http.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 16 || !c.permitted(req.URL) {
			return ErrBoundary
		}
		if len(via) > 0 && req.Method != http.MethodGet && req.Method != http.MethodHead {
			return ErrBoundary
		}
		return nil
	}
	return c
}

func origin(u *url.URL) string { return u.Scheme + "://" + u.Host }
func (c *Client) permitted(u *url.URL) bool {
	return u != nil && u.User == nil && u.Fragment == "" && c.allowed[origin(u)]
}

func (c *Client) load(ctx context.Context, method string, target *url.URL, values url.Values) (*page, error) {
	if !c.permitted(target) {
		return nil, ErrBoundary
	}
	var body io.Reader
	if values != nil {
		body = strings.NewReader(values.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, ErrRequest
	}
	req.Header.Set("User-Agent", "pi-sh/experimental-login-probe")
	req.Header.Set("Accept", "text/html")
	if values != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", origin(target))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, ErrBoundary) {
			return nil, ErrBoundary
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, requestFailure(err)
	}
	defer resp.Body.Close()
	challengeStatus := resp.StatusCode == http.StatusUnauthorized && method == http.MethodPost && origin(target) == c.identity && target.Path == "/session" && c.passwordSent
	if resp.StatusCode != 200 && resp.StatusCode != 422 && !challengeStatus {
		return nil, fmt.Errorf("%w (HTTP %d)", ErrRequest, resp.StatusCode)
	}
	if !strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
		return nil, ErrFlow
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w (response read)", ErrRequest)
	}
	if len(b) > 2*1024*1024 {
		return nil, fmt.Errorf("%w (response too large)", ErrRequest)
	}
	p, err := parse(resp.Request.URL, b)
	if err != nil {
		return nil, ErrFlow
	}
	if challengeStatus {
		matches := 0
		for i := range p.forms {
			if c.codeForm(p, &p.forms[i]) {
				matches++
			}
		}
		if matches != 1 {
			return nil, ErrRejected
		}
	}
	return p, nil
}

// Begin performs the observed ordinary Connect sign-in navigation. CSRF and
// OAuth parameters are obtained from server responses, never fabricated.
func (c *Client) Begin(ctx context.Context) (Stage, error) {
	if c.pending != nil || c.passwordSent {
		return "", ErrFlow
	}
	u, _ := url.Parse(c.root)
	p, err := c.load(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	var selected *form
	for i := range p.forms {
		f := &p.forms[i]
		if !f.invalid && origin(f.action) == c.root && f.action.Path == "/auth/raspberry_pi" && f.method == http.MethodPost && f.values.Get("prompt") == "" && f.values.Get("authenticity_token") != "" && strings.Contains(f.label, "Sign in with Raspberry Pi ID") {
			if selected != nil {
				return "", ErrFlow
			}
			selected = f
		}
	}
	if selected == nil {
		return "", ErrFlow
	}
	p, err = c.load(ctx, http.MethodPost, selected.action, selected.values)
	if err != nil {
		return "", err
	}
	return c.classify(p)
}

func (c *Client) classify(p *page) (Stage, error) {
	if origin(p.at) == c.root && p.at.Path == "/devices" && p.deviceLinks && !p.loginForm {
		c.pending = nil
		c.stage = Authenticated
		return c.stage, nil
	}
	if origin(p.at) != c.identity {
		return "", ErrFlow
	}
	var selected *form
	var next Stage
	for i := range p.forms {
		f := &p.forms[i]
		if f.invalid || !c.permitted(f.action) || origin(f.action) != origin(p.at) || f.method != http.MethodPost || f.values.Get("authenticity_token") == "" {
			continue
		}
		if f.action.Path == "/session" && f.fields["email"] == "email" && f.fields["password"] == "password" && knownControls(f, "email", "password") {
			if c.passwordSent {
				return "", ErrRejected
			}
			if selected != nil {
				return "", ErrFlow
			}
			selected = f
			next = PasswordRequired
		}
		// This exact route and field were observed on the ordinary ID challenge
		// after the operator entered credentials in an isolated browser.
		if c.passwordSent && c.codeForm(p, f) {
			if c.codeSent {
				return "", ErrRejected
			}
			if selected != nil {
				return "", ErrFlow
			}
			selected = f
			next = CodeRequired
		}
	}
	if selected == nil {
		return "", ErrFlow
	}
	c.pending = selected
	c.stage = next
	return c.stage, nil
}

func (c *Client) codeForm(p *page, f *form) bool {
	return !f.invalid && origin(p.at) == c.identity && p.at.Path == "/session" && c.permitted(f.action) && origin(f.action) == c.identity && f.action.Path == "/session/verify" && f.method == http.MethodPost && f.values.Get("authenticity_token") != "" && f.fields["otp"] == "text" && f.autocomplete["otp"] == "one-time-code" && knownControls(f, "otp")
}

func knownControls(f *form, names ...string) bool {
	for name, kind := range f.fields {
		if kind == "hidden" || kind == "submit" {
			continue
		}
		found := false
		for _, n := range names {
			if name == n {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func clone(v url.Values) url.Values {
	out := make(url.Values)
	for k, x := range v {
		out[k] = append([]string(nil), x...)
	}
	return out
}

func (c *Client) Password(ctx context.Context, email string, password []byte) (Stage, error) {
	if c.stage != PasswordRequired || c.pending == nil || c.passwordSent || email == "" || len(password) == 0 {
		return "", ErrFlow
	}
	f := c.pending
	v := clone(f.values)
	v.Set("email", email)
	v.Set("password", string(password))
	c.passwordSent = true
	c.pending = nil
	p, err := c.load(ctx, http.MethodPost, f.action, v)
	if err != nil {
		return "", err
	}
	return c.classify(p)
}

func (c *Client) Code(ctx context.Context, code []byte) (Stage, error) {
	if c.stage != CodeRequired || c.pending == nil || c.codeSent || len(code) == 0 {
		return "", ErrFlow
	}
	f := c.pending
	v := clone(f.values)
	v.Set("otp", string(code))
	c.codeSent = true
	c.pending = nil
	p, err := c.load(ctx, http.MethodPost, f.action, v)
	if err != nil {
		return "", err
	}
	return c.classify(p)
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
func has(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}
func parse(at *url.URL, b []byte) (*page, error) {
	n, err := html.Parse(strings.NewReader(string(b)))
	if err != nil {
		return nil, err
	}
	p := &page{at: at}
	unsafeBase := false
	var visit func(*html.Node, *form)
	visit = func(n *html.Node, f *form) {
		if n.Type == html.ElementNode {
			if n.Data == "base" {
				unsafeBase = true
				return
			}
			if n.Data == "a" {
				if u, e := at.Parse(attr(n, "href")); e == nil && origin(u) == origin(at) && devicePath.MatchString(u.Path) {
					p.deviceLinks = true
				}
			}
			if n.Data == "form" {
				u, e := at.Parse(attr(n, "action"))
				if e != nil {
					return
				}
				encoding := strings.ToLower(attr(n, "enctype"))
				p.forms = append(p.forms, form{action: u, method: strings.ToUpper(attr(n, "method")), values: make(url.Values), fields: make(map[string]string), autocomplete: make(map[string]string), invalid: encoding != "" && encoding != "application/x-www-form-urlencoded"})
				// Recurse separately: append can move the slice backing array.
				index := len(p.forms) - 1
				for child := n.FirstChild; child != nil; child = child.NextSibling {
					visit(child, &p.forms[index])
				}
				return
			}
			if f != nil && (n.Data == "select" || n.Data == "textarea" || n.Data == "fieldset" || has(n, "formaction") || has(n, "formmethod") || has(n, "formenctype") || has(n, "form")) {
				f.invalid = true
			}
			if f != nil && (n.Data == "input" || n.Data == "button") && !has(n, "disabled") {
				name := attr(n, "name")
				kind := attr(n, "type")
				if kind == "" {
					if n.Data == "button" {
						kind = "submit"
					} else {
						kind = "text"
					}
				}
				if name != "" {
					if _, exists := f.fields[name]; exists {
						f.invalid = true
					}
					f.fields[name] = kind
					f.autocomplete[name] = attr(n, "autocomplete")
					if kind == "hidden" || kind == "submit" {
						f.values.Add(name, attr(n, "value"))
					}
				}
				if kind == "password" || kind == "email" || attr(n, "autocomplete") == "one-time-code" {
					p.loginForm = true
				}
			}
		}
		if f != nil && n.Type == html.TextNode {
			f.label += " " + n.Data
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			visit(child, f)
		}
	}
	visit(n, nil)
	for i := range p.forms {
		if unsafeBase {
			p.forms[i].invalid = true
		}
		p.forms[i].label = strings.Join(strings.Fields(p.forms[i].label), " ")
	}
	return p, nil
}

var devicePath = regexp.MustCompile(`^/devices/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
