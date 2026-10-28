package sshconfig

import (
	"strings"
	"testing"
)

func TestKeywordDecisions(t *testing.T) {
	got, err := Parse(strings.NewReader("Compression no\nAddressFamily ANY\nCanonicalDomains a.example b.example\n"+
		"CanonicalizeMaxDots 0\nGSSAPIAuthentication false\nVisualHostKey no\nRemoteCommand=  printf '%s\\n' \"x  y\" # kept\n"), "pi")
	if err != nil || got["remotecommand"] != `printf '%s\n' "x  y" # kept` {
		t.Fatalf("%q %v", got["remotecommand"], err)
	}
	for _, text := range []string{
		"Compression yes", "AddressFamily inet", "SessionType subsystem", "CanonicalizeMaxDots -1",
		"CanonicalizeFallbackLocal maybe", "IdentityFile secret-path", "Host other\nCiphers secret-cipher",
		"SetEnv A=b", "RemoteCommand \"a\x00b\"", "Host pi\nRefuseConnection secret-message",
		// No SSH host key exists, so no fingerprint or visual key can be shown.
		"FingerprintHash sha256", "VisualHostKey yes",
	} {
		_, err := Parse(strings.NewReader(text), "pi")
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Errorf("%q: %v", text, err)
		}
	}
	// An inactive RefuseConnection only needs valid syntax, as in OpenSSH.
	if _, err := Parse(strings.NewReader("Host other\nRefuseConnection away\n"), "pi"); err != nil {
		t.Fatal(err)
	}
	if key, value, ok := RawCommand("remotecommand"); ok || key != "" || value != "" {
		t.Fatal("keyword without argument treated as a raw command")
	}
}

func TestExpandCommand(t *testing.T) {
	text, err := ExpandCommand("echo %h %n %r %%", "alias", "device", "me@example.com")
	if err != nil || text != "echo device alias me@example.com %" {
		t.Fatalf("%q %v", text, err)
	}
	for _, command := range []string{"echo %p", "echo %C", "echo %k", "echo %j", "echo %z", "echo %"} {
		if _, err := ExpandCommand(command, "alias", "device", "me@example.com"); err == nil || strings.Contains(err.Error(), "Match exec") {
			t.Errorf("%q: %v", command, err)
		}
	}
	if _, err := ExpandCommand("echo %h", "alias", "Pi; rm", ""); err == nil {
		t.Fatal("shell metacharacters spliced from %h")
	}
}
