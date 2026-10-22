package sshconfig

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// This file records an explicit decision for every OpenSSH 10.2 ssh_config
// keyword that pi-sh does not implement as a setting elsewhere. A keyword is
// either accepted only with the value that matches what pi-sh always does
// (so accepting it never hides behaviour), or refused with a specific reason.
// Unknown keywords keep the generic "unsupported" error. See
// docs/CONFIGURATION.md and docs/SSH_COMPATIBILITY.md.

// Shared reasons, also used for the matching command-line flags.
const (
	ReasonAuth       = "SSH authentication methods and keys do not apply: Connect sign-in is a Raspberry Pi ID password and one-time code (--direct-login) or the browser companion"
	ReasonHostKey    = "SSH host keys do not apply: the device is reached through the authenticated Connect service (HTTPS) and WebRTC DTLS, and pi-sh has no SSH host key to check or record"
	ReasonCrypto     = "SSH transport algorithms do not apply: Connect uses HTTPS (TLS) and WebRTC DTLS/SCTP negotiated by the Go and Pion stacks"
	ReasonCompress   = "Connect's WebRTC data channel offers no compression; only Compression no is accepted"
	ReasonCanonical  = "hostname canonicalization resolves DNS names, while Connect devices are selected by exact name or ID; only CanonicalizeHostname no is accepted"
	ReasonFamily     = "address family selection is not implemented for the HTTPS and WebRTC/ICE connections; only AddressFamily any is accepted"
	ReasonBind       = "local bind address/interface selection is not implemented for the HTTPS and WebRTC/ICE connections"
	ReasonFork       = "going to the background after authentication is not supported: the memory-only Connect session cannot be handed to a forked process; use shell job control"
	ReasonSession    = "only SessionType default is supported: Connect's shell channel always starts a shell (no -N/none) and no SSH subsystem request exists (use pi-sftp for SFTP)"
	ReasonEnv        = "environment passing is not implemented: Connect has no SSH environment request, and pi-sh does not inject variables into the remote shell"
	ReasonLocalCmd   = "LocalCommand is not implemented: pi-sh never runs local commands from configuration except Match exec; only PermitLocalCommand no is accepted"
	ReasonIgnore     = "IgnoreUnknown is not supported: pi-sh validates every directive in explicitly selected files so that no setting is silently skipped"
	ReasonForwarding = "forwarding, proxying, agent/X11 forwarding and connection sharing are not implemented (see docs/FORWARDING.md)"
	ReasonEscape     = "the escape command line (~C) is not implemented"
	ReasonQoS        = "WebRTC traffic class, rekeying and keystroke-timing obfuscation are not configurable in pi-sh; only ObscureKeystrokeTiming no is accepted"
	ReasonBanner     = "pi-sh sends no SSH version banner to extend"
	ReasonLogVerbose = "pi-sh has no per-function debug filters; use -v or LogLevel"
	ReasonObsolete   = "this keyword is obsolete in OpenSSH and has no pi-sh meaning"
)

// accepted lists keywords whose only accepted value is pi-sh's fixed
// behaviour; OpenSSH spells booleans yes/no (true/false also for flags).
var accepted = map[string]struct {
	values []string
	reason string
}{
	"compression":               {[]string{"no"}, ReasonCompress},
	"canonicalizehostname":      {[]string{"no"}, ReasonCanonical},
	"addressfamily":             {[]string{"any"}, ReasonFamily},
	"forkafterauthentication":   {[]string{"no"}, ReasonFork},
	"sessiontype":               {[]string{"default"}, ReasonSession},
	"permitlocalcommand":        {[]string{"no"}, ReasonLocalCmd},
	"gssapiauthentication":      {[]string{"no"}, ReasonAuth},
	"gssapidelegatecredentials": {[]string{"no"}, ReasonAuth},
	"hostbasedauthentication":   {[]string{"no"}, ReasonAuth},
	"enablesshkeysign":          {[]string{"no"}, ReasonAuth},
	"checkhostip":               {[]string{"no"}, ReasonHostKey},
	"visualhostkey":             {[]string{"no"}, ReasonHostKey},
	"obscurekeystroketiming":    {[]string{"no"}, ReasonQoS},
}

// refused keywords are known OpenSSH 10.2 keywords without a pi-sh equivalent.
var refused = map[string]string{
	"identityfile": ReasonAuth, "identitiesonly": ReasonAuth, "identityagent": ReasonAuth, "certificatefile": ReasonAuth,
	"addkeystoagent": ReasonAuth, "pkcs11provider": ReasonAuth, "securitykeyprovider": ReasonAuth, "usekeychain": ReasonAuth,
	"pubkeyauthentication": ReasonAuth, "passwordauthentication": ReasonAuth, "kbdinteractiveauthentication": ReasonAuth,
	"kbdinteractivedevices": ReasonAuth, "preferredauthentications": ReasonAuth, "challengeresponseauthentication": ReasonAuth,

	"stricthostkeychecking": ReasonHostKey, "userknownhostsfile": ReasonHostKey, "globalknownhostsfile": ReasonHostKey,
	"hostkeyalias": ReasonHostKey, "hashknownhosts": ReasonHostKey, "verifyhostkeydns": ReasonHostKey,
	"updatehostkeys": ReasonHostKey, "knownhostscommand": ReasonHostKey, "revokedhostkeys": ReasonHostKey,
	"nohostauthenticationforlocalhost": ReasonHostKey, "nohostauthenticationforproxycommand": ReasonHostKey,
	"fingerprinthash": ReasonHostKey,

	"ciphers": ReasonCrypto, "macs": ReasonCrypto, "kexalgorithms": ReasonCrypto, "hostkeyalgorithms": ReasonCrypto,
	"pubkeyacceptedalgorithms": ReasonCrypto, "pubkeyacceptedkeytypes": ReasonCrypto, "hostbasedacceptedalgorithms": ReasonCrypto,
	"hostbasedkeytypes": ReasonCrypto, "casignaturealgorithms": ReasonCrypto, "requiredrsasize": ReasonCrypto,
	"warnweakcrypto": ReasonCrypto,

	// OpenSSH 10.2 resolves the destination in DNS when permitted CNAMEs are
	// set, even with CanonicalizeHostname no (observed with ssh -G).
	"canonicalizepermittedcnames": ReasonCanonical,
	"bindaddress":                 ReasonBind, "bindinterface": ReasonBind,
	"sendenv": ReasonEnv, "setenv": ReasonEnv,
	"localcommand":  ReasonLocalCmd,
	"ignoreunknown": ReasonIgnore,
	"ipqos":         ReasonQoS, "rekeylimit": ReasonQoS,
	"versionaddendum":         ReasonBanner,
	"logverbose":              ReasonLogVerbose,
	"enableescapecommandline": ReasonEscape,

	"localforward": ReasonForwarding, "remoteforward": ReasonForwarding, "dynamicforward": ReasonForwarding,
	"gatewayports": ReasonForwarding, "exitonforwardfailure": ReasonForwarding, "clearallforwardings": ReasonForwarding,
	"permitremoteopen": ReasonForwarding, "forwardagent": ReasonForwarding, "forwardx11": ReasonForwarding,
	"forwardx11timeout": ReasonForwarding, "forwardx11trusted": ReasonForwarding, "xauthlocation": ReasonForwarding,
	"tunnel": ReasonForwarding, "tunneldevice": ReasonForwarding, "controlmaster": ReasonForwarding,
	"controlpath": ReasonForwarding, "controlpersist": ReasonForwarding, "proxyjump": ReasonForwarding,
	"proxycommand": ReasonForwarding, "proxyusefdpass": ReasonForwarding, "streamlocalbindmask": ReasonForwarding,
	"streamlocalbindunlink": ReasonForwarding,

	"protocol": ReasonObsolete, "cipher": ReasonObsolete, "rsaauthentication": ReasonObsolete,
	"rhostsrsaauthentication": ReasonObsolete, "compressionlevel": ReasonObsolete, "useroaming": ReasonObsolete,
	"useprivilegedport": ReasonObsolete, "fallbacktorsh": ReasonObsolete, "usersh": ReasonObsolete,
}

// Canonicalization tuning is inert in OpenSSH while CanonicalizeHostname is
// no, which is the only value pi-sh accepts; values are syntax-checked.
func validateCanonical(key, value string) (bool, error) {
	switch key {
	case "canonicaldomains":
		if value == "" {
			return true, errors.New("empty canonicalization list")
		}
		return true, nil
	case "canonicalizefallbacklocal":
		if !yesNo(value) {
			return true, errors.New("CanonicalizeFallbackLocal requires yes or no")
		}
		return true, nil
	case "canonicalizemaxdots":
		if n, err := strconv.ParseInt(value, 10, 32); err != nil || n < 0 {
			return true, errors.New("invalid CanonicalizeMaxDots value")
		}
		return true, nil
	}
	return false, nil
}

func yesNo(value string) bool {
	switch strings.ToLower(value) {
	case "yes", "true", "no", "false":
		return true
	}
	return false
}

// keywordDecision validates a keyword without its own setting. handled is
// false for keywords OpenSSH does not know either.
func keywordDecision(key, value string) (handled bool, err error) {
	if ok, err := validateCanonical(key, value); ok {
		return true, err
	}
	if a, ok := accepted[key]; ok {
		for _, v := range a.values {
			if strings.EqualFold(value, v) || (v == "no" && strings.EqualFold(value, "false")) {
				return true, nil
			}
		}
		return true, fmt.Errorf("%s: only %s is supported: %s", key, strings.Join(a.values, "/"), a.reason)
	}
	if reason, ok := refused[key]; ok {
		return true, fmt.Errorf("%s is not supported: %s", key, reason)
	}
	return false, nil
}

// MultiValue reports keywords whose OpenSSH argument is a list of words.
func MultiValue(key string) bool {
	return key == "canonicaldomains"
}

// RawCommand recognizes the directives whose argument OpenSSH keeps as the
// raw rest of the line (RemoteCommand): after the keyword, separator spaces
// and '=' are skipped and trailing whitespace is removed; quotes, '#' and
// backslashes are part of the command.
func RawCommand(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	i := strings.IndexAny(line, " \t=")
	if i < 0 {
		return "", "", false
	}
	key = strings.ToLower(line[:i])
	if key != "remotecommand" {
		return "", "", false
	}
	return key, strings.TrimLeft(line[i:], " \t="), true
}

// ExpandCommand applies the Match exec token set to a RemoteCommand: %% %h
// (effective target) %n (destination as typed) %r (account, or the local
// login name when none) %u %d %i %l %L. SSH-only tokens (%p %C %k %j) fail,
// and %h/%n/%r values with shell metacharacters are refused rather than
// spliced into remote shell text.
func ExpandCommand(command, original, target, user string) (string, error) {
	p := newParser(Context{Host: original})
	p.values["hostname"] = strings.ReplaceAll(target, "%", "%%")
	if user != "" {
		p.values["user"] = user
	}
	text, err := p.execCommand(command)
	if err != nil {
		return "", errors.New(strings.Replace(err.Error(), "Match exec ", "", 1))
	}
	return text, nil
}
