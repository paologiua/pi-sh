//go:build darwin || linux

package cliargs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenSSHSFTPAttachedArguments(t *testing.T) {
	client, err := exec.LookPath("sftp")
	if err != nil {
		t.Skip("local OpenSSH sftp unavailable")
	}
	server := ""
	for _, candidate := range []string{"/usr/libexec/sftp-server", "/usr/lib/openssh/sftp-server", "/usr/libexec/openssh/sftp-server"} {
		if _, err := os.Stat(candidate); err == nil {
			server = candidate
			break
		}
	}
	if server == "" {
		t.Skip("local OpenSSH sftp-server unavailable")
	}
	dir := t.TempDir()
	// In getopt, the '=' in -b=commands belongs to the filename. Go flag's
	// unadapted parser would instead look for 'commands'.
	if err := os.WriteFile(filepath.Join(dir, "=commands"), []byte("quit\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, arg := range []string{"-b-", "-b=commands"} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		cmd := exec.CommandContext(ctx, client, "-D", server, arg, "-Fnone", "-oBatchMode=yes")
		cmd.Dir, cmd.Stdin = dir, strings.NewReader("quit\n")
		output, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("local OpenSSH %s: %v %s", arg, err, output)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, client, "-D", server, "-f=false", "-b-")
	cmd.Stdin = strings.NewReader("quit\n")
	if err := cmd.Run(); err == nil {
		t.Fatal("OpenSSH unexpectedly accepted a value for -f")
	}
}
