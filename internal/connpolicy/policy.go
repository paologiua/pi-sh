// Package connpolicy validates the OpenSSH connection-policy settings that the
// Connect clients can honour, and rejects explicitly those they cannot. It is
// offline: it never prompts, reads files or contacts a host.
package connpolicy

import (
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"pi-sh/internal/account"
	"pi-sh/internal/sshdest"
)

// MaxConnectionAttempts bounds automatic re-establishment. Each attempt is a
// new Connect signaling request; a larger budget would only add service load.
const MaxConnectionAttempts = 10

var (
	// ErrUser is returned for a Unix-style remote user: destination user@host
	// (always), or a URI user, -l or User that is not an email address. The
	// Connect agent opens its shell as one fixed device account that the
	// client can neither select nor verify. The SSH user's role is taken by
	// the Raspberry Pi ID account, written email::device or -l/User email.
	ErrUser = errors.New("Unix-style remote user selection is not supported: Connect opens the shell as the device's fixed agent account; to choose the Raspberry Pi ID account use email::device, -l email (pi-sh) or User email")
	// ErrBrowserAccount is returned when an explicit account is combined with
	// the browser companion, which uses whichever account Chrome is signed in
	// to; pi-sh has no observed way to select or verify that account.
	ErrBrowserAccount = errors.New("--browser cannot select or verify the Raspberry Pi ID account (the companion uses the account signed in to Chrome); remove the account (email::, -l, User) or use --direct-login")
	// ErrPort is returned for any explicit port: URI :port, -p/-P or Port.
	// Connect reaches devices through its own service, not an SSH port.
	ErrPort = errors.New("port selection is not supported: Connect reaches the device through its service, not an SSH port; remove the port from the destination, -p/-P or Port")
	// ErrAddress is returned when the target is an IP address literal.
	ErrAddress = errors.New("network addresses do not select Connect devices; use the device name, its ID or a HostName alias")
)

// keys is ordered so that the first reported refusal is deterministic.
var keys = []string{"user", "port", "batchmode", "numberofpasswordprompts", "connecttimeout",
	"connectionattempts", "serveraliveinterval", "serveralivecountmax", "tcpkeepalive", "channeltimeout"}

// Known reports whether key (lowercase) is a connection-policy directive.
func Known(key string) bool {
	for _, k := range keys {
		if k == key {
			return true
		}
	}
	return false
}

// Policy is the effective connection policy. The zero value is not the
// default; use Default.
type Policy struct {
	// Account is the explicit Raspberry Pi ID account (destination email::,
	// URI user, -l or User). DefaultAccount is the local default
	// (PI_SH_ACCOUNT or the account file), used only by direct login.
	Account, DefaultAccount string
	BatchMode               bool
	// PasswordPrompts is 0 (never prompt for a password) or 1 (one attempt).
	PasswordPrompts int
	// ConnectTimeoutSeconds is -1 when unset ("none"); 0 means no extra bound.
	ConnectTimeoutSeconds int
	ConnectionAttempts    int
	// ServerAliveIntervalSeconds 0 disables the configured liveness bound.
	ServerAliveIntervalSeconds int
	ServerAliveCountMax        int
}

func Default() Policy {
	return Policy{PasswordPrompts: 1, ConnectTimeoutSeconds: -1, ConnectionAttempts: 1, ServerAliveCountMax: 3}
}

// Validate checks only OpenSSH value syntax, so an inactive Host block can
// hold a setting that would be rejected if it applied to this destination.
func Validate(key, value string) error {
	switch key {
	case "user":
		if value == "" || strings.IndexFunc(value, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
			return errors.New("invalid User value")
		}
	case "port":
		if !sshdest.ValidPort(value) {
			return errors.New("invalid Port value")
		}
	case "batchmode", "tcpkeepalive":
		_, err := flag(key, value)
		return err
	case "numberofpasswordprompts", "serveralivecountmax":
		_, err := integer(key, value, 0)
		return err
	case "connectionattempts":
		_, err := integer(key, value, 1)
		return err
	case "connecttimeout":
		if value != "none" {
			_, err := duration(key, value)
			return err
		}
	case "serveraliveinterval":
		if value != "none" {
			_, err := duration(key, value)
			return err
		}
	case "channeltimeout":
		if value != "none" {
			kind, interval, ok := strings.Cut(value, "=")
			if !ok || kind == "" {
				return errors.New("invalid ChannelTimeout value")
			}
			if _, err := duration(key, interval); err != nil {
				return err
			}
		}
	default:
		return errors.New("unsupported SSH configuration directive")
	}
	return nil
}

// FromValues builds the effective policy from first-obtained values (CLI,
// destination and configuration already merged). Settings with no
// demonstrated Connect equivalent are rejected rather than ignored.
func FromValues(values map[string]string) (Policy, error) {
	p := Default()
	for _, key := range keys {
		value, set := values[key]
		if !set {
			continue
		}
		if err := Validate(key, value); err != nil {
			return p, err
		}
		switch key {
		case "user":
			if account.Validate(value) != nil {
				return p, ErrUser
			}
			p.Account = value
		case "port":
			return p, ErrPort
		case "batchmode":
			p.BatchMode, _ = flag(key, value)
		case "numberofpasswordprompts":
			n, _ := integer(key, value, 0)
			if n > 1 {
				return p, errors.New("NumberOfPasswordPrompts above 1 is not supported: direct login submits one password per process and never retries automatically")
			}
			p.PasswordPrompts = n
		case "connecttimeout":
			if value != "none" {
				p.ConnectTimeoutSeconds, _ = duration(key, value)
			}
		case "connectionattempts":
			p.ConnectionAttempts, _ = integer(key, value, 1)
			if p.ConnectionAttempts > MaxConnectionAttempts {
				return p, fmt.Errorf("ConnectionAttempts above %d is not supported: each attempt is a new Connect signaling request", MaxConnectionAttempts)
			}
		case "serveraliveinterval":
			if value != "none" {
				p.ServerAliveIntervalSeconds, _ = duration(key, value)
			}
		case "serveralivecountmax":
			p.ServerAliveCountMax, _ = integer(key, value, 0)
		case "tcpkeepalive":
			if on, _ := flag(key, value); !on {
				return p, errors.New("TCPKeepAlive no is not supported: Connect HTTPS requests use the platform TCP keepalive and the WebRTC data path has no TCP connection to configure")
			}
		case "channeltimeout":
			if value != "none" {
				return p, errors.New("ChannelTimeout is not implemented: idle session channels are not closed automatically; use none")
			}
		}
	}
	return p, nil
}

// DestinationUser applies the account policy to a parsed destination before
// precedence: a plain OpenSSH user@host user is always refused (even when it
// would lose to -l or an earlier -o User), and the email::device form must
// carry a valid email. URI users are judged later as ordinary User values.
func DestinationUser(d sshdest.Destination) error {
	switch {
	case d.User == "" || d.URI:
		return nil
	case !d.Account:
		return ErrUser
	}
	return account.Validate(d.User)
}

// LoginAccount is the account direct login uses instead of prompting for an
// email, or "" when the person must type it.
func (p Policy) LoginAccount() string {
	if p.Account != "" {
		return p.Account
	}
	return p.DefaultAccount
}

// Target rejects effective device targets that are network address literals.
func Target(target string) error {
	if sshdest.AddressLiteral(target) {
		return ErrAddress
	}
	return nil
}

// Admit applies the authentication-related policy before any prompt, local
// listener or network request. No Connect sign-in method is unattended: both
// direct login and the browser companion need a person.
func (p Policy) Admit(direct bool) error {
	if p.BatchMode {
		return errors.New("BatchMode yes: Connect sign-in always needs a person (direct password/one-time code or browser companion) and no unattended method exists; refusing to prompt")
	}
	if direct && p.PasswordPrompts == 0 {
		return errors.New("NumberOfPasswordPrompts 0: direct login requires a password prompt; refusing to prompt")
	}
	if !direct && p.Account != "" {
		return ErrBrowserAccount
	}
	if !direct && p.ConnectionAttempts > 1 {
		return errors.New("ConnectionAttempts above 1 requires --direct-login: the browser companion signals only once")
	}
	return nil
}

// ConnectTimeout is the per-attempt establishment bound; 0 means only the
// built-in phase limits apply.
func (p Policy) ConnectTimeout() time.Duration {
	if p.ConnectTimeoutSeconds <= 0 {
		return 0
	}
	return time.Duration(p.ConnectTimeoutSeconds) * time.Second
}

// DeadPeerTimeout is how long the transport tolerates silence from the peer
// before closing, following ssh_config's ServerAliveInterval * CountMax
// (a count of 0 disconnects after one interval). 0 keeps transport defaults.
func (p Policy) DeadPeerTimeout() time.Duration {
	if p.ServerAliveIntervalSeconds <= 0 {
		return 0
	}
	count := max(p.ServerAliveCountMax, 1)
	total := int64(p.ServerAliveIntervalSeconds) * int64(count)
	if total > int64(math.MaxInt64/int64(time.Second)) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(total) * time.Second
}

// WriteConfig prints the accepted connection-policy fields in ssh -G form.
func (p Policy) WriteConfig(w io.Writer) error {
	yes := map[bool]string{true: "yes", false: "no"}
	timeout := "none"
	if p.ConnectTimeoutSeconds >= 0 {
		timeout = strconv.Itoa(p.ConnectTimeoutSeconds)
	}
	_, err := fmt.Fprintf(w, "batchmode %s\nconnectionattempts %d\nconnecttimeout %s\nserveralivecountmax %d\nserveraliveinterval %d\ntcpkeepalive yes\nchanneltimeout none\n",
		yes[p.BatchMode], p.ConnectionAttempts, timeout, p.ServerAliveCountMax, p.ServerAliveIntervalSeconds)
	return err
}

func flag(key, value string) (bool, error) {
	switch strings.ToLower(value) {
	case "yes", "true":
		return true, nil
	case "no", "false":
		return false, nil
	}
	return false, fmt.Errorf("%s requires yes or no", name(key))
}

// integer follows OpenSSH strtonum: optional sign, decimal digits, int range.
func integer(key, value string, minimum int) (int, error) {
	n, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid %s value", name(key))
	}
	if int(n) < minimum {
		return 0, fmt.Errorf("%s value too small", name(key))
	}
	return int(n), nil
}

// duration follows OpenSSH convtime: one or more decimal numbers, each with an
// optional s/m/h/d/w unit (case-insensitive), totalling at most INT_MAX seconds.
func duration(key, value string) (int, error) {
	invalid := fmt.Errorf("invalid %s time value", name(key))
	if value == "" {
		return 0, invalid
	}
	total := int64(0)
	for i := 0; i < len(value); {
		start := i
		for i < len(value) && value[i] >= '0' && value[i] <= '9' {
			i++
		}
		if i == start {
			return 0, invalid
		}
		n, err := strconv.ParseInt(value[start:i], 10, 64)
		if err != nil || n > math.MaxInt32 {
			return 0, invalid
		}
		unit := int64(1)
		if i < len(value) {
			switch value[i] {
			case 's', 'S':
			case 'm', 'M':
				unit = 60
			case 'h', 'H':
				unit = 3600
			case 'd', 'D':
				unit = 86400
			case 'w', 'W':
				unit = 604800
			default:
				return 0, invalid
			}
			i++
		}
		total += n * unit
		if total > math.MaxInt32 {
			return 0, invalid
		}
	}
	return int(total), nil
}

func name(key string) string {
	switch key {
	case "batchmode":
		return "BatchMode"
	case "tcpkeepalive":
		return "TCPKeepAlive"
	case "numberofpasswordprompts":
		return "NumberOfPasswordPrompts"
	case "serveralivecountmax":
		return "ServerAliveCountMax"
	case "connectionattempts":
		return "ConnectionAttempts"
	case "connecttimeout":
		return "ConnectTimeout"
	case "serveraliveinterval":
		return "ServerAliveInterval"
	case "channeltimeout":
		return "ChannelTimeout"
	}
	return key
}
