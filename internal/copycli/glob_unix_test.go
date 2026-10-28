//go:build darwin || linux

package copycli

import (
	"bytes"
	"context"
	"errors"
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
	for _, args := range [][]string{{"pi:~root/x", "local"}, {"local", "pi:~pi"}, {"scp://pi/~root/x", "local"}} {
		if _, err := New(args); err == nil || !strings.Contains(err.Error(), "expand-path") {
			t.Errorf("%q: ~user accepted before login: %v", args, err)
		}
	}
	if _, err := New([]string{"pi:~/x", "pi:~", "local"}); err != nil {
		t.Error(err)
	}
}

// With several operands scp reports each failing one and still copies the
// rest, exiting 1; pi-cp stopped at the first failure (seen on the device on
// 2026-10-28). A missing local upload source is fatal for scp (exit 255) after
// the earlier operands. Against OpenSSH 10.2 scp (SFTP mode).
func TestFailingOperandDoesNotStopTheRestLikeSCP(t *testing.T) {
	scp, err := exec.LookPath("scp")
	if err != nil {
		t.Skip("local scp unavailable")
	}
	client, server := recursiveClient(t)
	for _, tc := range []struct {
		name    string
		sources []string
		upload  bool
		fatal   bool
	}{
		{"download", []string{"r/a1", "r/missing", "r/zz*", "r/a2"}, false, false},
		{"upload", []string{"s/one", "s/missing", "s/two"}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			roots := [2]string{t.TempDir(), t.TempDir()}
			for _, root := range roots {
				for _, dir := range []string{"r", "s", "l"} {
					if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
						t.Fatal(err)
					}
				}
				for _, name := range []string{"r/a1", "r/a2", "s/one", "s/two"} {
					if err := os.WriteFile(filepath.Join(root, name), []byte("data "+name), 0644); err != nil {
						t.Fatal(err)
					}
				}
			}
			operands := func(root string, remote string) []string {
				var args []string
				for _, s := range tc.sources {
					if tc.upload {
						args = append(args, root+"/"+s)
					} else {
						args = append(args, remote+root+"/"+s)
					}
				}
				if tc.upload {
					return append(args, remote+root+"/l")
				}
				return append(args, root+"/l")
			}
			plan, err := New(operands(roots[0], "device:"))
			if err != nil {
				t.Fatal(err)
			}
			var diagnostic bytes.Buffer
			plan.Errors = &diagnostic
			ourErr := plan.Run(ctx, client, nil)
			output, refErr := exec.CommandContext(ctx, scp, append([]string{"-D", server}, operands(roots[1], "x:")...)...).CombinedOutput()
			if ourErr == nil || refErr == nil {
				t.Fatalf("status: ours %v; reference %v %s", ourErr, refErr, output)
			}
			// scp exits 255 for a missing local source, 1 for a failed download.
			if errors.Is(ourErr, ErrLocalSource) != tc.fatal || (refErr.(*exec.ExitError).ExitCode() == 255) != tc.fatal {
				t.Fatalf("fatal status: ours %v; reference %v", ourErr, refErr)
			}
			if got, want := strings.Count(diagnostic.String(), "\n")+1, strings.Count(string(output), "\n"); got != want {
				t.Fatalf("%d errors reported, scp %d:\n%s%v\n---\n%s", got, want, &diagnostic, ourErr, output)
			}
			if got, want := globTree(t, roots[0]+"/l"), globTree(t, roots[1]+"/l"); !reflect.DeepEqual(got, want) {
				t.Fatalf("destination differs:\n got %q\nwant %q", got, want)
			}
		})
	}
}
