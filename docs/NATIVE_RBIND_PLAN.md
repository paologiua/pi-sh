# Native -R bind addresses (wildcard, IPv6) — awaiting owner approval

Proposed 2026-10-28. Not yet authorized. One pi-sh process with one browser
companion step; nothing is written on the device; nothing is retried
automatically on failure.

## Why

The -R listener binds with sshd's `GatewayPorts clientspecified` rules
(FORWARDING.md "Remote forwarding"): no host means the loopback addresses,
`*` or an empty host every address, an explicit address only that one. On the
device only `127.0.0.1` has been exercised. The local tests run the same
listener on the Mac, whose address families and `bindv6only` differ from the
device's.

## Device facts (read-only check, 2026-10-28)

`lo` has `127.0.0.1/8` and `::1/128`; `wlan0` has one private IPv4 address and
an IPv6 link-local address only (no global IPv6); `eth0` is down;
`net.ipv6.bindv6only=0`; `localhost` resolves to `::1` and `127.0.0.1`;
`IO::Socket::IP` 0.42 is in perl-base; `hostname -I` prints one address.
Listening before the test: 127.0.0.1:11434 and port 22 only.

## Mac side

A temporary Perl counter on the Mac's 127.0.0.1:51125: it accepts at most 12
connections one at a time, reads each to EOF, replies with the byte count, and
exits after the 12th connection or after 90 s. Nothing else listens.

## The session

```sh
pi-sh --browser --experimental-command -T -v \
  -R 47131:127.0.0.1:51125 \
  -R '[::1]:47132:127.0.0.1:51125' \
  -R '*:47133:127.0.0.1:51125' \
  -R ':47134:127.0.0.1:51125' \
  -R '<device LAN IPv4>:47135:127.0.0.1:51125' \
  raspberrypi '<C>'
```

`<device LAN IPv4>` is the address `hostname -I` printed; `pi-sh -G` parses all
five forms exactly as `ssh -G` (checked locally). The command `<C>`, run from
memory:

```sh
ss -ltn '( sport >= :47131 and sport <= :47135 )'; L=$(hostname -I | cut -d" " -f1); timeout 30 perl -MIO::Socket::IP -e '$|=1; for (@ARGV) { my ($n,$a,$p)=/^(\w+)=(.*),(\d+)$/ or die; my $s=IO::Socket::IP->new(PeerHost=>$a,PeerPort=>$p,Timeout=>5); if(!$s){print "$p $n refused\n"; next} print $s "x" x 4096; shutdown($s,1); my $r=<$s>; print "$p $n reply $r\n"}' lo4=127.0.0.1,47131 lo6=::1,47131 lan=$L,47131 lo4=127.0.0.1,47132 lo6=::1,47132 lan=$L,47132 lo4=127.0.0.1,47133 lo6=::1,47133 lan=$L,47133 lo4=127.0.0.1,47134 lo6=::1,47134 lan=$L,47134 lo4=127.0.0.1,47135 lo6=::1,47135 lan=$L,47135
```

Each client connection sends 4096 bytes, shuts down its write side and prints
the reply; output names the address only by label (`lo4`, `lo6`, `lan`).

## Expected (sshd with GatewayPorts clientspecified)

| Forward | Listening sockets (`ss`) | lo4 | lo6 | lan |
| --- | --- | --- | --- | --- |
| `47131` (no host) | `127.0.0.1:47131`, `[::1]:47131` | 4096 | 4096 | refused |
| `[::1]:47132` | `[::1]:47132` | refused | 4096 | refused |
| `*:47133` | `0.0.0.0:47133`, `[::]:47133` | 4096 | 4096 | 4096 |
| `:47134` (empty host) | `0.0.0.0:47134`, `[::]:47134` | 4096 | 4096 | 4096 |
| `<LAN>:47135` | `<LAN>:47135` | refused | refused | 4096 |

Ten replies of `4096`; the Mac counter must count ten connections; the
command must end with status 0 and pi-sh with exit 0. Any other result is
recorded as found, not retried.

## Cleanup check (read-only)

A second session: `ps -eo pid,ppid,etime,args` and `ss -ltn`; nothing on
47131–47135, no `perl` or `nc` left.

## Exposure and bounds

- `*:47133`, `:47134` and `<LAN>:47135` listen on the device's Wi-Fi address,
  so other hosts on that LAN could connect while the session lasts (about
  10–20 s: the command is bounded by `timeout 30` and the session ends with it).
  A connection from elsewhere would reach only the Mac counter, which replies
  with a byte count and stops after 12 connections. Nothing is exposed to the
  internet beyond what the LAN's router already allows; no IPv6 global address
  exists.
- Ports 47131–47135 on the device; one device-side Perl process, bounded by
  `timeout 30`; about 60 KiB of data in total, generated in memory. No files,
  no installs, no configuration, no SSH transport.
- Excluded: IPv6 link-local targets (they need a scope id, `fe80::…%wlan0`)
  and remote Unix-domain listeners (refused by design; they would create a
  file).
