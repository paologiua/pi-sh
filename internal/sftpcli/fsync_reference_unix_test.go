//go:build darwin || linux

package sftpcli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestOpenSSHFlushFailureStatusReference(t *testing.T) {
	client, err := exec.LookPath("sftp")
	if err != nil {
		t.Skip("local OpenSSH sftp unavailable")
	}
	helper, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"absent", "advertised", "permission", "eof", "drop", "malformed", "write-error"} {
		for _, global := range []bool{false, true} {
			t.Run(mode+map[bool]string{false: "/command", true: "/global"}[global], func(t *testing.T) {
				dir := t.TempDir()
				data := make([]byte, 256*32)
				for i := range data {
					data[i] = byte(i)
				}
				input, output := filepath.Join(dir, "input"), filepath.Join(dir, "output")
				if err := os.WriteFile(input, data, 0600); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				args := []string{"-D", strconv.Quote(helper) + " -test.run=^TestFsyncReferenceServer$"}
				flags := " -f"
				if global {
					args = append(args, "-fb-")
					flags = ""
				} else {
					args = append(args, "-b-")
				}
				cmd := exec.CommandContext(ctx, client, args...)
				cmd.Env = append(os.Environ(), "PI_SH_TEST_FSYNC_HELPER="+mode)
				cmd.Stdin = strings.NewReader(fmt.Sprintf("put%s %q /remote\nget%s /remote %q\nquit\n", flags, input, flags, output))
				diagnostic, err := cmd.CombinedOutput()
				if mode == "write-error" {
					if err == nil || !strings.Contains(strings.ToLower(string(diagnostic)), "unsupported") {
						t.Fatalf("reference failed upload did not attempt flush: %v %s", err, diagnostic)
					}
					return
				}
				if mode == "drop" || mode == "malformed" {
					if err == nil {
						t.Fatal("reference unexpectedly accepted lost reply")
					}
					return
				}
				if err != nil {
					t.Fatalf("reference %v: %s", err, diagnostic)
				}
				got, err := os.ReadFile(output)
				if err != nil || !bytes.Equal(got, data) {
					t.Fatal("reference binary roundtrip", err)
				}
				if mode == "advertised" && !strings.Contains(strings.ToLower(string(diagnostic)), "unsupported") {
					t.Fatalf("reference did not report fsync failure: %s", diagnostic)
				}
			})
		}
	}
}
