// Package sshdest parses OpenSSH destination and remote-operand syntax for ssh,
// scp and sftp. It is purely syntactic: it never resolves names, opens files or
// contacts a host, and it does not decide whether a parsed user or port can be
// honoured. That policy belongs to the caller (see internal/connpolicy).
package sshdest

import (
	"errors"
	"net"
	"strconv"
	"strings"
)

// Destination is one parsed target. Empty User/Port mean "not specified".
// Account is true when User came from the pi-sh account form "email::device"
// (a Raspberry Pi ID account), as opposed to an OpenSSH "user@host" user.
type Destination struct {
	User    string
	Host    string
	Port    string
	Path    string
	HasPath bool
	URI     bool
	Account bool
}

// accountSplit recognizes the pi-sh account form "email::rest", where the
// part before the first "::" contains '@'. It is not OpenSSH syntax (ssh
// would read a user "local-part" and a host "domain::rest"); the owner chose
// it so a Raspberry Pi ID email can name the account as an SSH user would.
func accountSplit(arg string) (account, rest string, ok bool) {
	i := strings.Index(arg, "::")
	if i <= 0 || !strings.Contains(arg[:i], "@") {
		return "", "", false
	}
	return arg[:i], arg[i+2:], true
}

// errAccountHost covers "email::" without a device and devices containing '@'.
var errAccountHost = errors.New("account destination needs a device after '::' (email::device), and the device must not contain '@'")

// SSH parses an ssh(1) destination: ssh://[user@]host[:port][/] or
// [user@]host. As in OpenSSH 10.2p1, the URI scheme is case-sensitive, the
// plain form splits at the last '@', and a plain host keeps any brackets.
func SSH(arg string) (Destination, error) {
	if strings.HasPrefix(arg, "ssh://") {
		d, err := parseURI(arg[len("ssh://"):])
		if err != nil {
			return d, err
		}
		if d.HasPath {
			return Destination{}, errors.New("ssh:// destination must not contain a path")
		}
		return d, nil
	}
	if account, host, ok := accountSplit(arg); ok {
		if host == "" || strings.Contains(host, "@") {
			return Destination{}, errAccountHost
		}
		return Destination{User: account, Host: host, Account: true}, nil
	}
	var d Destination
	if i := strings.LastIndexByte(arg, '@'); i >= 0 {
		if i == 0 {
			return Destination{}, errors.New("destination has an empty user name")
		}
		d.User, arg = arg[:i], arg[i+1:]
	}
	if arg == "" {
		return Destination{}, errors.New("destination has no host")
	}
	d.Host = arg
	return d, nil
}

// Copy parses an scp(1) operand. remote is false for a local path, including
// paths whose first colon follows a slash, a leading colon, or a Windows drive.
func Copy(arg string) (d Destination, remote bool, err error) {
	if strings.HasPrefix(arg, "scp://") {
		d, err = parseURI(arg[len("scp://"):])
		if err != nil {
			return Destination{}, false, err
		}
		if !d.HasPath || d.Path == "" {
			d.Path, d.HasPath = ".", true
		}
		return d, true, nil
	}
	// Windows absolute drive paths are local even when parsed on Unix.
	if len(arg) >= 3 && arg[1] == ':' && isAlpha(arg[0]) && (arg[2] == '/' || arg[2] == '\\') {
		return Destination{}, false, nil
	}
	i := colon(arg)
	if i < 0 {
		return Destination{}, false, nil
	}
	if account, rest, ok := accountSplit(arg); ok && i == len(account) {
		// email::device:path; the device:path part follows scp's colon rules.
		j := colon(rest)
		if j < 0 {
			return Destination{}, false, errors.New("account copy operand needs a device and a path: email::device:path")
		}
		d = Destination{User: account, Host: cleanHost(rest[:j]), Path: rest[j+1:], HasPath: true, Account: true}
		if d.Path == "" {
			d.Path = "."
		}
		if d.Host == "" || strings.Contains(d.Host, "@") {
			return Destination{}, false, errAccountHost
		}
		return d, true, nil
	}
	d.Path, d.HasPath = arg[i+1:], true
	if d.Path == "" {
		d.Path = "."
	}
	host := arg[:i]
	if j := strings.LastIndexByte(host, '@'); j >= 0 {
		// scp ignores an empty user name before '@'.
		d.User, host = host[:j], host[j+1:]
	}
	d.Host = cleanHost(host)
	if d.Host == "" {
		return Destination{}, false, errors.New("remote operand has no host")
	}
	return d, true, nil
}

// SFTP parses an sftp(1) destination: sftp://[user@]host[:port][/path] or
// [user@]host[:path]. Without a colon the whole argument names the host.
func SFTP(arg string) (Destination, error) {
	if strings.HasPrefix(arg, "sftp://") {
		return parseURI(arg[len("sftp://"):])
	}
	var d Destination
	if account, rest, ok := accountSplit(arg); ok && colon(arg) == len(account) {
		d.User, d.Account = account, true
		host := rest
		if j := colon(rest); j >= 0 {
			host, d.Path = rest[:j], rest[j+1:]
			d.HasPath = d.Path != ""
		}
		d.Host = cleanHost(host)
		if d.Host == "" || strings.Contains(d.Host, "@") {
			return Destination{}, errAccountHost
		}
		return d, nil
	}
	host := arg
	if i := colon(arg); i >= 0 {
		host, d.Path = arg[:i], arg[i+1:]
		d.HasPath = d.Path != ""
	}
	if j := strings.LastIndexByte(host, '@'); j >= 0 {
		if j == 0 {
			return Destination{}, errors.New("destination has an empty user name")
		}
		d.User, host = host[:j], host[j+1:]
	}
	d.Host = cleanHost(host)
	if d.Host == "" {
		return Destination{}, errors.New("destination has no host")
	}
	return d, nil
}

// AddressLiteral reports whether host is an IP address, optionally bracketed
// and, for IPv6, carrying a zone. Connect selects devices by name or ID.
func AddressLiteral(host string) bool {
	h := host
	if len(h) >= 2 && h[0] == '[' && h[len(h)-1] == ']' {
		h = h[1 : len(h)-1]
	}
	if i := strings.IndexByte(h, '%'); i >= 0 && strings.Contains(h[:i], ":") {
		h = h[:i]
	}
	return net.ParseIP(h) != nil
}

// ValidPort follows OpenSSH a2port syntax: a decimal number 1..65535 (strtonum
// accepts a leading '+') or a service name. Service names are not resolved.
func ValidPort(s string) bool {
	digits := strings.TrimPrefix(s, "+")
	if digits != "" && strings.Trim(digits, "0123456789") == "" {
		n, err := strconv.ParseUint(digits, 10, 32)
		return err == nil && n >= 1 && n <= 65535
	}
	if s == "" || !isAlpha(s[0]) {
		return false
	}
	return strings.IndexFunc(s, func(r rune) bool {
		return !(r < 128 && (isAlpha(byte(r)) || (r >= '0' && r <= '9') || r == '-' || r == '_'))
	}) < 0
}

// parseURI implements the observed OpenSSH parse_uri grammar after the scheme:
// [user[;params]@]host[:port][/path], user and path percent-decoded, the
// host restricted to domain syntax (bracketed or not) and a nonzero port.
func parseURI(rest string) (Destination, error) {
	d := Destination{URI: true}
	if i := strings.IndexByte(rest, '@'); i >= 0 {
		info := rest[:i]
		if j := strings.IndexByte(info, ';'); j >= 0 {
			info = info[:j]
		}
		if info == "" {
			return Destination{}, errors.New("URI has an empty user name")
		}
		user, err := urlDecode(info)
		if err != nil {
			return Destination{}, err
		}
		d.User, rest = user, rest[i+1:]
	}
	var end int
	if strings.HasPrefix(rest, "[") {
		closing := strings.IndexByte(rest, ']')
		if closing < 0 {
			return Destination{}, errors.New("URI host has an unterminated bracket")
		}
		end = closing + 1
	} else if end = strings.IndexAny(rest, ":/"); end < 0 {
		end = len(rest)
	}
	host, tail := rest[:end], ""
	delim := byte(0)
	if end < len(rest) {
		delim = rest[end]
		if delim != ':' && delim != '/' {
			return Destination{}, errors.New("invalid URI host")
		}
		tail = rest[end+1:]
	}
	d.Host = cleanHost(host)
	if !validDomain(d.Host) {
		return Destination{}, errors.New("URI host must be a domain-style name; use the plain destination form for other Connect names")
	}
	if tail != "" && delim == ':' {
		port := tail
		tail = ""
		if i := strings.IndexByte(port, '/'); i >= 0 {
			port, tail = port[:i], port[i+1:]
		}
		if !ValidPort(port) {
			return Destination{}, errors.New("invalid URI port")
		}
		d.Port = port
	}
	if tail != "" {
		path, err := urlDecode(tail)
		if err != nil {
			return Destination{}, err
		}
		d.Path, d.HasPath = path, true
	}
	return d, nil
}

// colon mirrors OpenSSH colon(): a leading colon is part of a local filename,
// a bracketed host may contain colons, and a slash before any colon means local.
func colon(s string) int {
	if s == "" || s[0] == ':' {
		return -1
	}
	bracket := s[0] == '['
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '@' && i+1 < len(s) && s[i+1] == '[' {
			bracket = true
		}
		if c == ']' && i+1 < len(s) && s[i+1] == ':' && bracket {
			return i + 1
		}
		if c == ':' && !bracket {
			return i
		}
		if c == '/' {
			return -1
		}
	}
	return -1
}

func cleanHost(h string) string {
	if len(h) >= 2 && h[0] == '[' && h[len(h)-1] == ']' {
		return h[1 : len(h)-1]
	}
	return h
}

// validDomain follows the observed OpenSSH URI host check: an alphanumeric
// first byte, then alphanumerics, '-', '_' or '.', without consecutive dots.
func validDomain(h string) bool {
	if h == "" || !(isAlpha(h[0]) || isDigit(h[0])) {
		return false
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		if !(isAlpha(c) || isDigit(c) || c == '-' || c == '_' || c == '.') || (c == '.' && i > 0 && h[i-1] == '.') {
			return false
		}
	}
	return true
}

func urlDecode(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '%':
			if i+2 >= len(s) || !isHex(s[i+1]) || !isHex(s[i+2]) {
				return "", errors.New("invalid URI percent-encoding")
			}
			v, _ := strconv.ParseUint(s[i+1:i+3], 16, 8)
			if v == 0 {
				return "", errors.New("invalid URI percent-encoding")
			}
			b.WriteByte(byte(v))
			i += 2
		case '+':
			b.WriteByte(' ')
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), nil
}

func isAlpha(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isHex(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
