package sftpcli

import (
	"errors"
	"pi-sh/internal/sshconfig"
)

// Destination applies explicit HostName aliases and the pipe-only SFTP policy.
func Destination(original, config string, overrides []string) (string, error) {
	return resolve(original, config, overrides, nil)
}

// Settings is Destination plus the first obtained values of all accepted
// directives (for example loglevel), as ssh would see them for this session.
func Settings(original, config string, overrides []string) (string, map[string]string, error) {
	out := map[string]string{}
	target, err := resolve(original, config, overrides, out)
	return target, out, err
}

func resolve(original, config string, overrides []string, out map[string]string) (string, error) {
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
		file, err := sshconfig.LoadWithOverrides(config, original, values)
		if err != nil {
			return "", err
		}
		for k, v := range file {
			if _, set := values[k]; !set {
				values[k] = v
			}
		}
	}
	for k, v := range values {
		if out != nil {
			out[k] = v
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
