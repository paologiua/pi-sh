package forwardspec

import (
	"strings"
	"testing"
)

func noEnv(string) (string, bool) { return "", false }

func TestParseLocalTable(t *testing.T) {
	for _, tc := range []struct {
		spec, want string // want is the -G rendering; "" means rejected
	}{
		{"8080:h:80", "8080 [h]:80"},
		{"[::1]:81:[fe80::1]:22", "[::1]:81 [fe80::1]:22"},
		{"*:82:h:80", "[*]:82 [h]:80"},
		{":83:h:80", "[]:83 [h]:80"},
		{"/tmp/a.sock:h:80", "/tmp/a.sock [h]:80"},
		{"85:/r/s", "85 /r/s"},
		{"/a:/b", "/a /b"},
		{"1:h:2:", "1 [h]:2"},
		{"a\\:b:1:h:2", "[a:b]:1 [h]:2"},
		{"+5:h:2", "5 [h]:2"},
		{" 5:h:2", "5 [h]:2"},
		{"5:h: 2", "5 [h]:2"},
		{"1::2", "1 []:2"},
		{"1:h]:2", "1 [h]]:2"},
		{"[/a]:h:2", "/a [h]:2"},
		{"h", ""}, {"1:h", ""}, {"0:h:1", ""}, {"1:h:0", ""}, {"70000:h:1", ""},
		{"a:1:b:2:c", ""}, {"[::1:1:h:2", ""}, {"1:h:2 ", ""}, {"a\\/b:h:2", ""},
		{"[::1]x:1:h:2", ""}, {"/a:h", ""}, {"1:[h:2", ""}, {"", ""}, {"a\\", ""},
	} {
		f, ok := Local(tc.spec, noEnv)
		got := ""
		if ok {
			got = endpoint(f.Listen) + " " + endpoint(f.Connect)
		}
		if got != tc.want {
			t.Errorf("%q: got %q want %q", tc.spec, got, tc.want)
		}
	}
}

func TestParseRemoteDynamicStdio(t *testing.T) {
	if f, ok := Remote("9001", noEnv); !ok || !f.Dynamic() || f.Listen.Port != 9001 {
		t.Fatal("remote dynamic", f, ok)
	}
	if f, ok := Remote("0:h:22", noEnv); !ok || f.Listen.Port != 0 {
		t.Fatal("remote allocated port", f, ok)
	}
	for _, bad := range []string{"0:h:0", "1:h:0", "x"} {
		if _, ok := Remote(bad, noEnv); ok {
			t.Error("remote accepted", bad)
		}
	}
	for _, bad := range []string{"0", "a:b:c", "x", "70000"} {
		if _, ok := Dynamic(bad, noEnv); ok {
			t.Error("dynamic accepted", bad)
		}
	}
	if e, set, ok := Stdio("22", noEnv); !ok || set || e.Port != 22 {
		t.Fatal("bare -W port must parse without a target", e, set, ok)
	}
	if e, set, ok := Stdio("[::1]:22", noEnv); !ok || !set || e.Host != "::1" {
		t.Fatal(e, set, ok)
	}
	if e, set, ok := Stdio("/run/s", noEnv); !ok || !set || e.Path != "/run/s" {
		t.Fatal(e, set, ok)
	}
	if _, _, ok := Stdio("h", noEnv); ok {
		t.Fatal("-W h accepted")
	}
}

func TestDollarExpansion(t *testing.T) {
	env := func(k string) (string, bool) {
		if k == "DIR" {
			return "/tmp", true
		}
		return "", false
	}
	if f, ok := Local("${DIR}/s:h:1", env); !ok || f.Listen.Path != "/tmp/s" {
		t.Fatal(f, ok)
	}
	for _, bad := range []string{"${NOPE}:1:h:2", "${DIR:1:h:2", "${}:1:h:2"} {
		if _, ok := Local(bad, env); ok {
			t.Error("accepted", bad)
		}
	}
	if f, ok := Local("$DIR:1:h:2", env); !ok || f.Listen.Host != "$DIR" {
		t.Fatal("plain $ must stay literal", f, ok)
	}
}

func TestSettingsDirectivesAndFinish(t *testing.T) {
	s := NewSettings()
	if err := s.AddLocal("1:h:2", noEnv); err != nil {
		t.Fatal(err)
	}
	if err := s.AddLocal("1:h:2", noEnv); err != nil || len(s.Local) != 1 {
		t.Fatal("duplicate not dropped")
	}
	if err := s.Directive("localforward", []string{"1", "h:2"}, noEnv, false); err != nil || len(s.Local) != 1 {
		t.Fatal("config duplicate not dropped", err)
	}
	for _, tc := range []struct {
		key  string
		args []string
		msg  string
	}{
		{"localforward", []string{"3"}, "Missing target argument."},
		{"dynamicforward", []string{"5", "6"}, "extra arguments"},
		{"localforward", []string{"1", "h:2", "x"}, "extra arguments"},
		{"localforward", []string{"x", "h:2"}, "Bad forwarding specification."},
		{"permitremoteopen", []string{"h:1", "any"}, "must appear alone"},
		{"permitremoteopen", []string{"h"}, "bad port number"},
		{"permitremoteopen", []string{"::1:2"}, "bad port number"},
		{"permitremoteopen", []string{"h:0"}, "bad port number"},
		{"permitremoteopen", []string{"[h"}, "missing host"},
	} {
		if err := CheckDirective(tc.key, tc.args, noEnv); err == nil || !strings.Contains(err.Error(), tc.msg) {
			t.Errorf("%s %q: %v", tc.key, tc.args, err)
		}
	}
	if err := s.Directive("remoteforward", []string{"4"}, noEnv, false); err != nil || !s.Remote[0].Dynamic() {
		t.Fatal("RemoteForward with one argument is dynamic", err)
	}
	for _, v := range []string{"h:1", "*:*", "[::1]:2", "h/22", ":22"} {
		if err := CheckDirective("permitremoteopen", []string{v}, noEnv); err != nil {
			t.Error(v, err)
		}
	}
	for _, mask := range []string{"1777", "8", "abc", "07777"} {
		if ValidateScalar("streamlocalbindmask", mask) == nil {
			t.Error("mask accepted", mask)
		}
	}
	if err := s.SetScalar("streamlocalbindmask", "777"); err != nil || s.BindMask != 0o777 {
		t.Fatal(err)
	}
	if ValidateScalar("gatewayports", "clientspecified") == nil {
		t.Fatal("client GatewayPorts accepts only yes/no")
	}
	s.StdioSet = true
	s.Finish()
	if !s.ClearAll || !s.ExitOnForwardFailure || s.Local != nil || s.Remote != nil {
		t.Fatal("-W must clear forwardings and force ExitOnForwardFailure")
	}
}

func TestExpandPaths(t *testing.T) {
	s := NewSettings()
	for _, spec := range []string{"/tmp/%h-%%.sock:h:1", "1:/r/%n", "2:h:3"} {
		if err := s.AddLocal(spec, noEnv); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Expand(map[byte]string{'h': "dev", 'n': "alias"}); err != nil {
		t.Fatal(err)
	}
	if s.Local[0].Listen.Path != "/tmp/dev-%.sock" || s.Local[1].Connect.Path != "/r/alias" {
		t.Fatal(s.Local)
	}
	bad := NewSettings()
	_ = bad.AddLocal("/tmp/%r:h:1", noEnv)
	if err := bad.Expand(map[byte]string{'h': "dev"}); err == nil {
		t.Fatal("unsupported token accepted")
	}
}

func TestFeatureRefusals(t *testing.T) {
	for _, tc := range []struct {
		key  string
		args []string
		want error
	}{
		{"forwardagent", []string{"no"}, nil}, {"forwardagent", []string{"yes"}, ErrAgentForwarding},
		{"forwardagent", []string{"/tmp/s"}, ErrAgentForwarding},
		{"forwardx11", []string{"No"}, nil}, {"forwardx11", []string{"yes"}, ErrX11Forwarding},
		{"controlmaster", []string{"auto"}, ErrMultiplexing}, {"controlmaster", []string{"no"}, nil},
		{"controlpath", []string{"none"}, nil}, {"controlpath", []string{"/tmp/x"}, ErrMultiplexing},
		{"controlpersist", []string{"10m"}, ErrMultiplexing},
		{"tunnel", []string{"yes"}, ErrTunnel}, {"tunnel", []string{"no"}, nil},
		{"proxycommand", []string{"ssh", "-W", "%h:%p", "j"}, ErrProxy}, {"proxycommand", []string{"none"}, nil},
		{"proxyjump", []string{"j"}, ErrProxy}, {"proxyjump", []string{"none"}, nil},
		{"sessiontype", []string{"subsystem"}, ErrSubsystem}, {"sessiontype", []string{"none"}, nil},
	} {
		if err := ValidateFeature(tc.key, tc.args); err != tc.want {
			t.Errorf("%s %q: %v", tc.key, tc.args, err)
		}
	}
}
