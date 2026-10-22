// Package sshconfig implements the supported, non-executing SSH configuration
// subset. It never discovers default files, executes commands or resolves DNS.
package sshconfig

import (
	"errors"
	"io"
	"strings"
)

const (
	MaxBytes       = 1 << 20
	MaxTargetBytes = 4096
)

// Load reads the explicitly selected regular file and its Include directives.
// No default root file is discovered; paths and OS errors stay out of diagnostics.
func Load(path, host string) (map[string]string, error) {
	return LoadWithOverrides(path, host, nil)
}

// LoadWithOverrides seeds already-obtained CLI values before interpreting file
// directives, including HostName-dependent Include tokens. The input is copied.
func LoadWithOverrides(path, host string, overrides map[string]string) (map[string]string, error) {
	p := newParser(host)
	p.includes = true
	for key, value := range overrides {
		if err := Validate(key, value); err != nil {
			return nil, err
		}
		p.values[key] = value
	}
	if err := p.file(path, true, false, 0); err != nil {
		return nil, err
	}
	return p.values, nil
}

// Parse selects the first value from all matching blocks. Every line is checked,
// including inactive blocks and overridden values, so unsupported semantics
// cannot be hidden in a file that appears to have been successfully accepted.
func Parse(r io.Reader, host string) (map[string]string, error) {
	p := newParser(host)
	if err := p.parse(r, true, false, 0); err != nil {
		return nil, err
	}
	return p.values, nil
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
	default:
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
