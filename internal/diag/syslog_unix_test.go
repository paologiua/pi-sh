//go:build !windows && !plan9

package diag

import (
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// A private datagram socket stands in for the local syslog service, so tests
// never write to the host's real system log.
func TestSyslogDestinationAndRedaction(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "pisy")
	if err != nil {
		t.Skip("short socket directory unavailable")
	}
	defer os.RemoveAll(dir)
	addr := dir + "/s"
	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: addr, Net: "unixgram"})
	if err != nil {
		t.Skip("unixgram unavailable:", err)
	}
	defer conn.Close()
	var stderr strings.Builder
	l, err := Open(Config{Program: "pi-sh", Level: Debug1, Stderr: &stderr, Syslog: true, Facility: Local3, syslogNetwork: "unixgram", syslogAddr: addr})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.Fatal("fatal phase")
	l.Debug("debug phase")
	l.Debug2("hidden")
	l.Failure(Error, errorString("password=topsecret"))
	want := []struct {
		pri  string
		text string
	}{{"<154>", "pi-sh: fatal phase"}, {"<159>", "debug1: debug phase"}, {"<155>", "pi-sh: password=[redacted]"}}
	buf := make([]byte, 2048)
	for _, w := range want {
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		got := string(buf[:n])
		// LOCAL3=19: CRIT 19*8+2, DEBUG 19*8+7, ERR 19*8+3.
		if !strings.HasPrefix(got, w.pri) || !strings.Contains(got, "pi-sh[") || !strings.HasSuffix(strings.TrimSuffix(got, "\n"), w.text) {
			t.Errorf("got %q want %s ... %s", got, w.pri, w.text)
		}
		if strings.Contains(got, "topsecret") || strings.Contains(got, "\r") {
			t.Errorf("unsafe syslog record %q", got)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("-y also wrote stderr: %q", stderr.String())
	}
	if _, err := Open(Config{Syslog: true, syslogNetwork: "unixgram", syslogAddr: dir + "/absent"}); err != ErrSyslog {
		t.Fatalf("unavailable syslog: %v", err)
	}
}

type errorString string

func (e errorString) Error() string { return string(e) }
