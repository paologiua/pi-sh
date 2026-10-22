package main

import (
	"errors"
	"io"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"

	"pi-sh/internal/terminal"
)

// These queries describe this client's implementation, independently of account,
// remote executable presence and native verification. SSH algorithms are absent.
var capabilityQueries = map[string][]string{
	"pi-sh-auth":      {"browser-companion", "direct-login"},
	"pi-sh-command":   {"experimental-command-no-tty", "experimental-command-tty", "interactive-shell", "legacy-command-pty"},
	"pi-sh-config":    {"EscapeChar", "Host", "HostName", "Include", "LogLevel", "RequestTTY", "StdinNull", "SyslogFacility"},
	"pi-sh-escape":    {"disconnect", "help", "literal-prefix"},
	"pi-sh-transport": {"connect-webrtc"},
}

func queryCapabilities(query string) ([]string, error) {
	if query == "help" {
		names := []string{"help"}
		for name := range capabilityQueries {
			names = append(names, name)
		}
		sort.Strings(names)
		return names, nil
	}
	values, ok := capabilityQueries[query]
	if !ok {
		return nil, errors.New("unsupported capability query; use -Q help")
	}
	values = append([]string(nil), values...)
	if query == "pi-sh-escape" && terminal.CanSuspend() {
		values = append(values, "suspend")
	}
	sort.Strings(values)
	return values, nil
}

// Build metadata is read from the executable only. Never print build settings,
// VCS paths, environment variables or other configuration alongside the version.
func clientVersion() string {
	version := "devel"
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = versionToken(info.Main.Version)
	}
	return "pi-sh_" + version + " " + versionToken(runtime.Version()) + " " + runtime.GOOS + "/" + runtime.GOARCH + " (unofficial)\n"
}

func versionToken(value string) string {
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

func writeInspection(o options, stdout, stderr io.Writer) error {
	writer := stdout
	var text string
	switch o.inspection {
	case "version":
		writer = stderr
		text = clientVersion()
	case "query":
		values, err := queryCapabilities(o.query)
		if err != nil {
			return err
		}
		text = strings.Join(values, "\n") + "\n"
	default:
		return errors.New("unsupported local information request")
	}
	if writer == nil {
		return errors.New("cannot write local information")
	}
	n, err := io.WriteString(writer, text)
	if err != nil || n != len(text) {
		return errors.New("cannot write local information")
	}
	return nil
}
