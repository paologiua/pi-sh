//go:build darwin || linux

package sftpcli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

func TestResumeOpenSSHReferenceAndActualServer(t *testing.T) {
	clientPath, err := exec.LookPath("sftp")
	if err != nil {
		t.Skip("local OpenSSH sftp unavailable")
	}
	server := ""
	for _, candidate := range []string{"/usr/libexec/sftp-server", "/usr/lib/openssh/sftp-server", "/usr/libexec/openssh/sftp-server"} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			server = candidate
			break
		}
	}
	if server == "" {
		t.Skip("local OpenSSH sftp-server unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	process := exec.CommandContext(ctx, server)
	input, err := process.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := process.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	client, err := sftp.NewClientPipe(output, input, sftp.MaxPacketChecked(1024))
	if err != nil {
		input.Close()
		process.Wait()
		t.Fatal(err)
	}
	defer func() {
		client.Close()
		input.Close()
		if err := process.Wait(); err != nil {
			t.Error(err)
		}
	}()
	for _, tc := range resumeCases() {
		t.Run(tc.name(), func(t *testing.T) {
			runResumeCase(t, client, t.TempDir(), tc)
			dir := t.TempDir()
			source, prefix, want := resumeFixture(tc.state)
			a, b := filepath.Join(dir, "source"), filepath.Join(dir, "target")
			if err := os.WriteFile(a, source, 0600); err != nil {
				t.Fatal(err)
			}
			if tc.state != "missing" {
				if err := os.WriteFile(b, prefix, 0600); err != nil {
					t.Fatal(err)
				}
			}
			args := []string{"-D", server, "-b-"}
			if tc.form == "global" {
				args = append(args, "-a")
			}
			command := tc.args(a, b)
			var quoted []string
			for _, arg := range command {
				quoted = append(quoted, fmt.Sprintf("%q", arg))
			}
			cmd := exec.CommandContext(ctx, clientPath, args...)
			cmd.Stdin = strings.NewReader(strings.Join(quoted, " ") + "\nquit\n")
			diagnostic, err := cmd.CombinedOutput()
			wantFailure := tc.state == "larger" || tc.direction == "put" && (tc.state == "missing" || tc.state == "complete")
			if (err != nil) != wantFailure {
				t.Fatalf("OpenSSH status %v: %s", err, diagnostic)
			}
			if wantFailure {
				want = prefix
			}
			got, err := os.ReadFile(b)
			if tc.direction == "put" && tc.state == "missing" {
				if !os.IsNotExist(err) {
					t.Fatal("OpenSSH created missing upload target", err)
				}
				return
			}
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("OpenSSH data %d want %d: %v", len(got), len(want), err)
			}
		})
	}
}
