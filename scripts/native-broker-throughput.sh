#!/bin/zsh
# Read-only native broker throughput check (docs/SHELL_BROKER.md, 2026-10-23).
# Without --execute-approved it only prints the plan. Every run is one Connect
# session with its own browser companion step; nothing is replayed on failure.
# Remote commands only read: stdin to `wc -c`, `head -c` of /dev/zero and
# /dev/urandom, and `exit 0`. No remote file is written; nothing is installed.
# Timing starts at the first remote output byte (a marker printed before the
# transfer), so the manual companion step is excluded.
# Known issue: step 4 pipes pi-sh's stderr, which also carries the companion
# instruction, into the timer; run that step by hand with stderr to a file.
set -u
setopt pipefail
device=${PI_SH_DEVICE:-raspberrypi}
repo=${0:A:h:h}
size=1048576
plan="1. upload:   ${size}B of every byte value -> remote 'LC_ALL=C wc -c'
2. download: remote 'head -c $size /dev/zero' (stdout)
3. download: remote 'head -c $size /dev/urandom' (stdout)
4. stderr:   remote 'head -c 262144 /dev/zero >&2'
5. cleanup:  remote 'exit 0' with a local stdin that fails to read (a directory);
             expect 'local stdin read failed' and NO 'cleanup was not confirmed'"
if [[ ${1:-} != --execute-approved ]]; then
  print -r -- "$plan"
  print "Dry run only. Pass --execute-approved after owner approval."
  exit 0
fi
work=$(mktemp -d) || exit 1
cd "$work" || exit 1
pish=("$repo/bin/pi-sh" --browser --experimental-command -T "$device")
perl -e 'binmode STDOUT; print chr($_ % 256) for 0..'"$((size - 1))" > in.bin
# Reports bytes after the marker line and seconds from marker to EOF.
timer='use Time::HiRes "time"; binmode STDIN; my ($t0,$n,$buf);
while (my $r = sysread(STDIN, $buf, 65536)) { if (!defined $t0) { $t0 = time; $buf =~ s/\AS\n//s or die "no marker\n"; } $n += length $buf; }
defined $t0 or die "no output\n"; printf "%d %.3f\n", $n, time - $t0;'
report() { # name bytes seconds expected
  local rate=$(perl -e "printf '%.1f', $2 / 1024 / ($3 > 0 ? $3 : 1)")
  print "$1: bytes=$2 expected=$4 seconds=$3 KiB/s=$rate"
}

# Upload: prints the remote count and seconds from the marker to EOF.
counter='use Time::HiRes "time"; my $m = <STDIN>; $m eq "S\n" or die "no marker\n"; my $t0 = time;
local $/; my $c = <STDIN>; $c =~ s/\s+//g; printf "%s %.3f\n", $c, time - $t0;'

print "== 1 upload"
"${pish[@]}" 'printf "S\n"; LC_ALL=C wc -c' < in.bin | perl -e "$counter" | read n s && report upload "$n" "$s" "$size"

print "== 2 download /dev/zero"
"${pish[@]}" "printf 'S\n'; head -c $size /dev/zero" < /dev/null | perl -e "$timer" | read n s && report download-zero "$n" "$s" "$size"
print "== 3 download /dev/urandom"
"${pish[@]}" "printf 'S\n'; head -c $size /dev/urandom" < /dev/null | perl -e "$timer" | read n s && report download-urandom "$n" "$s" "$size"
print "== 4 stderr"
"${pish[@]}" "printf 'S\n' >&2; head -c 262144 /dev/zero >&2" < /dev/null 2>&1 >/dev/null | perl -e "$timer" | read n s && report stderr-zero "$n" "$s" 262144
print "== 5 cleanup after local stdin failure"
"${pish[@]}" 'exit 0' < / > c.out 2> c.err; status=$?
print "cleanup: status=$status"
grep -q 'local stdin read failed' c.err && print "cleanup: stdin failure reported" || print "cleanup: stdin failure NOT reported"
grep -q 'cleanup was not confirmed' c.err && print "cleanup: FAILED (unconfirmed)" || print "cleanup: confirmed (no unconfirmed diagnostic)"
cd / && rm -rf "$work"
