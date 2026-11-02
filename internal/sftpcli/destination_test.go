package sftpcli

import (
	"os"
	"path/filepath"
	"testing"
)

// Match sessiontype/command see what OpenSSH sftp would request.
func TestDestinationMatchSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	text := "Match sessiontype subsystem command sftp\nHostName subsystem-device\n" +
		"Match sessiontype exec command /usr/lib/sftp-server\nHostName exec-device\n" +
		"Match tagged t\nHostName tagged-device\n"
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		server, want string
		overrides    []string
	}{
		{"", "subsystem-device", nil},
		{"/usr/lib/sftp-server", "exec-device", nil},
		{"/other", "tagged-device", []string{"Tag=t"}},
		{"/other", "alias", nil},
	} {
		got, err := DestinationWithServer("alias", path, tc.overrides, tc.server)
		if err != nil || got != tc.want {
			t.Errorf("%q: %q %v", tc.server, got, err)
		}
	}
}

func TestAccountOnlyDestinationRefusesConfiguration(t *testing.T) {
	target, _, policy, err := ResolveSession("", "none", nil, "", map[string]string{"user": "me@example.com"})
	if err != nil || target != "" || policy.Account != "me@example.com" {
		t.Fatalf("account only: %q %+v %v", target, policy, err)
	}
	if _, _, _, err := ResolveSession("", filepath.Join(t.TempDir(), "missing"), nil, "", nil); err != ErrChooseDevice {
		t.Fatalf("-F: %v", err)
	}
	if _, _, _, err := ResolveSession("", "none", []string{"HostName=pi"}, "", nil); err != ErrChooseDevice {
		t.Fatalf("HostName: %v", err)
	}
}
