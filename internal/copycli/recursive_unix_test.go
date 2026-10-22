//go:build darwin || linux

package copycli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

func recursiveClient(t *testing.T, args ...string) (*sftp.Client, string) {
	t.Helper()
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
	cmd := exec.Command(server, args...)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
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

func recursiveFixture(t *testing.T, root string) {
	t.Helper()
	for _, name := range []string{root, root + "/sub", root + "/empty"} {
		if err := os.Mkdir(name, 0751); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range map[string][]byte{"file": bytes.Repeat([]byte{0, 255, 'x'}, 15000), "sub/世界 ' \n-leading:colon": []byte("nested"), "empty-file": {}} {
		if err := os.WriteFile(root+"/"+name, data, 0640); err != nil {
			t.Fatal(err)
		}
	}
	outside := filepath.Dir(root) + "/external-dir"
	if err := os.Mkdir(outside, 0751); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Dir(root) + "/external-file"
	for _, name := range []string{outsideFile, outside + "/child"} {
		if err := os.WriteFile(name, []byte("linked content"), 0640); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(name, time.Unix(1600000001, 0), time.Unix(1600000013, 0)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(outside, time.Unix(1600000001, 0), time.Unix(1600000013, 0)); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"file-link": "../external-file", "dir-link": "../external-dir"} {
		if err := os.Symlink(target, root+"/"+name); err != nil {
			t.Fatal(err)
		}
	}
	if err := filepath.Walk(root, func(name string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		return os.Chtimes(name, time.Unix(1600000001, 0), time.Unix(1600000013, 0))
	}); err != nil {
		t.Fatal(err)
	}
}

type recursiveEntry struct {
	Mode  os.FileMode
	Mtime int64
	Atime int64
	Data  string
}

func recursiveSnapshot(t *testing.T, client *sftp.Client, root string, preserve bool) map[string]recursiveEntry {
	t.Helper()
	entries := map[string]recursiveEntry{}
	var visit func(string) error
	visit = func(name string) error {
		info, err := os.Lstat(name)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		entry := recursiveEntry{Mode: info.Mode()}
		// Capture directory atime before enumerating it. filepath.Walk enumerates
		// a directory before its callback, so a fresh remote Stat there observes
		// the test's own read time instead of the copied timestamp.
		if preserve {
			entry.Mtime = info.ModTime().Unix()
			remote, err := client.Stat(name)
			if err != nil {
				return err
			}
			entry.Atime = remote.Sys().(*sftp.FileStat).AccessTime().Unix()
		}
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(name)
			if err != nil {
				return err
			}
			entry.Data = string(data)
		}
		if relative != "." {
			entries[relative] = entry
		}
		if info.IsDir() {
			children, err := os.ReadDir(name)
			if err != nil {
				return err
			}
			for _, child := range children {
				if err := visit(filepath.Join(name, child.Name())); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(root); err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestRecursiveCopyAgainstLocalSCP(t *testing.T) {
	scp, err := exec.LookPath("scp")
	if err != nil {
		t.Skip("local scp unavailable")
	}
	client, server := recursiveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, directoryMode := range []os.FileMode{0751, 0551} {
		for _, upload := range []bool{true, false} {
			for _, preserve := range []bool{false, true} {
				for _, existing := range []bool{false, true} {
					for _, suffix := range []string{"", "/", "/."} {
						t.Run(fmt.Sprintf("mode%o/upload%t/preserve%t/existing%t/suffix%q", directoryMode, upload, preserve, existing, suffix), func(t *testing.T) {
							root := t.TempDir()
							a, b := root+"/a", root+"/b"
							if err := os.Mkdir(a, 0700); err != nil {
								t.Fatal(err)
							}
							if err := os.Mkdir(b, 0700); err != nil {
								t.Fatal(err)
							}
							recursiveFixture(t, a+"/src")
							recursiveFixture(t, b+"/src")
							for _, source := range []string{a + "/src", b + "/src"} {
								if err := os.Chmod(source, directoryMode); err != nil {
									t.Fatal(err)
								}
							}
							// Nothing in the test reads this file. Endpoint scanners can read new
							// files and update atime; a changed control classifies such a failure.
							control := root + "/atime-control"
							if err := os.WriteFile(control, []byte("control"), 0600); err != nil {
								t.Fatal(err)
							}
							if err := os.Chtimes(control, time.Unix(1600000001, 0), time.Unix(1600000013, 0)); err != nil {
								t.Fatal(err)
							}
							t.Cleanup(func() {
								for _, parent := range []string{a, b} {
									filepath.Walk(parent, func(name string, info os.FileInfo, err error) error {
										if err == nil && info.IsDir() {
											os.Chmod(name, 0700)
										}
										return nil
									})
								}
							})
							dst, ref := a+"/target", b+"/target"
							if existing {
								for _, name := range []string{dst, ref} {
									if err := os.Mkdir(name, 0700); err != nil {
										t.Fatal(err)
									}
								}
							}
							src, referenceSource := a+"/src"+suffix, b+"/src"+suffix
							target, referenceTarget := "device:"+dst, "device:"+ref
							if !upload {
								src = "device:" + src
								referenceSource = "device:" + referenceSource
								target = dst
								referenceTarget = ref
							}
							plan, err := New([]string{src, target})
							if err != nil {
								t.Fatal(err)
							}
							var diagnostic bytes.Buffer
							plan.Recursive, plan.Preserve, plan.Errors = true, preserve, &diagnostic
							if err := plan.Run(ctx, client, nil); err != nil {
								t.Fatal(err)
							}
							args := []string{"-D", server, "-r"}
							if preserve {
								args = append(args, "-p")
							}
							args = append(args, referenceSource, referenceTarget)
							if output, err := exec.CommandContext(ctx, scp, args...).CombinedOutput(); err != nil {
								t.Fatalf("reference: %v %s", err, output)
							}
							got, want := recursiveSnapshot(t, client, dst, preserve), recursiveSnapshot(t, client, ref, preserve)
							if !reflect.DeepEqual(got, want) {
								for name, g := range got {
									w, ok := want[name]
									if !ok || !reflect.DeepEqual(g, w) {
										t.Errorf("entry %q differs: mode %o/%o atime %d/%d mtime %d/%d length %d/%d", name, g.Mode, w.Mode, g.Atime, w.Atime, g.Mtime, w.Mtime, len(g.Data), len(w.Data))
									}
								}
								controlAtime := int64(-1)
								if remote, err := client.Stat(control); err == nil {
									controlAtime = remote.Sys().(*sftp.FileStat).AccessTime().Unix()
								}
								t.Fatalf("recursive copy differs from OpenSSH; untouched control atime %d (1600000001 unless an external reader opened it)", controlAtime)
							}
							gi, err := os.Stat(dst)
							if err != nil {
								t.Fatal(err)
							}
							wi, err := os.Stat(ref)
							if err != nil {
								t.Fatal(err)
							}
							if gi.Mode() != wi.Mode() || (preserve && !existing && gi.ModTime().Unix() != wi.ModTime().Unix()) {
								t.Fatalf("root metadata differs: got %o/%d want %o/%d", gi.Mode(), gi.ModTime().Unix(), wi.Mode(), wi.ModTime().Unix())
							}
							if diagnostic.Len() != 0 {
								t.Fatalf("metadata diagnostics: %s", &diagnostic)
							}
						})
					}
				}
			}
		}
	}
}

func TestRecursiveCopyFailureAndCycle(t *testing.T) {
	client, _ := recursiveClient(t)
	for _, upload := range []bool{true, false} {
		for _, failure := range []string{"cycle", "dangling", "conflict", "cancel", "nonrecursive"} {
			t.Run(fmt.Sprintf("upload%t/%s", upload, failure), func(t *testing.T) {
				root := t.TempDir()
				src, dst := root+"/src", root+"/target"
				if err := os.Mkdir(src, 0700); err != nil {
					t.Fatal(err)
				}
				switch failure {
				case "cycle":
					if err := os.Symlink(".", src+"/loop"); err != nil {
						t.Fatal(err)
					}
				case "dangling":
					if err := os.Symlink("missing", src+"/link"); err != nil {
						t.Fatal(err)
					}
				case "conflict":
					if err := os.WriteFile(dst, []byte("keep"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				a, b := src, "device:"+dst
				if !upload {
					a, b = "device:"+src, dst
				}
				plan, err := New([]string{a, b})
				if err != nil {
					t.Fatal(err)
				}
				plan.Recursive = failure != "nonrecursive"
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if failure == "cancel" {
					cancel()
				}
				err = plan.Run(ctx, client, nil)
				if err == nil {
					t.Fatal("failed recursive copy returned success")
				}
				if failure == "cycle" {
					if _, statErr := os.Stat(dst + "/loop"); !os.IsNotExist(statErr) {
						t.Fatal("cycle created another destination level")
					}
				}
				if failure == "conflict" {
					data, err := os.ReadFile(dst)
					if err != nil || string(data) != "keep" {
						t.Fatal("conflicting destination changed", err)
					}
				}
				if failure == "cancel" {
					if _, err := os.Stat(dst); !os.IsNotExist(err) {
						t.Fatal("cancellation created destination")
					}
				}
			})
		}
	}
}

func TestRecursiveCopyPreservesSymlinkParentTraversal(t *testing.T) {
	client, _ := recursiveClient(t)
	for _, upload := range []bool{true, false} {
		t.Run(fmt.Sprint(upload), func(t *testing.T) {
			root := t.TempDir()
			for _, name := range []string{"branch", "branch/deep", "branch/src", "src"} {
				if err := os.Mkdir(root+"/"+name, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink("branch/deep", root+"/alias"); err != nil {
				t.Fatal(err)
			}
			for name, data := range map[string]string{"branch/src/file": "correct", "src/file": "wrong"} {
				if err := os.WriteFile(root+"/"+name, []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			source, target := root+"/alias/../src", root+"/target"
			a, b := source, "device:"+target
			if !upload {
				a, b = "device:"+source, target
			}
			plan, err := New([]string{a, b})
			if err != nil {
				t.Fatal(err)
			}
			plan.Recursive = true
			if err := plan.Run(context.Background(), client, nil); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(target + "/file")
			if err != nil || string(got) != "correct" {
				t.Fatal("path cleaned before filesystem resolution", err)
			}
		})
	}
}

func TestRecursiveUploadPrivateCreation(t *testing.T) {
	client, _ := recursiveClient(t, "-u", "077")
	root := t.TempDir()
	source, target := root+"/src", root+"/target"
	recursiveFixture(t, source)
	plan, err := New([]string{source, "device:" + target})
	if err != nil {
		t.Fatal(err)
	}
	plan.Recursive = true
	mask := os.FileMode(0022)
	if err := plan.Run(context.Background(), client, &mask); err != nil {
		t.Fatal(err)
	}
	for name, mode := range map[string]os.FileMode{target: 0751, target + "/sub": 0751, target + "/file": 0640} {
		info, err := os.Stat(name)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("private-server final mode differs for %q: %v", name, err)
		}
	}
}
