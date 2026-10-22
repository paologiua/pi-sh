package webauth

import (
	"context"
	"errors"
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
