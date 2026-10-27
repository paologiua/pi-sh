package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Keywords accepted only with the value matching pi-sh's fixed behaviour,
// RemoteCommand and RefuseConnection, compared with local ssh -G.
func TestCompletenessOpenSSHDifferential(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("local OpenSSH unavailable")
	}
	dir := t.TempDir()
	write := func(name, text string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	inert := write("inert", "Compression no\nCanonicalizeHostname no\nCanonicalDomains example.com example.org\n"+
		"CanonicalizeMaxDots 2\nCanonicalizeFallbackLocal yes\n"+
		"AddressFamily any\nForkAfterAuthentication no\nSessionType default\nPermitLocalCommand no\n"+
		"GSSAPIAuthentication no\nGSSAPIDelegateCredentials no\nHostbasedAuthentication no\nEnableSSHKeysign no\n"+
		"CheckHostIP no\nVisualHostKey no\nObscureKeystrokeTiming no\n")
	remote := write("remote", "Host pi\nRemoteCommand printf '%%s\\n' \"a  b\" # not a comment\\\nHost *\nRemoteCommand none\n")
	refuseInactive := write("refuse-inactive", "Host other\nRefuseConnection away\n")
	for _, args := range [][]string{
		{"-G", "-F", inert, "pi"},
		{"-G", "-F", "none", "-l", "me@example.com", "-o", "RemoteCommand=echo %h %% %n %r", "-oHostName=device", "pi"},
		{"-G", "-F", remote, "pi"},
		{"-G", "-F", remote, "other"},
		{"-G", "-F", "none", "-o", `RemoteCommand=echo  "a b" # c`, "pi"},
		{"-G", "-F", "none", "-oRemoteCommand=none", "pi"},
		{"-G", "-F", refuseInactive, "pi"},
	} {
		want, err := openSSHDump(t, ssh, args)
		if err != nil {
			t.Fatal(args, err)
		}
		compareDump(t, args, want)
		if _, ok := want["remotecommand"]; ok {
			o, _ := parseOptions(args)
			if o.remoteCommand != want["remotecommand"] {
				t.Errorf("%q: remotecommand %q, OpenSSH %q", args, o.remoteCommand, want["remotecommand"])
			}
		}
	}
	refuse := write("refuse", "Host pi\nRefuseConnection away\n")
	for _, args := range [][]string{
		{"-G", "-F", refuse, "pi"},
		{"-G", "-F", "none", "-oRefuseConnection=no", "pi"},
		{"-G", "-F", remote, "pi", "true"},
		{"-G", "-F", "none", "-oRemoteCommand=x", "pi", "true"},
	} {
		if _, err := exec.Command(ssh, args...).Output(); err == nil {
			t.Fatalf("reference accepted %q", args)
		}
		_, err := parseOptions(args)
		if err == nil {
			t.Errorf("pi-sh accepted %q", args)
		} else if strings.Contains(err.Error(), "away") {
			t.Errorf("RefuseConnection text echoed: %v", err)
		}
	}
}

// Documented differences: OpenSSH accepts these, pi-sh refuses them with a
// specific reason before authentication instead of ignoring them.
func TestCompletenessRefusals(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		reason string
	}{
		{[]string{"-G", "-4", "pi"}, "address family"},
		{[]string{"-G", "-6", "pi"}, "address family"},
		{[]string{"-G", "-oAddressFamily=inet", "pi"}, "address family"},
		{[]string{"-G", "-b", "127.0.0.1", "pi"}, "bind"},
		{[]string{"-G", "-B", "en0", "pi"}, "bind"},
		{[]string{"-G", "-oBindAddress=127.0.0.1", "pi"}, "bind"},
		{[]string{"-G", "-C", "pi"}, "compression"},
		{[]string{"-G", "-oCompression=yes", "pi"}, "compression"},
		{[]string{"-G", "-f", "pi"}, "background"},
		{[]string{"-G", "-oForkAfterAuthentication=yes", "pi"}, "background"},
		{[]string{"-G", "-s", "pi", "sftp"}, "subsystem"},
		{[]string{"-G", "-oSessionType=subsystem", "pi"}, "SessionType"},
		{[]string{"-G", "-i", "id", "pi"}, "authentication"},
		{[]string{"-G", "-I", "lib", "pi"}, "authentication"},
		{[]string{"-G", "-K", "pi"}, "authentication"},
		{[]string{"-G", "-k", "pi"}, "authentication"},
		{[]string{"-G", "-oIdentityFile=~/.ssh/id", "pi"}, "authentication"},
		{[]string{"-G", "-oPreferredAuthentications=password", "pi"}, "authentication"},
		{[]string{"-G", "-oPasswordAuthentication=yes", "pi"}, "authentication"},
		{[]string{"-G", "-oGSSAPIAuthentication=yes", "pi"}, "authentication"},
		{[]string{"-G", "-oHostbasedAuthentication=yes", "pi"}, "authentication"},
		{[]string{"-G", "-oCertificateFile=x", "pi"}, "authentication"},
		{[]string{"-G", "-oStrictHostKeyChecking=no", "pi"}, "host key"},
		{[]string{"-G", "-oUserKnownHostsFile=/dev/null", "pi"}, "host key"},
		{[]string{"-G", "-oCheckHostIP=yes", "pi"}, "host key"},
		{[]string{"-G", "-c", "aes128-ctr", "pi"}, "algorithms"},
		{[]string{"-G", "-m", "hmac-sha2-256", "pi"}, "algorithms"},
		{[]string{"-G", "-oCiphers=aes128-ctr", "pi"}, "algorithms"},
		{[]string{"-G", "-oKexAlgorithms=curve25519-sha256", "pi"}, "algorithms"},
		{[]string{"-G", "-oCanonicalizeHostname=yes", "pi"}, "canonicalization"},
		{[]string{"-G", "-oCanonicalizePermittedCNAMEs=*.a:*.b", "pi"}, "canonicalization"},
		{[]string{"-G", "-oSetEnv=A=b", "pi"}, "environment"},
		{[]string{"-G", "-oSendEnv=LANG", "pi"}, "environment"},
		{[]string{"-G", "-oPermitLocalCommand=yes", "pi"}, "LocalCommand"},
		{[]string{"-G", "-oIgnoreUnknown=Foo", "pi"}, "IgnoreUnknown"},
		{[]string{"-G", "-oIPQoS=lowdelay", "pi"}, "rekeying"},
		{[]string{"-G", "-oObscureKeystrokeTiming=yes", "pi"}, "keystroke"},
		{[]string{"-G", "-oVersionAddendum=x", "pi"}, "banner"},
		{[]string{"-G", "-oLogVerbose=kex.c:*", "pi"}, "debug"},
		{[]string{"-G", "-oProtocol=2", "pi"}, "obsolete"},
	} {
		_, err := parseOptions(tc.args)
		if err == nil || !strings.Contains(err.Error(), tc.reason) {
			t.Errorf("%q: %v", tc.args, err)
		}
	}
	if _, err := parseOptions([]string{"-G", "-oNoSuchKeyword=x", "pi"}); err == nil || !strings.Contains(err.Error(), "unsupported SSH configuration directive") {
		t.Errorf("unknown keyword: %v", err)
	}
}

// RemoteCommand supplies the command (expanded, joined as one command word)
// only with an explicit command adapter, as any other command does.
func TestRemoteCommandExecution(t *testing.T) {
	o, err := parseOptions([]string{"--direct-login", "--experimental-command", "-o", "RemoteCommand=echo %n %h %r %%", "-l", "me@example.com", "-oHostName=device", "pi"})
	if err != nil || !reflect.DeepEqual(o.args, []string{"pi", "echo pi device me@example.com %"}) {
		t.Fatalf("%q %v", o.args, err)
	}
	if _, err := parseOptions([]string{"--direct-login", "-oRemoteCommand=echo hi", "pi"}); err == nil {
		t.Fatal("RemoteCommand without a command adapter accepted")
	}
	for _, command := range []string{"echo %p", "echo %C", "echo %x", "echo %"} {
		if _, err := parseOptions([]string{"--direct-login", "--experimental-command", "-oRemoteCommand=" + command, "pi"}); err == nil {
			t.Errorf("%q accepted", command)
		}
	}
	if _, err := parseOptions([]string{"--direct-login", "--experimental-command", "-oRemoteCommand=echo %n", "Pi #1"}); err == nil {
		t.Error("%n with shell metacharacters must not be spliced into remote shell text")
	}
}
