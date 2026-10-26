package connpolicy

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"pi-sh/internal/account"
	"pi-sh/internal/sshdest"
)

func TestPolicyValuesAndRefusals(t *testing.T) {
	p, err := FromValues(map[string]string{"batchmode": "YES", "connecttimeout": "1m30", "connectionattempts": "+3",
		"serveraliveinterval": "15", "serveralivecountmax": "2", "tcpkeepalive": "true", "channeltimeout": "none",
		"numberofpasswordprompts": "0", "hostname": "ignored"})
	if err != nil || !p.BatchMode || p.ConnectTimeout() != 90*time.Second || p.ConnectionAttempts != 3 ||
		p.DeadPeerTimeout() != 30*time.Second || p.PasswordPrompts != 0 {
		t.Fatalf("%+v %v", p, err)
	}
	if d := (Policy{ServerAliveIntervalSeconds: 10, ServerAliveCountMax: 0}).DeadPeerTimeout(); d != 10*time.Second {
		t.Fatal("CountMax 0 must disconnect after one interval", d)
	}
	if d := Default().DeadPeerTimeout(); d != 0 || Default().ConnectTimeout() != 0 {
		t.Fatal("defaults must keep transport/built-in limits")
	}
	if p, _ := FromValues(map[string]string{"connecttimeout": "0"}); p.ConnectTimeout() != 0 || p.ConnectTimeoutSeconds != 0 {
		t.Fatal("ConnectTimeout 0 must add no bound", p)
	}
	for _, tc := range []struct {
		values map[string]string
		want   error
	}{
		{map[string]string{"user": "pi"}, ErrUser},
		{map[string]string{"port": "22"}, ErrPort},
		{map[string]string{"port": "ssh", "user": "pi"}, ErrUser},
	} {
		if _, err := FromValues(tc.values); !errors.Is(err, tc.want) {
			t.Errorf("%v: %v", tc.values, err)
		}
	}
	for _, values := range []map[string]string{
		{"numberofpasswordprompts": "3"}, {"connectionattempts": "11"}, {"tcpkeepalive": "no"},
		{"channeltimeout": "session=5m"}, {"connectionattempts": "0"}, {"connecttimeout": "-1"},
		{"connecttimeout": "None"}, {"connecttimeout": "2147483648"}, {"connecttimeout": "5x"}, {"batchmode": "1"},
		{"serveralivecountmax": "-1"}, {"connectionattempts": "0x10"}, {"channeltimeout": "bogus"}, {"port": "0"},
	} {
		if _, err := FromValues(values); err == nil {
			t.Errorf("accepted %v", values)
		}
	}
	if err := Target("::1"); !errors.Is(err, ErrAddress) || Target("raspberrypi") != nil {
		t.Fatal("address literal policy", err)
	}
}

func TestAdmitNeverPrompts(t *testing.T) {
	batch := Default()
	batch.BatchMode = true
	for _, direct := range []bool{true, false} {
		if err := batch.Admit(direct); err == nil || !strings.Contains(err.Error(), "BatchMode") {
			t.Fatal("BatchMode admitted a prompting sign-in", direct, err)
		}
	}
	none := Default()
	none.PasswordPrompts = 0
	if none.Admit(true) == nil || none.Admit(false) != nil {
		t.Fatal("NumberOfPasswordPrompts 0 affects only the direct password prompt")
	}
	retry := Default()
	retry.ConnectionAttempts = 2
	if retry.Admit(false) == nil || retry.Admit(true) != nil {
		t.Fatal("browser companion cannot repeat signaling")
	}
}

// TestPolicyOpenSSHDifferential compares the -G fields with local `ssh -G`
// (offline) and confirms that both accept or both reject the syntax.
func TestPolicyOpenSSHDifferential(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("local OpenSSH unavailable")
	}
	for _, option := range []string{"BatchMode=no", "BatchMode=YES", "BatchMode=false", "ConnectTimeout=5",
		"ConnectTimeout=1m30s", "ConnectTimeout=0", "ConnectTimeout=none", "ConnectTimeout=1h1h", "ConnectTimeout=05",
		"ConnectTimeout=1w", "ConnectionAttempts=3", "ConnectionAttempts=+3", "ServerAliveInterval=1m",
		"ServerAliveInterval=none", "ServerAliveInterval=0s", "ServerAliveCountMax=0", "TCPKeepAlive=yes",
		"ChannelTimeout=none", "NumberOfPasswordPrompts=1",
		// Rejected by both for syntax:
		"ConnectTimeout=-1", "ConnectTimeout=abc", "ConnectTimeout=+5", "ConnectTimeout=None", "ConnectTimeout=2147483648",
		"ConnectionAttempts=0", "ConnectionAttempts=-1", "ConnectionAttempts=1x", "ServerAliveCountMax=-1",
		"TCPKeepAlive=maybe", "BatchMode=ask", "ChannelTimeout=bogus", "Port=0"} {
		out, refErr := exec.Command(ssh, "-G", "-F", "none", "-o", option, "pi").Output()
		key, value, _ := strings.Cut(option, "=")
		p, err := FromValues(map[string]string{strings.ToLower(key): value})
		if (refErr == nil) != (err == nil) {
			t.Errorf("%s: OpenSSH %v, pi-sh %v", option, refErr, err)
			continue
		}
		if err != nil {
			continue
		}
		want := map[string]string{}
		for _, line := range strings.Split(string(out), "\n") {
			k, v, _ := strings.Cut(line, " ")
			want[k] = v
		}
		var got bytes.Buffer
		if err := p.WriteConfig(&got); err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSpace(got.String()), "\n") {
			k, v, _ := strings.Cut(line, " ")
			if want[k] != v {
				t.Errorf("%s: %s pi-sh %q, OpenSSH %q", option, k, v, want[k])
			}
		}
	}
}

// The Raspberry Pi ID account plays the SSH user's role: an email user is
// accepted from any source, a Unix-style name is refused, and the browser
// companion refuses an explicit account it cannot select or verify.
func TestAccountPolicy(t *testing.T) {
	p, err := FromValues(map[string]string{"user": "me@example.com"})
	if err != nil || p.Account != "me@example.com" || p.LoginAccount() != "me@example.com" {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := FromValues(map[string]string{"user": "me@example.com", "port": "22"}); !errors.Is(err, ErrPort) {
		t.Fatal("port is still refused with an account", err)
	}
	for _, user := range []string{"pi", "root", "me@localhost", "me@example.com@pi"} {
		if _, err := FromValues(map[string]string{"user": user}); !errors.Is(err, ErrUser) || !strings.Contains(err.Error(), "email::device") {
			t.Errorf("%q: %v", user, err)
		}
	}
	if err := p.Admit(true); err != nil {
		t.Fatal(err)
	}
	if err := p.Admit(false); !errors.Is(err, ErrBrowserAccount) {
		t.Fatal("browser companion must refuse an explicit account", err)
	}
	defaulted := Default()
	defaulted.DefaultAccount = "default@example.com"
	if defaulted.LoginAccount() != "default@example.com" || defaulted.Admit(false) != nil {
		t.Fatal("the local default only prefills direct login and never blocks the companion")
	}
	defaulted.Account = "me@example.com"
	if defaulted.LoginAccount() != "me@example.com" {
		t.Fatal("an explicit account overrides the local default")
	}
	for _, tc := range []struct {
		d    sshdest.Destination
		want error
	}{
		{sshdest.Destination{Host: "pi"}, nil},
		{sshdest.Destination{User: "pi", Host: "raspberrypi"}, ErrUser},
		{sshdest.Destination{User: "me@example.com", Host: "raspberrypi"}, ErrUser},
		{sshdest.Destination{User: "me@example.com", Host: "pi", Account: true}, nil},
		{sshdest.Destination{User: "me@local", Host: "pi", Account: true}, account.ErrInvalid},
		{sshdest.Destination{User: "pi", Host: "pi", URI: true}, nil}, // judged later as a User value
	} {
		if err := DestinationUser(tc.d); !errors.Is(err, tc.want) {
			t.Errorf("%+v: %v", tc.d, err)
		}
	}
}
