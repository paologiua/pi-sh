package webauth

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

func signedIn(t *testing.T, mode string) *Client {
	t.Helper()
	c, _ := fixture(t, mode)
	if _, err := c.Begin(context.Background()); err != nil {
		t.Fatal(err)
	}
	if stage, err := c.Password(context.Background(), "fixture@example.invalid", []byte("synthetic-password")); err != nil || stage != Authenticated {
		t.Fatalf("fixture login %s: %v", stage, err)
	}
	return c
}

func TestDirectSignalingOwnWebSession(t *testing.T) {
	c := signedIn(t, "none")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()
	s, err := c.Prepare(ctx, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.interval = time.Millisecond
	answer, err := s.Exchange(ctx, webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: "synthetic-offer"})
	if err != nil || answer.Type != webrtc.SDPTypeAnswer || answer.SDP != "synthetic-answer" {
		t.Fatalf("signaling: %v", err)
	}
	if _, err = s.Exchange(ctx, webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: "synthetic-offer"}); err == nil {
		t.Fatal("creation POST could be replayed")
	}
}

func TestDevicesListsAuthorizedDevicesSorted(t *testing.T) {
	if _, err := New().Devices(context.Background()); err == nil {
		t.Fatal("unauthenticated device listing allowed")
	}
	devices, err := signedIn(t, "two-devices").Devices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Device{{"00000000-0000-0000-0000-000000000002", "another"}, {"00000000-0000-0000-0000-000000000001", "fixture"}}
	if len(devices) != 2 || devices[0] != want[0] || devices[1] != want[1] {
		t.Fatalf("devices = %+v", devices)
	}
}

func TestDirectIdentityAndLocationBoundaries(t *testing.T) {
	if _, err := New().Prepare(context.Background(), "fixture"); err == nil {
		t.Fatal("unauthenticated discovery allowed")
	}
	c := signedIn(t, "wrong-device")
	if _, err := c.Prepare(context.Background(), "fixture"); err == nil {
		t.Fatal("mismatched shell identity accepted")
	}
	c = signedIn(t, "none")
	if _, err := c.Prepare(context.Background(), "missing"); err == nil {
		t.Fatal("nonmatching device selected")
	}
	c = signedIn(t, "bad-location")
	s, err := c.Prepare(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = s.Exchange(context.Background(), webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: "synthetic-offer"})
	if !errors.Is(err, ErrBoundary) || strings.Contains(err.Error(), "synthetic") {
		t.Fatalf("unexpected location diagnostic: %v", err)
	}
}

func refusedForReauth(t *testing.T, c *Client) *ShellPageError {
	t.Helper()
	_, err := c.Prepare(context.Background(), "fixture")
	var refused *ShellPageError
	if !errors.As(err, &refused) || !refused.Reauthentication {
		t.Fatalf("reauthentication page not recognised: %v", err)
	}
	return refused
}

func TestShellPageRefusalIsDescribedWithoutSecrets(t *testing.T) {
	refused := refusedForReauth(t, signedIn(t, "reauth-silent"))
	details := strings.Join(refused.Lines(), "\n")
	for _, want := range []string{"shell_controllers=0", `title "Reauthenticating with Raspberry Pi ID – Raspberry Pi Connect"`, "POST 127.0.0.1", "/auth/raspberry_pi invalid=false", "authenticity_token(hidden)<21 chars>", "origin(hidden)<66 chars>", `text "Continue"`} {
		if !strings.Contains(details, want) {
			t.Errorf("details lack %q:\n%s", want, details)
		}
	}
	if strings.Contains(details, "synthetic-reauth-csrf") || strings.Contains(details, "remote-shell-session\"") {
		t.Fatalf("details leak a token or field value:\n%s", details)
	}
	_, err := signedIn(t, "wrong-device").Prepare(context.Background(), "fixture")
	if !errors.As(err, &refused) || refused.Reauthentication || !strings.Contains(strings.Join(refused.Details, "\n"), "device_matched=false") {
		t.Fatalf("device mismatch described as reauthentication: %v", err)
	}
}

func TestReauthFormShapeIsStrict(t *testing.T) {
	c := New()
	at, _ := url.Parse(c.root + "/devices/x/remote-shell-session")
	ok := `<input type="hidden" name="authenticity_token" value="t"><input type="hidden" name="origin" value="/devices/x/remote-shell-session"><button>Continue</button>`
	for name, body := range map[string]string{
		"extra field":    `<form action="/auth/raspberry_pi" method="post">` + ok + `<input type="hidden" name="prompt" value="none"></form>`,
		"visible field":  `<form action="/auth/raspberry_pi" method="post">` + ok + `<input name="email"></form>`,
		"query":          `<form action="/auth/raspberry_pi?x=1" method="post">` + ok + `</form>`,
		"foreign action": `<form action="https://elsewhere.invalid/auth/raspberry_pi" method="post">` + ok + `</form>`,
		"foreign origin": `<form action="/auth/raspberry_pi" method="post"><input type="hidden" name="authenticity_token" value="t"><input type="hidden" name="origin" value="https://elsewhere.invalid/"></form>`,
		"no token":       `<form action="/auth/raspberry_pi" method="post"><input type="hidden" name="origin" value="/"></form>`,
		"get":            `<form action="/auth/raspberry_pi">` + ok + `</form>`,
		"two forms":      `<form action="/auth/raspberry_pi" method="post">` + ok + `</form><form action="/auth/raspberry_pi" method="post">` + ok + `</form>`,
	} {
		p, _ := parse(at, []byte(body))
		if c.reauthForm(p) != nil {
			t.Errorf("%s: accepted as the reauthentication form", name)
		}
	}
	p, _ := parse(at, []byte(`<form action="/auth/raspberry_pi" method="post">`+ok+`</form>`))
	if c.reauthForm(p) == nil {
		t.Fatal("observed reauthentication form rejected")
	}
}

// A still-valid Raspberry Pi ID session: the form leads straight back to the
// shell page, as the browser's Continue does with no prompt.
func TestReauthenticateWithLiveIdentitySession(t *testing.T) {
	c := signedIn(t, "reauth-silent")
	refused := refusedForReauth(t, c)
	if stage, err := c.Reauthenticate(context.Background(), refused); err != nil || stage != Authenticated {
		t.Fatalf("silent renewal: %s %v", stage, err)
	}
	s, err := c.Prepare(context.Background(), "fixture")
	if err != nil {
		t.Fatalf("shell page after renewal: %v", err)
	}
	s.Close()
	if _, err := c.Export(); err != nil {
		t.Fatalf("renewed session cannot be saved: %v", err)
	}
	if _, err := c.Reauthenticate(context.Background(), refused); !errors.Is(err, ErrFlow) {
		t.Fatalf("second renewal allowed: %v", err)
	}
}

func TestReauthenticateAsksForPasswordWhenIdentityDoes(t *testing.T) {
	c, passwords := fixture(t, "reauth-password")
	if _, err := c.Begin(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Password(context.Background(), "fixture@example.invalid", []byte("synthetic-password")); err != nil {
		t.Fatal(err)
	}
	refused := refusedForReauth(t, c)
	stage, err := c.Reauthenticate(context.Background(), refused)
	if err != nil || stage != PasswordRequired {
		t.Fatalf("password renewal: %s %v", stage, err)
	}
	if _, err := c.Prepare(context.Background(), "fixture"); err == nil {
		t.Fatal("device preparation allowed while renewal waits for the password")
	}
	if stage, err = c.Password(context.Background(), "fixture@example.invalid", []byte("synthetic-password")); err != nil || stage != Authenticated || *passwords != 2 {
		t.Fatalf("password step: %s %v passwords=%d", stage, err, *passwords)
	}
	s, err := c.Prepare(context.Background(), "fixture")
	if err != nil {
		t.Fatalf("shell page after renewal: %v", err)
	}
	s.Close()
}

// The account chooser shape is not observed yet: describe it, submit nothing.
func TestReauthenticateStopsOnUnrecognizedIdentityPage(t *testing.T) {
	c := signedIn(t, "reauth-chooser")
	refused := refusedForReauth(t, c)
	_, err := c.Reauthenticate(context.Background(), refused)
	var page *ReauthPageError
	if !errors.As(err, &page) {
		t.Fatalf("unrecognized page not reported: %v", err)
	}
	details := strings.Join(page.Lines(), "\n")
	for _, want := range []string{"id page (/authorize)", "/choose", "account(hidden)<15 chars>", "Sign in as Fixture"} {
		if !strings.Contains(details, want) {
			t.Errorf("details lack %q:\n%s", want, details)
		}
	}
	if strings.Contains(details, "synthetic-chooser-csrf") || strings.Contains(details, "fixture-account") {
		t.Fatalf("details leak a value:\n%s", details)
	}
	if _, err := c.Prepare(context.Background(), "fixture"); err == nil {
		t.Fatal("client still signed in after an unrecognized renewal page")
	}
}

func TestDirectAuthorizationAndLifetime(t *testing.T) {
	c := signedIn(t, "auth-revoked")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s, err := c.Prepare(ctx, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.interval = time.Millisecond
	s.timeCheck = 5 * time.Millisecond
	if _, err = s.Exchange(ctx, webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: "synthetic-offer"}); err != nil {
		t.Fatal(err)
	}
	select {
	case message := <-s.Failures():
		if message != "direct Connect authorization check failed" {
			t.Fatal("unrecognized diagnostic")
		}
	case <-ctx.Done():
		t.Fatal("failed authorization was ignored")
	}
	c = signedIn(t, "pending")
	s, err = c.Prepare(ctx, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	s.interval = time.Millisecond
	go func() { time.Sleep(10 * time.Millisecond); s.Close() }()
	if _, err = s.Exchange(ctx, webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: "synthetic-offer"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("session close did not cancel polling: %v", err)
	}
}
