// Package forwardspec parses OpenSSH 10.2 forwarding specifications and
// options (-L, -R, -D, -W, LocalForward, RemoteForward, DynamicForward and the
// related scalar keywords). It performs no I/O except reading the local
// services database for named ports, exactly where ssh's a2port would.
package forwardspec

import (
	"errors"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// PortStreamLocal marks a Unix-domain socket path endpoint, as in OpenSSH.
const PortStreamLocal = -2

// maxHost is NI_MAXHOST: OpenSSH rejects host fields of this length or longer.
const maxHost = 1025

// maxSunPath is sizeof(sun_path) on the local platform; OpenSSH rejects paths
// of this length or longer before any socket is created.
var maxSunPath = func() int {
	switch runtime.GOOS {
	case "darwin", "freebsd", "openbsd", "netbsd", "dragonfly", "ios":
		return 104
	}
	return 108
}()

// Endpoint is one side of a forwarding. Host distinguishes an absent host
// (HasHost false) from an empty one, which OpenSSH prints differently and binds
// differently. Port is PortStreamLocal when Path is used.
type Endpoint struct {
	Host    string
	HasHost bool
	Port    int
	Path    string
}

// Forward mirrors OpenSSH's struct Forward. A dynamic (SOCKS) forwarding has
// the connect host "socks" and port 0.
type Forward struct {
	Listen, Connect Endpoint
}

// Dynamic reports a SOCKS forwarding (-D, or -R with only a listen spec).
func (f Forward) Dynamic() bool {
	return f.Connect.HasHost && f.Connect.Host == "socks" && f.Connect.Port == 0 && f.Connect.Path == ""
}

// Equal is OpenSSH's forward_equals, used to drop duplicate forwardings.
func (f Forward) Equal(g Forward) bool {
	return f.Listen == g.Listen && f.Connect == g.Connect
}

type field struct {
	arg  string
	path bool
}

// nextField is parse_fwd_field. It returns ok=false at end of input or for a
// malformed bracketed field; rest is nil at end of input.
func nextField(s *string, end *bool) (field, bool) {
	cp := *s
	if cp == "" {
		*end = true
		return field{}, false
	}
	if cp[0] == '[' {
		ep := 1
		path := false
		for ep < len(cp) && cp[ep] != ']' {
			if cp[ep] == '/' {
				path = true
			}
			ep++
		}
		if ep == len(cp) || (ep+1 < len(cp) && cp[ep+1] != ':') {
			return field{}, false
		}
		arg := cp[1:ep]
		rest := cp[ep+1:]
		if rest != "" {
			rest = rest[1:]
		}
		*s = rest
		return field{arg, path}, true
	}
	var out strings.Builder
	path := false
	for i := 0; i < len(cp); i++ {
		switch cp[i] {
		case '\\':
			// The escaped byte is copied literally and not examined.
			if i+1 == len(cp) {
				return field{}, false
			}
			i++
			out.WriteByte(cp[i])
			continue
		case '/':
			path = true
		case ':':
			*s = cp[i+1:]
			return field{out.String(), path}, true
		}
		out.WriteByte(cp[i])
	}
	*s = ""
	*end = false
	return field{out.String(), path}, true
}

// Parse is OpenSSH's parse_forward. It returns the number of fields parsed and
// ok=false for a specification ssh would reject. getenv expands ${NAME}; a
// missing variable makes the specification invalid, as in ssh.
func Parse(spec string, dynamic, remote bool, getenv func(string) (string, bool)) (Forward, bool) {
	var f Forward
	p, ok := dollarExpand(spec, getenv)
	if !ok {
		return f, false
	}
	p = strings.TrimLeft(p, " \t\n\v\f\r")
	var fields [4]field
	n := 0
	end := false
	rest := p
	for n < 4 {
		fd, ok := nextField(&rest, &end)
		if !ok {
			break
		}
		fields[n] = fd
		n++
		if rest == "" {
			// A field without a trailing colon ends the input; one more call
			// would report the end without consuming anything.
			end = true
			break
		}
	}
	if !end && rest != "" {
		return f, false // trailing garbage, including a malformed field
	}
	socks := Endpoint{Host: "socks", HasHost: true}
	host := func(s string) Endpoint { return Endpoint{Host: s, HasHost: true} }
	path := func(s string) Endpoint { return Endpoint{Path: s, Port: PortStreamLocal} }
	switch n {
	case 1:
		if fields[0].path {
			f.Listen = path(fields[0].arg)
		} else {
			f.Listen.Port = a2port(fields[0].arg)
		}
		f.Connect = socks
	case 2:
		switch {
		case fields[0].path && fields[1].path:
			f.Listen, f.Connect = path(fields[0].arg), path(fields[1].arg)
		case fields[1].path:
			f.Listen.Port = a2port(fields[0].arg)
			f.Connect = path(fields[1].arg)
		default:
			f.Listen = host(fields[0].arg)
			f.Listen.Port = a2port(fields[1].arg)
			f.Connect = socks
		}
	case 3:
		switch {
		case fields[0].path:
			f.Listen = path(fields[0].arg)
			f.Connect = host(fields[1].arg)
			f.Connect.Port = a2port(fields[2].arg)
		case fields[2].path:
			f.Listen = host(fields[0].arg)
			f.Listen.Port = a2port(fields[1].arg)
			f.Connect = path(fields[2].arg)
		default:
			f.Listen.Port = a2port(fields[0].arg)
			f.Connect = host(fields[1].arg)
			f.Connect.Port = a2port(fields[2].arg)
		}
	case 4:
		f.Listen = host(fields[0].arg)
		f.Listen.Port = a2port(fields[1].arg)
		f.Connect = host(fields[2].arg)
		f.Connect.Port = a2port(fields[3].arg)
	default:
		return f, false
	}
	connectPath := f.Connect.Port == PortStreamLocal
	listenPath := f.Listen.Port == PortStreamLocal
	if dynamic {
		if n != 1 && n != 2 {
			return f, false
		}
	} else {
		if n != 3 && n != 4 && !connectPath && !listenPath {
			return f, false
		}
		if f.Connect.Port <= 0 && !connectPath {
			return f, false
		}
	}
	if (f.Listen.Port < 0 && !listenPath) || (!remote && f.Listen.Port == 0) {
		return f, false
	}
	if len(f.Connect.Host) >= maxHost || len(f.Listen.Host) >= maxHost ||
		len(f.Connect.Path) >= maxSunPath || len(f.Listen.Path) >= maxSunPath {
		return f, false
	}
	return f, true
}

// Local parses -L. Remote parses -R, falling back to a remote dynamic
// specification as ssh.c does. Dynamic parses -D.
func Local(spec string, getenv func(string) (string, bool)) (Forward, bool) {
	return Parse(spec, false, false, getenv)
}

func Remote(spec string, getenv func(string) (string, bool)) (Forward, bool) {
	if f, ok := Parse(spec, false, true, getenv); ok {
		return f, true
	}
	return Parse(spec, true, true, getenv)
}

func Dynamic(spec string, getenv func(string) (string, bool)) (Forward, bool) {
	return Parse(spec, true, false, getenv)
}

// Stdio parses -W. As in ssh.c, a bare port leaves no stdio host at all.
func Stdio(spec string, getenv func(string) (string, bool)) (target Endpoint, set bool, ok bool) {
	f, ok := Parse(spec, true, false, getenv)
	if !ok {
		return Endpoint{}, false, false
	}
	if f.Listen.Port == PortStreamLocal {
		return f.Listen, true, true
	}
	return f.Listen, f.Listen.HasHost, true
}

// a2port is OpenSSH's: a decimal 0..65535 as strtonum accepts it, otherwise a
// case-sensitive TCP service name from the local services database, else -1.
func a2port(s string) int {
	t := strings.TrimLeft(s, " \t\n\v\f\r")
	if t != "" {
		digits := t
		if digits[0] == '+' || digits[0] == '-' {
			digits = digits[1:]
		}
		if digits != "" && strings.Trim(digits, "0123456789") == "" {
			n, err := strconv.ParseInt(t, 10, 64)
			if err != nil || n < 0 || n > 65535 {
				return -1
			}
			return int(n)
		}
	}
	if port, ok := lookupService(s); ok {
		return port
	}
	return -1
}

// dollarExpand is OpenSSH's dollar_expand: only ${NAME} is special.
func dollarExpand(s string, getenv func(string) (string, bool)) (string, bool) {
	if getenv == nil {
		getenv = os.LookupEnv
	}
	var out strings.Builder
	missing := false
	for i := 0; i < len(s); i++ {
		if s[i] == '$' && i+1 < len(s) && s[i+1] == '{' {
			end := strings.IndexByte(s[i+2:], '}')
			if end <= 0 {
				return "", false
			}
			value, ok := getenv(s[i+2 : i+2+end])
			if !ok {
				missing = true
			}
			out.WriteString(value)
			i += 2 + end
			continue
		}
		out.WriteByte(s[i])
	}
	return out.String(), !missing
}

// ErrBadSpec is returned by callers that need an error value.
var ErrBadSpec = errors.New("bad forwarding specification")
