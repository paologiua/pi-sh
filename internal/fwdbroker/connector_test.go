package fwdbroker

import (
	"encoding/hex"
	"strings"
	"testing"

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
