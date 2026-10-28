package fwdbroker

import (
	"errors"
	"net/netip"
	"strconv"
	"strings"

	"pi-sh/internal/forward"
)

// Connector is a remote program, already present on the device, that turns
// stdio into one outbound connection. Each candidate's required behaviour
// (binary transparency, stdin EOF => socket write shutdown, remote EOF =>
// stdout EOF, nonzero exit on connect failure) must be confirmed on the
// device (docs/REAL_DEVICE_TESTS.md) before relying on it.
type Connector struct {
	Name string
	// Ready (TCP) and UnixReady are stderr substrings printed once the
	// connection is open, or "" when the program prints none (then a nonzero
	// exit without output is classified as an open failure).
	Ready, UnixReady string
	// Hex is set for a pi-sh program that speaks the adapter's hex records on
	// stdio itself instead of raw bytes (forward.bash).
	Hex  bool
	argv func(forward.Target) ([]string, error)
}

// ReadyText is the readiness marker for a target, or "".
func (c Connector) ReadyText(t forward.Target) string {
	if t.Path != "" {
		return c.UnixReady
	}
	return c.Ready
}

// Argv returns the program arguments for a target, validating it first.
func (c Connector) Argv(t forward.Target) ([]string, error) { return c.argv(t) }

var (
	// OpenBSD netcat (Debian netcat-openbsd): -N shuts the socket down after
	// stdin EOF; -v reports "Connection to ... succeeded!". Netcat reads the
	// socket only as fast as the adapter takes its output, so -I asks for a
	// large TCP receive buffer (capped by the device's net.core.rmem_max, 4 MiB
	// on Raspberry Pi OS): a server can then hand over its whole reply and
	// close normally instead of resetting a slowly drained connection, which
	// would drop the unread tail (16 and 32 concurrent HTTPS downloads,
	// 2026-10-28: 3 of 48 truncated without it, 0 of 64 with it).
	openBSDNetcat = Connector{Name: "openbsd-nc", Ready: " succeeded!", argv: func(t forward.Target) ([]string, error) {
		if t.Path != "" {
			if err := checkPath(t.Path); err != nil {
				return nil, err
			}
			return []string{"nc", "-N", "-U", "--", t.Path}, nil
		}
		if err := checkHostPort(t); err != nil {
			return nil, err
		}
		return []string{"nc", "-N", "-I", strconv.Itoa(netcatReceiveBuffer), "-v", "--", t.Host, strconv.Itoa(t.Port)}, nil
	}}
	// Nmap ncat half-closes by default (it documents --no-shutdown to disable it).
	nmapNcat = Connector{Name: "ncat", Ready: "Ncat: Connected to", argv: func(t forward.Target) ([]string, error) {
		if t.Path != "" {
			if err := checkPath(t.Path); err != nil {
				return nil, err
			}
			return []string{"ncat", "-U", "--", t.Path}, nil
		}
		if err := checkHostPort(t); err != nil {
			return nil, err
		}
		return []string{"ncat", "-v", "--", t.Host, strconv.Itoa(t.Port)}, nil
	}}
	// Perl through the perl-base modules Debian marks Essential (Socket), so
	// it is present on every Raspberry Pi OS. The program is sent as an argv
	// word (see perlConnector); nothing is written remotely.
	perlSockets = Connector{Name: "perl", Ready: perlReady, UnixReady: perlReady, Hex: true, argv: func(t forward.Target) ([]string, error) {
		if t.Path != "" {
			if err := checkPath(t.Path); err != nil {
				return nil, err
			}
			return []string{"perl", "-e", perlConnector, "--", t.Path}, nil
		}
		if err := checkHostPort(t); err != nil {
			return nil, err
		}
		return []string{"perl", "-e", perlConnector, "--", t.Host, strconv.Itoa(t.Port)}, nil
	}}
	// socat shuts down the write side on EOF but then waits only -t seconds
	// for the other direction; a one-day bound is used and documented.
	socat = Connector{Name: "socat", Ready: "starting data transfer loop", UnixReady: "starting data transfer loop", argv: func(t forward.Target) ([]string, error) {
		if t.Path != "" {
			if err := checkPath(t.Path); err != nil || strings.ContainsAny(t.Path, ",:!") {
				return nil, errors.New("remote socket path not representable as a socat address")
			}
			return []string{"socat", "-d", "-d", "-t", "86400", "-", "UNIX-CONNECT:" + t.Path}, nil
		}
		if err := checkHostPort(t); err != nil {
			return nil, err
		}
		host := t.Host
		if strings.Contains(host, ":") {
			if strings.Contains(host, "%") {
				return nil, errors.New("scoped IPv6 addresses are not supported with socat")
			}
			host = "[" + host + "]"
		}
		return []string{"socat", "-d", "-d", "-t", "86400", "-", "TCP:" + host + ":" + strconv.Itoa(t.Port)}, nil
	}}
)

const (
	// netcatReceiveBuffer is the TCP receive buffer netcat requests (-I).
	netcatReceiveBuffer = 4 << 20
	perlReady           = "pi-sh-perl: connected"
	// Printed by the adapter's detection only when Socket loads.
	perlFingerprint = "pi-sh perl sockets"
	// perlConnector behaves like nc -N: one connection (getaddrinfo order,
	// or a Unix socket for a single operand), stdin EOF shuts the socket's
	// write side, remote EOF closes stdout while input still flows, and a
	// failed connect prints the system reason and exits 1. It speaks the
	// adapter's hex records: stdin is hex lines, and each read from the
	// socket (at most 2040 bytes) becomes one "length\nhex\n" record written
	// with a single syswrite, below PIPE_BUF so the adapter reads it whole.
	// Like sshd filling a channel window, it reads the socket as data comes,
	// queuing up to 2 MiB (ssh's default channel window) while the adapter
	// is busy (stdout is non-blocking): a server that closes after sending
	// must not find its data stuck in the device's socket, or it may reset
	// the connection and the kernel drops the unread tail (seen with 16
	// concurrent HTTPS downloads, 2026-10-28).
	perlConnector = `use Socket qw(:addrinfo AF_UNIX SOCK_STREAM pack_sockaddr_un);use Fcntl qw(F_GETFL F_SETFL O_NONBLOCK);
$SIG{PIPE}="IGNORE";my($s,$e,$b,$n,$l,$c,$z,@a,@q)=(undef,"failed","","","","",0);
if(@ARGV==1){@a=({family=>AF_UNIX,addr=>pack_sockaddr_un($ARGV[0])})}
else{($e,@a)=getaddrinfo(@ARGV,{socktype=>SOCK_STREAM})}
for(@a){socket($s,$_->{family},SOCK_STREAM,0)&&connect($s,$_->{addr}) and last;$e="$!";undef $s}
$s or print(STDERR "pi-sh-perl: $e\n"),exit 1;
print STDERR "pi-sh-perl: connected\n";
sub w{my($f,$k,$d)=(@_,0);while($d<length$k){my $r=syswrite($f,$k,length($k)-$d,$d);defined $r or exit 1;$d+=$r}}
fcntl(STDOUT,F_SETFL,fcntl(STDOUT,F_GETFL,0)|O_NONBLOCK) or exit 1;
my($i,$o)=(1,1);
while($i||$o||@q||length$c){my($r,$v)=("","");vec($r,0,1)=1 if $i;vec($r,fileno $s,1)=1 if $o&&$z<2097152;vec($v,1,1)=1 if @q||length$c;
select($r,$v,undef,undef)>0 or next;
if(vec($v,1,1)){if(!length$c){my $d=shift@q;$z-=length$d;my $h=unpack("H*",$d);$c=length($h)."\n$h\n"}my $k=syswrite(STDOUT,$c);if(defined $k){substr($c,0,$k)=""}elsif(!$!{EAGAIN}){exit 1}}
if($i&&vec($r,0,1)){$n=sysread(STDIN,$l,65536,length$l);if($n){my $p=rindex($l,"\n")+1;if($p){(my $h=substr($l,0,$p,""))=~tr/\n//d;w($s,pack("H*",$h))}}else{$i=0;shutdown($s,1)}}
if($o&&vec($r,fileno $s,1)){$n=sysread($s,$b,2040);if($n){push@q,$b;$z+=$n}else{$o=0}}
!$o&&!@q&&!length$c&&defined fileno STDOUT and close STDOUT}`
)

// Select picks the first recognized connector from the adapter's detection
// lines (tool name -> first output lines). Recognition requires an
// implementation fingerprint, never a bare command name.
func Select(detected map[string][]string) (Connector, bool) {
	has := func(tool, needle string) bool {
		for _, line := range detected[tool] {
			if strings.Contains(line, needle) {
				return true
			}
		}
		return false
	}
	switch {
	case has("perl", perlFingerprint):
		return perlSockets, true
	case has("nc", "OpenBSD netcat"):
		return openBSDNetcat, true
	case has("ncat", "Ncat: Version") || (has("ncat", "Ncat") && has("ncat", "nmap.org")):
		return nmapNcat, true
	case has("socat", "socat version 1.") || has("socat", "socat by Gerhard Rieger"):
		return socat, true
	}
	return Connector{}, false
}

// checkHostPort keeps targets to DNS names and IP literals so no value can be
// mistaken for an option or a connector address keyword.
func checkHostPort(t forward.Target) error {
	if t.Port < 1 || t.Port > 65535 {
		return errors.New("invalid remote port")
	}
	h := t.Host
	if h == "" || len(h) > 255 || h[0] == '-' {
		return errors.New("invalid remote host")
	}
	if a, err := netip.ParseAddr(h); err == nil && a.IsValid() {
		return nil
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == '_') {
			return errors.New("invalid remote host")
		}
	}
	return nil
}

func checkPath(p string) error {
	// 107 bytes is the longest path a Linux sockaddr_un holds.
	if p == "" || len(p) > 107 || p[0] == '-' {
		return errors.New("invalid remote socket path")
	}
	for i := 0; i < len(p); i++ {
		if p[i] < 0x20 || p[i] == 0x7f {
			return errors.New("invalid remote socket path")
		}
	}
	return nil
}
