//go:build darwin || linux

package copycli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func globTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(root, func(name string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, name)
		entry := info.Mode().String()
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(name)
			if err != nil {
				return err
			}
			entry += " " + string(data)
		}
		out[rel] = entry
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestRemoteGlobAgainstLocalSCP compares pi-cp downloads of remote glob and
// tilde operands with OpenSSH 10.2 scp (SFTP mode) using the local sftp-server.
// The "~/" cases read this package's own source files, because the server's
// starting directory is the test's working directory; nothing is written there.
func TestRemoteGlobAgainstLocalSCP(t *testing.T) {
	scp, err := exec.LookPath("scp")
	if err != nil {
		t.Skip("local scp unavailable")
	}
	client, server := recursiveClient(t)
	for _, tc := range []struct {
		source, target string
		recursive      bool
	}{
		{"r/a*", "l/", false},
		{"r/a*", "l/newdir", false},
		{"r/a*", "l/file", false},
		{"r/zz*", "l/", false},
		{"r/zz", "l/", false},
		{`r/b\*x`, "l/", false},
		{"r/b*x", "l/one", false},
		{"r/s*", "l/", false},
		{"r/[x]", "l/", false},
		{`r/\[x]`, "l/", false},
		{"r/a[", "l/", false},
		{"r/.*", "l/", false},
		{"r/*/s1", "l/", false},
		{"r/s*", "l/rec", true},
		{"r/s*", "l/", true},
		{"r/sub", "l/", false},
		{"~/glob.go", "l/", false},
		{"~/glob*.go", "l/", false},
		{"~//copy.go", "l/single", false},
		{"~", "l/", false},
	} {
		t.Run(tc.source+"->"+tc.target, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			roots := [2]string{t.TempDir(), t.TempDir()}
			for _, root := range roots {
				for _, dir := range []string{"r/sub", "l"} {
					if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
						t.Fatal(err)
					}
				}
				for _, name := range []string{"a1", "a2", "b*x", ".hid", "[x]", "x", "sp ace", "sub/s1"} {
					if err := os.WriteFile(filepath.Join(root, "r", name), []byte("data "+name), 0644); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(filepath.Join(root, "l", "file"), []byte("old"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			remote := func(root string) string {
				if strings.HasPrefix(tc.source, "~") {
					return tc.source
				}
				return root + "/" + tc.source
			}
			plan, err := New([]string{"device:" + remote(roots[0]), roots[0] + "/" + tc.target})
			if err != nil {
				t.Fatal(err)
			}
			var diagnostic bytes.Buffer
			plan.Recursive, plan.Errors = tc.recursive, &diagnostic
			ourErr := plan.Run(ctx, client, nil)
			args := []string{"-D", server}
			if tc.recursive {
				args = append(args, "-r")
			}
			args = append(args, "x:"+remote(roots[1]), roots[1]+"/"+tc.target)
			output, refErr := exec.CommandContext(ctx, scp, args...).CombinedOutput()
			if (ourErr == nil) != (refErr == nil) {
				t.Fatalf("status differs: ours %v %s; reference %v %s", ourErr, &diagnostic, refErr, output)
			}
			if got, want := globTree(t, roots[0]+"/l"), globTree(t, roots[1]+"/l"); !reflect.DeepEqual(got, want) {
				t.Fatalf("destination differs:\n got %q\nwant %q\n(ours %v %s; reference %s)", got, want, ourErr, &diagnostic, output)
			}
		})
	}
}

func TestRemotePathTilde(t *testing.T) {
	for in, want := range map[string]string{"": ".", "~": ".", "~/": ".", "~///": ".", "~/x": "x", "~//x/y": "x/y", "/abs": "/abs", "x~": "x~", "./~": "./~"} {
		if got, err := remotePath(in); err != nil || got != want {
			t.Errorf("remotePath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, args := range [][]string{{"pi:~root/x", "local"}, {"local", "pi:~pi"}} {
		if _, err := New(args); err == nil || !strings.Contains(err.Error(), "expand-path") {
			t.Errorf("%q: ~user accepted before login: %v", args, err)
		}
	}
	if _, err := New([]string{"pi:~/x", "pi:~", "local"}); err != nil {
		t.Error(err)
	}
}
