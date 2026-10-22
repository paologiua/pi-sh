//go:build darwin || linux

package sftpcli

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func globFixture(t *testing.T, root string) {
	t.Helper()
	for _, dir := range []string{"r/sub", "l"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"a1", "a2", "b*x", ".hid", "q?", "[x]", "x", "sp ace", `back\slash`, "A9", "sub/s1"} {
		if err := os.WriteFile(filepath.Join(root, "r", name), []byte("data "+name), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

type globEntry struct {
	Kind, Data string
	Mode       fs.FileMode
}

func globSnapshot(t *testing.T, root string) map[string]globEntry {
	t.Helper()
	out := map[string]globEntry{}
	err := filepath.Walk(root, func(name string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, name)
		e := globEntry{Mode: info.Mode()}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			e.Kind = "link"
			e.Data, err = os.Readlink(name)
		case info.IsDir():
			e.Kind = "dir"
		default:
			e.Kind = "file"
			var data []byte
			data, err = os.ReadFile(name)
			e.Data = string(data)
		}
		out[rel] = e
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestGlobCommandsAgainstLocalOpenSSH runs each command through our console
// (including its makeargv-compatible tokenizer) and through OpenSSH 10.2 sftp
// with the local sftp-server, then compares success, ls output and the
// resulting trees. Only temporary local directories are used.
func TestGlobCommandsAgainstLocalOpenSSH(t *testing.T) {
	executable, err := exec.LookPath("sftp")
	if err != nil {
		t.Skip("OpenSSH sftp unavailable")
	}
	client, server := preservationServer(t)
	for _, tc := range []struct {
		ours, reference string // reference defaults to ours
		listing         bool   // compare printed lines (glob-format ls output)
	}{
		{"ls r/a*", "ls -1 r/a*", true},
		{"ls 'r/a*'", "ls -1 'r/a*'", true},
		{`ls "r/b*x"`, `ls -1 "r/b*x"`, true},
		{`ls r/b\*x`, `ls -1 r/b\*x`, true},
		{"ls r/*", "ls -1 r/*", true},
		{"ls r/zz*", "ls -1 r/zz*", true},
		{"ls r/[x]", "ls -1 r/[x]", true},
		{"ls 'r/[x]'", "ls -1 'r/[x]'", true},
		{`ls r/\[x]`, `ls -1 r/\[x]`, true},
		{"ls r/.*", "ls -1 r/.*", true},
		{"ls r/s*", "ls -1 r/s*", true},
		{"ls r/*/s1", "ls -1 r/*/s1", true},
		{"ls r/sp?ace", "ls -1 r/sp?ace", true},
		{`ls r/back\\slash`, `ls -1 r/back\\slash`, true},
		{`ls 'r/back\slash'`, `ls -1 'r/back\slash'`, true},
		{`ls "r/back\\slash"`, `ls -1 "r/back\\slash"`, true},
		{"ls r/{a1,x}", "ls -1 r/{a1,x}", true},
		{"ls r/{zz,yy}", "ls -1 r/{zz,yy}", true},
		{"ls r/a{1,2}{", "ls -1 r/a{1,2}{", true},
		{"ls r/[!a]*", "ls -1 r/[!a]*", true},
		{"ls r/[]x]", "ls -1 r/[]x]", true},
		{"ls r/[[:upper:]]*", "ls -1 r/[[:upper:]]*", true},
		{"ls r/[[:bogus:]]1", "ls -1 r/[[:bogus:]]1", true},
		{"ls r/a[", "ls -1 r/a[", true},
		{"ls r/a1", "ls -1 r/a1", true},
		{"ls r/a1/", "ls -1 r/a1/", true},
		{"ls r/a#1", "ls -1 r/a#1", true},
		{"ls ~", "ls -1 ~", true},
		{"ls ~/r", "ls -1 ~/r", true},
		{"ls " + "r/s*/", "", false},
		{"get r/a* l/", "", false},
		{"get r/a* l/single", "", false},
		{"get r/zz* l/", "", false},
		{"get r/zz l/", "", false},
		{"get r/s* l/", "", false},
		{"get 'r/b*x' l/", "", false},
		{"get r/b?x l/one", "", false},
		{`get r/a\1 l/plain`, "", false},
		{"get ~/r/a1 l/", "", false},
		{"put r/a* l/", "", false},
		{"put r/a* l/single", "", false},
		{"put r/zz* l/", "", false},
		{"put 'r/q?' l/", "", false},
		{"put r/s* l/", "", false},
		{"put r/.* l/", "", false},
		{"rm r/a*", "", false},
		{"rm 'r/b*x'", "", false},
		{"rm r/zz*", "", false},
		{"rm r/s*", "", false},
		{"chmod 600 r/a*", "", false},
		{"chmod 600 r/zz*", "", false},
		{"mkdir 'r/new*'", "", false},
		{`mkdir r/new\*2`, "", false},
		{"rename r/a1 'r/n?w'", "", false},
		{"symlink 'x*' r/lnk", "", false},
		{"cd 'r/s*'", "", false},
	} {
		reference := tc.reference
		if reference == "" {
			reference = tc.ours
		}
		t.Run(tc.ours, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			ourRoot, refRoot := t.TempDir(), t.TempDir()
			globFixture(t, ourRoot)
			globFixture(t, refRoot)
			var out, errs bytes.Buffer
			console := Console{Client: client, RemoteDir: ourRoot, LocalDir: ourRoot, Batch: true, Input: strings.NewReader("@" + tc.ours + "\n"), Output: &out, Errors: &errs}
			ourErr := console.Run(ctx)
			cmd := exec.CommandContext(ctx, executable, "-D", server, "-b", "-")
			cmd.Dir = refRoot
			cmd.Stdin = strings.NewReader("@" + reference + "\n")
			var refOut, refErrs bytes.Buffer
			cmd.Stdout, cmd.Stderr = &refOut, &refErrs
			refErr := cmd.Run()
			if (ourErr == nil) != (refErr == nil) {
				t.Fatalf("status differs: ours %v (%s) reference %v (%s)", ourErr, errs.String(), refErr, refErrs.String())
			}
			if tc.listing && out.String() != refOut.String() {
				t.Fatalf("listing differs: got %q want %q", out.String(), refOut.String())
			}
			if got, want := globSnapshot(t, ourRoot), globSnapshot(t, refRoot); !reflect.DeepEqual(got, want) {
				for name := range want {
					if got[name] != want[name] {
						t.Errorf("%q: got %+v want %+v", name, got[name], want[name])
					}
				}
				for name := range got {
					if _, ok := want[name]; !ok {
						t.Errorf("%q: unexpected %+v", name, got[name])
					}
				}
				t.Fatalf("trees differ (ours: %s; reference: %s)", errs.String(), refErrs.String())
			}
		})
	}
}

// TestManyGlobMatchesAgainstLocalOpenSSH checks that, like OpenSSH 10.2, more
// matches than OpenBSD's GLOB_LIMIT stat bound (2048) are expanded.
func TestManyGlobMatchesAgainstLocalOpenSSH(t *testing.T) {
	executable, err := exec.LookPath("sftp")
	if err != nil {
		t.Skip("OpenSSH sftp unavailable")
	}
	client, server := preservationServer(t)
	root := t.TempDir()
	for i := 0; i < 2100; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("f%04d", i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		pattern string
		fail    bool
	}{{"f*", false}, {"f00*", false}, {"g*", true}} {
		var out bytes.Buffer
		console := Console{Client: client, RemoteDir: root, Output: &out}
		_, ourErr := console.Execute(context.Background(), []string{"ls", tc.pattern})
		cmd := exec.Command(executable, "-D", server, "-b", "-")
		cmd.Dir = root
		cmd.Stdin = strings.NewReader("@ls -1 " + tc.pattern + "\n")
		refOut, refErr := cmd.Output()
		if (ourErr != nil) != tc.fail || (refErr != nil) != tc.fail {
			t.Fatalf("%s: ours %v reference %v", tc.pattern, ourErr, refErr)
		}
		if !tc.fail && out.String() != string(refOut) {
			t.Fatalf("%s: listing differs", tc.pattern)
		}
	}
}
