package sshconfig

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"pi-sh/internal/forwardspec"
)

// Directive is one active list directive and its arguments, as written.
type Directive struct {
	Key  string
	Args []string
}

const (
	MaxIncludeDepth = 16
	MaxFiles        = 256
	MaxGlobEntries  = 16384
	MaxPathBytes    = 4096
)

type parser struct {
	ctx                   Context
	env                   *localEnv
	values                map[string]string
	includes              bool
	final, wantFinal      bool
	home                  string
	bytes, files, entries int
	stack                 []os.FileInfo
}

func newParser(ctx Context) *parser {
	env := ctx.env
	if env == nil {
		env = systemEnv()
	}
	return &parser{ctx: ctx, env: env, values: make(map[string]string)}
}

type budgetReader struct {
	p *parser
	r io.Reader
}

func (r budgetReader) Read(b []byte) (int, error) {
	remaining := MaxBytes - r.p.bytes
	if remaining < 0 {
		return 0, errors.New("configuration exceeds aggregate size limit")
	}
	if len(b) > remaining+1 {
		b = b[:remaining+1]
	}
	n, err := r.r.Read(b)
	r.p.bytes += n
	return n, err
}

func (p *parser) file(name string, active, never bool, depth int) error {
	if depth > MaxIncludeDepth {
		return errors.New("configuration Include nesting exceeds limit")
	}
	if p.files >= MaxFiles {
		return errors.New("configuration file count exceeds limit")
	}
	info, err := os.Stat(name)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("cannot read configuration: expected a readable regular file")
	}
	f, err := os.Open(name)
	if err != nil {
		return errors.New("cannot open configuration file")
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("cannot read configuration: expected a readable regular file")
	}
	for _, parent := range p.stack {
		if os.SameFile(info, parent) {
			return errors.New("configuration Include cycle")
		}
	}
	p.files++
	p.stack = append(p.stack, info)
	defer func() { p.stack = p.stack[:len(p.stack)-1] }()
	return p.parse(f, active, never, depth)
}

func (p *parser) parse(r io.Reader, active, never bool, depth int) error {
	scanner := bufio.NewScanner(budgetReader{p, r})
	scanner.Buffer(make([]byte, 4096), 64<<10)
	line := 0
	for scanner.Scan() {
		line++
		if p.bytes > MaxBytes {
			return errors.New("configuration exceeds aggregate size limit")
		}
		if err := p.directive(scanner.Text(), &active, never, depth); err != nil {
			return fmt.Errorf("configuration line %d: %w", line, err)
		}
	}
	if p.bytes > MaxBytes {
		return errors.New("configuration exceeds aggregate size limit")
	}
	if scanner.Err() != nil {
		return errors.New("configuration read failed or line exceeds size limit")
	}
	return nil
}

func (p *parser) directive(text string, active *bool, never bool, depth int) error {
	if key, value, ok := RawCommand(text); ok {
		if err := Validate(key, value); err != nil {
			return err
		}
		if _, exists := p.values[key]; *active && !exists {
			p.values[key] = value
		}
		return nil
	}
	key, args, err := Fields(text)
	if err != nil || key == "" {
		return err
	}
	if key == "host" {
		if len(args) == 0 {
			return errors.New("Host requires patterns")
		}
		matched, negated := false, false
		for _, pattern := range args {
			negative := strings.HasPrefix(pattern, "!")
			if negative {
				pattern = pattern[1:]
			}
			if pattern == "" {
				return errors.New("empty Host pattern")
			}
			// Host patterns are case-sensitive against the destination as
			// typed (OpenSSH 10.2); only Match host/originalhost fold case.
			if match(pattern, p.ctx.Host) {
				if negative {
					negated = true
				} else {
					matched = true
				}
			}
		}
		*active = matched && !negated && !never
		return nil
	}
	if key == "match" {
		if len(args) == 0 {
			return errors.New("Match requires criteria")
		}
		// Every Match line is evaluated, even under an inactive Include: as in
		// OpenSSH, exec may run and "final" may request the second pass.
		matched, err := p.match(args)
		if err != nil {
			return err
		}
		*active = matched && !never
		return nil
	}
	if key == "include" {
		if !p.includes {
			return errors.New("Include requires file-based configuration loading")
		}
		if len(args) == 0 {
			return errors.New("Include requires path patterns")
		}
		for _, arg := range args {
			pattern, err := p.includePath(arg)
			if err != nil {
				return err
			}
			names, err := p.glob(pattern)
			if err != nil {
				return err
			}
			for _, name := range names {
				// Each file inherits the caller's context independently. Host lines
				// in the child do not leak back; an inactive Include never activates.
				if err := p.file(name, *active, never || !*active, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if forwardspec.ListKeyword(key) {
		if err := forwardspec.CheckDirective(key, args, os.LookupEnv); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		if *active && p.ctx.Lists != nil {
			*p.ctx.Lists = append(*p.ctx.Lists, Directive{Key: key, Args: append([]string(nil), args...)})
		}
		return nil
	}
	if forwardspec.FeatureKeyword(key) && len(args) > 1 {
		// Only ProxyCommand takes several words; ValidateFeature refuses
		// every value but none, so a multi-word command is always an error.
		return forwardspec.ValidateFeature(key, args)
	}
	if MultiValue(key) && len(args) > 1 {
		for _, arg := range args[1:] {
			if err := Validate(key, arg); err != nil {
				return err
			}
		}
		args = args[:1]
	}
	if len(args) != 1 {
		return errors.New("expected one scalar value")
	}
	if err := Validate(key, args[0]); err != nil {
		return err
	}
	if key == "refuseconnection" && *active {
		// OpenSSH 10.2 terminates as soon as an active line is read, even
		// for ssh -G. The message is configuration text and is not echoed.
		return errors.New("RefuseConnection: connection refused by configuration")
	}
	if _, exists := p.values[key]; *active && !exists {
		p.values[key] = args[0]
	}
	return nil
}

func (p *parser) homeDir() (string, error) {
	if p.home == "" {
		home, err := os.UserHomeDir()
		if err != nil || len(home) > MaxPathBytes || !filepath.IsAbs(home) || strings.IndexFunc(home, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
			return "", errors.New("local home directory unavailable for Include")
		}
		p.home = home
	}
	return p.home, nil
}

func (p *parser) includePath(value string) (string, error) {
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		switch {
		case value[i] == '%':
			i++
			if i == len(value) {
				return "", errors.New("incomplete Include token")
			}
			var token string
			switch value[i] {
			case '%':
				token = "%"
			case 'n':
				token = p.ctx.Host
			case 'h':
				token = p.ctx.Host
				if name := p.values["hostname"]; name != "" {
					var err error
					token, err = Hostname(name, p.ctx.Host)
					if err != nil {
						return "", err
					}
				}
			case 'd':
				var err error
				token, err = p.homeDir()
				if err != nil {
					return "", err
				}
			default:
				return "", errors.New("unsupported Include token")
			}
			if len(token) > MaxPathBytes-out.Len() {
				return "", errors.New("Include path exceeds size limit")
			}
			out.WriteString(token)
		case value[i] == '$' && i+1 < len(value) && value[i+1] == '{':
			end := strings.IndexByte(value[i+2:], '}')
			if end < 0 {
				return "", errors.New("unterminated Include environment variable")
			}
			end += i + 2
			name := value[i+2 : end]
			if name == "" || strings.IndexFunc(name, func(r rune) bool {
				return !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
			}) >= 0 {
				return "", errors.New("invalid Include environment variable")
			}
			expanded, ok := os.LookupEnv(name)
			if !ok {
				return "", errors.New("Include environment variable is unset")
			}
			if len(expanded) > MaxPathBytes-out.Len() {
				return "", errors.New("Include path exceeds size limit")
			}
			out.WriteString(expanded)
			i = end
		default:
			out.WriteByte(value[i])
		}
		if out.Len() > MaxPathBytes {
			return "", errors.New("Include path exceeds size limit")
		}
	}
	name := out.String()
	if name == "" || strings.IndexFunc(name, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
		return "", errors.New("invalid Include path")
	}
	if strings.HasPrefix(name, "~") {
		if name != "~" && !strings.HasPrefix(name, "~/") {
			return "", errors.New("named-user tilde expansion is not implemented")
		}
		home, err := p.homeDir()
		if err != nil {
			return "", err
		}
		name = home + name[1:]
	}
	name = filepath.FromSlash(name)
	if !filepath.IsAbs(name) {
		if filepath.VolumeName(name) != "" {
			return "", errors.New("ambiguous Include volume path")
		}
		home, err := p.homeDir()
		if err != nil {
			return "", err
		}
		name = home + string(os.PathSeparator) + ".ssh" + string(os.PathSeparator) + name
	}
	if len(name) > MaxPathBytes {
		return "", errors.New("Include path exceeds size limit")
	}
	return name, nil
}
