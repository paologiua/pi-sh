package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestOfflineHelpHasNoFlagPanic(t *testing.T) {
	for _, option := range []string{"-h", "--help"} {
		code, output := runCopy(t, option)
		if code != 0 || !strings.HasPrefix(output, "usage: pi-cp ") || strings.Contains(output, "panic") || strings.Contains(output, "Password (hidden") {
			t.Fatalf("%s: exit %d, output %q", option, code, output)
		}
		for _, required := range []string{"-q", "-X", "-V, --version", "--direct-login", "not supported:", "Tested with Raspberry Pi Connect 2.13.0."} {
			if !strings.Contains(output, required) {
				t.Errorf("help missing %q", required)
			}
		}
	}
}

func TestOfflineVersionDoesNotLoadConfigOrAuthenticate(t *testing.T) {
	for _, args := range [][]string{
		{"-V"}, {"--version"},
		{"-F", filepath.Join(t.TempDir(), "missing-config"), "--version"},
		{"--direct-login", "--browser", "-qV"},
	} {
		code, output := runCopy(t, args...)
		if code != 0 || !strings.HasPrefix(output, "pi-cp_") || !strings.Contains(output, " (unofficial)\n") || strings.Count(output, "\n") != 1 {
			t.Fatalf("%q: exit %d, output %q", args, code, output)
		}
	}
}
