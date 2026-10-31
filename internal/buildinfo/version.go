// Package buildinfo formats safe, offline version information for the CLI suite.
package buildinfo

import (
	"errors"
	"io"
	"runtime"
	"runtime/debug"
	"strings"
)

// Token limits build metadata to a single printable token, without paths or
// control characters. Invalid tokens are replaced with devel.
func Token(value string) string {
	if len(value) == 0 || len(value) > 128 {
		return "devel"
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '+' || c == '-') {
			return "devel"
		}
	}
	return value
}

// Release is the suite's selected version. All three clients share it.
const Release = "v0.1.0-alpha.1"

// Version identifies the selected release and, when available, the exact source
// build. Only a validated Git revision and the dirty bit enter build metadata.
func Version() string {
	info, _ := debug.ReadBuildInfo()
	return versionFromInfo(info)
}

func versionFromInfo(info *debug.BuildInfo) string {
	var revision string
	var dirty bool
	if info != nil {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				if len(setting.Value) >= 12 && len(setting.Value) <= 64 && strings.IndexFunc(setting.Value, func(c rune) bool {
					return !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F')
				}) == -1 {
					revision = "g" + strings.ToLower(setting.Value[:12])
				}
			case "vcs.modified":
				dirty = setting.Value == "true"
			}
		}
	}
	metadata := revision
	if dirty {
		if metadata != "" {
			metadata += "."
		}
		metadata += "dirty"
	}
	if metadata != "" {
		return Release + "+" + metadata
	}
	return Release
}

func Line(program string) string {
	return program + "_" + Version() + " " + Token(runtime.Version()) + " " + runtime.GOOS + "/" + runtime.GOARCH + " (unofficial)\n"
}

func WriteVersion(w io.Writer, program string) error {
	if w == nil {
		return errors.New("cannot write client version")
	}
	line := Line(program)
	n, err := io.WriteString(w, line)
	if err != nil || n != len(line) {
		return errors.New("cannot write client version")
	}
	return nil
}
