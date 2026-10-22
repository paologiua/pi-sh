#!/bin/zsh
# Driver for docs/NATIVE_CLI_PLAN.md. Without --execute-approved it prints the plan.
# Every Connect session uses the browser companion; nothing is replayed on failure.
set -u
root=/tmp/pi-sh-cli-1368401b474527931b94c1e129f64cbf
device=${PI_SH_DEVICE:-raspberrypi}
repo=${0:A:h:h}
if [[ ${1:-} != --execute-approved ]]; then
  sed -n '/^```text/,/^```/p' "$repo/docs/NATIVE_CLI_PLAN.md"
  print "Dry run only. Pass --execute-approved after owner approval."
  exit 0
fi
work=$(mktemp -d) || exit 1
cd "$work" || exit 1
fail() { print "FAILED: $1 (root $root; no replay, cleanup needs a separate approved session)"; exit 1; }
gen() { perl -e 'binmode STDOUT; print chr($_ % 256) for 0..'"$(($2 - 1))" > "$1"; }
gen a.bin 65536; gen b.bin 1048576; gen d.bin 4096; print plain > c.txt
mkdir -p tree/empty dl dl2; gen tree/x.bin 1024; print 0123456789abcdef > "tree/s p ' 世界.bin"
touch -m -t 202001020304.05 tree/x.bin
sum() { shasum -a 256 < "$1" | cut -d' ' -f1; }

print "== session 1: pi-sftp upload/glob/rename/download/resume"
cat > s1.batch <<EOF
mkdir $root
chmod 700 $root
cd $root
put a.bin
put b.bin
put c.txt
mkdir sub
put d.bin sub/d.bin
ls *.bin
chmod 600 *.bin
rename c.txt "n ' 世界.txt"
ls
get *.bin dl
EOF
"$repo/bin/pi-sftp" --browser -b s1.batch "$device" > s1.out 2>&1 || { cat s1.out; fail session-1; }
for f in a.bin b.bin; do [[ $(sum $f) == $(sum dl/$f) ]] || fail "glob download integrity $f"; done
print "glob-download-integrity: passed"
head -c 300000 b.bin > dl/b-partial.bin
print "cd $root\nreget b.bin dl/b-partial.bin" > s1b.batch
"$repo/bin/pi-sftp" --browser -b s1b.batch "$device" > s1b.out 2>&1 || { cat s1b.out; fail resume; }
[[ $(sum b.bin) == $(sum dl/b-partial.bin) ]] || fail "resume integrity"
print "resume-integrity: passed"

print "== session 2: pi-cp recursive upload, remote glob, tilde"
"$repo/bin/pi-cp" --browser -r -p tree "$device:$root/tree" || fail "pi-cp -r -p upload"
"$repo/bin/pi-cp" --browser "$device:$root/*.bin" dl2 || fail "pi-cp remote glob"
for f in a.bin b.bin; do [[ $(sum $f) == $(sum dl2/$f) ]] || fail "pi-cp glob integrity $f"; done
print "pi-cp-glob-integrity: passed"
if "$repo/bin/pi-cp" --browser "$device:~/pi-sh-cli-missing-1368401b" dl2/missing > s2t.out 2>&1; then
  fail "tilde missing file unexpectedly succeeded"
fi
[[ ! -e dl2/missing ]] || fail "tilde failure left a local file"
print "tilde-missing-failure: passed"

print "== session 3: pi-sh diagnostics"
"$repo/bin/pi-sh" --browser --experimental-command -T -v "$device" 'exit 0' > s3.out 2> s3.err || fail "pi-sh -v"
grep -Eiq 'candidate|a=fingerprint|ice-pwd|[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+|token|cookie' s3.err && fail "diagnostics leaked sensitive data"
grep -q 'debug1' s3.err || fail "no debug1 output"
print "diagnostics-redaction: passed"

print "== session 4: cleanup"
cat > s4.batch <<EOF
cd $root
rm a.bin
rm b.bin
rm "n ' 世界.txt"
rm sub/d.bin
rmdir sub
rm tree/x.bin
rm "tree/s p ' 世界.bin"
rmdir tree/empty
rmdir tree
cd /tmp
rmdir $root
EOF
"$repo/bin/pi-sftp" --browser -b s4.batch "$device" > s4.out 2>&1 || { cat s4.out; fail cleanup; }

print "== session 5: verify absence"
print "ls $root" > s5.batch
if "$repo/bin/pi-sftp" --browser -b s5.batch "$device" > s5.out 2>&1; then fail "root still present"; fi
grep -qi 'not found\|no such' s5.out || { cat s5.out; fail "absence not confirmed"; }
print "cleanup-absence: passed"
cd / && rm -rf "$work"
print "ALL PASSED"
