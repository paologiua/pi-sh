#!/bin/zsh
# READ-ONLY native check of the login-shell adapter, local suspension, escapes
# and connection-loss reporting (docs/TTY_COMMANDS.md "Login shell").
#
# Nothing is written on the Pi by this script: commands only print, sleep or
# query the terminal. Every interactive login shell first runs `unset HISTFILE`
# so Bash does not append to the account's history file on exit. Startup files
# (~/.profile, ~/.bashrc) are read, exactly as an SSH login would read them.
# Without --execute it only prints the plan. Nothing is replayed on failure.
set -u
setopt pipefail
device=${PI_SH_DEVICE:-raspberrypi}
mode=${PI_SH_LOGIN:---direct-login}
repo=${0:A:h:h}
pish=$repo/bin/pi-sh
if [[ ${1:-} != --execute ]]; then
  sed -n '/^# PLAN/,/^# END PLAN/p' "$0"
  print "Dry run only. Pass --execute to run the automated part, then follow the manual steps."
  exit 0
fi
# PLAN
# A1  -T login shell, piped stdin: argv0 starts with "-", cwd is $HOME,
#     no TTY, exit 23.                                   expect status 23
# A2  -n login shell: stdin EOF ends the shell.          expect status 0
# A3  -tt login shell, piped keystrokes: TTY present, TERM forwarded,
#     size 0x0 (no local terminal), exit 23.             expect status 23
# M1  pi-sh $mode $device  (interactive)
#       type:  unset HISTFILE                  (first, always)
#       type:  printf '%s %s\n' "$0" "$TERM"; stty size   -> -bash <your TERM>, your size
#       resize the window, run stty size again             -> new size
#       type:  sleep 30   then Ctrl-Z, then: jobs; kill %1 -> "Stopped", then terminated
#       type:  ~?  ~v  ~V  ~#  ~B  ~R  ~&  ~C (each after Enter) -> local notices only
#       type:  ~^Z  -> local shell prompt; then fg               -> session continues
#       from another terminal: kill -TSTP <pi-sh pid>; then fg  -> same as ~^Z
#       type:  exit 23 ; then in the local shell: echo $?      -> 23
# M2  pi-sh $mode $device, unset HISTFILE, then ~.  ; echo $?  -> 255
# M3  pi-sh $mode $device, unset HISTFILE, then: sleep 3017
#     disable the Mac's network until pi-sh exits             -> 255 within ~30 s,
#     "remote broker cleanup was not confirmed"; record the elapsed time.
#     Re-enable the network. Read-only follow-up over SSH:
#       ssh <pi> "pgrep -af 'sleep 3017' || echo none"        -> expect none
#     (a survivor is evidence that the agent did not hang up the carrier PTY).
# END PLAN
pass() { print "$1: passed"; }
fail() { print "$1: FAILED ($2)"; failed=1; }
failed=0

print "== A1"
out=$(print -r -- 'printf "argv0=%s home=%s tty=%s\n" "$0" "$([ "$PWD" = "$HOME" ] && echo yes || echo no)" "$([ -t 0 ] && echo yes || echo no)"; exit 23' |
  "$pish" "$mode" -T "$device")
code=$?
print -r -- "$out"
[[ $code == 23 && $out == *"argv0=-"*"home=yes tty=no"* ]] && pass A1 || fail A1 "status $code"

print "== A2"
"$pish" "$mode" -n "$device" </dev/null
code=$?
[[ $code == 0 ]] && pass A2 || fail A2 "status $code"

print "== A3"
out=$(print -r -- 'unset HISTFILE; printf "tty=%s term=%s\n" "$([ -t 0 ] && echo yes)" "$TERM"; stty size; exit 23' |
  TERM=xterm-256color "$pish" "$mode" -tt "$device")
code=$?
print -r -- "$out"
[[ $code == 23 && $out == *"tty=yes term=xterm-256color"* && $out == *"0 0"* ]] && pass A3 || fail A3 "status $code"

print "Automated part finished (failed=$failed). Now follow M1-M3 from the plan."
exit $failed
