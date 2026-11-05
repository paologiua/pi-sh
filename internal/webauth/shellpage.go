package webauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// ShellPageError reports a Connect remote-shell page that did not carry the
// expected shell controller. Details describes the page's structure for -vvv
// diagnostics (see describe).
type ShellPageError struct {
	// Reauthentication is set when the page is Connect's "Reauthenticating
	// with Raspberry Pi ID" page: a single POST /auth/raspberry_pi form with
	// authenticity_token and origin (observed 2026-11-04 on a saved session
	// two days old). Reauthenticate submits that form.
	Reauthentication bool
	Details          []string
	form             *form
	at               *url.URL
}

func (e *ShellPageError) Error() string {
	if e.Reauthentication {
		return "Connect asked for a recent sign-in before opening the remote shell; sign in again"
	}
	return "Connect shell authorization or device identity was not confirmed; sign in again"
}

// Lines returns the -vvv description of the refused page.
func (e *ShellPageError) Lines() []string { return e.Details }

// ReauthPageError reports a page in the reauthentication chain that pi-sh
// does not recognise. Nothing was submitted to that page.
type ReauthPageError struct {
	Details []string
}

func (e *ReauthPageError) Error() string {
	return "renewing the Connect sign-in reached an unrecognized page; nothing was submitted to it; sign in again"
}

// Lines returns the -vvv description of the unrecognized page.
func (e *ReauthPageError) Lines() []string { return e.Details }

// secretName marks field names whose values are never described.
var secretName = regexp.MustCompile(`(?i)token|csrf|state|nonce|code|secret|session|key|pass|otp|sig|auth|cookie|hint|email|mail|account|user|login|name|uid|sub|phone|id$`)

const maxDescribed = 160

func shorten(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > maxDescribed {
		return string(r[:maxDescribed]) + "…"
	}
	return s
}

// describe summarizes a page's structure: path, title, headings and, per
// form, method, host/path, query keys, field names and types, short values
// of non-secret hidden fields, and the form text. It never includes cookies,
// query values or the values of token-like fields; the logger sanitizes
// every line again (emails, URLs, opaque tokens).
func describe(what string, p *page) []string {
	lines := []string{
		fmt.Sprintf("%s title %q", what, p.title),
	}
	for _, h := range p.headings {
		lines = append(lines, fmt.Sprintf("%s heading %q", what, h))
	}
	for i, f := range p.forms {
		place := f.action.Host + f.action.Path
		if q := f.action.Query(); len(q) > 0 {
			keys := make([]string, 0, len(q))
			for k := range q {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			place += " query_keys=" + strings.Join(keys, ",")
		}
		method := f.method
		if method == "" {
			method = "GET"
		}
		lines = append(lines, fmt.Sprintf("%s form %d: %s %s invalid=%t", what, i+1, method, place, f.invalid))
		names := make([]string, 0, len(f.fields))
		for name := range f.fields {
			names = append(names, name)
		}
		sort.Strings(names)
		var fields []string
		for _, name := range names {
			kind := f.fields[name]
			item := name + "(" + kind + ")"
			if v := f.values.Get(name); v != "" && (kind == "hidden" || kind == "submit") {
				if secretName.MatchString(name) || len(v) > 40 {
					item += fmt.Sprintf("<%d chars>", len(v))
				} else {
					item += fmt.Sprintf("%q", v)
				}
			}
			fields = append(fields, item)
		}
		lines = append(lines, fmt.Sprintf("%s form %d fields: %s", what, i+1, strings.Join(fields, " ")))
		if f.label != "" {
			lines = append(lines, fmt.Sprintf("%s form %d text %q", what, i+1, shorten(f.label)))
		}
	}
	return lines
}

// reauthForm returns the page's Connect reauthentication form, or nil. Only
// the observed shape is accepted: exactly one valid POST form to Connect's
// /auth/raspberry_pi carrying a CSRF token, whose named controls are hidden
// or submit and limited to authenticity_token and origin, with origin (when
// present) a path on Connect itself.
func (c *Client) reauthForm(p *page) *form {
	if origin(p.at) != c.root || len(p.forms) != 1 {
		return nil
	}
	f := &p.forms[0]
	if f.invalid || f.method != http.MethodPost || origin(f.action) != c.root || f.action.Path != "/auth/raspberry_pi" || f.action.RawQuery != "" || f.values.Get("authenticity_token") == "" {
		return nil
	}
	for name, kind := range f.fields {
		if (kind != "hidden" && kind != "submit") || (name != "authenticity_token" && name != "origin") {
			return nil
		}
	}
	if back := f.values.Get("origin"); back != "" {
		u, err := p.at.Parse(back)
		if err != nil || origin(u) != c.root {
			return nil
		}
	}
	return f
}

// describeShellPage builds the error for a refused shell page.
func (c *Client) describeShellPage(at *url.URL, b []byte, controllers int, matched bool) *ShellPageError {
	e := &ShellPageError{at: at}
	p, err := parse(at, b)
	if err != nil {
		e.Details = []string{fmt.Sprintf("shell page (%s) unparsed bytes=%d", at.Path, len(b))}
		return e
	}
	e.Details = append([]string{fmt.Sprintf("shell page (%s) shell_controllers=%d device_matched=%t forms=%d bytes=%d", at.Path, controllers, matched, len(p.forms), p.size)}, describe("shell page", p)...)
	if controllers == 0 {
		if f := c.reauthForm(p); f != nil {
			e.Reauthentication, e.form = true, f
		}
	}
	return e
}

// Reauthenticate submits Connect's own reauthentication form, as the
// browser's "Continue" does, and follows redirects only between Connect and
// Raspberry Pi ID. It returns Authenticated when the chain ends back on
// Connect, signed in; PasswordRequired when Raspberry Pi ID asks for the
// password (Password and Code then continue as in an ordinary login); or a
// *ReauthPageError describing any other page, to which nothing is submitted.
// It runs at most once per client.
func (c *Client) Reauthenticate(ctx context.Context, refused *ShellPageError) (Stage, error) {
	if c.stage != Authenticated || refused == nil || !refused.Reauthentication || refused.form == nil || c.returnTo != "" {
		return "", ErrFlow
	}
	c.returnTo = refused.at.Path
	if back := refused.form.values.Get("origin"); back != "" {
		if u, err := refused.at.Parse(back); err == nil {
			c.returnTo = u.Path
		}
	}
	c.stage, c.pending, c.passwordSent, c.codeSent = "", nil, false, false
	p, err := c.load(ctx, http.MethodPost, refused.form.action, clone(refused.form.values))
	if err != nil {
		return "", err
	}
	stage, err := c.classify(p)
	if err == nil && (stage == Authenticated || stage == PasswordRequired) {
		return stage, nil
	}
	if err != nil && !errors.Is(err, ErrFlow) {
		return "", err
	}
	c.pending, c.stage = nil, ""
	what := "id page"
	if origin(p.at) == c.root {
		what = "connect page"
	}
	return "", &ReauthPageError{Details: append([]string{fmt.Sprintf("%s (%s) reached while renewing the sign-in forms=%d bytes=%d", what, p.at.Path, len(p.forms), p.size)}, describe(what, p)...)}
}
