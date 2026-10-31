package main

import (
	"errors"
	"io"
	"sort"
	"strings"

	"pi-sh/internal/buildinfo"
	"pi-sh/internal/terminal"
)

// These queries describe this client's implementation, independently of account,
// remote executable presence and native verification. SSH algorithms are absent.
var capabilityQueries = map[string][]string{
	"pi-sh-auth":      {"browser-companion", "direct-login"},
	"pi-sh-command":   {"experimental-command-no-tty", "experimental-command-tty", "interactive-shell", "legacy-command-pty"},
	"pi-sh-config":    {"EscapeChar", "Host", "HostName", "Include", "LogLevel", "Match", "RequestTTY", "StdinNull", "SyslogFacility", "Tag"},
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

// Version information uses the selected project version and whitelisted build
// provenance. VCS paths, environment values and other settings are never printed.
// The release token is shared with Match version.
func clientVersion() string {
	return buildinfo.Line("pi-sh")
}

func writeInspection(o options, stdout, stderr io.Writer) error {
	writer := stdout
	var text string
	switch o.inspection {
	case "help":
		text = clientHelp
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
