// Package sshconfig implements the supported SSH configuration subset. It
// never discovers default files or resolves DNS. Like OpenSSH, it runs a local
// shell command only for a Match exec criterion in an explicitly selected file.
package sshconfig

import (
	"bytes"
	"errors"
	"io"
	"strings"

	"pi-sh/internal/connpolicy"
	"pi-sh/internal/diag"
	"pi-sh/internal/forwardspec"
)

const (
	MaxBytes       = 1 << 20
	MaxTargetBytes = 4096
)

// Load reads the explicitly selected regular file and its Include directives.
// No default root file is discovered; paths and OS errors stay out of diagnostics.
func Load(path, host string) (map[string]string, error) {
	return LoadContext(path, Context{Host: host})
}

// LoadWithOverrides seeds already-obtained CLI values before interpreting file
// directives, including HostName-dependent Include tokens. The input is copied.
func LoadWithOverrides(path, host string, overrides map[string]string) (map[string]string, error) {
	return LoadContext(path, Context{Host: host, Overrides: overrides})
}

// LoadContext evaluates the file for one invocation. If a non-negated
// "Match final" was seen, the files are read again as OpenSSH's final pass:
// earlier values are kept (first value wins), HostName is fixed to the
// effective target, and "final"/"canonical" criteria then match.
func LoadContext(path string, ctx Context) (map[string]string, error) {
	return evaluate(ctx, func(p *parser) error {
		p.includes = true
		return p.file(path, true, false, 0)
	})
}

func evaluate(ctx Context, read func(*parser) error) (map[string]string, error) {
	p := newParser(ctx)
	for key, value := range ctx.Overrides {
		// ssh -P accepts an empty tag; -o Tag= is rejected before this point.
		if key == "tag" && value == "" {
			p.values[key] = value
			continue
		}
		if err := Validate(key, value); err != nil {
			return nil, err
		}
		p.values[key] = value
	}
	if err := read(p); err != nil {
		return nil, err
	}
	if !p.wantFinal {
		return p.values, nil
	}
	host, err := p.currentHost()
	if err != nil {
		return nil, err
	}
	final := newParser(ctx)
	final.env, final.home, final.final = p.env, p.home, true
	final.values = p.values
	// Callers expand the stored HostName again; escape it so it stays literal.
	final.values["hostname"] = strings.ReplaceAll(host, "%", "%%")
	if err := read(final); err != nil {
		return nil, err
	}
	return final.values, nil
}

// Parse selects the first value from all matching blocks. Every line is checked,
// including inactive blocks and overridden values, so unsupported semantics
// cannot be hidden in a file that appears to have been successfully accepted.
func Parse(r io.Reader, host string) (map[string]string, error) {
	return ParseContext(r, Context{Host: host})
}

// ParseContext is Parse with Match context. The bounded input is buffered so a
// Match final pass can read it again; Include is not available.
func ParseContext(r io.Reader, ctx Context) (map[string]string, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return nil, errors.New("configuration read failed or line exceeds size limit")
	}
	if len(data) > MaxBytes {
		return nil, errors.New("configuration exceeds aggregate size limit")
	}
	return evaluate(ctx, func(p *parser) error { return p.parse(bytes.NewReader(data), true, false, 0) })
}

// Fields supports keyword whitespace/= separators, double-quoted arguments and
// comments at token boundaries outside quotes. Backslash syntax is rejected
// rather than reinterpreted.
func Fields(line string) (string, []string, error) {
	if strings.ContainsAny(line, "\x00\r\n") {
		return "", nil, errors.New("unsupported configuration control syntax")
	}
	line = strings.TrimSpace(line)
	if line == "" || line[0] == '#' {
		return "", nil, nil
	}
	i := strings.IndexAny(line, "= \t")
	if i < 0 {
		return strings.ToLower(line), nil, nil
	}
	key := strings.ToLower(line[:i])
	if key == "" {
		return "", nil, errors.New("missing configuration keyword")
	}
	rest := strings.TrimLeft(line[i:], " \t")
	if strings.HasPrefix(rest, "=") {
		rest = strings.TrimLeft(rest[1:], " \t")
	}
	var fields []string
	var field strings.Builder
	quoted, started := false, false
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if c == '\\' {
			return "", nil, errors.New("configuration backslash syntax is not implemented")
		}
		if c == '"' {
			quoted = !quoted
			started = true
			continue
		}
		if !quoted && c == '#' && !started {
			break
		}
		if !quoted && (c == ' ' || c == '\t') {
			if started {
				fields = append(fields, field.String())
				field.Reset()
				started = false
			}
			continue
		}
		field.WriteByte(c)
		started = true
	}
	if quoted {
		return "", nil, errors.New("unterminated configuration quote")
	}
	if started {
		fields = append(fields, field.String())
	}
	return key, fields, nil
}

func Validate(key, value string) error {
	if value == "" || strings.ContainsAny(value, "\x00\r\n") {
		return errors.New("empty or invalid configuration value")
	}
	switch key {
	case "escapechar":
		_, err := Escape(value)
		return err
	case "stdinnull":
		if value != "yes" && value != "no" {
			return errors.New("StdinNull requires yes or no")
		}
	case "requesttty":
		if value != "auto" && value != "no" && value != "yes" && value != "force" {
			return errors.New("invalid RequestTTY value")
		}
	case "hostname":
		_, err := Hostname(value, "placeholder")
		return err
	case "user", "port", "batchmode", "numberofpasswordprompts", "connecttimeout", "connectionattempts",
		"serveraliveinterval", "serveralivecountmax", "tcpkeepalive", "channeltimeout":
		// Syntax only; connpolicy.FromValues rejects effective settings that
		// have no Connect equivalent (for example any User or Port).
		return connpolicy.Validate(key, value)
	case "tag":
		// Any single nonempty argument, including "none", is a literal tag.
	case "loglevel":
		_, err := diag.ParseLevel(value)
		return err
	case "syslogfacility":
		_, err := diag.ParseFacility(value)
		return err
	case "remotecommand":
		// Raw command text (see RawCommand); "none" selects no command.
	case "refuseconnection":
		// Any message; an active RefuseConnection fails the whole load.
	case "gatewayports", "exitonforwardfailure", "clearallforwardings", "streamlocalbindunlink", "streamlocalbindmask":
		return forwardspec.ValidateScalar(key, value)
	case "forwardagent", "forwardx11", "forwardx11trusted", "controlmaster", "controlpath",
		"controlpersist", "tunnel", "proxycommand", "proxyjump", "proxyusefdpass", "sessiontype":
		// Accepted only with the values that leave the feature off.
		return forwardspec.ValidateFeature(key, []string{value})
	default:
		if handled, err := keywordDecision(key, value); handled {
			return err
		}
		return errors.New("unsupported SSH configuration directive")
	}
	return nil
}

func Escape(value string) (rune, error) {
	if value == "none" {
		return -1, nil
	}
	if len(value) == 1 && value[0] < 128 {
		return rune(value[0]), nil
	}
	if len(value) == 2 && value[0] == '^' && value[1] >= 64 && value[1] < 128 {
		return rune(value[1] & 31), nil
	}
	return 0, errors.New("invalid escape character: use one ASCII character, ^X control notation, or none")
}

// Hostname maps an alias to an exact Connect device name/ID. Only the HostName
// tokens %h (original destination) and %% are supported; expansion is one pass.
func Hostname(value, original string) (string, error) {
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] != '%' {
			if out.Len() >= MaxTargetBytes {
				return "", errors.New("HostName exceeds size limit")
			}
			out.WriteByte(value[i])
			continue
		}
		i++
		if i == len(value) {
			return "", errors.New("incomplete HostName token")
		}
		switch value[i] {
		case 'h':
			if len(original) > MaxTargetBytes-out.Len() {
				return "", errors.New("HostName exceeds size limit")
			}
			out.WriteString(original)
		case '%':
			if out.Len() >= MaxTargetBytes {
				return "", errors.New("HostName exceeds size limit")
			}
			out.WriteByte('%')
		default:
			return "", errors.New("unsupported HostName token")
		}
	}
	name := out.String()
	if strings.TrimSpace(name) == "" || strings.IndexFunc(name, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
		return "", errors.New("HostName must be nonempty and contain no control characters")
	}
	return name, nil
}

// OpenSSH Host patterns have only * and ? wildcards, not filepath.Match's
// character classes or path-separator rules. Matching operates on bytes.
func match(pattern, value string) bool {
	p, v, star, retry := 0, 0, -1, 0
	for v < len(value) {
		if p < len(pattern) && (pattern[p] == '?' || pattern[p] == value[v]) {
			p++
			v++
			continue
		}
		if p < len(pattern) && pattern[p] == '*' {
			star = p
			p++
			retry = v
			continue
		}
		if star < 0 {
			return false
		}
		retry++
		v = retry
		p = star + 1
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}
