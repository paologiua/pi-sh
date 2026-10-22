# Independently authored pi-sh in-memory broker. No here-documents, here-strings,
# process substitution, named pipes, files or Python. Bash >=4.3 is required.
set +m
set +e
trap '' PIPE
_ps_emit() { printf '%s%s\n' "$_ps_prefix" "$*"; }
_ps_fail() { _ps_emit 'Z protocol'; exit 125; }
_ps_cleanup() {
  trap - EXIT HUP TERM CHLD
  [[ ${_ps_done:-0} == 1 ]] || {
    [[ ${_ps_pid:-} =~ ^[0-9]+$ ]] && kill -KILL -- "-$_ps_pid" 2>/dev/null
    # Also cover cancellation before the command worker has entered setsid.
    [[ ${_ps_pid:-} =~ ^[0-9]+$ ]] && kill -KILL -- "$_ps_pid" 2>/dev/null
    [[ ${_ps_pid:-} =~ ^[0-9]+$ ]] && kill -KILL -- "-$_ps_pid" 2>/dev/null
  }
  [[ ${_ps_writer_pid:-} =~ ^[0-9]+$ ]] && kill "$_ps_writer_pid" 2>/dev/null
  [[ ${_ps_err_pid:-} =~ ^[0-9]+$ ]] && kill "$_ps_err_pid" 2>/dev/null
  [[ ${_ps_meta_pid:-} =~ ^[0-9]+$ ]] && kill "$_ps_meta_pid" 2>/dev/null
  [[ ${_ps_idle_pid:-} =~ ^[0-9]+$ ]] && kill "$_ps_idle_pid" 2>/dev/null
  [[ ${_ps_relay_pid:-} =~ ^[0-9]+$ ]] && kill "$_ps_relay_pid" 2>/dev/null
  wait "${_ps_pid:-}" "${_ps_writer_pid:-}" "${_ps_err_pid:-}" "${_ps_meta_pid:-}" "${_ps_idle_pid:-}" "${_ps_relay_pid:-}" 2>/dev/null
  :
}
_ps_main() {
  local _ps_shell=${SHELL:-/bin/sh}
  local _ps_pid _ps_writer_pid _ps_err_pid _ps_meta_pid _ps_idle_pid _ps_relay_pid _ps_done=0
  exec 2>/dev/null
  trap _ps_cleanup EXIT
  trap 'exit 125' HUP TERM
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
    printf 'start\n' >&6 || exit 125
    local line seq hex escaped i
    while IFS= read -r line; do
      if [[ $line == F ]]; then exec 6>&-; printf 'F\n'; exit 0; fi
      seq=${line%% *}; hex=${line#* }
      [[ $seq =~ ^[0-9]+$ && $hex != *[!0123456789abcdef]* && $((${#hex}%2)) == 0 && ${#hex} -le 2048 ]] || exit 125
      escaped=
      for ((i=0;i<${#hex};i+=2)); do escaped+="\\x${hex:i:2}"; done
      if ! printf '%b' "$escaped" >&6; then printf 'K\n'; exit 0; fi
      printf 'A %s\n' "$seq"
    done
    exec 6>&-
  } 2>/dev/null
  _ps_writer_pid=$_PS_INPUT_PID
  exec 7<&${_PS_INPUT[0]} 8>&${_PS_INPUT[1]}
  eval "exec ${_PS_INPUT[0]}<&- ${_PS_INPUT[1]}>&-"
  exec 6>&-
  # Bash 5.2 read -t can report a timeout after consuming the delimiter. Data
  # descriptors are therefore polled with -t 0 and read without a timeout; the
  # loop sleeps with timed reads on this pipe, which nothing ever writes.
  coproc _PS_IDLE { exec cat 3<&- 5<&- 7<&- 8>&- 9<&- 10>&- 2>/dev/null; }
  _ps_idle_pid=$_PS_IDLE_PID
  exec 11<&${_PS_IDLE[0]} 12>&${_PS_IDLE[1]}
  eval "exec ${_PS_IDLE[0]}<&- ${_PS_IDLE[1]}>&-"
  # Bash may buffer terminal reads (macOS), hiding queued records from -t 0.
  # Pipes are always read unbuffered, so relay the carrier through one.
  exec 13<&0
  coproc _PS_RELAY { exec cat <&13 3<&- 5<&- 7<&- 8>&- 9<&- 10>&- 11<&- 12>&- 13<&- 2>/dev/null; }
  _ps_relay_pid=$_PS_RELAY_PID
  exec 0<&${_PS_RELAY[0]} 13<&-
  eval "exec ${_PS_RELAY[0]}<&- ${_PS_RELAY[1]}>&-"
  local out_seq=0 err_seq=0 out_ack=0 err_ack=0 in_seq=0
  local out_eof=0 err_eof=0 input_closed=0 input_pending=0
  local record= line= status ch hex byte fd name seqvar eofvar ackvar
  local meta_line= meta_state= terminal_kind=unknown unknown_since=-1 refreshed=0
  _ps_emit R
  while :; do
    # Only this parent writes carrier records; workers communicate through pipes.
    # The client sends whole records, so a started line completes promptly.
    if IFS= read -r -t 0; then
      IFS= read -r record || exit 125
      ((${#record}<=2304+${#_ps_prefix})) || _ps_fail
      [[ $record == "$_ps_prefix"* ]] || _ps_fail
      record=${record#"$_ps_prefix"}
      case $record in
        'A O '*) line=${record#A O }; [[ $line =~ ^[0-9]+$ && $line -eq $((out_ack+1)) && $line -le $out_seq ]] || _ps_fail; out_ack=$line ;;
        'A E '*) line=${record#A E }; [[ $line =~ ^[0-9]+$ && $line -eq $((err_ack+1)) && $line -le $err_seq ]] || _ps_fail; err_ack=$line ;;
        'I '*)
          ((input_closed==0 && input_pending==0)) || _ps_fail
          line=${record#I }; status=${line%% *}; hex=${line#* }
          [[ $status =~ ^[0-9]+$ && $status -eq $((in_seq+1)) && $hex != *[!0123456789abcdef]* && $((${#hex}%2)) == 0 && ${#hex} -le 2048 ]] || _ps_fail
          in_seq=$status; input_pending=1
          printf '%s %s\n' "$status" "$hex" >&8 || { input_closed=1; _ps_emit K; }
          ;;
        F) ((input_pending==0)) || _ps_fail; if ((input_closed==0)); then printf 'F\n' >&8 || :; exec 8>&-; input_closed=1; fi ;;
        # D is a pi-sh application acknowledgement, never native agent status.
        # Kill/reap owned groups and helpers before confirming cancellation.
        C) _ps_cleanup; _ps_done=1; _ps_emit D; return 125 ;;
        *) _ps_fail ;;
      esac
    else
      IFS= read -r -t 0.001 -u11 line
      (($?>128)) || exit 125
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
      # Replies are single short writes; a failed read means the worker ended.
      if IFS= read -r -u7 line; then
        case $line in
          'A '*) [[ ${line#A } == "$in_seq" ]] || _ps_fail; input_pending=0; _ps_emit "A $in_seq" ;;
          K) input_closed=1; input_pending=0; exec 8>&-; _ps_emit K ;;
          *) _ps_fail ;;
        esac
      else
        input_closed=1; input_pending=0; exec 8>&-; _ps_emit K
      fi
    fi
    for name in O E; do
      if [[ $name == O ]]; then fd=5; seqvar=out_seq; eofvar=out_eof; ackvar=out_ack; else fd=3; seqvar=err_seq; eofvar=err_eof; ackvar=err_ack; fi
      local -n sequence=$seqvar ended=$eofvar acknowledged=$ackvar
      ((ended==0 && sequence-acknowledged<4)) || continue
      hex=
      for ((byte=0;byte<128;byte++)); do
        IFS= read -r -t 0 -u"$fd" || break
        ch=
        if IFS= read -r -d '' -n1 -u"$fd" ch; then
          if [[ -z $ch ]]; then hex+=00; else printf -v status '%02x' "'$ch"; hex+=$status; fi
        else
          status=$?
          if ((status==1)); then ended=1; fi
          break
        fi
      done
      if [[ -n $hex ]]; then ((sequence++)); _ps_emit "$name $sequence $hex"; fi
      if ((ended==1)); then _ps_emit "F $name"; fi
    done
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
