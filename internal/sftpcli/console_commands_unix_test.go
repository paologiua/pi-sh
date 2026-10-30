//go:build darwin || linux

package sftpcli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func TestLnAgainstLocalOpenSSH(t *testing.T) {
	client, server := preservationServer(t)
	for _, symbolic := range []bool{false, true} {
		ours, ref := t.TempDir(), t.TempDir()
		for _, root := range []string{ours, ref} {
			if err := os.WriteFile(filepath.Join(root, "source"), []byte("link data"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		flag := ""
		if symbolic {
			flag = "-s "
		}
		line := "ln " + flag + "source linked"
		var out bytes.Buffer
		c := Console{Client: client, RemoteDir: ours, LocalDir: ours, Output: &out}
		args, _ := Split(line)
		if _, err := c.Execute(context.Background(), args); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("sftp", "-D", server, "-b", "-")
		cmd.Dir = ref
		cmd.Stdin = strings.NewReader("@" + line + "\n")
		want, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		if out.String() != string(want) {
			t.Fatalf("output %q want %q", out.String(), want)
		}
		if symbolic {
			got, err := os.Readlink(filepath.Join(ours, "linked"))
			if err != nil || got != "source" {
				t.Fatal(got, err)
			}
		} else {
			a, err := os.Stat(filepath.Join(ours, "source"))
			if err != nil {
				t.Fatal(err)
			}
			b, err := os.Stat(filepath.Join(ours, "linked"))
			if err != nil || !os.SameFile(a, b) {
				t.Fatal("hardlink identity", err)
			}
		}
	}
}

func TestChownLocalServer(t *testing.T) {
	client, _ := preservationServer(t)
	root := t.TempDir()
	source := filepath.Join(root, "file")
	if err := os.WriteFile(source, []byte("contents"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("file", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	c := Console{Client: client, RemoteDir: root, Batch: true}
	for _, args := range [][]string{{"chown", strconv.Itoa(os.Getuid()), "f*"}, {"chown", "-h", strconv.Itoa(os.Getuid()), "link"}} {
		if _, err := c.Execute(context.Background(), args); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	if int(info.Sys().(*syscall.Stat_t).Uid) != os.Getuid() {
		t.Fatal("UID differs")
	}
	link, err := os.Readlink(filepath.Join(root, "link"))
	if err != nil || link != "file" {
		t.Fatal(link, err)
	}
	if _, err := c.Execute(context.Background(), []string{"chown", "4294967296", "file"}); err == nil {
		t.Fatal("overflow UID accepted")
	}
}

func TestChgrpLocalServer(t *testing.T) {
	client, _ := preservationServer(t)
	root := t.TempDir()
	source := filepath.Join(root, "file")
	if err := os.WriteFile(source, []byte("contents"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("file", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	c := Console{Client: client, RemoteDir: root, Batch: true}
	for _, args := range [][]string{{"chgrp", strconv.Itoa(os.Getgid()), "f*"}, {"chgrp", "-h", strconv.Itoa(os.Getgid()), "link"}} {
		if _, err := c.Execute(context.Background(), args); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	if int(stat.Gid) != os.Getgid() || int(stat.Uid) != os.Getuid() {
		t.Fatal("ownership differs")
	}
}

func TestChmodSpecialBitsAndLink(t *testing.T) {
	client, server := preservationServer(t)
	roots := []string{t.TempDir(), t.TempDir()}
	for _, root := range roots {
		if err := os.Mkdir(filepath.Join(root, "dir"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("dir", filepath.Join(root, "link")); err != nil {
			t.Fatal(err)
		}
	}
	c := Console{Client: client, RemoteDir: roots[0], Batch: true}
	for _, line := range []string{"chmod 1777 dir", "chmod -h 700 link"} {
		args, _ := Split(line)
		_, ourErr := c.Execute(context.Background(), args)
		cmd := exec.Command("sftp", "-D", server, "-b", "-")
		cmd.Dir = roots[1]
		cmd.Stdin = strings.NewReader("@" + line + "\n")
		out, refErr := cmd.CombinedOutput()
		if (ourErr == nil) != (refErr == nil) {
			t.Fatalf("%s: ours %v reference %v %s", line, ourErr, refErr, out)
		}
	}
	a, _ := os.Stat(filepath.Join(roots[0], "dir"))
	b, _ := os.Stat(filepath.Join(roots[1], "dir"))
	if a.Mode() != b.Mode() {
		t.Fatal(a.Mode(), b.Mode())
	}
}

func TestDfLocalServer(t *testing.T) {
	client, _ := preservationServer(t)
	var out bytes.Buffer
	c := Console{Client: client, RemoteDir: t.TempDir(), Output: &out}
	for _, args := range [][]string{{"df"}, {"df", "-h"}, {"df", "-i"}, {"df", "-hi"}} {
		out.Reset()
		if _, err := c.Execute(context.Background(), args); err != nil {
			t.Fatal(err)
		}
		if strings.Count(out.String(), "\n") != 2 || !strings.Contains(out.String(), "%Capacity") {
			t.Fatalf("bad df output %q", out.String())
		}
	}
}

func TestRemoteCopyLocalServer(t *testing.T) {
	client, server := preservationServer(t, "-u", "077")
	mask := os.FileMode(022)
	for _, existing := range []bool{false, true} {
		ours, ref := t.TempDir(), t.TempDir()
		data := bytes.Repeat([]byte{0, 255, 'x'}, 12000)
		for _, root := range []string{ours, ref} {
			if err := os.WriteFile(filepath.Join(root, "source"), data, 0640); err != nil {
				t.Fatal(err)
			}
			if existing {
				if err := os.WriteFile(filepath.Join(root, "target"), []byte("old"), 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
		c := Console{Client: client, RemoteDir: ours, RemoteUmask: &mask, Batch: true}
		if _, err := c.Execute(context.Background(), []string{"copy", "source", "target"}); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("sftp", "-D", server, "-b", "-")
		cmd.Dir = ref
		cmd.Stdin = strings.NewReader("@copy source target\n")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
		got, err := os.ReadFile(filepath.Join(ours, "target"))
		if err != nil || !bytes.Equal(got, data) {
			t.Fatal("copy bytes differ", err)
		}
		a, _ := os.Stat(filepath.Join(ours, "target"))
		b, _ := os.Stat(filepath.Join(ref, "target"))
		if a.Mode() != b.Mode() {
			t.Fatal(a.Mode(), b.Mode())
		}
	}
}

func TestCpAliasLocalServer(t *testing.T) {
	client, _ := preservationServer(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "source"), []byte("alias"), 0600); err != nil {
		t.Fatal(err)
	}
	c := Console{Client: client, RemoteDir: root, Batch: true}
	if _, err := c.Execute(context.Background(), []string{"cp", "source", "target"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "target"))
	if err != nil || string(data) != "alias" {
		t.Fatal(string(data), err)
	}
}

func TestLocalShellAgainstOpenSSH(t *testing.T) {
	client, server := preservationServer(t)
	t.Setenv("SHELL", "/bin/sh")
	script := "@!printf '%s\\n' 'literal * with space'\n@!exit 7\n@version\n"
	var out, errs bytes.Buffer
	c := Console{Client: client, RemoteDir: t.TempDir(), LocalDir: t.TempDir(), Batch: true, Input: strings.NewReader(script), Output: &out, Errors: &errs}
	if err := c.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sftp", "-D", server, "-b", "-")
	cmd.Stdin = strings.NewReader(script)
	var want, wantErr bytes.Buffer
	cmd.Stdout = &want
	cmd.Stderr = &wantErr
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if out.String() != want.String() || errs.String() != wantErr.String() {
		t.Fatalf("got %q / %q want %q / %q", out.String(), errs.String(), want.String(), wantErr.String())
	}
}

func TestLlsAgainstOpenSSH(t *testing.T) {
	client, server := preservationServer(t)
	root := t.TempDir()
	for _, name := range []string{"a", "file with space", ".hidden"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	script := "@lls -1a\n@lls -1 'file with space'\n"
	t.Setenv("SHELL", "/bin/sh")
	var out, errs bytes.Buffer
	c := Console{Client: client, RemoteDir: root, LocalDir: root, Batch: true, Input: strings.NewReader(script), Output: &out, Errors: &errs}
	if err := c.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sftp", "-D", server, "-b", "-")
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(script)
	want, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != string(want) || errs.Len() != 0 {
		t.Fatalf("got %q / %q want %q", out.String(), errs.String(), want)
	}
}
