package webauth

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

// sibling is a fresh client for the same fixture servers.
func sibling(c *Client) *Client {
	s := New()
	s.root, s.identity, s.allowed = c.root, c.identity, c.allowed
	s.http.Transport = c.http.Transport
	return s
}

func TestExportRestoreVerify(t *testing.T) {
	ctx := context.Background()
	c := signedIn(t, "none")
	if c.Account() != "fixture@example.invalid" {
		t.Fatalf("account = %q", c.Account())
	}
	data, err := c.Export()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("synthetic-password")) {
		t.Fatal("exported session contains the password")
	}
	if !bytes.Contains(data, []byte("synthetic-session")) {
		t.Fatal("session cookie not exported")
	}
	restored := sibling(c)
	if _, err := restored.Prepare(ctx, "fixture"); err == nil {
		t.Fatal("restored session used before Verify")
	}
	if err := restored.Restore(data); err != nil {
		t.Fatal(err)
	}
	if err := restored.Verify(ctx); err != nil {
		t.Fatal(err)
	}
	if restored.Account() != c.Account() {
		t.Fatal("account not restored")
	}
	s, err := restored.Prepare(ctx, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if err := restored.Restore(data); err == nil {
		t.Fatal("restore into a signed-in client accepted")
	}
}

func TestExportRequiresSignedInSession(t *testing.T) {
	c, _ := fixture(t, "none")
	if _, err := c.Export(); err == nil {
		t.Fatal("unauthenticated session exported")
	}
}

func TestRestoreRejectsForeignOriginsAndFormats(t *testing.T) {
	c, _ := fixture(t, "none")
	for _, data := range []string{
		`{"format":1,"account":"a@example.com","cookies":[{"url":"https://evil.example/","name":"x","value":"y"}]}`,
		`{"format":2,"account":"a@example.com","cookies":[]}`,
		`{"format":1,"account":"","cookies":[]}`,
		`not json`,
	} {
		if err := sibling(c).Restore([]byte(data)); err == nil {
			t.Errorf("accepted %s", data)
		}
	}
}

func TestVerifyDetectsSignedOutSession(t *testing.T) {
	c, _ := fixture(t, "none")
	data := []byte(`{"format":1,"account":"fixture@example.invalid","cookies":[{"url":"` + c.root + `/callback","name":"connect_session","value":"expired","path":"/","secure":true}]}`)
	if err := c.Restore(data); err != nil {
		t.Fatal(err)
	}
	if err := c.Verify(context.Background()); !errors.Is(err, ErrSignedOut) {
		t.Fatalf("expired session: %v", err)
	}
	if _, err := c.Devices(context.Background()); err == nil {
		t.Fatal("signed-out session listed devices")
	}
}

func TestLogoutSubmitsSignOutFormAndForgets(t *testing.T) {
	ctx := context.Background()
	c := signedIn(t, "none")
	data, err := c.Export()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Logout(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Export(); err == nil {
		t.Fatal("session still exportable after logout")
	}
	if len(c.jar.export()) != 0 {
		t.Fatal("cookies kept after logout")
	}
	// The fixture server ended the session, so an old copy no longer works.
	old := sibling(c)
	if err := old.Restore(data); err != nil {
		t.Fatal(err)
	}
	if err := old.Verify(ctx); !errors.Is(err, ErrSignedOut) {
		t.Fatalf("old copy after logout: %v", err)
	}
}
