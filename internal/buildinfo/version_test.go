package buildinfo

import (
	"bytes"
	"errors"
	"io"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

func TestVersionLineAndClientIdentity(t *testing.T) {
	for _, name := range []string{"pi-sh", "pi-cp", "pi-sftp"} {
		var out bytes.Buffer
		if err := WriteVersion(&out, name); err != nil {
			t.Fatal(err)
		}
		line := out.String()
		if !strings.HasPrefix(line, name+"_") || !strings.Contains(line, " "+runtime.GOOS+"/"+runtime.GOARCH+" (unofficial)\n") || strings.Count(line, "\n") != 1 {
			t.Fatalf("invalid version line: %q", line)
		}
	}
}

func TestUnsafeBuildVersionIsNeverPrinted(t *testing.T) {
	for _, value := range []string{"", "(devel)", "\x1bPRIVATE", "/private/build-path", "private\nsecond-line", strings.Repeat("v", 129)} {
		if Token(value) != "devel" {
			t.Fatalf("unsafe version accepted")
		}
	}
	if Token("v0.1.0-rc.1+build_2") != "v0.1.0-rc.1+build_2" {
		t.Fatal("valid version rejected")
	}
}

type failedWriter struct{ short bool }

func (w failedWriter) Write(p []byte) (int, error) {
	if w.short {
		return len(p) - 1, nil
	}
	return 0, errors.New("PRIVATE-WRITER-SENTINEL")
}

func TestVersionWriteFailuresAreRedacted(t *testing.T) {
	for _, w := range []io.Writer{nil, failedWriter{}, failedWriter{short: true}} {
		err := WriteVersion(w, "pi-sh")
		if err == nil || strings.Contains(err.Error(), "PRIVATE-WRITER-SENTINEL") {
			t.Fatal("failed write lost or exposed")
		}
	}
}

func TestSelectedAlphaVersionAndSafeProvenance(t *testing.T) {
	for _, tc := range []struct {
		info *debug.BuildInfo
		want string
	}{
		{nil, "v0.1.0-alpha.1"},
		{&debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "ABCDEF0123456789abcdef0123456789abcdef01"}}}, "v0.1.0-alpha.1+gabcdef012345"},
		{&debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abcdef0123456789abcdef0123456789abcdef01"}, {Key: "vcs.modified", Value: "true"}}}, "v0.1.0-alpha.1+gabcdef012345.dirty"},
		{&debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.modified", Value: "true"}}}, "v0.1.0-alpha.1+dirty"},
		{&debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "PRIVATE-SENTINEL"}, {Key: "vcs.modified", Value: "PRIVATE-SENTINEL"}, {Key: "-ldflags", Value: "PRIVATE-SENTINEL"}}}, "v0.1.0-alpha.1"},
	} {
		if got := versionFromInfo(tc.info); got != tc.want {
			t.Fatalf("version %q, want %q", got, tc.want)
		}
	}
}
