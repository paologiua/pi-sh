//go:build !windows && !plan9

package diag

import "log/syslog"

var facilityPriority = [...]syslog.Priority{
	syslog.LOG_USER, syslog.LOG_DAEMON, syslog.LOG_AUTH, syslog.LOG_AUTHPRIV,
	syslog.LOG_LOCAL0, syslog.LOG_LOCAL1, syslog.LOG_LOCAL2, syslog.LOG_LOCAL3,
	syslog.LOG_LOCAL4, syslog.LOG_LOCAL5, syslog.LOG_LOCAL6, syslog.LOG_LOCAL7,
}

type unixSyslog struct{ w *syslog.Writer }

// openSyslog uses only the standard library's pure-Go client over the local
// socket (/dev/log, /var/run/syslog or /var/run/log); it never uses a network
// destination unless a test supplies one. Absence is reported, not ignored.
func openSyslog(network, addr string, f Facility, tag string) (sysLogger, error) {
	if f < User || f > Local7 {
		f = User
	}
	w, err := syslog.Dial(network, addr, facilityPriority[f]|syslog.LOG_INFO, tag)
	if err != nil {
		return nil, ErrSyslog
	}
	return unixSyslog{w}, nil
}

// Severity mapping follows OpenSSH log.c: FATAL→CRIT, ERROR→ERR,
// INFO/VERBOSE→INFO, DEBUG1-3→DEBUG.
func (s unixSyslog) write(level Level, line string) error {
	switch level {
	case Fatal:
		return s.w.Crit(line)
	case Error:
		return s.w.Err(line)
	case Info, Verbose:
		return s.w.Info(line)
	default:
		return s.w.Debug(line)
	}
}

func (s unixSyslog) Close() error { return s.w.Close() }

// SyslogSupported reports whether -y can be attempted on this platform.
const SyslogSupported = true
