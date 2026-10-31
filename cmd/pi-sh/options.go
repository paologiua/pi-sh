package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"strconv"
	"strings"

	"pi-sh/internal/account"
	"pi-sh/internal/connpolicy"
	"pi-sh/internal/diag"
	"pi-sh/internal/forwardspec"
	"pi-sh/internal/sshconfig"
	"pi-sh/internal/sshdest"
)

// This is a deliberately bounded OpenSSH option subset. No SSH config files or
// local commands are read or executed implicitly.
type options struct {
	browser, direct, broker, legacy, dump bool
	// legacyShell selects the agent's raw shell stream instead of the login
	// shell launched through the in-memory TTY/no-TTY adapters.
	legacyShell          bool
	stdinNull            bool
	escape               string
	tty                  string
	args                 []string
	configFile, hostname string
	tag                  string
	tagSet               bool
	inspection, query    string
	// connection holds first-obtained connection-policy values (User, Port,
	// BatchMode, timeouts ...); policy is the validated result.
	connection map[string]string
	policy     connpolicy.Policy
	logging    logOptions
	// remoteCommand is the first obtained RemoteCommand (raw; "none" = no
	// command); it supplies the command when none is given on the CLI.
	remoteCommand string
	// destinationParsed records that the destination's account/port were
	// seeded at its command-line position (first obtained, as in ssh.c).
	destinationParsed bool
	// fwd holds -L/-D/-W and the forwarding directives; features holds the
	// accepted (off) values of refused features and SessionType, so -G
	// prints them. Remote (-R) forwardings with a remote socket path are refused.
	fwd      forwardspec.Settings
	features forwardspec.Features
}

// noSession reports ssh's SessionType none (-N, -W): no shell or command runs,
// and any command words are ignored, as ssh(1) ignores them.
func (o options) noSession() bool { return o.features.SessionNone }

// forwarding reports whether a forwarding carrier is needed.
func (o options) forwarding() bool {
	return len(o.fwd.Local) > 0 || len(o.fwd.Remote) > 0 || o.fwd.StdioSet
}

func parseOptions(args []string) (options, error) {
	o := options{escape: "~", tty: "auto", connection: map[string]string{}, policy: connpolicy.Default(), fwd: forwardspec.NewSettings()}
	seen := make(map[string]bool)
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			o.args = append(o.args, args[i+1:]...)
			break
		}
		if a == "-" || !strings.HasPrefix(a, "-") {
			if len(o.args) == 0 {
				// OpenSSH resumes option parsing after the destination, until
				// the first command word or an explicit -- terminates it.
				o.args = append(o.args, a)
				if err := o.parseDestination(seen); err != nil {
					return o, err
				}
				continue
			}
			o.args = append(o.args, args[i:]...)
			break
		}
		if strings.HasPrefix(a, "--") {
			switch a {
			case "--help":
				o.inspection = "help"
				return o, nil
			case "--browser":
				o.browser = true
			case "--direct-login":
				o.direct = true
			case "--experimental-command":
				o.broker = true
			case "--legacy-command":
				o.legacy = true
			case "--legacy-shell":
				o.legacyShell = true
			default:
				return o, errors.New("unsupported pi-sh option")
			}
			continue
		}
		for j := 1; j < len(a); j++ {
			c := a[j]
			switch c {
			case 'h':
				o.inspection = "help"
				return o, nil
			case 'n':
				o.stdinNull = true
				seen["stdinnull"] = true
			case 'G':
				o.dump = true
			case 'V':
				o.inspection = "version"
				return o, nil
			case 'T':
				o.tty = "no"
				seen["requesttty"] = true
			case 't':
				seen["requesttty"] = true
				if o.tty == "yes" || o.tty == "force" {
					o.tty = "force"
				} else {
					o.tty = "yes"
				}
			case 'v':
				o.logging.verbosity.Verbose()
			case 'q':
				o.logging.verbosity.Quiet()
			case 'y':
				o.logging.syslog = true
			case 'N':
				// ssh -N assigns SessionType none and no TTY directly, like -W,
				// and rejects an earlier other SessionType.
				if seen["sessiontype"] && !o.features.SessionNone {
					return o, errors.New("Cannot specify -N with -s/SessionType")
				}
				o.tty, o.features.SessionNone = "no", true
				seen["requesttty"], seen["sessiontype"] = true, true
			case 'g':
				o.fwd.GatewayPorts = true
				seen["gatewayports"] = true
			case 'a', 'x':
				// Disable agent/X11 forwarding: already pi-sh's only behaviour.
			case 'A':
				return o, forwardspec.ErrAgentForwarding
			case 'X', 'Y':
				return o, forwardspec.ErrX11Forwarding
			case 'M':
				return o, forwardspec.ErrMultiplexing
			case 'e', 'o', 'F', 'P', 'Q', 'E', 'l', 'p', 'L', 'R', 'D', 'W', 'S', 'O', 'w', 'J':
				var err error
				value := a[j+1:]
				if j+1 == len(a) {
					i++
					if i == len(args) {
						return o, fmt.Errorf("option -%c requires an argument", c)
					}
					value = args[i]
				}
				switch c {
				case 'Q':
					o.inspection, o.query = "query", value
					return o, nil
				case 'L':
					err = o.fwd.AddLocal(value, os.LookupEnv)
				case 'D':
					err = o.fwd.AddDynamic(value, os.LookupEnv)
				case 'R':
					err = o.fwd.AddRemote(value, os.LookupEnv)
				case 'W':
					err = o.fwd.SetStdio(value, os.LookupEnv)
					// As ssh.c: -W assigns no TTY and SessionType none directly.
					o.tty, o.features.SessionNone = "no", true
					seen["requesttty"], seen["sessiontype"] = true, true
				case 'S', 'O':
					return o, forwardspec.ErrMultiplexing
				case 'w':
					return o, forwardspec.ErrTunnel
				case 'J':
					return o, forwardspec.ErrProxy
				case 'e':
					o.escape = value
					seen["escapechar"] = true
				case 'l', 'p':
					// First obtained, like ssh.c (and relative to the
					// destination's position). -l selects the Raspberry Pi ID
					// account; a non-email user and any port are refused by
					// connpolicy after all sources are merged.
					key := map[byte]string{'l': "user", 'p': "port"}[c]
					if err := connpolicy.Validate(key, value); err != nil {
						return o, err
					}
					if !seen[key] {
						o.connection[key] = value
						seen[key] = true
					}
				case 'P':
					// ssh -P is first-obtained like -o Tag and may be empty.
					if !seen["tag"] {
						o.tag, o.tagSet = value, true
						seen["tag"] = true
					}
				case 'E':
					o.logging.file, o.logging.useFile = value, true
				case 'F':
					if value == "" {
						return o, errors.New("configuration path must not be empty")
					}
					o.configFile = value
				case 'o':
					if key, words, ferr := sshconfig.Fields(value); ferr == nil && forwardspec.ListKeyword(key) {
						if err := o.fwd.Directive(key, words, os.LookupEnv, seen[key]); err != nil {
							return o, fmt.Errorf("%s: %w", key, err)
						}
						seen[key] = key == "permitremoteopen" || seen[key]
						break
					}
					key, val, err := scalarOption(value)
					if err != nil {
						return o, err
					}
					// Validate even repeated values; first obtained configuration
					// value wins, while short flags assign directly as in ssh.
					if err := o.apply(key, val, seen); err != nil {
						return o, err
					}
				}
				if err != nil {
					return o, err
				}
				j = len(a)
			case '4', '6', 'C', 'f', 'K', 'k', 's', 'B', 'b', 'c', 'I', 'i', 'm':
				// Known ssh(1) options with an explicit decision; see
				// refusedFlag. Their operands, if any, are never interpreted.
				return o, fmt.Errorf("option -%c is not supported: %s", c, refusedFlag(c))
			default:
				if c < 'A' || c > 'z' {
					return o, errors.New("unsupported SSH option")
				}
				return o, fmt.Errorf("unsupported SSH option -%c", c)
			}
		}
	}
	if o.logging.syslog && o.logging.useFile {
		return o, diag.ErrBothDestinations
	}
	if len(o.args) > 0 && !o.destinationParsed {
		// A destination after an explicit -- follows every option.
		if err := o.parseDestination(seen); err != nil {
			return o, err
		}
	}
	defaultAccount, err := account.Default()
	if err != nil {
		return o, err
	}
	if o.configFile != "" && o.configFile != "none" {
		if len(o.args) == 0 {
			return o, errors.New("explicit configuration file requires a destination")
		}
		initial := make(map[string]string)
		if seen["hostname"] {
			initial["hostname"] = o.hostname
		}
		if o.tagSet {
			initial["tag"] = o.tag
		}
		// Match user/%r see the CLI or destination user as OpenSSH would; the
		// user is still refused by connpolicy after evaluation.
		if user, ok := o.connection["user"]; ok {
			initial["user"] = user
		}
		// Match command/sessiontype see the words ssh would join for exec.
		var lists []sshconfig.Directive
		ctx := sshconfig.Context{
			Host: o.args[0], Overrides: initial, Command: strings.Join(o.args[1:], " "), DefaultUser: defaultAccount, Lists: &lists,
		}
		if o.noSession() {
			ctx.SessionType = sshconfig.SessionNone
		}
		values, err := sshconfig.LoadContext(o.configFile, ctx)
		if err != nil {
			return o, configError{err}
		}
		// File forwardings follow the command line's, as ssh appends them.
		for _, d := range lists {
			if err := o.fwd.Directive(d.Key, d.Args, os.LookupEnv, seen[d.Key]); err != nil {
				return o, configError{err}
			}
			seen[d.Key] = d.Key == "permitremoteopen" || seen[d.Key]
		}
		for key, value := range values {
			if seen[key] {
				continue // CLI value seeded above, already validated and applied
			}
			if err := o.apply(key, value, seen); err != nil {
				return o, configError{err}
			}
		}
	}
	if o.hostname != "" {
		if len(o.args) == 0 {
			return o, errors.New("HostName requires a destination")
		}
		var err error
		o.hostname, err = sshconfig.Hostname(o.hostname, o.args[0])
		if err != nil {
			return o, err
		}
	}
	if _, err := parseEscape(o.escape); err != nil {
		return o, err
	}
	if err := o.finishForwarding(defaultAccount); err != nil {
		return o, err
	}
	if err := o.applyRemoteCommand(defaultAccount); err != nil {
		return o, err
	}
	if o.policy, err = connpolicy.FromValues(o.connection); err != nil {
		return o, err
	}
	o.policy.DefaultAccount = defaultAccount
	if len(o.args) > 0 {
		if err := connpolicy.Target(o.destination()); err != nil {
			return o, err
		}
	}
	if o.browser && o.direct {
		return o, errors.New("select only one authentication mode")
	}
	if o.broker && o.legacy {
		return o, errors.New("select only one command adapter")
	}
	if o.dump {
		return o, nil
	}
	if !o.browser && !o.direct {
		return o, errors.New("select --browser or --direct-login")
	}
	if (o.legacy || o.legacyShell) && (o.forwarding() || o.noSession()) {
		return o, errors.New("forwarding and -N require the in-memory adapters, not the legacy streams")
	}
	if o.noSession() {
		// ssh(1) with SessionType none runs nothing and ignores command words.
		if len(o.args) == 0 {
			return o, errors.New("a destination is required")
		}
		o.args = o.args[:1]
		return o, nil
	}
	command := o.broker || o.legacy
	if (command && len(o.args) < 2) || (!command && len(o.args) > 1) {
		return o, errors.New("command execution requires an explicit adapter, device and command")
	}
	if o.legacy && o.tty != "auto" && o.tty != "no" {
		return o, errors.New("TTY requests require the experimental command broker")
	}
	if o.legacy && o.tty == "no" {
		return o, errors.New("legacy command adapter cannot provide no-TTY semantics")
	}
	if o.legacyShell && command {
		return o, errors.New("the legacy shell stream cannot run a command")
	}
	// The raw agent shell stream has no TTY policy, stdin EOF or exit status.
	if o.legacyShell && (o.tty == "no" || o.stdinNull) {
		return o, errors.New("noninteractive shell requires the in-memory adapter, not the legacy shell stream")
	}
	return o, nil
}

// finishForwarding applies ssh's -W/ClearAllForwardings rules, refuses what
// pi-sh cannot do and expands Unix-socket paths.
func (o *options) finishForwarding(defaultAccount string) error {
	o.fwd.Finish()
	for _, f := range o.fwd.Remote {
		if f.Listen.Port == forwardspec.PortStreamLocal {
			return errRemoteUnixListen
		}
	}
	tokens := map[byte]string{}
	if len(o.args) > 0 {
		tokens['h'], tokens['n'] = o.destination(), o.args[0]
	}
	if account := o.connection["user"]; account != "" {
		tokens['r'] = account
	} else if defaultAccount != "" {
		tokens['r'] = defaultAccount
	}
	if home, err := os.UserHomeDir(); err == nil {
		tokens['d'] = home
	}
	if u, err := user.Current(); err == nil {
		tokens['u'] = u.Username
	}
	tokens['i'] = strconv.Itoa(os.Getuid())
	return o.fwd.Expand(tokens)
}

// errRemoteUnixListen refuses -R with a remote socket path: binding it would
// create a file on the device (docs/FORWARDING.md).
var errRemoteUnixListen = errors.New("remote Unix-domain socket forwarding (-R path:…) is not supported: it would create a socket file on the device; see docs/FORWARDING.md")

// applyRemoteCommand follows ssh(1): RemoteCommand together with a
// command-line command is an error (also for -G); otherwise its %-expanded
// text becomes the command, which still needs an explicit command adapter.
// -G prints the expanded text, as ssh -G does.
func (o *options) applyRemoteCommand(defaultAccount string) error {
	if o.remoteCommand == "" || o.remoteCommand == "none" {
		return nil
	}
	if len(o.args) > 1 {
		return errors.New("Cannot execute command-line and remote command.")
	}
	if len(o.args) == 0 {
		return nil
	}
	user := o.connection["user"]
	if user == "" {
		user = defaultAccount
	}
	text, err := sshconfig.ExpandCommand(o.remoteCommand, o.args[0], o.destination(), user)
	if err != nil {
		return fmt.Errorf("RemoteCommand: %w", err)
	}
	o.remoteCommand = text
	if !o.dump {
		o.args = append(o.args, text)
	}
	return nil
}

// parseDestination splits o.args[0] into the target and seeds its account
// and port where they are first obtained. A plain user@host is refused here,
// even if -l or -o User was given earlier.
func (o *options) parseDestination(seen map[string]bool) error {
	o.destinationParsed = true
	target, err := sshdest.SSH(o.args[0])
	if err != nil {
		return err
	}
	if err := connpolicy.DestinationUser(target); err != nil {
		return err
	}
	for key, value := range map[string]string{"user": target.User, "port": target.Port} {
		if value != "" && !seen[key] {
			o.connection[key] = value
			seen[key] = true
		}
	}
	o.args[0] = target.Host
	_, err = sshconfig.Hostname("%h", o.args[0])
	return err
}

// Like OpenSSH, a non-forced request is suppressed when stdin is not a terminal
// or StdinNull is selected, with a notice whenever a TTY had been wanted. A
// command's automatic policy requests no TTY; a login shell's requests one.
func (o options) requestTTY(stdinTerminal bool) (enabled, warning bool) {
	wanted := o.tty == "force" || o.tty == "yes" || (o.tty == "auto" && !o.broker && !o.legacy)
	if !wanted {
		return false, false
	}
	if o.tty == "force" {
		return true, false
	}
	if !stdinTerminal || o.stdinNull {
		return false, true
	}
	return true, false
}

// Parse only one scalar directive; reject additional tokens instead of silently
// ignoring local-execution directives or malformed configuration.
func scalarOption(s string) (string, string, error) {
	if key, value, ok := sshconfig.RawCommand(s); ok {
		return key, value, nil
	}
	key, args, err := sshconfig.Fields(s)
	if err != nil {
		return "", "", err
	}
	if key == "" || len(args) != 1 {
		return "", "", errors.New("configuration option requires one scalar value")
	}
	return key, args[0], nil
}

func (o *options) apply(key, value string, seen map[string]bool) error {
	if err := sshconfig.Validate(key, value); err != nil {
		return err
	}
	if !seen[key] {
		switch key {
		case "escapechar":
			o.escape = value
		case "stdinnull":
			o.stdinNull = value == "yes"
		case "requesttty":
			o.tty = value
		case "hostname":
			o.hostname = value
		default:
			if connpolicy.Known(key) {
				o.connection[key] = value
			}
		case "tag":
			o.tag, o.tagSet = value, true
		case "loglevel":
			o.logging.level = value
		case "syslogfacility":
			o.logging.facility = value
		case "remotecommand":
			o.remoteCommand = value
		case "gatewayports", "exitonforwardfailure", "clearallforwardings", "streamlocalbindunlink", "streamlocalbindmask":
			if err := o.fwd.SetScalar(key, value); err != nil {
				return err
			}
		case "forwardagent", "forwardx11", "forwardx11trusted", "controlmaster", "controlpath",
			"controlpersist", "tunnel", "proxycommand", "proxyjump", "proxyusefdpass", "sessiontype":
			o.features.Set(key, value)
		case "refuseconnection":
			// As in OpenSSH 10.2, an obtained value always terminates.
			return errors.New("RefuseConnection: connection refused by configuration")
		}
		seen[key] = true
	}
	return nil
}

// refusedFlag explains the decision for known ssh(1) flags that have no
// Connect equivalent. Forwarding/multiplexing flags are documented in
// docs/FORWARDING.md and keep the generic refusal.
func refusedFlag(c byte) string {
	switch c {
	case '4', '6':
		return sshconfig.ReasonFamily
	case 'B', 'b':
		return sshconfig.ReasonBind
	case 'C':
		return sshconfig.ReasonCompress
	case 'f':
		return sshconfig.ReasonFork
	case 'N', 's':
		return sshconfig.ReasonSession
	case 'c', 'm':
		return sshconfig.ReasonCrypto
	}
	return sshconfig.ReasonAuth // -i, -I, -K, -k
}

func (o options) destination() string {
	if o.hostname != "" {
		return o.hostname
	}
	if len(o.args) > 0 {
		return o.args[0]
	}
	return ""
}

func (o options) stdin(input io.Reader) io.Reader {
	if o.stdinNull {
		return strings.NewReader("")
	}
	return input
}

func (o options) writeConfig(w io.Writer) error {
	// ssh -G prints the effective user; for pi-sh that is the Raspberry Pi
	// ID account, explicit or the local default, and nothing when unknown.
	if user := o.policy.LoginAccount(); user != "" {
		if _, err := fmt.Fprintf(w, "user %s\n", user); err != nil {
			return err
		}
	}
	if len(o.args) > 0 {
		if _, err := fmt.Fprintf(w, "hostname %s\n", o.destination()); err != nil {
			return err
		}
	}
	stdin := "no"
	if o.stdinNull {
		stdin = "yes"
	}
	escape, _ := parseEscape(o.escape)
	label := "none"
	if escape >= 0 {
		label = escapeLabel(escape)
		if escape < 32 || escape == 127 {
			label = "\\" + label
		}
		if escape == ' ' {
			label = "\\040"
		}
		if escape == '\\' {
			label = "\\\\"
		}
	}
	tty := o.tty
	if tty == "no" {
		tty = "false"
	}
	if tty == "yes" {
		tty = "true"
	}
	if _, err := fmt.Fprintf(w, "escapechar %s\nstdinnull %s\nrequesttty %s\n", label, stdin, tty); err != nil {
		return err
	}
	if o.tagSet {
		if _, err := fmt.Fprintf(w, "tag %s\n", o.tag); err != nil {
			return err
		}
	}
	if o.remoteCommand != "" && o.remoteCommand != "none" {
		if _, err := fmt.Fprintf(w, "remotecommand %s\n", o.remoteCommand); err != nil {
			return err
		}
	}
	if err := o.logging.writeConfig(w); err != nil {
		return err
	}
	if err := o.fwd.WriteConfig(w); err != nil {
		return err
	}
	if err := o.features.WriteConfig(w); err != nil {
		return err
	}
	return o.policy.WriteConfig(w)
}
