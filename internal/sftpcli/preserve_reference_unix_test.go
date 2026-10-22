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

func preservationServer(t *testing.T, flags ...string) (*sftp.Client, string) {
	t.Helper()
	server := ""
	for _, name := range []string{"/usr/libexec/sftp-server", "/usr/lib/openssh/sftp-server", "/usr/libexec/openssh/sftp-server"} {
		if info, err := os.Stat(name); err == nil && !info.IsDir() {
			server = name
			break
		}
	}
	if server == "" {
		t.Skip("local OpenSSH sftp-server unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, server, flags...)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	client, err := sftp.NewClientPipe(out, in)
	if err != nil {
		in.Close()
		cmd.Wait()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		client.Close()
		in.Close()
		if err := cmd.Wait(); err != nil {
			t.Error(err)
		}
	})
	return client, server
}

func TestPreservationMatchesLocalOpenSSH(t *testing.T) {
	sftpPath, err := exec.LookPath("sftp")
	if err != nil {
		t.Skip("OpenSSH sftp unavailable")
	}
	client, server := preservationServer(t, "-u", "077")
	atime, mtime := time.Unix(1600000001, 987654321), time.Unix(1600000013, 123456789)
	data := bytes.Repeat([]byte{0, 255, 'x'}, 3000)
	for _, direction := range []string{"get", "put"} {
		for _, existing := range []bool{false, true} {
			for _, mode := range []os.FileMode{0600, 0640, 0755, 0777 | os.ModeSetuid | os.ModeSetgid} {
				for _, form := range []string{"command", "global", "resume-flush"} {
					t.Run(fmt.Sprintf("%s/existing%t/%s/%s", direction, existing, mode, form), func(t *testing.T) {
						dir := t.TempDir()
						ours, reference := filepath.Join(dir, "ours"), filepath.Join(dir, "reference")
						seed := func(name string) {
							if err := os.WriteFile(name, data, 0600); err != nil {
								t.Fatal(err)
							}
							if err := os.Chmod(name, mode); err != nil {
								t.Fatal(err)
							}
							if err := os.Chtimes(name, atime, mtime); err != nil {
								t.Fatal(err)
							}
						}
						source, refSource := filepath.Join(dir, "source"), filepath.Join(dir, "ref-source")
						seed(source)
						seed(refSource)
						resume := form == "resume-flush"
						if existing || resume {
							for _, target := range []string{ours, reference} {
								content := []byte("old")
								if resume {
									content = data[:17]
								}
								if err := os.WriteFile(target, content, 0600); err != nil {
									t.Fatal(err)
								}
								if err := os.Chmod(target, 0600); err != nil {
									t.Fatal(err)
								}
							}
						}
						var diagnostics bytes.Buffer
						mask := os.FileMode(077)
						c := Console{Client: client, RemoteUmask: &mask, Errors: &diagnostics, Preserve: form == "global"}
						command := []string{direction, "-p", source, ours}
						refCommand := direction + " -p"
						refArgs := []string{"-D", server + " -u 077", "-b-"}
						if form == "global" {
							command = []string{direction, source, ours}
							refCommand = direction
							refArgs = append(refArgs, "-p")
						}
						if resume {
							command[1] = "-afp"
							refCommand = direction + " -afp"
						}
						if _, err := c.Execute(context.Background(), command); err != nil {
							t.Fatal(err)
						}
						ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer cancel()
						cmd := exec.CommandContext(ctx, sftpPath, refArgs...)
						cmd.Stdin = strings.NewReader(fmt.Sprintf("%s %q %q\nquit\n", refCommand, refSource, reference))
						if output, err := cmd.CombinedOutput(); err != nil {
							t.Fatalf("reference %v: %s", err, output)
						}
						for _, name := range []string{ours, reference} {
							info, err := os.Stat(name)
							if err != nil {
								t.Fatal(err)
							}
							access, ok := localAccessTime(info)
							if !ok || access.Unix() != atime.Unix() || access.Nanosecond() != 0 || info.ModTime().Unix() != mtime.Unix() || info.ModTime().Nanosecond() != 0 || info.Mode() != mode.Perm() {
								t.Fatalf("metadata %s atime=%v mtime=%v", info.Mode(), access, info.ModTime())
							}
							got, err := os.ReadFile(name)
							if err != nil || !bytes.Equal(got, data) {
								t.Fatal("content differs", err)
							}
						}
						if diagnostics.Len() != 0 {
							t.Fatalf("unexpected diagnostic: %s", &diagnostics)
						}
					})
				}
			}
		}
	}
}

func TestPreservationMetadataDenialIsAdvisory(t *testing.T) {
	client, server := preservationServer(t, "-P", "setstat,fsetstat")
	dir := t.TempDir()
	source, target := filepath.Join(dir, "source"), filepath.Join(dir, "target")
	data := []byte("metadata denied but data transferred")
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, nil, 0600); err != nil {
		t.Fatal(err)
	}
	var diagnostics bytes.Buffer
	c := Console{Client: client, Preserve: true, Errors: &diagnostics}
	if _, err := c.Execute(context.Background(), []string{"put", source, target}); err != nil {
		t.Fatal("metadata status became fatal", err)
	}
	if !strings.Contains(diagnostics.String(), "remote set metadata failed") {
		t.Fatal("missing metadata warnings")
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("data failure", err)
	}
	if _, err := exec.LookPath("sftp"); err != nil {
		t.Skip("OpenSSH sftp unavailable")
	}
	cmd := exec.Command("sftp", "-D", server+" -P setstat,fsetstat", "-b-", "-p")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("put %q %q\nquit\n", source, target))
	output, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(strings.ToLower(string(output)), "permission denied") {
		t.Fatalf("reference advisory %v: %s", err, output)
	}
}

func TestDownloadReadFailureSkipsPreservation(t *testing.T) {
	client, _ := preservationServer(t, "-P", "read")
	dir := t.TempDir()
	source, target := filepath.Join(dir, "source"), filepath.Join(dir, "target")
	if err := os.WriteFile(source, []byte("unreadable through SFTP"), 0755); err != nil {
		t.Fatal(err)
	}
	mtime := time.Unix(1600000013, 0)
	if err := os.Chtimes(source, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, nil, 0600); err != nil {
		t.Fatal(err)
	}
	c := Console{Client: client, Preserve: true}
	if _, err := c.Execute(context.Background(), []string{"get", source, target}); err == nil {
		t.Fatal("READ failure hidden")
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0600 || info.ModTime().Unix() == mtime.Unix() {
		t.Fatal("failed download metadata applied", err)
	}
}
