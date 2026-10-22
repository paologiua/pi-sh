package sftpcli

import (
	"errors"

	"pi-sh/internal/account"
	"pi-sh/internal/connpolicy"
	"pi-sh/internal/sshconfig"
)

// Destination applies explicit HostName aliases, the pipe-only SFTP policy and
// the connection policy, returning only the target.
func Destination(original, config string, overrides []string) (string, error) {
	target, _, _, err := resolve(original, config, overrides, "", nil)
	return target, err
}

// DestinationWithServer is Destination for an explicit remote server path.
// Match sessiontype/command see what OpenSSH sftp would request: the "sftp"
// subsystem by default, or an exec of a server path (sftp -s /path).
func DestinationWithServer(original, config string, overrides []string, server string) (string, error) {
	target, _, _, err := resolve(original, config, overrides, server, nil)
	return target, err
}

// Settings is Destination plus the first obtained values of all accepted
// directives (for example loglevel), as ssh would see them for this session.
func Settings(original, config string, overrides []string) (string, map[string]string, error) {
	return SettingsWithServer(original, config, overrides, "")
}

// SettingsWithServer is Settings with DestinationWithServer's Match session.
func SettingsWithServer(original, config string, overrides []string, server string) (string, map[string]string, error) {
	target, values, _, err := resolve(original, config, overrides, server, nil)
	return target, values, err
}

// Resolve merges -o values, then operand values (user/port from the parsed
// destination or -P; earlier -o values win, as in OpenSSH), then an explicit
// file. Any effective user or port, and other settings without a Connect
// equivalent, are refused by connpolicy before authentication.
func Resolve(original, config string, overrides []string, operand map[string]string) (string, connpolicy.Policy, error) {
	target, _, policy, err := resolve(original, config, overrides, "", operand)
	return target, policy, err
}

// ResolveSession is Resolve with SettingsWithServer's Match session and values.
func ResolveSession(original, config string, overrides []string, server string, operand map[string]string) (string, map[string]string, connpolicy.Policy, error) {
	return resolve(original, config, overrides, server, operand)
}

func resolve(original, config string, overrides []string, server string, operand map[string]string) (string, map[string]string, connpolicy.Policy, error) {
	policy := connpolicy.Default()
	if _, err := sshconfig.Hostname("%h", original); err != nil {
		return "", nil, policy, err
	}
	values := map[string]string{}
	for _, line := range overrides {
		key, args, err := sshconfig.Fields(line)
		if raw, value, ok := sshconfig.RawCommand(line); ok {
			key, args, err = raw, []string{value}, nil
		}
		if err != nil {
			return "", nil, policy, err
		}
		if len(args) != 1 {
			return "", nil, policy, errors.New("configuration requires one scalar value")
		}
		if err := sshconfig.Validate(key, args[0]); err != nil {
			return "", nil, policy, err
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
			return "", nil, policy, err
		}
		if _, set := values[key]; !set {
			values[key] = value
		}
	}
	defaultAccount, err := account.Default()
	if err != nil {
		return "", nil, policy, err
	}
	if config != "none" {
		// A CLI/destination account is seeded so Match user/%r evaluate as in
		// OpenSSH; connpolicy validates it afterwards.
		session := sshconfig.Context{Host: original, Overrides: values, SessionType: sshconfig.SessionSubsystem, Command: "sftp", DefaultUser: defaultAccount}
		if server != "" {
			session.SessionType, session.Command = sshconfig.SessionExec, server
		}
		file, err := sshconfig.LoadContext(config, session)
		if err != nil {
			return "", nil, policy, err
		}
		for k, v := range file {
			if _, set := values[k]; !set {
				values[k] = v
			}
		}
	}
	if _, set := values["refuseconnection"]; set {
		return "", values, policy, errors.New("RefuseConnection: connection refused by configuration")
	}
	if command := values["remotecommand"]; command != "" && command != "none" {
		// ssh(1) refuses a RemoteCommand together with the sftp subsystem.
		return "", values, policy, errors.New("Cannot execute command-line and remote command: RemoteCommand conflicts with the SFTP session")
	}
	if tty := values["requesttty"]; tty != "" && tty != "no" && tty != "auto" {
		return "", values, policy, errors.New("SFTP requires the broker's pipe/no-TTY mode")
	}
	if values["stdinnull"] == "yes" {
		return "", values, policy, errors.New("SFTP needs its private protocol stdin; StdinNull yes is incompatible")
	}
	policy, err = connpolicy.FromValues(values)
	if err != nil {
		return "", values, policy, err
	}
	policy.DefaultAccount = defaultAccount
	target := original
	if name := values["hostname"]; name != "" {
		if target, err = sshconfig.Hostname(name, original); err != nil {
			return "", values, policy, err
		}
	}
	return target, values, policy, connpolicy.Target(target)
}
