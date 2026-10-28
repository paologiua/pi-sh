package browserauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

func testBridge(t *testing.T) *Bridge {
	t.Helper()
	b, err := New(context.Background(), "test-device")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

func request(t *testing.T, b *Bridge, method, path, origin, body string) *http.Response {
	t.Helper()
	r, err := http.NewRequest(method, "http://"+b.listener.Addr().String()+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	r.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestCompanionRejectsUntrustedRequests(t *testing.T) {
	b := testBridge(t)
	for _, tc := range []struct{ origin, path string }{{"", b.prefix + "companion.js"}, {"https://attacker.invalid", b.prefix + "companion.js"}, {Origin, "/wrong-capability/companion.js"}} {
		r := request(t, b, "GET", tc.path, tc.origin, "")
		_ = r.Body.Close()
		if r.StatusCode != 404 || r.Header.Get("Access-Control-Allow-Origin") != "" {
			t.Fatal("untrusted request received companion access")
		}
	}
	r := request(t, b, "OPTIONS", b.prefix+"configuration", Origin, "")
	defer r.Body.Close()
	if r.StatusCode != 204 || r.Header.Get("Access-Control-Allow-Origin") != Origin || r.Header.Get("Access-Control-Allow-Private-Network") != "true" {
		t.Fatal("invalid trusted preflight")
	}
	spoofed, err := http.NewRequest("GET", b.ScriptURL(), nil)
	if err != nil {
		t.Fatal(err)
	}
	spoofed.Host = "attacker.invalid"
	spoofed.Header.Set("Origin", Origin)
	reply, err := http.DefaultClient.Do(spoofed)
	if err != nil {
		t.Fatal(err)
	}
	defer reply.Body.Close()
	if reply.StatusCode != 404 || reply.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("spoofed Host accepted")
	}
}

func TestInMemorySignalingExchange(t *testing.T) {
	b := testBridge(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	config := `{"device":{"id":"test-id","name":"test-device"},"configuration":{"iceServers":[]}}`
	r := request(t, b, "POST", b.prefix+"configuration", Origin, config)
	_ = r.Body.Close()
	if r.StatusCode != 204 {
		t.Fatal(r.StatusCode)
	}
	cfg, err := b.Configuration(ctx)
	if err != nil || cfg.Device.ID != "test-id" {
		t.Fatal("configuration exchange failed")
	}
	answer := make(chan webrtc.SessionDescription, 1)
	errs := make(chan error, 1)
	go func() {
		a, err := b.Exchange(ctx, webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: "in-memory-test-offer"})
		answer <- a
		errs <- err
	}()
	o := request(t, b, "GET", b.prefix+"offer", Origin, "")
	var offer webrtc.SessionDescription
	if err := json.NewDecoder(o.Body).Decode(&offer); err != nil {
		t.Fatal(err)
	}
	_ = o.Body.Close()
	if offer.SDP != "in-memory-test-offer" {
		t.Fatal("offer altered")
	}
	a := request(t, b, "POST", b.prefix+"answer", Origin, `{"type":"answer","sdp":"in-memory-test-answer"}`)
	_ = a.Body.Close()
	if a.StatusCode != 204 {
		t.Fatal(a.StatusCode)
	}
	if got := <-answer; got.SDP != "in-memory-test-answer" {
		t.Fatal("answer altered")
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
}

func TestCompanionErrorsDoNotExposeBody(t *testing.T) {
	b := testBridge(t)
	r := request(t, b, "POST", b.prefix+"failure", Origin, `{"error":"PRIVATE-TEST-SENTINEL"}`)
	_ = r.Body.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := b.Configuration(ctx)
	if err == nil || strings.Contains(err.Error(), "PRIVATE-TEST-SENTINEL") {
		t.Fatal("browser exception escaped")
	}
}

func TestReauthenticationReportsSafeRecovery(t *testing.T) {
	b := testBridge(t)
	r := request(t, b, "POST", b.prefix+"failure", Origin, `{"code":"reauthentication_required","error":"PRIVATE-TEST-SENTINEL"}`)
	_ = r.Body.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := b.Configuration(ctx)
	if err == nil || !strings.Contains(err.Error(), "requires recent sign-in") || strings.Contains(err.Error(), "PRIVATE-TEST-SENTINEL") {
		t.Fatal("reauthentication did not produce safe recovery instructions")
	}
}

func TestUnknownFailureCodeIsRedacted(t *testing.T) {
	if got := failureMessage("PRIVATE-TEST-SENTINEL"); got != "browser operation failed" {
		t.Fatal("untrusted browser code escaped into diagnostics")
	}
}

func TestDestinationAndBodyLimits(t *testing.T) {
	b := testBridge(t)
	for _, body := range []string{`{"device":{"id":"different","name":"other"}}`, `{"type":"offer","sdp":"bad-answer-type"}`, strings.Repeat("x", 1024*1024+1)} {
		path := "configuration"
		if strings.Contains(body, "bad-answer-type") {
			path = "answer"
		}
		r := request(t, b, "POST", b.prefix+path, Origin, body)
		_ = r.Body.Close()
		if r.StatusCode != http.StatusBadRequest {
			t.Fatal("invalid body accepted")
		}
	}
	r := request(t, b, "GET", b.prefix+"companion.js", Origin, "")
	defer r.Body.Close()
	data, err := io.ReadAll(r.Body)
	if err != nil || !strings.Contains(string(data), "credentials: \"omit\"") {
		t.Fatal("companion script unavailable")
	}
}

func TestMissingBrowserAuthorizationExpires(t *testing.T) {
	b := testBridge(t)
	go b.watchAuthorization(25 * time.Millisecond)
	select {
	case <-b.Failures():
	case <-time.After(2 * time.Second):
		t.Fatal("missing browser heartbeat did not expire")
	}
}

func TestCancellationStopsBrowserWaits(t *testing.T) {
	b := testBridge(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.Configuration(ctx); err == nil {
		t.Fatal("cancelled configuration wait succeeded")
	}
	_ = b.Close()
	if _, err := b.Exchange(context.Background(), webrtc.SessionDescription{Type: webrtc.SDPTypeOffer}); err == nil {
		t.Fatal("closed bridge exchange succeeded")
	}
}

// A second Exchange (forwarding carrier or a retried attempt) is served by the
// same companion and shares one watcher: with one heartbeat per interval, the
// session must not expire.
func TestSecondExchangeSharesOneAuthorizationWatcher(t *testing.T) {
	b := testBridge(t)
	b.heartbeat = 300 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i, sdp := range []string{"first-offer", "second-offer"} {
		errs := make(chan error, 1)
		go func() {
			a, err := b.Exchange(ctx, webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: sdp})
			if err == nil && a.SDP != sdp+"-answer" {
				err = errors.New("answer altered")
			}
			errs <- err
		}()
		o := request(t, b, "GET", b.prefix+"offer", Origin, "")
		var offer webrtc.SessionDescription
		if err := json.NewDecoder(o.Body).Decode(&offer); err != nil || offer.SDP != sdp {
			t.Fatalf("offer %d: %v %q", i, err, offer.SDP)
		}
		_ = o.Body.Close()
		a := request(t, b, "POST", b.prefix+"answer", Origin, `{"type":"answer","sdp":"`+sdp+`-answer"}`)
		_ = a.Body.Close()
		if err := <-errs; err != nil {
			t.Fatal(i, err)
		}
	}
	stop := time.After(1500 * time.Millisecond)
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case message := <-b.Failures():
			t.Fatalf("heartbeat split between watchers: %s", message)
		case <-tick.C:
			r := request(t, b, "POST", b.prefix+"authorized", Origin, "{}")
			_ = r.Body.Close()
		case <-stop:
			return
		}
	}
}

// An offer taken by a poll that already ended is kept for the next poll, and an
// answer left by an abandoned exchange is not handed to the next offer. Before,
// the first lost the offer (Exchange then waited for an answer that could not
// come) and the second paired a new offer with a stale answer.
func TestUndeliveredOfferAndStaleAnswer(t *testing.T) {
	b := testBridge(t)
	ended, cancelPoll := context.WithCancel(context.Background())
	cancelPoll()
	poll := httptest.NewRequest("GET", "http://"+b.listener.Addr().String()+b.prefix+"offer", nil).WithContext(ended)
	b.deliver(httptest.NewRecorder(), poll, webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: "kept-offer"})
	o := request(t, b, "GET", b.prefix+"offer", Origin, "")
	var offer webrtc.SessionDescription
	if err := json.NewDecoder(o.Body).Decode(&offer); err != nil || offer.SDP != "kept-offer" {
		t.Fatalf("offer %q %v", offer.SDP, err)
	}
	o.Body.Close()

	stale := request(t, b, "POST", b.prefix+"answer", Origin, `{"type":"answer","sdp":"stale-answer"}`)
	stale.Body.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got := make(chan string, 1)
	go func() {
		a, _ := b.Exchange(ctx, webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: "new-offer"})
		got <- a.SDP
	}()
	o = request(t, b, "GET", b.prefix+"offer", Origin, "")
	o.Body.Close()
	fresh := request(t, b, "POST", b.prefix+"answer", Origin, `{"type":"answer","sdp":"fresh-answer"}`)
	fresh.Body.Close()
	if sdp := <-got; sdp != "fresh-answer" {
		t.Fatalf("exchange got %q", sdp)
	}
}

// pi-cp and pi-sftp share the bridge; its advice named pi-sh (seen natively
// from pi-cp on 2026-10-28).
func TestFailureAdviceNamesNoProgram(t *testing.T) {
	for _, code := range []string{"reauthentication_required", "local_companion_failed", "device_mismatch"} {
		if msg := failureMessage(code); strings.Contains(msg, "pi-sh") {
			t.Errorf("%s: %q", code, msg)
		}
	}
}
