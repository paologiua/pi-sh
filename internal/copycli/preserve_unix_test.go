//go:build darwin || linux

package copycli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

func TestCopyPreservationAgainstLocalSCP(t *testing.T) {
	scp, err := exec.LookPath("scp")
	if err != nil {
		t.Skip("local OpenSSH scp unavailable")
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, server)
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
	defer func() {
		client.Close()
		in.Close()
		if err := cmd.Wait(); err != nil {
			t.Error(err)
		}
	}()
	for _, upload := range []bool{false, true} {
		for _, existing := range []bool{false, true} {
			for _, mode := range []os.FileMode{0640, 0755} {
				t.Run(fmt.Sprintf("upload%t/existing%t/%o", upload, existing, mode), func(t *testing.T) {
					dir := t.TempDir()
					source, refSource := filepath.Join(dir, "source"), filepath.Join(dir, "ref-source")
					target, reference := filepath.Join(dir, "target"), filepath.Join(dir, "reference")
					data := bytes.Repeat([]byte{0, 255, 'x'}, 3000)
					atime, mtime := time.Unix(1600000001, 0), time.Unix(1600000013, 0)
					for _, name := range []string{source, refSource} {
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
					if existing {
						for _, name := range []string{target, reference} {
							if err := os.WriteFile(name, []byte("old"), 0600); err != nil {
								t.Fatal(err)
							}
						}
					}
					a, b := source, "device:"+target
					refA, refB := refSource, "device:"+reference
					if !upload {
						a, b = "device:"+source, target
						refA, refB = "device:"+refSource, reference
					}
					plan, err := New([]string{a, b})
					if err != nil {
						t.Fatal(err)
					}
					var diagnostic bytes.Buffer
					plan.Preserve, plan.Errors = true, &diagnostic
					if err := plan.Run(ctx, client, nil); err != nil {
						t.Fatal(err)
					}
					ref := exec.CommandContext(ctx, scp, "-D", server, "-p", refA, refB)
					if output, err := ref.CombinedOutput(); err != nil {
						t.Fatalf("reference %v: %s", err, output)
					}
					for _, name := range []string{target, reference} {
						info, err := client.Stat(name)
						if err != nil {
							t.Fatal(err)
						}
						stat := info.Sys().(*sftp.FileStat)
						if info.Mode().Perm() != mode || stat.AccessTime().Unix() != atime.Unix() || info.ModTime().Unix() != mtime.Unix() {
							t.Fatal("copy metadata differs")
						}
						got, err := os.ReadFile(name)
						if err != nil || !bytes.Equal(got, data) {
							t.Fatal("copy data differs", err)
						}
					}
					if diagnostic.Len() != 0 {
						t.Fatalf("unexpected metadata diagnostic: %s", &diagnostic)
					}
				})
			}
		}
	}
}
