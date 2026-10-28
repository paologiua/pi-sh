package fwdbroker

import (
	"context"
	"encoding/hex"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"pi-sh/internal/forward"
)

// Perl comes first: perl-base is Essential and its connector encodes records
// itself, so it is much faster than netcat behind the Bash byte loop.
func TestSelectPrefersPerlThenNetcat(t *testing.T) {
	perl := map[string][]string{"perl": {perlFingerprint}}
	if c, ok := Select(perl); !ok || c.Name != "perl" {
		t.Fatal(c.Name, ok)
	}
	both := map[string][]string{"nc": {"OpenBSD netcat (Debian patchlevel 1.229-1)"}, "perl": {perlFingerprint}, "socat": {"socat version 1.8"}}
	if c, _ := Select(both); c.Name != "perl" {
		t.Fatal(c.Name)
	}
	delete(both, "perl")
	if c, _ := Select(both); c.Name != "openbsd-nc" {
		t.Fatal(c.Name)
	}
	// A perl whose Socket module failed to load prints an error instead.
	if _, ok := Select(map[string][]string{"perl": {"Can't locate Socket.pm in @INC"}}); ok {
		t.Fatal("perl without Socket selected")
	}
}

// The largest targets must fit the adapter's open record and word bounds.
func TestPerlOpenFitsAdapterRecord(t *testing.T) {
	for _, target := range []forward.Target{
		{Host: strings.Repeat("a", 255), Port: 65535},
		{Path: "/" + strings.Repeat("s", 106)},
	} {
		argv, err := perlSockets.Argv(target)
		if err != nil {
			t.Fatal(err)
		}
		size := len("O 1234567890 h")
		for _, a := range argv {
			if len(a) > maxWord {
				t.Fatalf("word of %d bytes", len(a))
			}
			size += 1 + len(hex.EncodeToString([]byte(a)))
		}
		if size > maxOpen {
			t.Fatalf("open record of %d bytes", size)
		}
	}
	if _, err := perlSockets.Argv(forward.Target{Path: "/" + strings.Repeat("s", 107)}); err == nil {
		t.Fatal("path longer than sockaddr_un accepted")
	}
}

// The adapter's own failure record reaches the caller with its reason; any
// other text after Z is not echoed. Before, "Z protocol" fell through to the
// generic ErrProtocol and the reason was lost.
func TestAdapterFailureRecordIsReported(t *testing.T) {
	m := &Mux{streams: map[int]*stream{}}
	if err := m.dispatch("Z protocol"); err == nil || err.Error() != "forwarding adapter failed: protocol" {
		t.Fatal(err)
	}
	if err := m.dispatch("Z \x1b[2J"); err == nil || strings.Contains(err.Error(), "\x1b") {
		t.Fatal(err)
	}
}

// silentCarrier accepts every send and never answers, like a device that
// stalls before the adapter is ready.
type silentCarrier struct{}

func (silentCarrier) Send(ctx context.Context, b []byte) error { return ctx.Err() }
func (silentCarrier) Receive(ctx context.Context) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// Staging is bounded by time and by the setup context. Before, a device that
// never finished detection held pi-sh (and ignored Ctrl-C) indefinitely.
func TestStartIsBounded(t *testing.T) {
	old := stageTimeout
	stageTimeout = 200 * time.Millisecond
	defer func() { stageTimeout = old }()
	life, stop := context.WithCancel(context.Background())
	defer stop()
	if _, err := Start(context.Background(), life, silentCarrier{}); !errors.Is(err, ErrStageTimeout) {
		t.Fatal(err)
	}
	stageTimeout = time.Minute
	setup, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	if _, err := Start(setup, life, silentCarrier{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

// OpenBSD netcat asks for a large TCP receive buffer so a slowly drained
// server can finish its reply instead of resetting the connection (native
// concurrent downloads, 2026-10-28); a Unix socket has no such option.
func TestNetcatRequestsLargeReceiveBuffer(t *testing.T) {
	argv, err := openBSDNetcat.Argv(forward.Target{Host: "example.org", Port: 443})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(argv, " "), "nc -N -I 4194304 -v -- example.org 443"; got != want {
		t.Fatalf("argv %q, want %q", got, want)
	}
	argv, err = openBSDNetcat.Argv(forward.Target{Path: "/run/x.sock"})
	if err != nil || strings.Contains(strings.Join(argv, " "), "-I") {
		t.Fatalf("unix argv %q, %v", argv, err)
	}
}

// The adapter and the mux must agree on the window, or the adapter fails the
// protocol when pi-sh sends more records than it accepts.
// adapter.pl is embedded in a single-quoted Bash word.
func TestPerlAdapterHasNoApostrophe(t *testing.T) {
	if strings.Contains(perlSource, "'") {
		t.Fatal("adapter.pl contains an apostrophe")
	}
}

func TestAdapterWindowMatchesMux(t *testing.T) {
	m := regexp.MustCompile(`(?m)^_fw_win=([0-9]+) `).FindStringSubmatch(source)
	if m == nil {
		t.Fatal("_fw_win not found in forward.bash")
	}
	n, _ := strconv.Atoi(m[1])
	for name, w := range map[string]int{"inputWindow": inputWindow, "outWindow": outWindow} {
		if w != n {
			t.Errorf("_fw_win=%d, %s=%d", n, name, w)
		}
	}
}
