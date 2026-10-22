//go:build darwin || linux

package shellbroker

import (
	"strings"
	"testing"
)

// These are reference observations. The production text filter is tested in
// cmd/pi-sh; this fixture runs the installed OpenSSH against a local sshd.
func TestOpenSSHEscapeHelpConsumption(t *testing.T) {
	reference := ttySSHCommand(t)
	for _, tc := range []struct {
		name, escape, input, want string
		help                      bool
	}{
		{"default", "~", "~?value\r", "value", true},
		{"repeated", "~", "~?~?value\r", "value", true},
		{"alternate", "!", "!?value\r", "value", true},
		{"control", "^]", "\x1d?value\r", "value", true},
		{"disabled", "none", "~?value\r", "~?value", false},
		{"quoted", "~", "~~?value\r", "~?value", false},
		{"middle", "~", "x~?value\r", "x~?value", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := reference("stty -echo; printf 'fixture-ready\\n'; IFS= read -r x; printf 'remote[%s]\\n' \"$x\"; exit 23")
			cmd.Args = append([]string{cmd.Args[0], "-e", tc.escape}, cmd.Args[1:]...)
			code, out := runPTYFixture(t, cmd, "", []byte(tc.input), false)
			if code != 23 || !strings.Contains(out, "remote["+tc.want+"]") {
				t.Fatalf("code%d %q", code, out)
			}
			if strings.Contains(out, "Supported escape sequences:") != tc.help {
				t.Fatalf("help output differed: %q", out)
			}
		})
	}
}
