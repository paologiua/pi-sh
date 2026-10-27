package forwardspec

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Settings is the forwarding-related part of an effective ssh configuration.
// Lists accumulate (duplicates dropped, as add_local_forward does); scalars are
// first-obtained and are assigned by the caller's first-wins bookkeeping.
type Settings struct {
	// Local holds -L and -D (dynamic) forwardings in command/config order.
	Local  []Forward
	Remote []Forward
	// Stdio is the -W target when StdioSet. ssh -W with a bare port parses but
	// leaves no stdio target; it still selects SessionType none.
	Stdio    Endpoint
	StdioSet bool
	stdioArg bool

	GatewayPorts, ExitOnForwardFailure, ClearAll, BindUnlink bool
	BindMask                                                 uint32
	PermitRemoteOpen                                         []string
}

// NewSettings returns OpenSSH's client defaults.
func NewSettings() Settings { return Settings{BindMask: 0o177} }

// Bad*Spec errors carry ssh's exact messages; the CLI exits 255 on them.
func badSpec(kind, spec string) error {
	return fmt.Errorf("Bad %s forwarding specification '%s'", kind, spec)
}

// Getenv is the environment lookup used for ${NAME} expansion; tests replace it.
type Getenv func(string) (string, bool)

// AddLocal handles -L.
func (s *Settings) AddLocal(spec string, getenv Getenv) error {
	f, ok := Local(spec, getenv)
	if !ok {
		return badSpec("local", spec)
	}
	s.Local = appendUnique(s.Local, f)
	return nil
}

// AddRemote handles -R.
func (s *Settings) AddRemote(spec string, getenv Getenv) error {
	f, ok := Remote(spec, getenv)
	if !ok {
		return badSpec("remote", spec)
	}
	s.Remote = appendUnique(s.Remote, f)
	return nil
}

// AddDynamic handles -D.
func (s *Settings) AddDynamic(spec string, getenv Getenv) error {
	f, ok := Dynamic(spec, getenv)
	if !ok {
		return badSpec("dynamic", spec)
	}
	s.Local = appendUnique(s.Local, f)
	return nil
}

// SetStdio handles -W.
func (s *Settings) SetStdio(spec string, getenv Getenv) error {
	if s.stdioArg {
		return errors.New("stdio forward already specified")
	}
	target, set, ok := Stdio(spec, getenv)
	if !ok {
		return fmt.Errorf("Bad stdio forwarding specification '%s'", spec)
	}
	s.stdioArg = true
	s.Stdio, s.StdioSet = target, set
	return nil
}

func appendUnique(list []Forward, f Forward) []Forward {
	for _, g := range list {
		if g.Equal(f) {
			return list
		}
	}
	return append(list, f)
}

// ListKeyword reports directives that take several arguments or accumulate.
func ListKeyword(key string) bool {
	switch key {
	case "localforward", "remoteforward", "dynamicforward", "permitremoteopen":
		return true
	}
	return false
}

// CheckDirective validates a list directive exactly as readconf does, whether
// or not its block is active.
func CheckDirective(key string, args []string, getenv Getenv) error {
	_, err := directive(key, args, getenv)
	return err
}

// Directive applies an active list directive. PermitRemoteOpen is
// first-obtained; permitSet reports whether an earlier value already won.
func (s *Settings) Directive(key string, args []string, getenv Getenv, permitSet bool) error {
	value, err := directive(key, args, getenv)
	if err != nil {
		return err
	}
	switch key {
	case "localforward", "dynamicforward":
		s.Local = appendUnique(s.Local, value.(Forward))
	case "remoteforward":
		s.Remote = appendUnique(s.Remote, value.(Forward))
	case "permitremoteopen":
		if !permitSet {
			s.PermitRemoteOpen = value.([]string)
		}
	}
	return nil
}

func directive(key string, args []string, getenv Getenv) (any, error) {
	if key == "permitremoteopen" {
		return permitRemoteOpen(args)
	}
	if len(args) == 0 || args[0] == "" {
		return nil, errors.New("Missing argument.")
	}
	remote, dynamic := key == "remoteforward", key == "dynamicforward"
	spec := args[0]
	used := 1
	if !dynamic {
		if len(args) < 2 || args[1] == "" {
			if !remote {
				return nil, errors.New("Missing target argument.")
			}
			dynamic = true
		} else {
			spec = args[0] + ":" + args[1]
			used = 2
		}
	}
	if len(args) > used {
		return nil, fmt.Errorf("keyword %s extra arguments at end of line", key)
	}
	f, ok := Parse(spec, dynamic, remote, getenv)
	if !ok {
		return nil, errors.New("Bad forwarding specification.")
	}
	return f, nil
}

func permitRemoteOpen(args []string) ([]string, error) {
	if len(args) == 0 {
		return nil, errors.New("Missing argument.")
	}
	for i, arg := range args {
		lower := strings.ToLower(arg)
		if lower == "none" || lower == "any" {
			if i > 0 || len(args) > 1 {
				return nil, fmt.Errorf("keyword permitremoteopen \"%s\" argument must appear alone.", arg)
			}
			continue
		}
		_, port, hasPort, ok := hpdelim(arg)
		if !ok {
			return nil, errors.New("missing host in permitremoteopen")
		}
		if !hasPort || (port != "*" && a2port(port) <= 0) {
			return nil, errors.New("bad port number in permitremoteopen")
		}
	}
	return append([]string(nil), args...), nil
}

// hpdelim is OpenSSH's hpdelim2: the host ends at the first ':' or '/' (or
// after a closing bracket); ok=false for an unclosed bracket or a bracket
// followed by anything else.
func hpdelim(s string) (host, port string, hasPort, ok bool) {
	end := strings.IndexAny(s, ":/")
	if strings.HasPrefix(s, "[") {
		close := strings.IndexByte(s, ']')
		if close < 0 {
			return "", "", false, false
		}
		end = close + 1
	} else if end < 0 {
		end = len(s)
	}
	if end == len(s) {
		return s, "", false, true
	}
	if s[end] != ':' && s[end] != '/' {
		return "", "", false, false
	}
	return s[:end], s[end+1:], true, true
}

// ScalarKeyword reports the single-valued forwarding directives.
func ScalarKeyword(key string) bool {
	switch key {
	case "gatewayports", "exitonforwardfailure", "clearallforwardings", "streamlocalbindunlink", "streamlocalbindmask":
		return true
	}
	return false
}

// ValidateScalar checks a scalar forwarding directive value.
func ValidateScalar(key, value string) error {
	if key == "streamlocalbindmask" {
		_, err := parseMask(value)
		return err
	}
	if _, ok := Flag(value); !ok {
		return fmt.Errorf("unsupported option \"%s\".", value)
	}
	return nil
}

// SetScalar assigns an already-validated scalar (the caller enforces first-wins).
func (s *Settings) SetScalar(key, value string) error {
	if err := ValidateScalar(key, value); err != nil {
		return err
	}
	if key == "streamlocalbindmask" {
		s.BindMask, _ = parseMask(value)
		return nil
	}
	on, _ := Flag(value)
	switch key {
	case "gatewayports":
		s.GatewayPorts = on
	case "exitonforwardfailure":
		s.ExitOnForwardFailure = on
	case "clearallforwardings":
		s.ClearAll = on
	case "streamlocalbindunlink":
		s.BindUnlink = on
	}
	return nil
}

// Flag parses ssh's yes/no/true/false (case-insensitive) flag values.
func Flag(value string) (bool, bool) {
	switch strings.ToLower(value) {
	case "yes", "true":
		return true, true
	case "no", "false":
		return false, true
	}
	return false, false
}

func parseMask(value string) (uint32, error) {
	n, err := strconv.ParseUint(value, 8, 32)
	if err != nil || n > 0o777 {
		return 0, errors.New("Bad mask.")
	}
	return uint32(n), nil
}

// Finish applies ssh's post-configuration rules: -W forces
// ClearAllForwardings and ExitOnForwardFailure, and ClearAllForwardings drops
// every -L/-R/-D forwarding from all sources.
func (s *Settings) Finish() {
	if s.StdioSet {
		s.ClearAll = true
		s.ExitOnForwardFailure = true
	}
	if s.ClearAll {
		s.Local, s.Remote = nil, nil
	}
}

// Expand applies OpenSSH's client percent expansion to Unix-socket paths in
// forwardings. Only the tokens with a pi-sh meaning are accepted; any other
// token is an error, as an unknown key is fatal in ssh.
func (s *Settings) Expand(tokens map[byte]string) error {
	expand := func(e *Endpoint) error {
		if e.Port != PortStreamLocal {
			return nil
		}
		value, err := percentExpand(e.Path, tokens)
		if err != nil {
			return err
		}
		e.Path = value
		return nil
	}
	for _, list := range [][]Forward{s.Local, s.Remote} {
		for i := range list {
			if err := expand(&list[i].Listen); err != nil {
				return err
			}
			if err := expand(&list[i].Connect); err != nil {
				return err
			}
		}
	}
	return nil
}

func percentExpand(value string, tokens map[byte]string) (string, error) {
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] != '%' {
			out.WriteByte(value[i])
			continue
		}
		i++
		if i == len(value) {
			return "", errors.New("invalid format: forwarding path ends with %")
		}
		if value[i] == '%' {
			out.WriteByte('%')
			continue
		}
		replacement, ok := tokens[value[i]]
		if !ok {
			return "", fmt.Errorf("forwarding path token %%%c is not supported by pi-sh", value[i])
		}
		out.WriteString(replacement)
	}
	return out.String(), nil
}

// WriteConfig prints the -G lines in OpenSSH's dump order and format.
func (s Settings) WriteConfig(w io.Writer) error {
	flag := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "clearallforwardings %s\nexitonforwardfailure %s\ngatewayports %s\nstreamlocalbindunlink %s\n",
		flag(s.ClearAll), flag(s.ExitOnForwardFailure), flag(s.GatewayPorts), flag(s.BindUnlink))
	dump(&b, "dynamicforward", s.Local, true)
	dump(&b, "localforward", s.Local, false)
	dump(&b, "remoteforward", s.Remote, false)
	if len(s.PermitRemoteOpen) == 0 {
		b.WriteString("permitremoteopen any\n")
	} else {
		fmt.Fprintf(&b, "permitremoteopen %s\n", strings.Join(s.PermitRemoteOpen, " "))
	}
	fmt.Fprintf(&b, "streamlocalbindmask 0%o\n", s.BindMask)
	_, err := io.WriteString(w, b.String())
	return err
}

// dump is dump_cfg_forwards, including its treatment of a forwarding with a
// connect path (no connect host) as dynamic as well as local.
func dump(b *strings.Builder, name string, list []Forward, dynamic bool) {
	for _, f := range list {
		socks := f.Connect.HasHost && f.Connect.Host == "socks"
		if dynamic && f.Connect.HasHost && !socks {
			continue
		}
		if name == "localforward" && socks {
			continue
		}
		b.WriteString(name)
		b.WriteString(" " + endpoint(f.Listen))
		if !dynamic {
			b.WriteString(" " + endpoint(f.Connect))
		}
		b.WriteByte('\n')
	}
}

func endpoint(e Endpoint) string {
	switch {
	case e.Port == PortStreamLocal:
		return e.Path
	case !e.HasHost:
		return strconv.Itoa(e.Port)
	}
	return "[" + e.Host + "]:" + strconv.Itoa(e.Port)
}

// String renders an endpoint for diagnostics.
func (e Endpoint) String() string { return endpoint(e) }
