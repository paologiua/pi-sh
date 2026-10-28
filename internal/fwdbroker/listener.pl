# pi-sh remote listener for -R, run by perl-base from memory (see listener.go).
# It binds like sshd with GatewayPorts clientspecified, then multiplexes every
# accepted connection over stdio. Frames in both directions are
# pack("aNN", type, id, length) . data:
#   to pi-sh:   L 0 port | F 0 reason | N id "address port" | D id data |
#               E id (remote EOF) | A id N32 (bytes written) | C id (closed)
#   from pi-sh: D id data | E id (shut down write) | A id N32 (bytes consumed) |
#               C id (close)
# Each connection may have at most $W unacknowledged bytes in each direction.
# The frames travel inside the adapter's hex records: the loader's hin()
# decodes input into $p, and out() writes "length\nhex\n" records of at most
# 2040 bytes, each with one syswrite below PIPE_BUF.
use strict;
use Socket qw(:addrinfo SOCK_STREAM SOL_SOCKET SO_REUSEADDR SOMAXCONN AF_INET AF_INET6
  IPPROTO_IPV6 IPV6_V6ONLY inet_ntop sockaddr_family pack_sockaddr_in unpack_sockaddr_in
  pack_sockaddr_in6 unpack_sockaddr_in6);
use Fcntl qw(F_GETFL F_SETFL O_NONBLOCK);
$SIG{PIPE} = 'IGNORE';
my ($host, $port) = @ARGV;
$host = '' if $host eq '*';
my $W = 65536;
my $MAX = 64;
my (@ls, %c, $next);

sub frame {
  my ($t, $id, $d) = @_;
  $d = '' unless defined $d;
  my $b = pack('aNN', $t, $id, length $d) . $d;
  for (my $o = 0; $o < length $b; $o += 2040) {
    my $h = unpack('H*', substr($b, $o, 2040));
    my $r = length($h) . "\n$h\n";
    while (length $r) {
      my $k = syswrite(STDOUT, $r);
      if (!defined $k) { next if $!{EINTR}; exit 4 }
      substr($r, 0, $k) = '';
    }
  }
}
sub fail { frame('F', 0, $_[0]); exit 1 }
sub nonblock { my $f = fcntl($_[0], F_GETFL, 0); fcntl($_[0], F_SETFL, $f | O_NONBLOCK) }
sub addr_port {
  my ($sa) = @_;
  if (sockaddr_family($sa) == AF_INET6) { my ($p, $ip) = unpack_sockaddr_in6($sa); return (inet_ntop(AF_INET6, $ip), $p) }
  my ($p, $ip) = unpack_sockaddr_in($sa);
  return (inet_ntop(AF_INET, $ip), $p);
}
sub with_port {
  my ($sa, $p) = @_;
  if (sockaddr_family($sa) == AF_INET6) { my (undef, $ip, $s, $f) = unpack_sockaddr_in6($sa); return pack_sockaddr_in6($p, $ip, $s, $f) }
  my (undef, $ip) = unpack_sockaddr_in($sa);
  return pack_sockaddr_in($p, $ip);
}

# sshd refuses privileged ports to non-root users.
fail('privileged port') if $port > 0 && $port < 1024 && $> != 0;
my %hints = (socktype => SOCK_STREAM);
my $name;
if ($host eq '') { $hints{flags} = AI_PASSIVE }
elsif ($host ne 'localhost') { $name = $host }
my ($err, @ai) = getaddrinfo($name, $port, \%hints);
fail("$err") if $err;
my $why = 'no usable address';
my $bound = $port;
for my $ai (@ai) {
  my $s;
  if (!socket($s, $ai->{family}, SOCK_STREAM, 0)) { $why = "$!"; next }
  setsockopt($s, SOL_SOCKET, SO_REUSEADDR, 1);
  setsockopt($s, IPPROTO_IPV6, IPV6_V6ONLY, 1) if $ai->{family} == AF_INET6;
  # An allocated port is reused for the other addresses, as sshd does.
  my $sa = $bound ? with_port($ai->{addr}, $bound) : $ai->{addr};
  if (!bind($s, $sa) || !listen($s, SOMAXCONN)) { $why = "$!"; close $s; next }
  (undef, $bound) = addr_port(getsockname($s)) if !$bound;
  nonblock($s);
  push @ls, $s;
}
fail($why) if !@ls;
frame('L', 0, $bound);

sub drop { my ($id) = @_; my $k = delete $c{$id} or return; close $k->{s} }
sub parse {
  while (length $p >= 9) {
    my ($t, $id, $len) = unpack('aNN', $p);
    exit 5 if $len > $W;
    last if length $p < 9 + $len;
    my $d = substr($p, 9, $len);
    substr($p, 0, 9 + $len) = '';
    my $k = $c{$id} or next;
    if ($t eq 'D') { exit 5 if $k->{weof} || length($k->{out}) + $len > $W; $k->{out} .= $d }
    elsif ($t eq 'E') { $k->{weof} ||= 1 }
    elsif ($t eq 'A') { $k->{credit} += unpack('N', $d) }
    # Like sshd on CHANNEL_CLOSE after EOF: write what is buffered first.
    elsif ($t eq 'C') { if ($k->{weof} && length $k->{out}) { $k->{closing} = 1 } else { drop($id) } }
    else { exit 5 }
  }
}

# $p (the loader's decoded input) may already hold frames.
parse();
while (1) {
  my ($r, $w) = ('', '');
  vec($r, 0, 1) = 1;
  # At $MAX connections new ones wait in the listen backlog, as a -L
  # connection beyond the adapter's limit waits for a free stream.
  if (keys %c < $MAX) { vec($r, fileno $_, 1) = 1 for @ls }
  for my $k (values %c) {
    vec($r, fileno $k->{s}, 1) = 1 if !$k->{reof} && !$k->{closing} && $k->{credit} > 0;
    vec($w, fileno $k->{s}, 1) = 1 if length $k->{out};
  }
  if (select($r, $w, undef, undef) < 0) { next if $!{EINTR}; exit 6 }
  if (vec($r, 0, 1)) {
    my $k = hin();
    if (!defined $k) { exit 6 unless $!{EINTR} }
    elsif ($k == 0) { exit 0 }
    else { parse() }
  }
  for my $l (@ls) {
    next unless vec($r, fileno $l, 1);
    last if keys %c >= $MAX;
    my $peer = accept(my $s, $l) or next;
    nonblock($s);
    my $id = ++$next;
    $c{$id} = { s => $s, out => '', credit => $W, reof => 0, weof => 0 };
    my ($pa, $pp) = addr_port($peer);
    frame('N', $id, "$pa $pp");
  }
  for my $id (keys %c) {
    my $k = $c{$id} or next;
    my $f = fileno $k->{s};
    if (length $k->{out} && vec($w, $f, 1)) {
      my $x = syswrite($k->{s}, $k->{out});
      if (defined $x) { substr($k->{out}, 0, $x) = ''; frame('A', $id, pack('N', $x)) }
      elsif (!$!{EAGAIN} && !$!{EINTR}) { drop($id); frame('C', $id); next }
    }
    if ($k->{weof} == 1 && !length $k->{out}) { shutdown($k->{s}, 1); $k->{weof} = 2 }
    # pi-sh already forgot a closing connection: no frame is sent.
    if ($k->{closing}) { drop($id) if $k->{weof} == 2; next }
    if (!$k->{reof} && $k->{credit} > 0 && vec($r, $f, 1)) {
      my $n = $k->{credit} < 16384 ? $k->{credit} : 16384;
      my $x = sysread($k->{s}, my $b, $n);
      if ($x) { $k->{credit} -= $x; frame('D', $id, $b) }
      elsif (defined $x) { $k->{reof} = 1; frame('E', $id) }
      elsif (!$!{EAGAIN} && !$!{EINTR}) { drop($id); frame('C', $id); next }
    }
    if ($k->{reof} && $k->{weof} == 2) { drop($id); frame('C', $id) }
  }
}
