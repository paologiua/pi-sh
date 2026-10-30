//go:build darwin || linux

package sftpcli

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pi-sh/internal/sftpclient"
	"pi-sh/internal/shellbroker"
)

func TestFlushAgainstActualLocalOpenSSHServer(t *testing.T) {
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
	for _, recursive := range []bool{false, true} {
		for _, allowed := range []bool{true, false} {
			t.Run(map[bool]string{true: "advertised", false: "disabled"}[allowed]+map[bool]string{true: "/recursive", false: "/file"}[recursive], func(t *testing.T) {
				var logs, diagnostic bytes.Buffer
				runner := func(ctx context.Context, text string, stdio shellbroker.IO) (int, error) {
					text += " -l DEBUG3"
					if !allowed {
						text += " -P fsync"
					}
					cmd := exec.CommandContext(ctx, "/bin/sh", "-c", text)
					cmd.Stdin, cmd.Stdout, cmd.Stderr = stdio.Stdin, stdio.Stdout, io.MultiWriter(stdio.Stderr, &logs)
					err := cmd.Run()
					if e, ok := err.(*exec.ExitError); ok {
						return e.ExitCode(), nil
					}
					return 0, err
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				session, err := sftpclient.Open(ctx, runner, sftpclient.Options{Server: server})
				if err != nil {
					t.Fatal(err)
				}
				defer session.Close()
				version, present := session.Client.HasExtension("fsync@openssh.com")
				if present != allowed || present && version != "1" {
					t.Fatal("unexpected server fsync advertisement")
				}
				dir := t.TempDir()
				data := make([]byte, 256*32)
				for i := range data {
					data[i] = byte(i)
				}
				if err := os.WriteFile(filepath.Join(dir, "input"), data, 0600); err != nil {
					t.Fatal(err)
				}
				commands := "put input upload\nget upload download\nquit\n"
				download := "download"
				if recursive {
					if err := os.MkdirAll(filepath.Join(dir, "tree/nested"), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.Rename(filepath.Join(dir, "input"), filepath.Join(dir, "tree/nested/input")); err != nil {
						t.Fatal(err)
					}
					commands = "mput -r tree upload\nmget -r upload download\nquit\n"
					download = "download/nested/input"
				}
				c := Console{Client: session.Client, RemoteUmask: &session.OriginalUmask, Flush: true, Batch: true, LocalDir: dir, RemoteDir: dir, Output: io.Discard, Errors: &diagnostic, Input: strings.NewReader(commands)}
				err = c.Run(ctx)
				closed := session.Close() // joins all stderr writes before log inspection
				if err != nil || closed != nil || diagnostic.Len() != 0 {
					t.Fatal("local server transfer/flush failed", err, closed, diagnostic.String())
				}
				got, err := os.ReadFile(filepath.Join(dir, download))
				if err != nil || !bytes.Equal(got, data) {
					t.Fatal("actual-server flush changed bytes", err)
				}
				syncAt := bytes.Index(logs.Bytes(), []byte("fsync \""))
				if allowed {
					writeAt := bytes.LastIndex(logs.Bytes(), []byte(": write \""))
					closeAt := bytes.Index(logs.Bytes(), []byte("close \""))
					if syncAt < 0 || writeAt < 0 || syncAt <= writeAt || closeAt <= syncAt {
						t.Fatal("fsync was not observed after writes and before handle close")
					}
				} else if syncAt >= 0 {
					t.Fatal("fsync sent despite missing advertisement")
				}
			})
		}
	}
}
