//go:build darwin || linux

package sftpcli

import (
	"context"
	"fmt"
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

func TestTransferCreationPermissionsAgainstOpenSSH(t *testing.T) {
	server := ""
	for _, candidate := range []string{"/usr/libexec/sftp-server", "/usr/lib/openssh/sftp-server", "/usr/libexec/openssh/sftp-server"} {
		if _, err := os.Stat(candidate); err == nil {
			server = candidate
			break
		}
	}
	if server == "" {
		t.Skip("local sftp-server unavailable")
	}
	if _, err := exec.LookPath("sftp"); err != nil {
		t.Skip("OpenSSH sftp unavailable")
	}
	for _, mask := range []string{"022", "077"} {
		t.Run(mask, func(t *testing.T) {
			dir := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			run := func(ctx context.Context, text string, stdio shellbroker.IO) (int, error) {
				cmd := exec.CommandContext(ctx, "/bin/sh", "-c", "umask "+mask+"; "+text)
				cmd.Stdin, cmd.Stdout, cmd.Stderr = stdio.Stdin, stdio.Stdout, stdio.Stderr
				err := cmd.Run()
				if e, ok := err.(*exec.ExitError); ok {
					return e.ExitCode(), nil
				}
				return 0, err
			}
			session, err := sftpclient.Open(ctx, run, sftpclient.Options{Server: server})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			c := Console{Client: session.Client, RemoteUmask: &session.OriginalUmask, LocalDir: dir, RemoteDir: dir}
			if _, err := c.Execute(ctx, []string{"mkdir", "created-directory"}); err != nil {
				t.Fatal(err)
			}
			createdDir, err := os.Stat(filepath.Join(dir, "created-directory"))
			if err != nil || createdDir.Mode().Perm() != 0777&^session.OriginalUmask {
				t.Fatal("directory umask changed", err)
			}
			refMkdir := exec.CommandContext(ctx, "/bin/sh", "-c", `umask `+mask+`; exec sftp -D "$1" -b -`, "sftp-reference", server)
			refMkdir.Stdin = strings.NewReader(fmt.Sprintf("mkdir %q\nquit\n", filepath.Join(dir, "reference-directory")))
			if output, err := refMkdir.CombinedOutput(); err != nil {
				t.Fatalf("reference mkdir: %v %s", err, output)
			}
			refDir, err := os.Stat(filepath.Join(dir, "reference-directory"))
			if err != nil || refDir.Mode().Perm() != createdDir.Mode().Perm() {
				t.Fatal("directory creation mode differs from OpenSSH", err)
			}
			for _, mode := range []os.FileMode{0600, 0640, 0700, 0777} {
				prefix := fmt.Sprintf("%03o", mode)
				source := filepath.Join(dir, prefix+"-source")
				if err := os.WriteFile(source, []byte("private contents"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(source, mode); err != nil {
					t.Fatal(err)
				}
				ours, reference := prefix+"-ours", prefix+"-reference"
				if _, err := c.Execute(ctx, []string{"put", source, ours}); err != nil {
					t.Fatal(err)
				}
				cmd := exec.CommandContext(ctx, "/bin/sh", "-c", `umask `+mask+`; exec sftp -D "$1" -b -`, "sftp-reference", server)
				cmd.Stdin = strings.NewReader(fmt.Sprintf("put %q %q\nquit\n", source, filepath.Join(dir, reference)))
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("reference: %v %s", err, output)
				}
				got, err := os.Stat(filepath.Join(dir, ours))
				if err != nil {
					t.Fatal(err)
				}
				want, err := os.Stat(filepath.Join(dir, reference))
				if err != nil {
					t.Fatal(err)
				}
				if got.Mode().Perm() != want.Mode().Perm() {
					t.Fatalf("put %03o vs OpenSSH %03o", got.Mode().Perm(), want.Mode().Perm())
				}
				if err := os.Chmod(filepath.Join(dir, ours), 0640); err != nil {
					t.Fatal(err)
				}
				if _, err := c.Execute(ctx, []string{"put", source, ours}); err != nil {
					t.Fatal(err)
				}
				got, err = os.Stat(filepath.Join(dir, ours))
				if err != nil || got.Mode().Perm() != 0640 {
					t.Fatal("existing upload permissions changed", err)
				}
				if _, err := c.Execute(ctx, []string{"get", source, prefix + "-download"}); err != nil {
					t.Fatal(err)
				}
				got, err = os.Stat(filepath.Join(dir, prefix+"-download"))
				if err != nil {
					t.Fatal(err)
				}
				if got.Mode().Perm() & ^mode != 0 {
					t.Fatal("download permissions exceed source permissions")
				}
				refGet := filepath.Join(dir, prefix+"-reference-download")
				cmd = exec.CommandContext(ctx, "sftp", "-D", server, "-b", "-")
				cmd.Stdin = strings.NewReader(fmt.Sprintf("get %q %q\nquit\n", source, refGet))
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("reference get: %v %s", err, output)
				}
				want, err = os.Stat(refGet)
				if err != nil || got.Mode().Perm() != want.Mode().Perm() {
					t.Fatal("download creation mode differs from OpenSSH", err)
				}
				if err := os.Chmod(filepath.Join(dir, prefix+"-download"), 0640); err != nil {
					t.Fatal(err)
				}
				if _, err := c.Execute(ctx, []string{"get", source, prefix + "-download"}); err != nil {
					t.Fatal(err)
				}
				got, err = os.Stat(filepath.Join(dir, prefix+"-download"))
				if err != nil || got.Mode().Perm() != 0640 {
					t.Fatal("existing download permissions changed", err)
				}
			}
			if err := session.Close(); err != nil {
				t.Fatal(err)
			}
			denyChmod := func(ctx context.Context, text string, stdio shellbroker.IO) (int, error) {
				return run(ctx, text+" -P fsetstat", stdio)
			}
			restricted, err := sftpclient.Open(ctx, denyChmod, sftpclient.Options{Server: server})
			if err != nil {
				t.Fatal(err)
			}
			defer restricted.Close()
			c = Console{Client: restricted.Client, RemoteUmask: &restricted.OriginalUmask, LocalDir: dir, RemoteDir: dir}
			if _, err := c.Execute(ctx, []string{"put", "600-source", "denied-chmod"}); err == nil {
				t.Fatal("chmod rejection ignored")
			}
			empty, err := os.Stat(filepath.Join(dir, "denied-chmod"))
			if err != nil || empty.Size() != 0 || empty.Mode().Perm()&0077 != 0 {
				t.Fatal("failed upload was not empty and private from creation", err)
			}
			if err := restricted.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestConsoleAgainstLocalOpenSSHServerPreservesSymlinkTraversal(t *testing.T) {
	server := ""
	for _, candidate := range []string{"/usr/libexec/sftp-server", "/usr/lib/openssh/sftp-server", "/usr/libexec/openssh/sftp-server"} {
		if _, err := os.Stat(candidate); err == nil {
			server = candidate
			break
		}
	}
	if server == "" {
		t.Skip("local sftp-server unavailable")
	}
	dir := t.TempDir()
	local := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "real", "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real/sub", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	for file, data := range map[string]string{filepath.Join(dir, "target"): "wrong", filepath.Join(dir, "real", "target"): "actual"} {
		if err := os.WriteFile(file, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Quote-sensitive executable path verifies ServerCommand without evaluating
	// any filename or transfer path in the shell.
	alias := filepath.Join(t.TempDir(), "sftp ' server")
	if err := os.Symlink(server, alias); err != nil {
		t.Fatal(err)
	}
	runner := func(ctx context.Context, text string, stdio shellbroker.IO) (int, error) {
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", text)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = stdio.Stdin, stdio.Stdout, stdio.Stderr
		if err := cmd.Run(); err != nil {
			if e, ok := err.(*exec.ExitError); ok {
				return e.ExitCode(), nil
			}
			return 0, err
		}
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := sftpclient.Open(ctx, runner, sftpclient.Options{Server: alias})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	c := Console{Client: session.Client, RemoteUmask: &session.OriginalUmask, RemoteDir: dir, LocalDir: local, Output: io.Discard}
	if _, err = c.Execute(ctx, []string{"get", "link/../target", "download"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(local, "download"))
	if err != nil || string(data) != "actual" {
		t.Fatal("get normalized away remote symlink traversal", err)
	}
	c.LocalDir = dir
	if _, err = c.Execute(ctx, []string{"put", "link/../target", "uploaded"}); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(filepath.Join(dir, "uploaded"))
	if err != nil || string(data) != "actual" {
		t.Fatal("put normalized away local symlink traversal", err)
	}
	if _, err = c.Execute(ctx, []string{"chmod", "600", "uploaded"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "uploaded"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("chmod mode not applied", err)
	}
	if err = session.Close(); err != nil {
		t.Fatal(err)
	}
	readonly, err := sftpclient.Open(ctx, runner, sftpclient.Options{Server: alias, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer readonly.Close()
	if _, err = readonly.Client.OpenFile(filepath.Join(dir, "read-only-blocked"), os.O_WRONLY|os.O_CREATE); err == nil {
		t.Fatal("read-only server allowed a write")
	}
	if _, err = os.Stat(filepath.Join(dir, "read-only-blocked")); !os.IsNotExist(err) {
		t.Fatal("read-only rejection created a file")
	}
	if _, err = readonly.Client.Stat(filepath.Join(dir, "target")); err != nil {
		t.Fatal("read-only metadata failed", err)
	}
	if err = readonly.Close(); err != nil {
		t.Fatal(err)
	}
}
