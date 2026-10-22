package webauth

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func fixture(t *testing.T, mfa string) (*Client, *int) {
	t.Helper()
	passwords := new(int)
	var connect, identity *httptest.Server
	identity = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/sign-in":
			if _, e := r.Cookie("fresh_id"); e != nil {
				t.Error("redirect session cookie missing")
			}
			fmt.Fprint(w, `<form action="/session" method="post"><input type="hidden" name="authenticity_token" value="synthetic-id-csrf"><input type="email" name="email" required><input type="password" name="password" required><input type="submit" name="commit" value="Sign in"></form>`)
		case "/session":
			*passwords++
			if r.Method != "POST" || r.FormValue("authenticity_token") != "synthetic-id-csrf" || r.FormValue("email") != "fixture@example.invalid" || r.FormValue("password") != "synthetic-password" || r.Header.Get("Origin") != identity.URL {
				t.Error("password/CSRF/origin mismatch")
			}
			if mfa == "unknown" {
				fmt.Fprint(w, `<form method="post" action="/unexpected"><input required name="unrecognized-challenge"></form>`)
				return
			}
			if mfa == "rejected" {
				http.Redirect(w, r, "/sign-in", http.StatusSeeOther)
				return
			}
			if mfa == "code" || mfa == "unverified" {
				action := "/session/verify"
				if mfa == "unverified" {
					action = "/unverified"
				}
				// The live browser's POST /session returned 401 while rendering
				// the legitimate OTP challenge. Preserve that actual status here.
				w.WriteHeader(http.StatusUnauthorized)
				fmt.Fprintf(w, `<form method="post" action="%s"><input type="hidden" name="authenticity_token" value="synthetic-code-csrf"><input name="otp" autocomplete="one-time-code" required></form>`, action)
				return
			}
			http.Redirect(w, r, connect.URL+"/callback?code=synthetic-code", http.StatusSeeOther)
		case "/session/verify":
			if r.FormValue("otp") != "123456" || r.FormValue("authenticity_token") != "synthetic-code-csrf" || r.FormValue("password") != "" {
				t.Error("derived code form mismatch or password replay")
			}
			http.Redirect(w, r, connect.URL+"/callback?code=synthetic-code", http.StatusSeeOther)
		default:
			http.NotFound(w, r)
		}
	}))
	connect = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `<form action="/auth/raspberry_pi" method="post"><input type="hidden" name="authenticity_token" value="synthetic-connect-csrf"><button>Sign in with Raspberry&nbsp;Pi&nbsp;ID</button></form>`)
		case "/auth/raspberry_pi":
			if r.Method != "POST" || r.FormValue("authenticity_token") != "synthetic-connect-csrf" {
				t.Error("Connect CSRF mismatch")
			}
			http.SetCookie(w, &http.Cookie{Name: "fresh_id", Value: "synthetic-cookie", Path: "/", Secure: true, HttpOnly: true})
			http.Redirect(w, r, identity.URL+"/sign-in", http.StatusSeeOther)
		case "/callback":
			if r.FormValue("password") != "" {
				t.Error("password reached Connect callback")
			}
			http.Redirect(w, r, "/devices", http.StatusSeeOther)
		case "/devices":
			fmt.Fprint(w, `<meta name="csrf-token" content="synthetic-session-csrf"><a href="/devices/00000000-0000-0000-0000-000000000001">fixture</a>`)
		case "/devices/00000000-0000-0000-0000-000000000001/remote-shell-session":
			id := "00000000-0000-0000-0000-000000000001"
			if mfa == "wrong-device" {
				id = "00000000-0000-0000-0000-000000000002"
			}
			fmt.Fprintf(w, `<div data-controller="shell" data-shell-device-value='{"id":"%s","name":"fixture"}'></div>`, id)
		case "/devices/00000000-0000-0000-0000-000000000001/ice-configuration":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"iceServers":[]}`)
		case "/devices/00000000-0000-0000-0000-000000000001/connections":
			if r.Method != "POST" || r.Header.Get("X-CSRF-Token") != "synthetic-session-csrf" || r.Header.Get("Origin") != connect.URL || r.Header.Get("Content-Type") != "application/octet-stream" {
				t.Error("signaling request boundary mismatch")
			}
			location := "/devices/00000000-0000-0000-0000-000000000001/connections/fixture-poll"
			if mfa == "bad-location" {
				location = "https://untrusted.invalid/poll?secret=synthetic"
			}
			w.Header().Set("Location", location)
			w.WriteHeader(http.StatusCreated)
		case "/devices/00000000-0000-0000-0000-000000000001/connections/fixture-poll":
			w.Header().Set("Content-Type", "application/json")
			if mfa == "pending" {
				fmt.Fprint(w, `{}`)
			} else {
				fmt.Fprint(w, `{"answer":{"type":"answer","sdp":"synthetic-answer"}}`)
			}
		case "/devices/00000000-0000-0000-0000-000000000001.json":
			if mfa == "auth-revoked" {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{}`)
			} else {
				w.WriteHeader(http.StatusNoContent)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(identity.Close)
	t.Cleanup(connect.Close)
	pool := x509.NewCertPool()
	pool.AddCert(identity.Certificate())
	pool.AddCert(connect.Certificate())
	c := New()
	c.root = connect.URL
	c.identity = identity.URL
	c.allowed = map[string]bool{connect.URL: true, identity.URL: true}
	c.http.Transport = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}
	t.Cleanup(c.http.CloseIdleConnections)
	return c, passwords
}

func TestFreshWebSession(t *testing.T) {
	for _, mfa := range []string{"none", "code", "unknown", "rejected"} {
		t.Run(mfa, func(t *testing.T) {
			c, calls := fixture(t, mfa)
			ctx := context.Background()
			stage, err := c.Begin(ctx)
			if err != nil || stage != PasswordRequired || *calls != 0 {
				t.Fatalf("preflight %s: %v", stage, err)
			}
			stage, err = c.Password(ctx, "fixture@example.invalid", []byte("synthetic-password"))
			if mfa == "unknown" || mfa == "rejected" {
				if err == nil {
					t.Fatal("unknown or rejected challenge accepted")
				}
				if _, err = c.Password(ctx, "fixture@example.invalid", []byte("synthetic-password")); err == nil || *calls != 1 {
					t.Fatal("automatic/repeated credential retry")
				}
				return
			}
			if mfa == "code" {
				if err != nil || stage != CodeRequired {
					t.Fatalf("code stage %s: %v", stage, err)
				}
				stage, err = c.Code(ctx, []byte("123456"))
			}
			if err != nil || stage != Authenticated || *calls != 1 {
				t.Fatalf("login %s: %v", stage, err)
			}
		})
	}
}

func TestCredentialRedirectBoundary(t *testing.T) {
	for _, code := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			c, _ := fixture(t, "none")
			target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("credential body crossed origin") }))
			defer target.Close()
			source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL+"/sink", code) }))
			defer source.Close()
			c.allowed[source.URL] = true
			c.allowed[target.URL] = true
			u, _ := url.Parse(source.URL)
			_, err := c.load(context.Background(), "POST", u, url.Values{"password": {"synthetic-secret"}})
			if !errors.Is(err, ErrBoundary) {
				t.Fatalf("redirect accepted: %v", err)
			}
		})
	}
}

func TestUnexpectedOriginAndErrorRedaction(t *testing.T) {
	c := New()
	for _, s := range []string{"http://id.raspberrypi.com/session", "https://id.raspberrypi.com.evil.invalid/session", "https://id.raspberrypi.com:444/session", "https://user@id.raspberrypi.com/session"} {
		u, _ := url.Parse(s)
		if _, e := c.load(context.Background(), "POST", u, url.Values{"password": {"synthetic-secret"}}); !errors.Is(e, ErrBoundary) {
			t.Fatal("unexpected origin accepted")
		}
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://untrusted.invalid/callback?code=synthetic-secret", http.StatusFound)
	}))
	defer server.Close()
	c.allowed[server.URL] = true
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	c.http.Transport = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}
	defer c.http.CloseIdleConnections()
	u, _ := url.Parse(server.URL)
	_, err := c.load(context.Background(), "GET", u, nil)
	if !errors.Is(err, ErrBoundary) || strings.Contains(err.Error(), "synthetic-secret") || strings.Contains(err.Error(), "untrusted.invalid") {
		t.Fatalf("unsafe redirect diagnostic: %v", err)
	}
}

func TestLoginPageCannotMeanSuccess(t *testing.T) {
	c := New()
	u, _ := url.Parse(c.root + "/devices")
	p, _ := parse(u, []byte(`<form action="/sign-in"><input type="password" name="password"></form>`))
	if _, err := c.classify(p); err == nil {
		t.Fatal("HTTP200 login page reported authenticated")
	}
	jar := c.http.Jar
	u, _ = url.Parse("https://connect.raspberrypi.com")
	jar.SetCookies(u, []*http.Cookie{{Name: "bad", Value: "synthetic", Domain: "com"}})
	if len(jar.Cookies(u)) != 0 {
		t.Fatal("public-suffix cookie accepted")
	}
}

func TestUnverifiedMFAStopsBeforeCode(t *testing.T) {
	c, _ := fixture(t, "unverified")
	if _, err := c.Begin(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Password(context.Background(), "fixture@example.invalid", []byte("synthetic-password")); err == nil {
		t.Fatal("unverified MFA route accepted")
	}
}

func TestChangedCredentialFormsRejected(t *testing.T) {
	c := New()
	at, _ := url.Parse(c.identity + "/sign-in")
	base := `<input type="hidden" name="authenticity_token" value="synthetic"><input type="email" name="email" required><input type="password" name="password" required>`
	for name, html := range map[string]string{
		"duplicate": `<form action="/session" method="post">` + base + `<input type="hidden" name="authenticity_token" value="other"></form>`,
		"encoding":  `<form action="/session" method="post" enctype="multipart/form-data">` + base + `</form>`,
		"submitter": `<form action="/session" method="post">` + base + `<button formaction="/other">Sign in</button></form>`,
		"select":    `<form action="/session" method="post">` + base + `<select required name="challenge"><option>x</option></select></form>`,
		"ambiguous": `<form action="/session" method="post">` + base + `</form><form action="/session" method="post">` + base + `</form>`,
	} {
		t.Run(name, func(t *testing.T) {
			p, _ := parse(at, []byte(html))
			if _, err := c.classify(p); err == nil {
				t.Fatal("changed credential form accepted")
			}
		})
	}
	devices, _ := url.Parse(c.root + "/devices")
	p, _ := parse(devices, []byte(`<a href="/devices/">Sign in</a>`))
	if _, err := c.classify(p); err == nil {
		t.Fatal("empty device ID reported authenticated")
	}
}
