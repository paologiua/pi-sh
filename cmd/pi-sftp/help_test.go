package main

import (
	"strings"
	"testing"
)

func TestOfflineHelpHasNoFlagPanic(t *testing.T) {
	for _, option := range []string{"-h", "--help"} {
		code, output := runApp(t, option)
		if code != 0 || !strings.HasPrefix(output, "usage: pi-sftp ") || strings.Contains(output, "panic") || strings.Contains(output, "Password (hidden") {
			t.Fatalf("%s: exit %d, output %q", option, code, output)
		}
		for _, required := range []string{"-q", "-X", "--direct-login", "not supported:"} {
			if !strings.Contains(output, required) {
				t.Errorf("help missing %q", required)
			}
		}
	}
}
