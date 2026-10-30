//go:build darwin || linux

package sftpcli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// All mutations belong to local temporary roots; -D never opens SSH/Connect.
func TestRecursiveConsoleAgainstLocalOpenSSH(t *testing.T) {
	executable, err := exec.LookPath("sftp")
	if err != nil {
		t.Skip("OpenSSH sftp unavailable")
	}
	client, server := preservationServer(t)
	cases := []string{
		"mget r/a* l/", "mput r/a* l/",
		"mget r/a* l/single", "mput r/a* l/single", "mget r/missing l/", "mput r/missing l/",
	}
	for _, op := range []string{"get", "put", "mget", "mput"} {
		for _, suffix := range []string{"", "/", "/."} {
			for _, dest := range []string{"l/new", "l", ""} {
				cases = append(cases, strings.TrimSpace(op+" -r r/sub"+suffix+" "+dest))
			}
		}
		for _, flags := range []string{"-R", "-rp", "-rafp"} {
			cases = append(cases, op+" "+flags+" r/sub l", op+" "+flags+" r/sub l/new")
		}
		cases = append(cases, op+" -r r/a* l", op+" -r r/s* l", op+" -r r/linkdir l", op+" -r 'r/b*x' l")
	}
	cases = append(cases, "conflict:get -r r/sub l", "conflict:put -r r/sub l", "conflict:mget -rp r/sub l", "conflict:mput -rp r/sub l", "global:get r/sub l", "global:put r/sub l", "global:mget r/sub l/new", "global:mput r/sub l/new", "get r/sub l/new", "put r/sub l/new", "get -r r/sub r/a1", "put -r r/sub r/a1", "reget -r r/sub l", "reput -r r/sub l")
	for _, line := range cases {
		t.Run(line, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			ours, ref := t.TempDir(), t.TempDir()
			fixture := func(root string) {
				globFixture(t, root)
				for _, dir := range []string{"r/sub/nested", "r/sub/empty", "l/sub/nested"} {
					if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
						t.Fatal(err)
					}
				}
				for name, data := range map[string]string{"r/sub/nested/bytes": "\x00\xff\x01complete", "r/sub/.hidden": "hidden", "l/sub/s1": "data", "l/sub/nested/bytes": "\x00\xff"} {
					if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0600); err != nil {
						t.Fatal(err)
					}
				}
				for name, target := range map[string]string{"r/sub/linkfile": "s1", "r/sub/linkdir": "nested", "r/sub/cycle": ".", "r/sub/dangling": "missing", "r/linkdir": "sub"} {
					if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Chmod(filepath.Join(root, "r/sub/empty"), 0500); err != nil {
					t.Fatal(err)
				}
				stamp := time.Unix(1600000013, 0)
				if err := filepath.Walk(filepath.Join(root, "r/sub"), func(name string, info os.FileInfo, err error) error {
					if err != nil {
						return err
					}
					if info.Mode()&os.ModeSymlink != 0 {
						return nil
					}
					return os.Chtimes(name, stamp, stamp)
				}); err != nil {
					t.Fatal(err)
				}
			}
			fixture(ours)
			fixture(ref)
			if strings.HasPrefix(line, "conflict:") {
				for _, root := range []string{ours, ref} {
					if err := os.WriteFile(filepath.Join(root, "r/sub/nested/blocked"), []byte("conflict"), 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(filepath.Join(root, "l/sub/nested/blocked"), 0700); err != nil {
						t.Fatal(err)
					}
				}
			}
			var errs bytes.Buffer
			c := Console{Client: client, RemoteDir: ours, LocalDir: ours, Batch: true, Input: strings.NewReader("@" + line + "\n"), Errors: &errs}
			command := strings.TrimPrefix(strings.TrimPrefix(line, "global:"), "conflict:")
			c.Input = strings.NewReader("@" + command + "\n")
			if strings.HasPrefix(line, "global:") {
				c.Recursive = true
				c.Input = strings.NewReader("@" + command + "\n")
			}
			ourErr := c.Run(ctx)
			// Capture before reference traversal can affect the filesystem observer.
			got := globSnapshot(t, ours)
			flags := []string{"-D", server, "-b", "-"}
			if c.Recursive {
				flags = append(flags, "-r")
			}
			cmd := exec.CommandContext(ctx, executable, flags...)
			cmd.Dir = ref
			cmd.Stdin = strings.NewReader("@" + command + "\n")
			output, refErr := cmd.CombinedOutput()
			if (ourErr == nil) != (refErr == nil) {
				t.Fatalf("status: ours %v (%s), reference %v (%s)", ourErr, &errs, refErr, output)
			}
			want := globSnapshot(t, ref)
			if !reflect.DeepEqual(got, want) {
				for name, entry := range want {
					if got[name] != entry {
						t.Errorf("%s: got %+v want %+v", name, got[name], entry)
					}
				}
				t.Fatalf("trees differ: ours %v reference %v", got, want)
			}
			if (strings.Contains(line, "-rp") || strings.Contains(line, "-rafp")) && !strings.HasPrefix(line, "re") {
				// Directory/file mtime only: reading snapshots changes atime.
				target := "l/sub"
				if strings.HasSuffix(line, "l/new") {
					target = "l/new"
				}
				for _, name := range []string{target, target + "/nested/bytes"} {
					a, ea := os.Stat(filepath.Join(ours, name))
					b, eb := os.Stat(filepath.Join(ref, name))
					if ea == nil && eb == nil && !a.ModTime().Equal(b.ModTime()) {
						t.Error(fmt.Sprintf("%s mtime differs: %v %v", name, a.ModTime(), b.ModTime()))
					}
				}
			}
		})
	}
}
