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
