//go:build darwin || linux

package forwardspec

import (
	"os/exec"
	"strings"
	"testing"
)

// Forwarding lines printed by local `ssh -G` are compared, in order, with
// Settings.WriteConfig for the same flags. Parsing is offline in both.
func TestForwardDumpOpenSSHDifferential(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("local OpenSSH unavailable")
	}
	for _, flags := range [][]string{
		{"-L", "8080:h:80", "-L", "[::1]:81:[fe80::1]:22", "-L", "*:82:h:80", "-L", ":83:h:80", "-L", "localhost:84:h:80",
			"-L", "/tmp/a.sock:h:80", "-L", "85:/r/s", "-D", "1080", "-D", "[::1]:1081", "-D", "h:1082",
			"-R", "9000:h:22", "-R", "0:h:22", "-R", "9001", "-R", "/r/sock:h:22", "-R", "9002:/l/s", "-g"},
		{"-D", "1", "-L", "2:h:3", "-D", "4", "-o", "DynamicForward 5", "-o", "LocalForward 6 h:7", "-L", "2:h:3"},
		{"-L", "1:h:ssh", "-L", "a\\:b:1:h:2", "-L", "1:h:2:", "-L", "+5:h: 2", "-L", "1::2"},
		{"-o", "ClearAllForwardings=yes", "-L", "1:h:2"},
		{"-W", "h:22", "-L", "1:h:2"},
		{"-o", "PermitRemoteOpen h:1 [::1]:2 *:*", "-o", "StreamLocalBindMask 022", "-o", "StreamLocalBindUnlink yes", "-o", "ExitOnForwardFailure yes"},
		{"-o", "StreamLocalBindMask 0", "-o", "GatewayPorts true"},
	} {
		args := append([]string{"-G", "-F", "none"}, flags...)
		out, err := exec.Command(ssh, append(args, "x")...).Output()
		if err != nil {
			t.Fatal(flags, err)
		}
		s := NewSettings()
		gw := false
		for i := 0; i < len(flags); i++ {
			var err error
			switch flags[i] {
			case "-g":
				gw = true
				continue
			case "-L":
				err = s.AddLocal(flags[i+1], noEnv)
			case "-D":
				err = s.AddDynamic(flags[i+1], noEnv)
			case "-R":
				err = s.AddRemote(flags[i+1], noEnv)
			case "-W":
				err = s.SetStdio(flags[i+1], noEnv)
			case "-o":
				line := strings.Replace(flags[i+1], "=", " ", 1)
				words := strings.Fields(line)
				key := strings.ToLower(words[0])
				if ListKeyword(key) {
					err = s.Directive(key, words[1:], noEnv, false)
				} else {
					err = s.SetScalar(key, words[1])
				}
			}
			if err != nil {
				t.Fatal(flags, err)
			}
			i++
		}
		if gw {
			s.GatewayPorts = true
		}
		s.Finish()
		var got strings.Builder
		if err := s.WriteConfig(&got); err != nil {
			t.Fatal(err)
		}
		want := relevant(string(out), got.String())
		if strings.Join(want, "\n") != strings.Join(lines(got.String()), "\n") {
			t.Errorf("%q:\npi-sh:\n%s\nOpenSSH:\n%s", flags, got.String(), strings.Join(want, "\n"))
		}
	}
}

func lines(s string) []string { return strings.Split(strings.TrimSpace(s), "\n") }

// relevant keeps the reference lines whose keys pi-sh prints, in pi-sh order
// for scalars and in reference order for each repeated key.
func relevant(reference, ours string) []string {
	byKey := map[string][]string{}
	for _, line := range lines(reference) {
		key, _, _ := strings.Cut(line, " ")
		byKey[key] = append(byKey[key], line)
	}
	var out []string
	seen := map[string]bool{}
	for _, line := range lines(ours) {
		key, _, _ := strings.Cut(line, " ")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, byKey[key]...)
	}
	// Keys that pi-sh omitted entirely (an empty forwarding list).
	for _, key := range []string{"dynamicforward", "localforward", "remoteforward"} {
		if !seen[key] {
			out = append(out, byKey[key]...)
		}
	}
	return out
}

func TestRejectionsMatchOpenSSH(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("local OpenSSH unavailable")
	}
	for _, tc := range []struct{ flag, spec string }{
		{"-L", "h"}, {"-L", "0:h:1"}, {"-L", "1:h:Ssh"}, {"-L", "SSH:h:2"}, {"-L", "1:h:2 "},
		{"-L", "/" + strings.Repeat("a", maxSunPath-1) + ":h:1"}, {"-D", "0"}, {"-D", "a:b:c"},
		{"-R", "1:h:0"}, {"-W", "h"}, {"-W", "a:b:c"}, {"-W", ""},
	} {
		if exec.Command(ssh, "-G", "-F", "none", tc.flag, tc.spec, "x").Run() == nil {
			t.Fatalf("reference accepted %s %q", tc.flag, tc.spec)
		}
		s := NewSettings()
		var err error
		switch tc.flag {
		case "-L":
			err = s.AddLocal(tc.spec, noEnv)
		case "-D":
			err = s.AddDynamic(tc.spec, noEnv)
		case "-R":
			err = s.AddRemote(tc.spec, noEnv)
		case "-W":
			err = s.SetStdio(tc.spec, noEnv)
		}
		if err == nil {
			t.Errorf("pi-sh accepted %s %q", tc.flag, tc.spec)
		}
	}
	// One byte shorter than sun_path is accepted by both.
	path := "/" + strings.Repeat("a", maxSunPath-2) + ":h:1"
	if exec.Command(ssh, "-G", "-F", "none", "-L", path, "x").Run() != nil {
		t.Fatal("reference rejected maximal path")
	}
	if _, ok := Local(path, noEnv); !ok {
		t.Fatal("pi-sh rejected maximal path")
	}
}
