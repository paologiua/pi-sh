package fwdbroker

import (
	"errors"
	"net/netip"
	"strconv"
	"strings"

	"pi-sh/internal/forward"
)

// Connector is a remote program, already present on the device, that turns
// stdio into one outbound connection. Each candidate's required behaviour
// (binary transparency, stdin EOF => socket write shutdown, remote EOF =>
// stdout EOF, nonzero exit on connect failure) must be confirmed on the
// device with scripts/probe-forwarding-loopback.sh before relying on it.
type Connector struct {
	Name string
	// Ready (TCP) and UnixReady are stderr substrings printed once the
	// connection is open, or "" when the program prints none (then a nonzero
	// exit without output is classified as an open failure).
	Ready, UnixReady string
	argv             func(forward.Target) ([]string, error)
}

// ReadyText is the readiness marker for a target, or "".
func (c Connector) ReadyText(t forward.Target) string {
	if t.Path != "" {
		return c.UnixReady
	}
	return c.Ready
}

// Argv returns the program arguments for a target, validating it first.
func (c Connector) Argv(t forward.Target) ([]string, error) { return c.argv(t) }

var (
	// OpenBSD netcat (Debian netcat-openbsd): -N shuts the socket down after
	// stdin EOF; -v reports "Connection to ... succeeded!".
	openBSDNetcat = Connector{Name: "openbsd-nc", Ready: " succeeded!", argv: func(t forward.Target) ([]string, error) {
		if t.Path != "" {
			if err := checkPath(t.Path); err != nil {
				return nil, err
			}
			return []string{"nc", "-N", "-U", "--", t.Path}, nil
		}
		if err := checkHostPort(t); err != nil {
			return nil, err
		}
		return []string{"nc", "-N", "-v", "--", t.Host, strconv.Itoa(t.Port)}, nil
	}}
	// Nmap ncat half-closes by default (it documents --no-shutdown to disable it).
	nmapNcat = Connector{Name: "ncat", Ready: "Ncat: Connected to", argv: func(t forward.Target) ([]string, error) {
		if t.Path != "" {
			if err := checkPath(t.Path); err != nil {
				return nil, err
			}
			return []string{"ncat", "-U", "--", t.Path}, nil
		}
		if err := checkHostPort(t); err != nil {
			return nil, err
		}
		return []string{"ncat", "-v", "--", t.Host, strconv.Itoa(t.Port)}, nil
	}}
	// socat shuts down the write side on EOF but then waits only -t seconds
	// for the other direction; a one-day bound is used and documented.
	socat = Connector{Name: "socat", Ready: "starting data transfer loop", UnixReady: "starting data transfer loop", argv: func(t forward.Target) ([]string, error) {
		if t.Path != "" {
			if err := checkPath(t.Path); err != nil || strings.ContainsAny(t.Path, ",:!") {
				return nil, errors.New("remote socket path not representable as a socat address")
			}
			return []string{"socat", "-d", "-d", "-t", "86400", "-", "UNIX-CONNECT:" + t.Path}, nil
		}
		if err := checkHostPort(t); err != nil {
			return nil, err
		}
		host := t.Host
		if strings.Contains(host, ":") {
			if strings.Contains(host, "%") {
				return nil, errors.New("scoped IPv6 addresses are not supported with socat")
			}
			host = "[" + host + "]"
		}
		return []string{"socat", "-d", "-d", "-t", "86400", "-", "TCP:" + host + ":" + strconv.Itoa(t.Port)}, nil
	}}
)

// Select picks the first recognized connector from the adapter's detection
// lines (tool name -> first output lines). Recognition requires an
// implementation fingerprint, never a bare command name.
func Select(detected map[string][]string) (Connector, bool) {
	has := func(tool, needle string) bool {
		for _, line := range detected[tool] {
			if strings.Contains(line, needle) {
				return true
			}
		}
		return false
	}
	switch {
	case has("nc", "OpenBSD netcat"):
		return openBSDNetcat, true
	case has("ncat", "Ncat: Version") || (has("ncat", "Ncat") && has("ncat", "nmap.org")):
		return nmapNcat, true
	case has("socat", "socat version 1.") || has("socat", "socat by Gerhard Rieger"):
		return socat, true
	}
	return Connector{}, false
}

// checkHostPort keeps targets to DNS names and IP literals so no value can be
// mistaken for an option or a connector address keyword.
func checkHostPort(t forward.Target) error {
	if t.Port < 1 || t.Port > 65535 {
		return errors.New("invalid remote port")
	}
	h := t.Host
	if h == "" || len(h) > 255 || h[0] == '-' {
		return errors.New("invalid remote host")
	}
	if a, err := netip.ParseAddr(h); err == nil && a.IsValid() {
		return nil
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == '_') {
			return errors.New("invalid remote host")
		}
	}
	return nil
}

func checkPath(p string) error {
	if p == "" || len(p) > 4096 || p[0] == '-' {
		return errors.New("invalid remote socket path")
	}
	for i := 0; i < len(p); i++ {
		if p[i] < 0x20 || p[i] == 0x7f {
			return errors.New("invalid remote socket path")
		}
	}
	return nil
}
