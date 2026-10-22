package sftpcli

import (
	"errors"

	"pi-sh/internal/connpolicy"
	"pi-sh/internal/sshconfig"
)

// Destination applies explicit HostName aliases, the pipe-only SFTP policy and
// the connection policy, returning only the target.
func Destination(original, config string, overrides []string) (string, error) {
	target, _, err := Resolve(original, config, overrides, nil)
	return target, err
}

// Resolve merges -o values, then operand values (user/port from the parsed
// destination or -P; earlier -o values win, as in OpenSSH), then an explicit
// file. Any effective user or port, and other settings without a Connect
// equivalent, are refused by connpolicy before authentication.
func Resolve(original, config string, overrides []string, operand map[string]string) (string, connpolicy.Policy, error) {
	policy := connpolicy.Default()
	if _, err := sshconfig.Hostname("%h", original); err != nil {
		return "", policy, err
	}
	values := map[string]string{}
	for _, line := range overrides {
		key, args, err := sshconfig.Fields(line)
		if err != nil {
			return "", policy, err
		}
		if len(args) != 1 {
			return "", policy, errors.New("configuration requires one scalar value")
		}
		if err := sshconfig.Validate(key, args[0]); err != nil {
			return "", policy, err
		}
		if _, set := values[key]; !set {
			values[key] = args[0]
		}
	}
	for key, value := range operand {
		if value == "" {
			continue
		}
		if err := sshconfig.Validate(key, value); err != nil {
			return "", policy, err
		}
		if _, set := values[key]; !set {
			values[key] = value
		}
	}
	if config != "none" {
		file, err := sshconfig.LoadWithOverrides(config, original, values)
		if err != nil {
			return "", policy, err
		}
		for k, v := range file {
			if _, set := values[k]; !set {
				values[k] = v
			}
		}
	}
	if tty := values["requesttty"]; tty != "" && tty != "no" && tty != "auto" {
		return "", policy, errors.New("SFTP requires the broker's pipe/no-TTY mode")
	}
	if values["stdinnull"] == "yes" {
		return "", policy, errors.New("SFTP needs its private protocol stdin; StdinNull yes is incompatible")
	}
	policy, err := connpolicy.FromValues(values)
	if err != nil {
		return "", policy, err
	}
	target := original
	if name := values["hostname"]; name != "" {
		if target, err = sshconfig.Hostname(name, original); err != nil {
			return "", policy, err
		}
	}
	return target, policy, connpolicy.Target(target)
}
