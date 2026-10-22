package sshdest

import "testing"

// Expectations were observed offline with OpenSSH_10.2p1 (ssh -G, and scp/sftp
// with -S pointing at a local argument-printing program); see openssh_unix_test.go.
func TestSSHDestinations(t *testing.T) {
	for _, tc := range []struct{ in, user, host, port string }{
		{"pi", "", "pi", ""}, {"alice@pi", "alice", "pi", ""}, {"a@b@pi", "a@b", "pi", ""},
		{"Pi #1", "", "Pi #1", ""}, {"[::1]", "", "[::1]", ""}, {"pi:22", "", "pi:22", ""},
		{"SSH://pi", "", "SSH://pi", ""}, {"u ser@pi", "u ser", "pi", ""},
		{"ssh://pi", "", "pi", ""}, {"ssh://pi/", "", "pi", ""}, {"ssh://pi:", "", "pi", ""},
		{"ssh://alice@pi:2222", "alice", "pi", "2222"}, {"ssh://al%40ice@pi", "al@ice", "pi", ""},
		{"ssh://a+b@pi", "a b", "pi", ""}, {"ssh://alice;x=y@pi", "alice", "pi", ""},
		{"ssh://alice:pw@pi", "alice:pw", "pi", ""}, {"ssh://[pi]:23", "", "pi", "23"},
		{"ssh://pi:http", "", "pi", "http"}, {"ssh://pi:+22", "", "pi", "+22"}, {"ssh://pi_x", "", "pi_x", ""},
		{"ssh://9pi", "", "9pi", ""}, {"ssh://pi:22/", "", "pi", "22"},
	} {
		d, err := SSH(tc.in)
		if err != nil || d.User != tc.user || d.Host != tc.host || d.Port != tc.port {
			t.Errorf("%q: %+v %v", tc.in, d, err)
		}
	}
	for _, in := range []string{"@pi", "a@", "ssh://", "ssh://@pi", "ssh://alice@", "ssh://;x@pi", "ssh://pi/path",
		"ssh://pi:0", "ssh://pi:65536", "ssh://pi:abc!", "ssh://[::1]", "ssh://[::1]:22", "ssh://pi;fingerprint=x",
		"ssh://p%20i", "ssh://al%zz@pi", "ssh://al%00x@pi", "ssh://al%4", "ssh://a@b@pi", "ssh://-pi", "ssh://.pi",
		"ssh://pi..x", "ssh://[pi]x", "ssh://pi:/", "ssh://[pi"} {
		if d, err := SSH(in); err == nil {
			t.Errorf("%q accepted: %+v", in, d)
		}
	}
}

func TestCopyOperands(t *testing.T) {
	for _, tc := range []struct {
		in, user, host, port, path string
		remote                     bool
	}{
		{"pi:x", "", "pi", "", "x", true}, {"alice@pi:x", "alice", "pi", "", "x", true},
		{"a@b@pi:x", "a@b", "pi", "", "x", true}, {"@pi:x", "", "pi", "", "x", true}, {"pi:", "", "pi", "", ".", true},
		{"[::1]:x", "", "::1", "", "x", true}, {"alice@[::1]:x", "alice", "::1", "", "x", true}, {"[pi]:x", "", "pi", "", "x", true},
		{"scp://alice@pi:2222/x", "alice", "pi", "2222", "x", true}, {"scp://pi//abs", "", "pi", "", "/abs", true},
		{"scp://pi", "", "pi", "", ".", true}, {"scp://pi/", "", "pi", "", ".", true}, {"scp://pi:22", "", "pi", "22", ".", true},
		{"Pi #1:x", "", "Pi #1", "", "x", true}, {"SCP://pi/x", "", "SCP", "", "//pi/x", true},
		{"scp://a%20b@pi/p%20q", "a b", "pi", "", "p q", true},
		{":x", "", "", "", "", false}, {"a/b:c", "", "", "", "", false}, {"alice@pi", "", "", "", "", false},
		{"[::1]", "", "", "", "", false}, {"C:\\x", "", "", "", "", false}, {"C:/x", "", "", "", "", false},
	} {
		d, remote, err := Copy(tc.in)
		if err != nil || remote != tc.remote || d.User != tc.user || d.Host != tc.host || d.Port != tc.port || d.Path != tc.path {
			t.Errorf("%q: %+v remote=%v %v", tc.in, d, remote, err)
		}
	}
	for _, in := range []string{"scp://[::1]/x", "scp://a@b@pi/x", "@:x", "x@:p", "scp://pi:0/x"} {
		if d, _, err := Copy(in); err == nil {
			t.Errorf("%q accepted: %+v", in, d)
		}
	}
}

func TestSFTPDestinations(t *testing.T) {
	for _, tc := range []struct{ in, user, host, port, path string }{
		{"pi", "", "pi", "", ""}, {"alice@pi", "alice", "pi", "", ""}, {"a@b@pi", "a@b", "pi", "", ""},
		{"pi:dir", "", "pi", "", "dir"}, {"pi:", "", "pi", "", ""}, {"alice@pi:dir", "alice", "pi", "", "dir"},
		{"[::1]", "", "::1", "", ""}, {"[::1]:dir", "", "::1", "", "dir"}, {"alice@[::1]", "alice", "::1", "", ""},
		{"sftp://alice@pi:2222/dir", "alice", "pi", "2222", "dir"}, {"sftp://pi", "", "pi", "", ""},
		{"sftp://pi/", "", "pi", "", ""}, {"sftp://pi//abs", "", "pi", "", "/abs"}, {"SFTP://pi", "", "SFTP", "", "//pi"},
		{"Pi #1", "", "Pi #1", "", ""}, {":x", "", ":x", "", ""}, {"scp://pi", "", "scp", "", "//pi"},
	} {
		d, err := SFTP(tc.in)
		if err != nil || d.User != tc.user || d.Host != tc.host || d.Port != tc.port || d.Path != tc.path || d.HasPath != (tc.path != "") {
			t.Errorf("%q: %+v %v", tc.in, d, err)
		}
	}
	for _, in := range []string{"", "@pi", "sftp://[::1]", "sftp://pi:0", "sftp://@pi"} {
		if d, err := SFTP(in); err == nil {
			t.Errorf("%q accepted: %+v", in, d)
		}
	}
}

func TestAddressLiteralsAndPorts(t *testing.T) {
	for _, h := range []string{"::1", "[::1]", "fe80::1%en0", "[fe80::1%en0]", "192.0.2.1", "[192.0.2.1]"} {
		if !AddressLiteral(h) {
			t.Errorf("%q not detected", h)
		}
	}
	for _, h := range []string{"pi", "Pi #1", "[pi]", "pi%1", "0.pi"} {
		if AddressLiteral(h) {
			t.Errorf("%q treated as address", h)
		}
	}
	for _, p := range []string{"1", "22", "+22", "022", "65535", "http", "ssh-alt", "x_y"} {
		if !ValidPort(p) {
			t.Errorf("port %q rejected", p)
		}
	}
	for _, p := range []string{"", "0", "65536", "-1", "+", "1x", "9http", "a b", "a!"} {
		if ValidPort(p) {
			t.Errorf("port %q accepted", p)
		}
	}
}
