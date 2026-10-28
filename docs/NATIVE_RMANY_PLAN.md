# Native -R with many connections — awaiting owner approval

Proposed 2026-10-28. Not yet authorized. One pi-sh process with one browser
companion step; nothing is written on the device; the only listener is the -R
listener on the device's 127.0.0.1, which ends with the session; nothing is
retried automatically on failure.

## Why

-L was verified with 64 concurrent connections on the device (FORWARDING.md
"Many streams"). -R multiplexes all its connections over one adapter stream
through the Perl listener; locally 16 and 64 concurrent connections carried
every byte, and a 65th now waits in the backlog instead of being closed
(`TestRemoteForwardingConnectionBeyondLimitWaits`, commit `bfa2a6a`). The
device's real carrier and kernel have not been exercised with many -R
connections.

## Device facts (read-only check, 2026-10-28)

`IO::Select` 1.55, `IO::Socket::INET` and `Time::HiRes` load from perl-base;
`ulimit -n` is 1024; nothing listens on 47141.

## Mac side

A temporary counter on the Mac's 127.0.0.1:51141 (Python, scratchpad): each
connection is read to EOF, answered with 65536 bytes and closed; it records
each request's length and exits after 150 connections or 300 s.

## The session

```sh
pi-sh --browser --experimental-command -T -v \
  -R 127.0.0.1:47141:127.0.0.1:51141 raspberrypi '<C>'
```

`<C>`, run from memory: three rounds of one Perl process each (one process,
not one per connection), bounded by `timeout 120`:

```sh
for k in 16 64 65; do timeout 120 perl -e '<CLIENT>' $k 65536 47141; done
```

`<CLIENT>` opens k connections to 127.0.0.1:47141, sends each 64 KiB read from
/dev/urandom (read only), shuts down its write side, reads the reply to EOF
and prints `k=K ok=N of K, T s, slowest S s` (a connection is ok when it
received 65536 bytes):

```perl
use IO::Socket::INET;use IO::Select;use Time::HiRes "time";$|=1;my($k,$n,$p)=@ARGV;open my $u,"<","/dev/urandom" or die;read $u,my $d,$n;my $t=time;my(%o,%g,%e);my $r=IO::Select->new;my $w=IO::Select->new;for(1..$k){my $s=IO::Socket::INET->new("127.0.0.1:$p") or die "connect: $!\n";$s->blocking(0);$o{$s}=0;$g{$s}=0;$r->add($s);$w->add($s)}while($r->count){my($rr,$ww)=IO::Select->select($r,$w->count?$w:undef,undef,60);last unless $rr||$ww;for my $s(@{$ww||[]}){my $x=syswrite($s,$d,$n-$o{$s},$o{$s});$o{$s}+=$x if $x;if($o{$s}>=$n){shutdown($s,1);$w->remove($s)}}for my $s(@{$rr||[]}){my $x=sysread($s,my $b,65536);if($x){$g{$s}+=$x}elsif(defined $x){$e{$s}=time-$t;$r->remove($s)}}}my $ok=grep{$g{$_}==$n}keys %g;printf "k=%d ok=%d of %d, %.1f s, slowest %.1f s\n",$k,$ok,$k,time-$t,(sort{$b<=>$a}values %e)[0]//0
```

(Checked locally against the Mac counter directly: `k=65 ok=65 of 65`.)

## Expected

Each round prints `ok=K of K`; in the 65 round one connection waits until
another ends, then completes. The Mac counter sees 145 connections, all with
65536 request bytes. Command status 0, pi-sh exit 0. Anything else is recorded
as found, not retried.

## Cleanup check (read-only)

A second session: `ps -eo pid,ppid,etime,args` and `ss -ltn`; nothing on
47141, no `perl` left.

## Bounds

Port 47141 on the device's loopback only; at most one device-side Perl client
process at a time (plus the -R listener), each bounded by `timeout 120`; at
most 65 device-side sockets at once; about 18 MiB in total (145 × 64 KiB each
way), generated in memory. No files, no installs, no configuration, no SSH
transport, nothing reachable from the LAN.
