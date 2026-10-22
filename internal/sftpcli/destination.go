package sftpcli

import (
	"errors"
	"pi-sh/internal/sshconfig"
)

// Destination applies explicit HostName aliases and the pipe-only SFTP policy.
func Destination(original, config string, overrides []string) (string, error) {
	return DestinationWithServer(original, config, overrides, "")
}

// DestinationWithServer is Destination for an explicit remote server path.
// Match sessiontype/command see what OpenSSH sftp would request: the "sftp"
// subsystem by default, or an exec of a server path (sftp -s /path).
func DestinationWithServer(original, config string, overrides []string, server string) (string, error) {
	if _, err := sshconfig.Hostname("%h", original); err != nil {
		return "", err
	}
	values := map[string]string{}
	for _, line := range overrides {
		key, args, err := sshconfig.Fields(line)
		if err != nil {
			return "", err
		}
		if len(args) != 1 {
			return "", errors.New("configuration requires one scalar value")
		}
		if err := sshconfig.Validate(key, args[0]); err != nil {
			return "", err
		}
		if _, set := values[key]; !set {
			values[key] = args[0]
		}
	}
	if config != "none" {
		session := sshconfig.Context{Host: original, Overrides: values, SessionType: sshconfig.SessionSubsystem, Command: "sftp"}
		if server != "" {
			session.SessionType, session.Command = sshconfig.SessionExec, server
		}
		file, err := sshconfig.LoadContext(config, session)
		if err != nil {
			return "", err
		}
		for k, v := range file {
			if _, set := values[k]; !set {
				values[k] = v
			}
		}
	}
	if tty := values["requesttty"]; tty != "" && tty != "no" && tty != "auto" {
		return "", errors.New("SFTP requires the broker's pipe/no-TTY mode")
	}
	if values["stdinnull"] == "yes" {
		return "", errors.New("SFTP needs its private protocol stdin; StdinNull yes is incompatible")
	}
	if name := values["hostname"]; name != "" {
		return sshconfig.Hostname(name, original)
	}
	return original, nil
}
