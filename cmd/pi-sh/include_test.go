package main

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
)

func TestIncludeOpenSSHDifferential(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("local OpenSSH unavailable")
	}
	for _, name := range []string{"lexical", "argument-order", "caller-context", "file-context", "inactive", "nested", "quoted", "hidden", "negated-class", "no-match", "tokens", "cli-hostname-token", "environment", "environment-literal-token", "tilde", "relative-home-base", "nested-relative-home-base", "home-token"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			quote := func(s string) string { return fmt.Sprintf("%q", filepath.ToSlash(s)) }
			write := func(name, text string) string {
				file := filepath.Join(dir, name)
				if err := os.WriteFile(file, []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
				return file
			}
			a := write("10.conf", "EscapeChar !\n")
			b := write("20.conf", "EscapeChar none\nStdinNull yes\n")
			var config string
			var flags []string
			switch name {
			case "lexical":
				config = "Include " + quote(filepath.Join(dir, "*.conf")) + "\n"
			case "argument-order":
				config = "Include " + quote(b) + " " + quote(a) + "\n"
			case "caller-context":
				write("10.conf", "Host other\nStdinNull yes\n")
				config = "Host alias\nInclude " + quote(a) + "\nRequestTTY force\n"
			case "file-context":
				write("10.conf", "Host other\nStdinNull yes\n")
				config = "Host alias\nInclude " + quote(a) + " " + quote(b) + "\n"
			case "inactive":
				write("10.conf", "Host *\nStdinNull yes\n")
				config = "Host other\nInclude " + quote(a) + "\nHost *\nEscapeChar none\n"
			case "nested":
				leaf := write("leaf", "Host alias\nRequestTTY force\n")
				write("10.conf", "Include "+quote(leaf)+"\nEscapeChar !\n")
				config = "Include " + quote(a) + "\nStdinNull yes\n"
			case "quoted":
				config = "Include " + quote(write("file with spaces", "EscapeChar !\n")) + "\n"
			case "hidden":
				write(".hidden.conf", "EscapeChar #\n")
				config = "Include " + quote(filepath.Join(dir, "*.conf")) + " " + quote(filepath.Join(dir, "[.]hidden.conf")) + "\n"
			case "negated-class":
				config = "Include " + quote(filepath.Join(dir, "[!2]*.conf")) + "\n"
			case "no-match":
				config = "Include " + quote(filepath.Join(dir, "missing")) + " " + quote(filepath.Join(dir, "missing-*")) + "\n"
			case "tokens", "cli-hostname-token":
				write("device-alias", "EscapeChar !\n")
				write("cli-device", "EscapeChar none\n")
				write("alias-%", "StdinNull yes\n")
				config = "HostName device-%h\nInclude " + quote(filepath.Join(dir, "%h")) + " " + quote(filepath.Join(dir, "%n-%%")) + "\n"
				if name == "cli-hostname-token" {
					flags = []string{"-oHostName=cli-device"}
				}
			case "environment":
				t.Setenv("PI_SH_TEST_INCLUDE_BASE", filepath.ToSlash(dir))
				config = "Include ${PI_SH_TEST_INCLUDE_BASE}/10.conf\n"
			case "environment-literal-token":
				literal := filepath.Join(dir, "literal-%r")
				if err := os.Mkdir(literal, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(literal, "part"), []byte("EscapeChar !\n"), 0600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("PI_SH_TEST_INCLUDE_LITERAL", filepath.ToSlash(literal))
				config = "Include ${PI_SH_TEST_INCLUDE_LITERAL}/part\n"
			case "tilde", "relative-home-base", "nested-relative-home-base", "home-token":
				home, err := os.UserHomeDir()
				if err != nil {
					t.Skip("local home unavailable")
				}
				base := home
				prefix := "~/"
				if name == "relative-home-base" || name == "nested-relative-home-base" {
					base, prefix = filepath.Join(home, ".ssh"), ""
					if info, err := os.Stat(base); err != nil || !info.IsDir() {
						t.Skip("relative Include reference needs an existing ~/.ssh; never created by test")
					}
				}
				if name == "home-token" {
					prefix = "%d/"
				}
				rel, err := filepath.Rel(base, a)
				if err != nil {
					t.Skip("temporary file is on another volume")
				}
				config = "Include " + quote(prefix+filepath.ToSlash(rel)) + "\n"
				if name == "nested-relative-home-base" {
					child := write("nested", config)
					config = "Include " + quote(child) + "\n"
				}
			}
			root := write("root", config)
			args := append([]string{"-G", "-F", root}, flags...)
			args = append(args, "alias")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			out, err := exec.CommandContext(ctx, ssh, args...).Output()
			if err != nil {
				t.Fatal("OpenSSH reference", err)
			}
			o, err := parseOptions(args)
			if err != nil {
				t.Fatal(err)
			}
			var got bytes.Buffer
			if err := o.writeConfig(&got); err != nil {
				t.Fatal(err)
			}
			want := map[string]string{}
			for _, line := range strings.Split(string(out), "\n") {
				key, value, _ := strings.Cut(line, " ")
				want[key] = value
			}
			for _, line := range strings.Split(strings.TrimSpace(got.String()), "\n") {
				key, value, _ := strings.Cut(line, " ")
				if value != want[key] {
					t.Errorf("%s: pi-sh %q, OpenSSH %q", key, value, want[key])
				}
			}
		})
	}
}
