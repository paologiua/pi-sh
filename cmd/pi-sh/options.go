package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"pi-sh/internal/diag"
	"pi-sh/internal/sshconfig"
)

// This is a deliberately bounded OpenSSH option subset. No SSH config files or
// local commands are read or executed implicitly.
type options struct {
	browser, direct, broker, legacy, dump bool
	stdinNull                             bool
	escape                                string
	tty                                   string
	args                                  []string
	configFile, hostname                  string
	tag                                   string
	tagSet                                bool
	inspection, query                     string
	logging                               logOptions
}

func parseOptions(args []string) (options, error) {
	o := options{escape: "~", tty: "auto"}
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
				continue
			}
			o.args = append(o.args, args[i:]...)
			break
		}
		if strings.HasPrefix(a, "--") {
			switch a {
			case "--browser":
				o.browser = true
			case "--direct-login":
				o.direct = true
			case "--experimental-command":
				o.broker = true
			case "--legacy-command":
				o.legacy = true
			default:
				return o, errors.New("unsupported pi-sh option")
			}
			continue
		}
		for j := 1; j < len(a); j++ {
			c := a[j]
			switch c {
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
			case 'e', 'o', 'F', 'P', 'Q', 'E':
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
				case 'e':
					o.escape = value
					seen["escapechar"] = true
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
				j = len(a)
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
	if len(o.args) > 0 {
		if _, err := sshconfig.Hostname("%h", o.args[0]); err != nil {
			return o, err
		}
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
		// Match command/sessiontype see the words ssh would join for exec.
		values, err := sshconfig.LoadContext(o.configFile, sshconfig.Context{
			Host: o.args[0], Overrides: initial, Command: strings.Join(o.args[1:], " "),
		})
		if err != nil {
			return o, configError{err}
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
	if !command && (o.tty == "no" || o.stdinNull) {
		return o, errors.New("noninteractive shell requires the command broker and an explicit command")
	}
	return o, nil
}

// Like OpenSSH, a non-forced request is suppressed when stdin is not a terminal
// or StdinNull is selected. A command's automatic policy requests no TTY.
func (o options) requestTTY(stdinTerminal bool) (enabled, warning bool) {
	wanted := o.tty == "force" || o.tty == "yes" || (o.tty == "auto" && !o.broker && !o.legacy)
	if !wanted {
		return false, false
	}
	if o.tty == "force" {
		return true, false
	}
	if !stdinTerminal || o.stdinNull {
		return false, o.tty == "yes"
	}
	return true, false
}

// Parse only one scalar directive; reject additional tokens instead of silently
// ignoring local-execution directives or malformed configuration.
func scalarOption(s string) (string, string, error) {
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
		case "tag":
			o.tag, o.tagSet = value, true
		case "loglevel":
			o.logging.level = value
		case "syslogfacility":
			o.logging.facility = value
		}
		seen[key] = true
	}
	return nil
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
	return o.logging.writeConfig(w)
}
