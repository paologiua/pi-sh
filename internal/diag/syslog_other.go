//go:build windows || plan9

package diag

import "errors"

// openSyslog explicitly rejects -y where Go provides no syslog client.
func openSyslog(string, string, Facility, string) (sysLogger, error) {
	return nil, errors.New("syslog logging (-y) is not supported on this platform")
}

const SyslogSupported = false
