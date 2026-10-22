//go:build unix

package sshdest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSSHDestinationOpenSSHDifferential compares acceptance, user, host and
// numeric port with the local `ssh -G -F none`, which never connects.
func TestSSHDestinationOpenSSHDifferential(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("local OpenSSH unavailable")
	}
	for _, in := range []string{"alice@pi", "a@b@pi", "@pi", "ssh://pi", "ssh://pi/", "ssh://pi:", "ssh://alice@pi:2222",
		"ssh://al%40ice@pi", "ssh://a+b@pi", "ssh://alice;x=y@pi", "ssh://alice:pw@pi", "ssh://[pi]:23", "ssh://pi:022",
		"ssh://pi:+22", "ssh://pi_x", "ssh://9pi", "ssh://pi:22/", "ssh://@pi", "ssh://alice@", "ssh://;x@pi", "ssh://pi/path",
		"ssh://pi:0", "ssh://pi:65536", "ssh://[::1]", "ssh://[::1]:22", "ssh://pi;fingerprint=x", "ssh://p%20i",
		"ssh://al%zz@pi", "ssh://al%00x@pi", "ssh://a@b@pi", "ssh://-pi", "ssh://.pi", "ssh://pi..x", "ssh://[pi]x",
		"ssh://pi:/", "ssh://", "SSH://pi", "pi:22"} {
		out, refErr := exec.Command(ssh, "-G", "-F", "none", "--", in).Output()
		d, err := SSH(in)
		if (refErr == nil) != (err == nil) {
			t.Errorf("%q: OpenSSH error %v, pi-sh error %v", in, refErr, err)
			continue
		}
		if err != nil {
			continue
		}
		fields := map[string]string{}
		for _, line := range strings.Split(string(out), "\n") {
			key, value, _ := strings.Cut(line, " ")
			fields[key] = value
		}
		port := d.Port
		if port == "" {
			port = "22"
		}
		port = strings.TrimLeft(strings.TrimPrefix(port, "+"), "0")
		if !strings.EqualFold(fields["hostname"], d.Host) || fields["port"] != port || (d.User != "" && fields["user"] != d.User) {
			t.Errorf("%q: OpenSSH user=%q host=%q port=%q, pi-sh %+v", in, fields["user"], fields["hostname"], fields["port"], d)
		}
	}
}

// TestCopyAndSFTPOperandOpenSSHDifferential runs scp/sftp with -S naming a
// local program that prints its arguments and exits; nothing connects.
func TestCopyAndSFTPOperandOpenSSHDifferential(t *testing.T) {
	scp, err1 := exec.LookPath("scp")
	sftp, err2 := exec.LookPath("sftp")
	if err1 != nil || err2 != nil {
		t.Skip("local OpenSSH scp/sftp unavailable")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake-ssh")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '<%s>' \"$a\" >&2; done\necho >&2\nexit 255\n"
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(dir, "local")
	if err := os.WriteFile(local, nil, 0600); err != nil {
		t.Fatal(err)
	}
	observe := func(cmd *exec.Cmd) (user, host, port string, invoked bool) {
		cmd.Dir = dir
		out, _ := cmd.CombinedOutput()
		line, _, _ := strings.Cut(string(out), "\n")
		if !strings.HasPrefix(line, "<") {
			return "", "", "", false
		}
		args := strings.Split(strings.TrimSuffix(strings.TrimPrefix(line, "<"), ">"), "><")
		for i := 0; i < len(args); i++ {
			switch {
			case args[i] == "-l" && i+1 < len(args):
				user = args[i+1]
			case args[i] == "-p" && i+1 < len(args):
				port = args[i+1]
			case strings.HasPrefix(args[i], "-oPort "):
				port = strings.TrimPrefix(args[i], "-oPort ")
			case args[i] == "--" && i+1 < len(args):
				host = args[i+1]
			}
		}
		return user, host, port, true
	}
	for _, in := range []string{"pi:x", "alice@pi:x", "a@b@pi:x", "@pi:x", "pi:", "[::1]:x", "alice@[::1]:x", "[pi]:x",
		"scp://alice@pi:2222/x", "scp://pi/x", "scp://pi", "scp://pi:22", "Pi #1:x", "u ser@pi:x", "scp://a%20b@pi/p%20q",
		"SCP://pi/x", "scp://[::1]/x", "scp://a@b@pi/x"} {
		user, host, port, invoked := observe(exec.Command(scp, "-F", "none", "-S", fake, local, in))
		d, remote, err := Copy(in)
		if invoked != (err == nil && remote) || (invoked && (d.User != user || d.Host != host || d.Port != port)) {
			t.Errorf("scp %q: OpenSSH invoked=%v user=%q host=%q port=%q; pi-cp %+v remote=%v %v", in, invoked, user, host, port, d, remote, err)
		}
	}
	for _, in := range []string{"pi", "alice@pi", "a@b@pi", "pi:dir", "pi:", "alice@pi:dir", "[::1]", "[::1]:dir",
		"alice@[::1]", "sftp://alice@pi:2222/dir", "sftp://pi", "sftp://pi/", "sftp://pi//abs", "sftp://[::1]",
		"sftp://pi:0", "SFTP://pi", "Pi #1", ":x"} {
		cmd := exec.Command(sftp, "-F", "none", "-S", fake, in)
		cmd.Stdin = strings.NewReader("")
		user, host, port, invoked := observe(cmd)
		d, err := SFTP(in)
		if invoked != (err == nil) || (invoked && (d.User != user || d.Host != host || d.Port != port)) {
			t.Errorf("sftp %q: OpenSSH invoked=%v user=%q host=%q port=%q; pi-sftp %+v %v", in, invoked, user, host, port, d, err)
		}
	}
}
