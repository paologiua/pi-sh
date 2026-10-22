// Package diag implements OpenSSH-style client diagnostics: LogLevel and
// SyslogFacility values, -v/-q accumulation, and a redacting logger writing to
// stderr, an -E log file or the local syslog (-y).
//
// The logger accepts only fixed phase messages and typed fields (counts,
// durations, enumerated state tokens). Errors are passed through Sanitize.
// No API accepts remote terminal or stdio payload bytes.
package diag

import (
	"errors"
	"flag"
)

// Level follows OpenSSH's SYSLOG_LEVEL ordering; a message is emitted when its
// level is at most the configured level.
type Level int8

const (
	Quiet Level = iota
	Fatal
	Error
	Info
	Verbose
	Debug1
	Debug2
	Debug3
)

var levelNames = [...]string{"QUIET", "FATAL", "ERROR", "INFO", "VERBOSE", "DEBUG", "DEBUG2", "DEBUG3"}

// String returns the name printed by ssh -G (DEBUG1 is reported as DEBUG).
func (l Level) String() string {
	if l < Quiet || l > Debug3 {
		return "INFO"
	}
	return levelNames[l]
}

// ParseLevel accepts the ssh_config(5) LogLevel names case-insensitively
// (ASCII only), including the DEBUG/DEBUG1 synonym.
func ParseLevel(s string) (Level, error) {
	switch asciiUpper(s) {
	case "QUIET":
		return Quiet, nil
	case "FATAL":
		return Fatal, nil
	case "ERROR":
		return Error, nil
	case "INFO":
		return Info, nil
	case "VERBOSE":
		return Verbose, nil
	case "DEBUG", "DEBUG1":
		return Debug1, nil
	case "DEBUG2":
		return Debug2, nil
	case "DEBUG3":
		return Debug3, nil
	}
	return Info, errors.New("unsupported LogLevel value")
}

// Facility is an ssh_config(5) SyslogFacility value. It only affects -y.
type Facility int8

const (
	User Facility = iota
	Daemon
	Auth
	AuthPriv
	Local0
	Local1
	Local2
	Local3
	Local4
	Local5
	Local6
	Local7
)

var facilityNames = [...]string{"USER", "DAEMON", "AUTH", "AUTHPRIV", "LOCAL0", "LOCAL1", "LOCAL2", "LOCAL3", "LOCAL4", "LOCAL5", "LOCAL6", "LOCAL7"}

func (f Facility) String() string {
	if f < User || f > Local7 {
		return "USER"
	}
	return facilityNames[f]
}

// ParseFacility accepts OpenSSH's facility names case-insensitively (ASCII).
func ParseFacility(s string) (Facility, error) {
	u := asciiUpper(s)
	for i, name := range facilityNames {
		if u == name {
			return Facility(i), nil
		}
	}
	return User, errors.New("unsupported SyslogFacility value")
}

func asciiUpper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 'a' + 'A'
		}
	}
	return string(b)
}

// Verbosity applies ssh(1)'s -v and -q in command-line order. As in ssh.c, the
// first -v selects DEBUG1 from any level; later ones increment the current level
// up to DEBUG3; -q selects QUIET. Thus "-v -q -v" yields FATAL, as OpenSSH does.
type Verbosity struct {
	set   bool
	debug int
	level Level
}

func (v *Verbosity) Verbose() {
	if v.debug == 0 {
		v.debug, v.level = 1, Debug1
	} else if v.level < Debug3 {
		v.debug++
		v.level++
	}
	v.set = true
}

func (v *Verbosity) Quiet() { v.level, v.set = Quiet, true }

// Set reports whether any -v/-q was given; such a level overrides LogLevel.
func (v Verbosity) Set() bool { return v.set }

// Resolve combines command-line flags with the first obtained LogLevel
// directive (from -o or a configuration file). Flags always win; an empty
// directive means INFO, the OpenSSH default.
func (v Verbosity) Resolve(directive string) (Level, error) {
	if v.set {
		return v.level, nil
	}
	if directive == "" {
		return Info, nil
	}
	return ParseLevel(directive)
}

type verbosityFlag struct {
	v     *Verbosity
	quiet bool
}

func (f verbosityFlag) String() string   { return "" }
func (f verbosityFlag) IsBoolFlag() bool { return true }
func (f verbosityFlag) Set(s string) error {
	if s != "true" {
		return errors.New("option does not take a value")
	}
	if f.quiet {
		f.v.Quiet()
	} else {
		f.v.Verbose()
	}
	return nil
}

// VerboseFlag and QuietFlag adapt -v/-q to flag.FlagSet while preserving their
// relative order, which determines the level as in scp/sftp passing them to ssh.
func (v *Verbosity) VerboseFlag() flag.Value { return verbosityFlag{v: v} }
func (v *Verbosity) QuietFlag() flag.Value   { return verbosityFlag{v: v, quiet: true} }
