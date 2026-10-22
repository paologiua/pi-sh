package main

import (
	"fmt"
	"io"
	"strings"

	"pi-sh/internal/diag"
)

// logOptions holds -v/-q/-E/-y and the first obtained LogLevel/SyslogFacility.
type logOptions struct {
	verbosity       diag.Verbosity
	level, facility string
	file            string
	useFile, syslog bool
}

// configError marks failures found while reading configuration files. As in
// ssh(1), these are reported through the already-selected log destination and
// level (so -q suppresses them and -E captures them), unlike option errors.
type configError struct{ error }

func (e configError) Unwrap() error { return e.error }

func (l logOptions) effective() (diag.Level, diag.Facility) {
	level, err := l.verbosity.Resolve(l.level)
	if err != nil {
		level = diag.Info
	}
	facility := diag.User
	if l.facility != "" {
		if f, err := diag.ParseFacility(l.facility); err == nil {
			facility = f
		}
	}
	return level, facility
}

func (l logOptions) open(stderr io.Writer) (*diag.Logger, error) {
	level, facility := l.effective()
	return diag.Open(diag.Config{Program: "pi-sh", Level: level, Stderr: stderr, File: l.file, UseFile: l.useFile, Syslog: l.syslog, Facility: facility})
}

func (l logOptions) writeConfig(w io.Writer) error {
	level, facility := l.effective()
	_, err := fmt.Fprintf(w, "loglevel %s\nsyslogfacility %s\n", level, facility)
	return err
}

// Phase messages are fixed text; see docs/DIAGNOSTICS.md for the level table.
const (
	msgVersion      diag.Message = "unofficial client"
	msgConfigFile   diag.Message = "configuration: reading explicit configuration file"
	msgConfigNone   diag.Message = "configuration: no configuration file selected"
	msgConfigLevel  diag.Message = "configuration: effective diagnostics"
	msgAuthBrowser  diag.Message = "authentication: browser companion mode selected"
	msgAuthDirect   diag.Message = "authentication: direct login mode selected"
	msgNoTTY        diag.Message = "pseudo-terminal will not be allocated because stdin is not a terminal or is disabled"
	msgEscape       diag.Message = "disconnected locally; remote TTY cleanup was not confirmed"
	msgLegacy       diag.Message = "experimental command adapter; remote PTY, merged stdout/stderr, stdin /dev/null"
	msgBroker       diag.Message = "experimental shell-only broker; binary stdin, separate stdout/stderr; no Python"
	msgTTYCommand   diag.Message = "experimental TTY command; merged stdout/stderr; abnormal remote cleanup remains unverified"
	msgSessionShell diag.Message = "session: starting interactive shell"
	msgSessionMode  diag.Message = "session: starting command adapter"
	msgExitStatus   diag.Message = "session: remote command finished"
)

func logStartup(log *diag.Logger, o options) {
	// clientVersion is "pi-sh_V goV os/arch (unofficial)", built from tokens.
	parts := strings.Fields(clientVersion())
	if len(parts) >= 3 {
		goos, arch, _ := strings.Cut(parts[2], "/")
		log.Debug(msgVersion, diag.State("version", parts[0]), diag.State("go", parts[1]), diag.State("os", goos), diag.State("arch", arch))
	}
	if o.configFile != "" && o.configFile != "none" {
		log.Debug(msgConfigFile)
	} else {
		log.Debug(msgConfigNone)
	}
	level, facility := o.logging.effective()
	log.Debug2(msgConfigLevel, diag.State("loglevel", level.String()), diag.State("syslogfacility", facility.String()),
		diag.Bool("logfile", o.logging.useFile), diag.Bool("syslog", o.logging.syslog))
}
