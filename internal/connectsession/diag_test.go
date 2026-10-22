package connectsession

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"pi-sh/internal/diag"
	"pi-sh/internal/shellbroker"
	"pi-sh/internal/transport"
)

// The companion step is an instruction carrying a loopback capability: -q must
// not suppress it, and it must never reach an -E log file. Only a loopback
// listener is opened; nothing contacts Connect.
func TestBrowserInstructionBypassesQuietAndLogFile(t *testing.T) {
	for _, level := range []diag.Level{diag.Quiet, diag.Debug3} {
		path := filepath.Join(t.TempDir(), "client.log")
		log, err := diag.Open(diag.Config{Program: "pi-sh", Level: level, File: path, UseFile: true})
		if err != nil {
			t.Fatal(err)
		}
		var notices bytes.Buffer
		ctx, cancel := context.WithCancel(context.Background())
		source, err := Authenticate(diag.With(ctx, log), "device", false, &notices, "pi-sh")
		if err != nil {
			t.Fatal(err)
		}
		source.Close()
		cancel()
		log.Close()
		logged, _ := os.ReadFile(path)
		text := notices.String()
		if !strings.Contains(text, "run in the browser console") || !strings.Contains(text, "fetch(\"http://127.0.0.1:") {
			t.Fatalf("%v: instruction suppressed: %q", level, text)
		}
		if strings.Contains(text, "experimental browser authentication") {
			t.Fatalf("%v: informational notice not routed through the logger", level)
		}
		if strings.Contains(string(logged), "127.0.0.1") || strings.Contains(string(logged), "companion.js") || strings.Contains(string(logged), "fetch(") {
			t.Fatalf("%v: capability reached log file: %q", level, logged)
		}
		if level == diag.Quiet && len(logged) != 0 {
			t.Fatalf("quiet logged %q", logged)
		}
		if level == diag.Debug3 && (!strings.Contains(string(logged), "pi-sh: experimental browser authentication") || !strings.Contains(string(logged), "debug1: authentication: loopback companion bridge listening")) {
			t.Fatalf("debug log lacks phases: %q", logged)
		}
	}
	// Without a configured logger the previous INFO notice behavior remains.
	var notices bytes.Buffer
	source, err := Authenticate(context.Background(), "device", false, &notices, "pi-cp")
	if err != nil {
		t.Fatal(err)
	}
	source.Close()
	if !strings.Contains(notices.String(), "pi-cp: experimental browser authentication") {
		t.Fatalf("default notice missing: %q", notices.String())
	}
}

// Dial phases and WebRTC states are logged at their levels with no SDP, ICE
// or address material, using a loopback-only peer pair.
func TestDialPhaseDiagnosticsAreSecretFree(t *testing.T) {
	var out lockedBuffer
	log, err := diag.Open(diag.Config{Program: "pi-sh", Level: diag.Debug3, Stderr: &out})
	if err != nil {
		t.Fatal(err)
	}
	lifetime, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	peer, err := transport.New(lifetime, transport.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	ctx := diag.With(lifetime, log)
	session, err := Dial(ctx, localSource{peer})
	if err != nil {
		t.Fatal(err)
	}
	remote, err := peer.Accept(lifetime)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := remote.Receive(lifetime); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, _ = session.Run(cancelled, "true", shellbroker.IO{})
	session.Close()
	text := out.String()
	for _, want := range []string{
		"debug1: signaling: requesting session configuration\r\n",
		"debug2: signaling: session configuration received ice_servers=0\r\n",
		"debug1: signaling: sending local offer\r\n",
		"debug1: signaling: remote answer received\r\n",
		"debug1: transport: shell data channel open elapsed=",
		"debug2: transport: peer connection state state=connected\r\n",
		"debug3: transport: ICE connection state state=",
		"debug1: broker: starting shell-only broker\r\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	for _, secret := range []string{"v=0", "candidate", "ice-pwd", "ice-ufrag", "fingerprint", "127.0.0.1", "typ host"} {
		if strings.Contains(text, secret) {
			t.Errorf("diagnostics contain %q:\n%s", secret, text)
		}
	}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
