# Independently authored pi-sh in-memory broker. No here-documents, here-strings,
# process substitution, named pipes, files or Python. Bash >=4.3 is required.
# Unacknowledged input records (inputWindow in broker.go, which must match)
# and output records per stream (this side only; broker.go "inputWindow").
_ps_in_win=8 _ps_out_win=32
# The whole broker runs in Perl (broker.pl, in _ps_perl_src) when the device's
# perl compiles it; this Bash broker is the fallback, kept as it is
# (SHELL_BROKER.md "Perl broker"). PI_SH_REMOTE_IMPL=bash keeps it here. It runs
# before SIGPIPE is ignored, so perl starts with the dispositions of entry.
if [[ -z ${_ps_bash_impl-} ]] && perl -c -e "$_ps_perl_src" </dev/null >/dev/null 2>&1; then
  shopt -s execfail
  exec perl -e "$_ps_perl_src" -- "$_ps_prefix" "$_ps_in_win" "$_ps_out_win" "${SHELL:-/bin/sh}" "$_ps_lc_set" "$_ps_old_lc" "$_ps_command" 2>/dev/null
  shopt -u execfail
fi
set +m
set +e
trap '' PIPE
_ps_emit() { printf '%s%s\n' "$_ps_prefix" "$*"; }
_ps_fail() { _ps_emit 'Z protocol'; exit 125; }
_ps_cleanup() {
  # A second hangup during cleanup must not end it: ignore, never default.
  trap '' HUP TERM; trap - EXIT CHLD
  [[ ${_ps_done:-0} == 1 ]] || {
    [[ ${_ps_pid:-} =~ ^[0-9]+$ ]] && kill -KILL -- "-$_ps_pid" 2>/dev/null
    # Also cover cancellation before the command worker has entered setsid.
    [[ ${_ps_pid:-} =~ ^[0-9]+$ ]] && kill -KILL -- "$_ps_pid" 2>/dev/null
    [[ ${_ps_pid:-} =~ ^[0-9]+$ ]] && kill -KILL -- "-$_ps_pid" 2>/dev/null
  }
  [[ ${_ps_writer_pid:-} =~ ^[0-9]+$ ]] && kill "$_ps_writer_pid" 2>/dev/null
  [[ ${_ps_err_pid:-} =~ ^[0-9]+$ ]] && kill "$_ps_err_pid" 2>/dev/null
  [[ ${_ps_out_pid:-} =~ ^[0-9]+$ ]] && kill "$_ps_out_pid" 2>/dev/null
  [[ ${_ps_eout_pid:-} =~ ^[0-9]+$ ]] && kill "$_ps_eout_pid" 2>/dev/null
  [[ ${_ps_meta_pid:-} =~ ^[0-9]+$ ]] && kill "$_ps_meta_pid" 2>/dev/null
  [[ ${_ps_idle_pid:-} =~ ^[0-9]+$ ]] && kill "$_ps_idle_pid" 2>/dev/null
  [[ ${_ps_relay_pid:-} =~ ^[0-9]+$ ]] && kill "$_ps_relay_pid" 2>/dev/null
  wait "${_ps_pid:-}" "${_ps_writer_pid:-}" "${_ps_err_pid:-}" "${_ps_out_pid:-}" "${_ps_eout_pid:-}" "${_ps_meta_pid:-}" "${_ps_idle_pid:-}" "${_ps_relay_pid:-}" 2>/dev/null
  :
}
# Output reader worker. Each newline token from the parent permits one record.
# Bash cannot learn how many pipe bytes are available and no timed read may
# consume data (Bash 5.2), so bytes after the first are polled with -t 0 and
# read singly. The first byte may block: this worker does nothing else. Each
# record is encoded by one printf; "'" with no character encodes NUL as 00.
_ps_reader() {
  local fd=$1 c a IFS=
  while read -r c; do
    a=()
    if read -r -d '' -n1 -u"$fd" c; then
      a[0]="'$c"
      while ((${#a[@]}<2048)) && read -t 0 -u"$fd"; do
        if read -r -d '' -n1 -u"$fd" c; then
          a+=("'$c")
        else
          printf -v c '%02x' "${a[@]}"
          printf '%s\n%s\nF\n' "${#c}" "$c"
          exit 0
        fi
      done
      printf -v c '%02x' "${a[@]}"
      printf '%s\n%s\n' "${#c}" "$c" || exit 0
    else
      printf 'F\n'
      exit 0
    fi
  done
}
# Output reader in Perl when the device has it (perl-base is Essential on
# Raspberry Pi OS): sysread takes what the pipe holds, up to 2048 bytes, where
# the Bash reader below reads one byte per call (about 18 KiB/s of command
# output on the device, 2026-10-28). Same contract: one record per newline
# token, "length\nhex\n", "F" at end of output. It is a single-quoted word, so
# it holds no apostrophe; it runs only if perl compiles it, else Bash reads.
_ps_perl_reader='use strict; open(my $i, "<&=", $ARGV[0]) or exit 125;
while (defined(my $t = <STDIN>)) {
  my $n = sysread($i, my $b, 2048);
  $n = sysread($i, $b, 2048) while !defined $n && $!{EINTR};
  if (!$n) { syswrite(STDOUT, "F\n"); exit 0 }
  my $h = unpack("H*", $b);
  my $r = length($h) . "\n$h\n";
  while (length $r) { my $w = syswrite(STDOUT, $r); exit 0 if !defined $w; substr($r, 0, $w) = "" }
}'
# Input writer in Perl when the device has it, for the same reason as the
# reader: the Bash writer decodes each record with printf %b (about 600 KiB/s
# locally, 300 KiB/s on the device). Same contract: "start" to the command
# first, then per "seq length" line and its hex the decoded bytes, "A seq"
# once written, "K" when the command stops reading, "F" after closing.
_ps_perl_writer='use strict; $SIG{PIPE} = "IGNORE";
open(my $o, ">&=", 9) or exit 125;
sub put { my $s = shift; while (length $s) { my $w = syswrite($o, $s); return 0 if !defined $w; substr($s, 0, $w) = "" } 1 }
put("start\n") or exit 125;
while (defined(my $l = <STDIN>)) {
  chomp $l;
  if ($l eq "F") { close($o); syswrite(STDOUT, "F\n"); exit 0 }
  my ($seq, $n) = $l =~ /\A([0-9]+) ([0-9]+)\z/ or exit 125;
  $n > 0 && $n <= 4096 && $n % 2 == 0 or exit 125;
  my $h;
  (read(STDIN, $h, $n + 1) // 0) == $n + 1 && substr($h, $n) eq "\n" or exit 125;
  $h = substr($h, 0, $n);
  $h =~ /\A[0-9a-f]*\z/ or exit 125;
  if (!put(pack("H*", $h))) { syswrite(STDOUT, "K\n"); exit 0 }
  syswrite(STDOUT, "A $seq\n");
}
close($o);'
_ps_read() {
  # The stream descriptor (3 or 5) comes from a coproc descriptor, so it is
  # close-on-exec, and Bash copies that flag when duplicating it; opening
  # /dev/fd/N instead gives perl a new descriptor 9 without it.
  if [[ -z ${_ps_bash_workers-} && -r /dev/fd/$1 ]] && perl -c -e "$_ps_perl_reader" </dev/null >/dev/null 2>&1; then
    shopt -s execfail
    exec perl -e "$_ps_perl_reader" 9 9<"/dev/fd/$1"
  fi
  _ps_reader "$1"
}
_ps_main() {
  local _ps_shell=${SHELL:-/bin/sh}
  local _ps_pid _ps_writer_pid _ps_err_pid _ps_out_pid _ps_eout_pid _ps_meta_pid _ps_idle_pid _ps_relay_pid _ps_done=0
  exec 2>/dev/null
  trap _ps_cleanup EXIT
  # The handler ignores further hangups first: a second one during cleanup
  # (from the EXIT trap) would otherwise exit before the group is killed.
  trap 'trap "" HUP TERM; exit 125' HUP TERM
  coproc _PS_ERR { exec cat 2>/dev/null; }
  _ps_err_pid=$_PS_ERR_PID
  exec 3<&${_PS_ERR[0]} 4>&${_PS_ERR[1]}
  eval "exec ${_PS_ERR[0]}<&- ${_PS_ERR[1]}>&-"
  coproc _PS_META { exec cat 3<&- 4>&- 2>/dev/null; }
  _ps_meta_pid=$_PS_META_PID
  exec 9<&${_PS_META[0]} 10>&${_PS_META[1]}
  eval "exec ${_PS_META[0]}<&- ${_PS_META[1]}>&-"
  # Noninteractive Bash can print and delete a signalled job before jobs -l
  # sees it (notably SIGKILL). Capture its own notice in the same metadata pipe.
  exec 2>&10
  # jobs retains exit-vs-signal information that numeric wait alone loses.
  # Write metadata directly to an anonymous pipe; never spawn inside this trap.
  trap 'jobs -l >&10' CHLD
  coproc _PS_CMD {
    IFS= read -r _ps_start || exit 125
    trap - INT QUIT PIPE
    exec 2>&4 3<&- 4>&- 9<&- 10>&-
    if [[ $_ps_lc_set == x ]]; then export LC_ALL=$_ps_old_lc; else unset LC_ALL; fi
    exec setsid --wait "$_ps_shell" -c "$_ps_command"
  } 2>/dev/null
  _ps_pid=$_PS_CMD_PID
  exec 5<&${_PS_CMD[0]} 6>&${_PS_CMD[1]}
  eval "exec ${_PS_CMD[0]}<&- ${_PS_CMD[1]}>&-"
  exec 4>&-
  coproc _PS_INPUT {
    exec 3<&- 5<&- 9<&- 10>&- 2>/dev/null
    # As in _ps_read, the command's input (6) is reopened for perl as 9.
    if [[ -z ${_ps_bash_workers-} && -w /dev/fd/6 ]] && perl -c -e "$_ps_perl_writer" </dev/null >/dev/null 2>&1; then
      shopt -s execfail
      exec perl -e "$_ps_perl_writer" 9>"/dev/fd/6" 6>&-
    fi
    printf 'start\n' >&6 || exit 125
    local seq n hex escaped i fast=0
    # Bash >=5.2 expands & in a substitution to the match: one-step escaping.
    if ((BASH_VERSINFO[0]>5||(BASH_VERSINFO[0]==5&&BASH_VERSINFO[1]>=2))); then
      shopt -s patsub_replacement && fast=1
    fi
    # The parent writes "seq length", then exactly length hex digits and a
    # newline, only after the previous acknowledgement: -N cannot stall.
    while IFS=' ' read -r seq n; do
      if [[ $seq == F ]]; then exec 6>&-; printf 'F\n'; exit 0; fi
      [[ $seq =~ ^[0-9]+$ && $n =~ ^[0-9]+$ ]] && ((n>0 && n<=4096 && n%2==0)) || exit 125
      IFS= read -r -N $((n+1)) hex || exit 125
      [[ ${hex:n} == $'\n' ]] || exit 125
      hex=${hex:0:n}
      [[ $hex != *[!0123456789abcdef]* ]] || exit 125
      if ((fast)); then
        escaped=${hex//??/\\x&}
      else
        escaped=
        for ((i=0;i<n;i+=2)); do escaped+="\\x${hex:i:2}"; done
      fi
      if ! printf '%b' "$escaped" >&6; then printf 'K\n'; exit 0; fi
      printf 'A %s\n' "$seq"
    done
    exec 6>&-
  } 2>/dev/null
  _ps_writer_pid=$_PS_INPUT_PID
  exec 7<&${_PS_INPUT[0]} 8>&${_PS_INPUT[1]}
  eval "exec ${_PS_INPUT[0]}<&- ${_PS_INPUT[1]}>&-"
  exec 6>&-
  coproc _PS_OUT { exec 3<&- 7<&- 8>&- 9<&- 10>&- 2>/dev/null; _ps_read 5; } 2>/dev/null
  _ps_out_pid=$_PS_OUT_PID
  exec 14<&${_PS_OUT[0]} 15>&${_PS_OUT[1]}
  eval "exec ${_PS_OUT[0]}<&- ${_PS_OUT[1]}>&-"
  coproc _PS_EOUT { exec 5<&- 7<&- 8>&- 9<&- 10>&- 14<&- 15>&- 2>/dev/null; _ps_read 3; } 2>/dev/null
  _ps_eout_pid=$_PS_EOUT_PID
  exec 16<&${_PS_EOUT[0]} 17>&${_PS_EOUT[1]}
  eval "exec ${_PS_EOUT[0]}<&- ${_PS_EOUT[1]}>&-"
  exec 3<&- 5<&-
  # Bash 5.2 read -t can report a timeout after consuming the delimiter. Data
  # descriptors are therefore polled with -t 0 and read without a timeout; the
  # loop sleeps with timed reads on this pipe, which nothing ever writes.
  coproc _PS_IDLE { exec cat 7<&- 8>&- 9<&- 10>&- 14<&- 15>&- 16<&- 17>&- 2>/dev/null; }
  _ps_idle_pid=$_PS_IDLE_PID
  exec 11<&${_PS_IDLE[0]} 12>&${_PS_IDLE[1]}
  eval "exec ${_PS_IDLE[0]}<&- ${_PS_IDLE[1]}>&-"
  # Bash may buffer terminal reads (macOS), hiding queued records from -t 0.
  # Pipes are always read unbuffered, so relay the carrier through one.
  exec 13<&0
  coproc _PS_RELAY { exec cat <&13 7<&- 8>&- 9<&- 10>&- 11<&- 12>&- 13<&- 14<&- 15>&- 16<&- 17>&- 2>/dev/null; }
  _ps_relay_pid=$_PS_RELAY_PID
  exec 0<&${_PS_RELAY[0]} 13<&-
  eval "exec ${_PS_RELAY[0]}<&- ${_PS_RELAY[1]}>&-"
  local out_seq=0 err_seq=0 out_ack=0 err_ack=0 out_req=0 err_req=0
  local in_seq=0 in_fwd=0 in_ack=0 out_eof=0 err_eof=0 input_closed=0 input_pending=0
  local record= line= status hex n fd tfd name seqvar eofvar ackvar reqvar idle plen=${#_ps_prefix} quiet=0 nap
  local -a inq=()
  local meta_line= meta_state= terminal_kind=unknown unknown_since=-1 refreshed=0
  _ps_emit R
  while :; do
    idle=1
    # Only this parent writes carrier records; workers communicate through pipes.
    # The client sends whole records, so a started record completes promptly.
    # read -N fetches its exact remaining length in few syscalls, never more.
    if IFS= read -r -t 0; then
      idle=0
      IFS= read -r -N $((plen+2)) record || exit 125
      [[ ${record:0:plen} == "$_ps_prefix" ]] || _ps_fail
      case ${record:plen} in
        'A ')
          IFS= read -r line || exit 125
          ((${#line}<=24)) || _ps_fail
          case $line in
            'O '*) line=${line#O }; [[ $line =~ ^[0-9]+$ && $line -eq $((out_ack+1)) && $line -le $out_seq ]] || _ps_fail; out_ack=$line ;;
            'E '*) line=${line#E }; [[ $line =~ ^[0-9]+$ && $line -eq $((err_ack+1)) && $line -le $err_seq ]] || _ps_fail; err_ack=$line ;;
            *) _ps_fail ;;
          esac
          ;;
        'I ')
          # "I seq length hex": up to _ps_in_win unacknowledged records of at most
          # 2048 bytes. Records arriving after K are validated and dropped.
          IFS= read -r -d ' ' status || exit 125
          IFS= read -r -d ' ' n || exit 125
          [[ $status =~ ^[0-9]{1,9}$ && $n =~ ^[0-9]{1,4}$ ]] && ((status==in_seq+1 && n>0 && n<=4096 && n%2==0 && in_seq-in_ack<_ps_in_win)) || _ps_fail
          IFS= read -r -N $((n+1)) hex || exit 125
          [[ ${hex:n} == $'\n' ]] || _ps_fail
          hex=${hex:0:n}
          [[ $hex != *[!0123456789abcdef]* ]] || _ps_fail
          in_seq=$status
          ((input_closed)) || inq[status]=$hex
          ;;
        $'F\n') ((input_pending==0 && in_fwd==in_seq)) || _ps_fail; if ((input_closed==0)); then printf 'F\n' >&8 || :; exec 8>&-; input_closed=1; fi ;;
        # D is a pi-sh application acknowledgement, never native agent status.
        # Kill/reap owned groups and helpers before confirming cancellation.
        $'C\n') _ps_cleanup; _ps_done=1; _ps_emit D; return 125 ;;
        *) _ps_fail ;;
      esac
    fi
    # The writer holds at most one record; the rest wait here, in memory.
    if ((input_closed==0 && input_pending==0 && in_fwd<in_seq)); then
      ((in_fwd++)); input_pending=1; idle=0
      printf '%s %s\n%s\n' "$in_fwd" "${#inq[in_fwd]}" "${inq[in_fwd]}" >&8 || { input_closed=1; input_pending=0; in_fwd=$in_seq; inq=(); exec 8>&-; _ps_emit K; }
      unset "inq[in_fwd]"
    fi
    while IFS= read -r -t 0 -u9; do
      # This shell wrote each notice completely before the pipe exposed it.
      if IFS= read -r -u9 meta_line; then
        ((${#meta_line}<=4096)) || _ps_fail
        if [[ $meta_line =~ ^\[[0-9]+\][+-]?[[:space:]]+([0-9]+)[[:space:]]+(.*)$ && ${BASH_REMATCH[1]} == "$_ps_pid" ]]; then
          meta_state=${BASH_REMATCH[2]}
          case $meta_state in
            Done*|'Exit '*) terminal_kind=exit ;;
            Running*|Stopped*) : ;;
            *) terminal_kind=signal ;;
          esac
        elif [[ $meta_line =~ ^.*:[[:space:]]+line[[:space:]]+[0-9]+:[[:space:]]+([0-9]+)[[:space:]]+(.*)$ && ${BASH_REMATCH[1]} == "$_ps_pid" ]]; then
          terminal_kind=signal
        fi
      else
        break
      fi
    done
    if ((input_closed==0)) && IFS= read -r -t 0 -u7; then
      idle=0
      # Replies are single short writes; a failed read means the worker ended.
      if IFS= read -r -u7 line; then
        case $line in
          'A '*) [[ ${line#A } == "$in_fwd" && $input_pending == 1 ]] || _ps_fail; input_pending=0; in_ack=$in_fwd; _ps_emit "A $in_fwd" ;;
          K) input_closed=1; input_pending=0; in_fwd=$in_seq; inq=(); exec 8>&-; _ps_emit K ;;
          *) _ps_fail ;;
        esac
      else
        input_closed=1; input_pending=0; in_fwd=$in_seq; inq=(); exec 8>&-; _ps_emit K
      fi
    fi
    for name in O E; do
      if [[ $name == O ]]; then fd=14; tfd=15; seqvar=out_seq; eofvar=out_eof; ackvar=out_ack; reqvar=out_req; else fd=16; tfd=17; seqvar=err_seq; eofvar=err_eof; ackvar=err_ack; reqvar=err_req; fi
      local -n sequence=$seqvar ended=$eofvar acknowledged=$ackvar requested=$reqvar
      ((ended==0)) || continue
      # The reader writes each "length / hex" pair whole, so -N cannot stall.
      while IFS= read -r -t 0 -u"$fd"; do
        idle=0
        if ! IFS= read -r -u"$fd" n || [[ $n == F ]]; then ended=1; _ps_emit "F $name"; break; fi
        [[ $n =~ ^[0-9]{1,4}$ ]] && ((n>0 && n<=4096 && sequence<requested)) || _ps_fail
        IFS= read -r -N $((n+1)) -u"$fd" hex || _ps_fail
        [[ ${hex:n} == $'\n' ]] || _ps_fail
        ((sequence++))
        _ps_emit "$name $sequence ${hex:0:n}"
      done
      # One token per record: at most _ps_out_win unacknowledged output records.
      while ((ended==0 && requested-acknowledged<_ps_out_win)); do
        printf '\n' >&"$tfd" || break
        ((requested++))
      done
    done
    # Idle rounds sleep 1 ms while records or acknowledgements are expected
    # within a round trip, then 20 ms once nothing has happened for about
    # 100 rounds: polling every millisecond kept an idle session's broker at
    # a quarter of a device core (2026-10-28). The first event after a pause
    # waits at most 20 ms more.
    if ((idle)); then
      if ((quiet<100)); then ((++quiet)); nap=0.001; else nap=0.02; fi
      IFS= read -r -t $nap -u11 line
      (($?>128)) || exit 125
    else
      quiet=0
    fi
    if ((out_eof && err_eof && out_seq==out_ack && err_seq==err_ack)); then
      if ! kill -0 "$_ps_pid" 2>/dev/null; then
        if [[ $terminal_kind == unknown ]]; then
          # SIGCHLD notifications can coalesce when helpers exit together.
          # Query the same shell's retained job table once more, directly.
          if ((refreshed==0)); then jobs -l >&10; refreshed=1; fi
          ((unknown_since>=0)) || unknown_since=$SECONDS
          ((SECONDS-unknown_since<5)) || _ps_fail
          continue
        fi
        wait "$_ps_pid"; status=$?
        trap - CHLD
        _ps_done=1
        _ps_cleanup
        if [[ $terminal_kind == signal && $status -gt 128 ]]; then
          _ps_emit "Y $((status-128))"
        elif [[ $terminal_kind == exit ]]; then
          _ps_emit "X $status"
        else
          _ps_fail
        fi
        return 0
      fi
    fi
  done
}
_ps_main
