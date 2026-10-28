# Native forwarding re-verification — approved and run

Proposed 2026-10-28 after the forwarding throughput change (FORWARDING.md
"Throughput"); approved by the owner and run the same day (results in
REAL_DEVICE_TESTS.md, "Native forwarding re-verification"). Each step is one pi-sh process with its own
browser companion step; nothing is written on the device; every listener binds
the device's or the Mac's loopback only and ends with the session. Nothing is
retried automatically on failure.

## Why

Read-only checks cannot reach these paths: the Perl connector now speaks hex
records and closes gracefully (`G`), and the -R listener now writes hex records
and drains buffered data on close. Both were re-verified only locally.

## Device-side programs (run from memory as the session's command)

L1, a one-connection counter on 127.0.0.1:47123 (the program approved on
2026-10-28, unchanged):

```sh
timeout 60 perl -MIO::Socket::INET -e '$|=1; my $l=IO::Socket::INET->new(LocalAddr=>"127.0.0.1",LocalPort=>47123,Listen=>1,ReuseAddr=>1) or die "listen: $!\n"; print "listening\n"; my $c=$l->accept; my $n=0; while(my $r=sysread($c,my $b,65536)){$n+=$r} print $c "$n"; close $c'
```

R1, a client of the -R listener that sends 1 MiB of /dev/urandom (read only),
shuts down its write side and prints the reply:

```sh
timeout 90 perl -MIO::Socket::INET -MTime::HiRes=time -e 'open my $u,"<","/dev/urandom" or die; read $u,my $d,1048576; my $s=IO::Socket::INET->new("127.0.0.1:47125") or die "connect: $!\n"; my $t=time; my $o=0; while($o<length $d){$o+=syswrite($s,$d,length($d)-$o,$o)} shutdown($s,1); my $r=<$s>; printf "reply %s %.1f s\n",$r,time-$t'
```

## Steps

1. `-L` half-close with the hex Perl connector, then with netcat:
   `PI_SH_FORWARD_CONNECTOR=perl pi-sh --browser --experimental-command -T -v -L 127.0.0.1:47124:127.0.0.1:47123 raspberrypi '<L1>'`;
   after `listening`, a Mac client sends 1 MiB of random bytes to 47124, shuts
   down its write side and expects `1048576`. Repeat with
   `PI_SH_FORWARD_CONNECTOR=nc`.
2. `-R` throughput, half-close, `~#` and end-of-session wait: a Mac-local Perl
   counter on 127.0.0.1:51125 (as on 2026-10-28), then under a real terminal
   (`expect`): `pi-sh --browser --experimental-command -tt -v -R 127.0.0.1:47125:127.0.0.1:51125 raspberrypi '<R1>; sleep 5'`;
   expect `reply 1048576`; type `\r~#` while R1 runs (expected line
   `#N 127.0.0.1 (Connect forwarding carrier)`); the exit status must be 0.
3. `-R 0` allocation: `pi-sh --browser -N -v -R 127.0.0.1:0:127.0.0.1:51125 raspberrypi`;
   expect `Allocated port N for remote forward to 127.0.0.1:51125`, then SIGTERM
   (exit 0). No device-side client connects.
4. Cleanup check (read-only): `ps -eo pid,ppid,etime,args` and `ss -ltn`;
   nothing on 47123, 47125 or the allocated port; no `perl`/`nc` left.

## Bounds

Ports 47123, 47124 (Mac), 47125 and one kernel-allocated port on the device's
127.0.0.1; at most two device-side Perl processes at a time, each bounded by
`timeout`; about 3 MiB of data in total, generated in memory. No files, no
installs, no configuration, no SSH transport.
