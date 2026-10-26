//go:build darwin || linux

package progress

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

func TestProgressTerminalQueriesPreserveDescriptorFlags(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	file, err := os.OpenFile(slave.Name(), os.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	raw, err := file.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	flags := func() int {
		var value int
		var getErr error
		if err := raw.Control(func(fd uintptr) { value, getErr = unix.FcntlInt(fd, unix.F_GETFL, 0) }); err != nil {
			t.Fatal(err)
		}
		if getErr != nil {
			t.Fatal(getErr)
		}
		return value
	}
	before := flags()
	if before&unix.O_NONBLOCK == 0 {
		t.Fatal("fixture must be nonblocking")
	}
	d := NewTerminal(file, false)
	if d == nil {
		t.Fatal("terminal not detected")
	}
	d.terminal()
	if got := flags(); got != before {
		t.Fatalf("terminal query changed flags: %x -> %x", before, got)
	}
}

func TestProgressPTYHelper(t *testing.T) {
	if os.Getenv("PI_SH_PROGRESS_HELPER") != "1" {
		return
	}
	size, _ := strconv.Atoi(os.Getenv("PI_SH_PROGRESS_SIZE"))
	m := NewTerminal(os.Stdout, os.Getenv("PI_SH_PROGRESS_QUIET") == "1").Start(os.Getenv("PI_SH_PROGRESS_NAME"), int64(size), 0)
	m.Add(size)
	m.Stop()
	os.Exit(0)
}

func TestProgressAgainstLocalSCP(t *testing.T) {
	scp, err := exec.LookPath("scp")
	if err != nil {
		t.Skip("local scp unavailable")
	}
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
	// Only this local executable/server is used; no SSH daemon, network or Pi.
	type fixture struct {
		name          string
		size, columns int
		flags         []string
		locale        string
		redirected    bool
	}
	cases := []fixture{
		{"sample.bin", 2048, 80, nil, "C", false},
		{"empty", 0, 80, nil, "C", false},
		{"ten-k", 10000, 120, nil, "C", false},
		{strings.Repeat("long", 30), 1, 60, nil, "C", false},
		{"escape\x1bname", 1, 80, nil, "C", false},
		{"narrow", 0, 20, nil, "C", false},
		{"default-width", 1, 0, nil, "C", false},
		{"bounded-width", 1, 600, nil, "C", false},
		{"quiet", 1, 80, []string{"-q"}, "C", false},
		{"quiet-verbose", 1, 80, []string{"-qv"}, "C", false},
		{"connection-quiet", 1, 80, []string{"-oLogLevel=QUIET"}, "C", false},
		{"redirected", 1, 80, nil, "C", true},
		{"世界-é.bin", 1, 80, nil, "C", false},
	}
	utf8Locale := "C.UTF-8"
	if runtime.GOOS == "darwin" {
		utf8Locale = "en_US.UTF-8"
	}
	cases = append(cases, fixture{"世界-e\u0301.bin", 1, 80, nil, utf8Locale, false})
	rate := regexp.MustCompile(`[ 0-9]{3}\.[0-9][KMGT]B/s`)
	duration := regexp.MustCompile(`([0-9]+:)?[0-9]{2}:[0-9]{2}`)
	normalize := func(s string) string {
		s = strings.ReplaceAll(s, "\r\n", "\n")
		s = rate.ReplaceAllString(s, "<rate>")
		return duration.ReplaceAllString(s, "<time>")
	}
	for i, tc := range cases {
		t.Run(fmt.Sprintf("case%d", i), func(t *testing.T) {
			dir := t.TempDir()
			source, target := filepath.Join(dir, tc.name), filepath.Join(dir, "target")
			if err := os.WriteFile(source, make([]byte, tc.size), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			args := append([]string{"-D", server}, tc.flags...)
			ref := exec.CommandContext(ctx, scp, append(args, source, "device:"+target)...)
			own := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProgressPTYHelper$")
			quiet := "0"
			for _, flag := range tc.flags {
				if strings.HasPrefix(flag, "-q") {
					quiet = "1"
				}
			}
			own.Env = append(os.Environ(), "PI_SH_PROGRESS_HELPER=1", "PI_SH_PROGRESS_NAME="+tc.name, "PI_SH_PROGRESS_SIZE="+strconv.Itoa(tc.size), "PI_SH_PROGRESS_QUIET="+quiet, "LC_ALL="+tc.locale)
			ref.Env = append(os.Environ(), "LC_ALL="+tc.locale)
			run := func(cmd *exec.Cmd) string {
				var stderr, stdout bytes.Buffer
				cmd.Stderr = &stderr
				if tc.redirected {
					cmd.Stdout = &stdout
					if err := cmd.Run(); err != nil {
						t.Fatalf("run failed: %v %s", err, stderr.String())
					}
					return stdout.String()
				}
				master, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: uint16(tc.columns)})
				if err != nil {
					t.Fatal(err)
				}
				defer master.Close()
				// Drain concurrently: a wide meter can exceed a PTY's small buffer.
				done := make(chan string, 1)
				go func() { b, _ := io.ReadAll(master); done <- string(b) }()
				if err := cmd.Wait(); err != nil {
					t.Fatalf("PTY command failed: %v %s", err, stderr.String())
				}
				select {
				case s := <-done:
					return s
				case <-ctx.Done():
					t.Fatal("PTY did not close")
					return ""
				}
			}
			rawWant, rawGot := run(ref), run(own)
			want, got := normalize(rawWant), normalize(rawGot)
			if got != want {
				t.Fatalf("scp progress mismatch (only rate/time normalized)\n got: %q\nwant: %q\nraw got: %q\nraw want: %q", got, want, rawGot, rawWant)
			}
		})
	}
}
