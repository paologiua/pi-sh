package sshconfig

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strconv"
	"strings"

	"pi-sh/internal/buildinfo"
)

// Session types reported to Match sessiontype, as in OpenSSH 10.2.
const (
	SessionShell     = "shell"
	SessionExec      = "exec"
	SessionSubsystem = "subsystem"
	SessionNone      = "none"
)

// Context is the invocation that Match criteria evaluate. It never contains
// credentials. The SSH user's role is played by the Raspberry Pi ID account
// email; the device's shell account is fixed by the agent and unknown.
type Context struct {
	// Host is the original destination as typed (Host, originalhost and %n).
	Host string
	// Overrides are already obtained CLI values, seeded before any file line.
	Overrides map[string]string
	// SessionType is shell, exec, subsystem or none. Empty selects exec when
	// Command is nonempty and shell otherwise, like ssh(1) without -s/-N.
	SessionType string
	// Command is the remote command words joined by single spaces, or the
	// subsystem name, as matched by Match command.
	Command string
	// DefaultUser is the local default account (PI_SH_ACCOUNT or the account
	// file). Like OpenSSH's local login name default, Match user and %r see
	// it when no User/-l/destination account has been obtained.
	DefaultUser string
	// Stderr receives Match exec standard error; nil selects os.Stderr.
	Stderr io.Writer
	// Lists receives active list directives (LocalForward, DynamicForward,
	// RemoteForward, PermitRemoteOpen) in file order; both Match final passes
	// append, as ssh adds to its lists twice and drops duplicates. With nil they
	// are validated and dropped, as scp and sftp run ssh with
	// ClearAllForwardings=yes.
	Lists *[]Directive

	env *localEnv // test hook; nil selects the real local system
}

// localEnv isolates local account, host and interface lookups for tests.
type localEnv struct {
	user     func() (string, error)
	hostname func() (string, error)
	uid      func() int
	addrs    func() ([]netip.Addr, error)
	run      func(command string, stderr io.Writer) (int, error)
	goos     string
}

func systemEnv() *localEnv {
	return &localEnv{user: localUser, hostname: os.Hostname, uid: os.Getuid, addrs: localAddrs, run: runShell, goos: runtime.GOOS}
}

func localUser() (string, error) {
	u, err := user.Current()
	if err != nil || u.Username == "" || strings.IndexFunc(u.Username, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
		return "", errors.New("local user name unavailable for Match")
	}
	return u.Username, nil
}

// OpenSSH enumerates getifaddrs(3) entries that are up. A scoped (link-local)
// IPv6 numeric form carries %zone and never parses as a CIDR match there, so
// those addresses are skipped for equivalent results.
func localAddrs() ([]netip.Addr, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, errors.New("cannot enumerate local network interfaces for Match localnetwork")
	}
	var out []netip.Addr
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			return nil, errors.New("cannot enumerate local network addresses for Match localnetwork")
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			default:
				continue
			}
			if v4 := ip.To4(); v4 != nil {
				out = append(out, netip.AddrFrom4([4]byte(v4)))
			} else if len(ip) == net.IPv6len && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsInterfaceLocalMulticast() {
				out = append(out, netip.AddrFrom16([16]byte(ip)))
			}
		}
	}
	return out, nil
}

// runShell follows OpenSSH's execute_in_shell: $SHELL (or /bin/sh) -c command,
// stdin and stdout on the null device, stderr inherited, environment unchanged.
func runShell(command string, stderr io.Writer) (int, error) {
	shell, ok := os.LookupEnv("SHELL")
	if !ok {
		shell = "/bin/sh"
	}
	if info, err := os.Stat(shell); err != nil || info.IsDir() || info.Mode().Perm()&0111 == 0 {
		return 0, errors.New("Match exec shell is not executable")
	}
	cmd := &exec.Cmd{Path: shell, Args: []string{shell, "-c", command}, Stderr: stderr}
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.Exited() {
		return exit.ExitCode(), nil
	}
	return 0, errors.New("Match exec command exited abnormally")
}

// Release is the client version string compared by Match version. It names
// this unofficial client, never OpenSSH, so "Match version OpenSSH_*" is false.
func Release() string {
	return "pi-sh_" + buildinfo.Version()
}

// Criteria accepting an argument, including the criterion=argument spelling.
var argumentCriteria = []string{"host", "originalhost", "user", "localuser", "localnetwork", "version", "sessiontype", "command", "tagged", "exec"}

func argumentCriterion(name string) bool {
	for _, c := range argumentCriteria {
		if strings.EqualFold(name, c) {
			return true
		}
	}
	return false
}

// match evaluates one Match line with OpenSSH 10.2 semantics: criteria are
// ANDed, evaluation continues after a failure so every criterion is validated,
// and exec runs only while all preceding criteria have matched.
func (p *parser) match(args []string) (bool, error) {
	result, attributes := true, 0
	for i := 0; i < len(args); i++ {
		attrib := args[i]
		if strings.HasPrefix(attrib, "#") {
			break
		}
		original := attrib
		negate := strings.HasPrefix(attrib, "!")
		if negate {
			attrib = attrib[1:]
		}
		if strings.EqualFold(attrib, "all") {
			rest := args[i+1:]
			if attributes > 1 || len(rest) > 0 && rest[0] != "" && !strings.HasPrefix(rest[0], "#") {
				return false, errors.New("'all' cannot be combined with other Match attributes")
			}
			if len(rest) > 1 && rest[0] == "" && !strings.HasPrefix(rest[1], "#") {
				return false, errors.New("Match has extra arguments at end of line")
			}
			if result {
				result = !negate
			}
			return result, nil
		}
		attributes++
		if strings.EqualFold(attrib, "canonical") || strings.EqualFold(attrib, "final") {
			if strings.EqualFold(attrib, "final") && !negate {
				p.wantFinal = true
			}
			if p.final == negate {
				result = false
			}
			continue
		}
		var arg string
		if name, value, ok := strings.Cut(attrib, "="); ok && argumentCriterion(name) {
			attrib, arg = name, value
		} else {
			if i+1 == len(args) || strings.HasPrefix(args[i+1], "#") {
				return false, fmt.Errorf("missing argument for Match criterion %q", truncate(original))
			}
			i++
			arg = args[i]
		}
		lower := strings.ToLower(attrib)
		if arg == "" && lower != "tagged" && lower != "command" && argumentCriterion(lower) {
			return false, fmt.Errorf("missing Match criteria for %s", lower)
		}
		var r bool
		switch lower {
		case "host":
			host, err := p.currentHost()
			if err != nil {
				return false, err
			}
			r = matchPatternList(strings.ToLower(host), arg, true) == 1
		case "originalhost":
			r = matchPatternList(strings.ToLower(p.ctx.Host), arg, true) == 1
		case "user":
			name, err := p.remoteUser()
			if err != nil {
				return false, err
			}
			r = matchPatternList(name, arg, false) == 1
		case "localuser":
			name, err := p.env.user()
			if err != nil {
				return false, err
			}
			r = matchPatternList(name, arg, false) == 1
		case "localnetwork":
			prefixes, err := parseCIDRList(arg)
			if err != nil {
				return false, err
			}
			addrs, err := p.env.addrs()
			if err != nil {
				return false, err
			}
			r = containsAny(prefixes, addrs)
		case "tagged":
			r = matchEmptyOrList(p.values["tag"], arg)
		case "command":
			r = matchEmptyOrList(p.ctx.Command, arg)
		case "sessiontype":
			r = matchPatternList(p.sessionType(), arg, false) == 1
		case "version":
			r = matchPatternList(Release(), arg, false) == 1
		case "exec":
			// Tokens are validated even when exec is skipped, which is stricter
			// than OpenSSH but never hides an unsupported token.
			command, err := p.execCommand(arg)
			if err != nil {
				return false, err
			}
			if !result {
				continue
			}
			if p.env.goos == "windows" {
				return false, errors.New("Match exec is not implemented on Windows")
			}
			stderr := p.ctx.Stderr
			if stderr == nil {
				stderr = os.Stderr
			}
			status, err := p.env.run(command, stderr)
			if err != nil {
				return false, err
			}
			r = status == 0
		default:
			return false, fmt.Errorf("unsupported Match attribute %q", truncate(attrib))
		}
		if r == negate {
			result = false
		}
	}
	if attributes == 0 {
		return false, errors.New("one or more attributes required for Match")
	}
	return result, nil
}

func truncate(s string) string {
	if len(s) > 64 {
		return s[:64] + "..."
	}
	return s
}

// An unset (empty) tag or command matches only the empty pattern.
func matchEmptyOrList(value, pattern string) bool {
	if value == "" {
		return pattern == ""
	}
	return matchPatternList(value, pattern, false) == 1
}

// matchPatternList mirrors OpenSSH match_pattern_list: comma-separated
// subpatterns, '!' negation that wins over positive matches, optional pattern
// lowercasing, and no match for a subpattern of 1023 bytes or more.
func matchPatternList(value, pattern string, lower bool) int {
	positive := 0
	for i := 0; i < len(pattern); {
		negated := pattern[i] == '!'
		if negated {
			i++
		}
		end := strings.IndexByte(pattern[i:], ',')
		if end < 0 {
			end = len(pattern) - i
		}
		sub := pattern[i : i+end]
		if len(sub) >= 1023 {
			return 0
		}
		i += end
		if i < len(pattern) {
			i++
		}
		if lower {
			sub = asciiLower(sub)
		}
		if match(sub, value) {
			if negated {
				return -1
			}
			positive = 1
		}
	}
	return positive
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// currentHost is the HostName-expanded target at this line; in the final pass
// it is the effective target fixed after the first pass.
func (p *parser) currentHost() (string, error) {
	if name := p.values["hostname"]; name != "" {
		return Hostname(name, p.ctx.Host)
	}
	return p.ctx.Host, nil
}

// remoteUser follows OpenSSH's Match user and %r: the user obtained so far
// (User/-l/destination; for pi-sh the Raspberry Pi ID account email), else
// the default. pi-sh's default is the local default account when one is
// configured, otherwise the local login name as in OpenSSH.
func (p *parser) remoteUser() (string, error) {
	if name, ok := p.values["user"]; ok {
		return name, nil
	}
	if p.ctx.DefaultUser != "" {
		return p.ctx.DefaultUser, nil
	}
	return p.env.user()
}

func (p *parser) sessionType() string {
	if p.ctx.SessionType != "" {
		return p.ctx.SessionType
	}
	if p.ctx.Command != "" {
		return SessionExec
	}
	return SessionShell
}

// OpenSSH valid_hostname/valid_ruser reject these in command-line names. pi-sh
// Connect names may contain spaces, so they are refused only where a token
// would otherwise inject them into a local shell command.
func shellSafe(value string, user bool) bool {
	if strings.HasPrefix(value, "-") {
		return false
	}
	reject := "'`\"$\\;&<>|(){},"
	if user {
		reject = "'`\";&<>|(){}"
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c < 32 || c == 127 || strings.IndexByte(reject, c) >= 0 {
			return false
		}
		if !user && (c == ' ' || c == '\t' || c == '\v' || c == '\f') {
			return false
		}
		if user && (c == ' ' || c == '\t') && i+1 < len(value) && value[i+1] == '-' {
			return false
		}
		if user && c == '\\' && i+1 == len(value) {
			return false
		}
	}
	return true
}

// execCommand performs OpenSSH's one-pass Match exec percent expansion for the
// tokens with a pi-sh meaning. SSH transport tokens (%p, %C, %k, %j) and
// unknown tokens fail rather than inventing port, host-key or jump values.
func (p *parser) execCommand(value string) (string, error) {
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] != '%' {
			out.WriteByte(value[i])
			continue
		}
		i++
		if i == len(value) {
			return "", errors.New("incomplete Match exec token")
		}
		var token string
		var err error
		switch value[i] {
		case '%':
			token = "%"
		case 'h':
			token, err = p.currentHost()
			if err == nil && !shellSafe(token, false) {
				err = errors.New("Match exec refuses to expand %h containing whitespace or shell metacharacters")
			}
		case 'n':
			token = p.ctx.Host
			if !shellSafe(token, false) {
				err = errors.New("Match exec refuses to expand %n containing whitespace or shell metacharacters")
			}
		case 'r':
			token, err = p.remoteUser()
			if err == nil && !shellSafe(token, true) {
				err = errors.New("Match exec refuses to expand %r containing shell metacharacters")
			}
		case 'u':
			token, err = p.env.user()
		case 'd':
			token, err = p.homeDir()
		case 'i':
			token = strconv.Itoa(p.env.uid())
		case 'l', 'L':
			token, err = p.env.hostname()
			if err != nil || token == "" {
				err = errors.New("local host name unavailable for Match exec")
			} else if value[i] == 'L' {
				token, _, _ = strings.Cut(token, ".")
			}
		case 'p', 'C', 'k', 'j':
			return "", fmt.Errorf("Match exec token %%%c has no pi-sh meaning (no SSH port, host key alias or jump host)", value[i])
		default:
			return "", errors.New("unsupported Match exec token")
		}
		if err != nil {
			return "", err
		}
		out.WriteString(token)
		if out.Len() > 64<<10 {
			return "", errors.New("Match exec command exceeds size limit")
		}
	}
	return out.String(), nil
}

const cidrChars = "0123456789abcdefABCDEF.:/"

// parseCIDRList mirrors OpenSSH addr_match_cidr_list validation: no negation,
// no empty entries, limited characters/length, and no host bits past the mask.
func parseCIDRList(list string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, entry := range strings.Split(list, ",") {
		invalid := errors.New("invalid Match localnetwork CIDR list")
		if entry == "" || len(entry) > 46+3 || strings.Trim(entry, cidrChars) != "" {
			return nil, invalid
		}
		text, mask, hasMask := strings.Cut(entry, "/")
		addr, err := netip.ParseAddr(text)
		if err != nil {
			return nil, invalid
		}
		bits := addr.BitLen()
		if hasMask {
			if mask == "" || strings.Trim(mask, "0123456789") != "" {
				return nil, invalid
			}
			n, err := strconv.ParseUint(mask, 10, 32)
			if err != nil || n > uint64(addr.BitLen()) {
				return nil, invalid
			}
			bits = int(n)
		}
		prefix := netip.PrefixFrom(addr, bits)
		if prefix.Masked().Addr() != addr {
			return nil, invalid
		}
		out = append(out, prefix)
	}
	return out, nil
}

func containsAny(prefixes []netip.Prefix, addrs []netip.Addr) bool {
	for _, a := range addrs {
		for _, p := range prefixes {
			if p.Contains(a) {
				return true
			}
		}
	}
	return false
}
