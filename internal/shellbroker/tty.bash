# Independently authored in-memory TTY adapter. Existing Bash/cat/stty only.
_pt_linux_pendin() {
  # GNU stty has no pendin option. Change only this bit in a fresh, native
  # GNU/Linux snapshot; never import a client's termios layout or save string.
  local enabled=$1 version state word flags
  case $OSTYPE/$HOSTTYPE in
    linux*/aarch64|linux*/arm*|linux*/x86_64|linux*/i?86) :;;
    *) return 125;;
  esac
  version=$(stty --version 2>/dev/null) || return 125
  [[ $version == 'stty (GNU coreutils)'* ]] || return 125
  state=$(stty -g) || return 125
  ((${#state}<=1024)) || return 125
  [[ $state =~ ^[0-9a-fA-F]+(:[0-9a-fA-F]+)+$ ]] || return 125
  local IFS=:
  local -a fields=($state)
  ((${#fields[@]}==36)) || return 125
  local i
  for ((i=0;i<36;i++)); do
    word=${fields[i]}
    if ((i<4)); then ((${#word}<=8)) || return 125
    else ((${#word}<=2)) || return 125; fi
  done
  flags=$((16#${fields[3]}))
  if [[ $enabled == 1 ]]; then ((flags|=0x4000)); else ((flags&=~0x4000)); fi
  printf -v 'fields[3]' '%x' "$flags"
  state=${fields[*]}
  stty "$state" || return 125
  [[ $(stty -g) == "$state" ]]
}
_pt_stty_modes() {
  local state i
  state=$(stty -g) || return 125
  if [[ $state == gfmt1:* ]]; then
    # BSD stty parses a bare caret as NUL and a hex string as its first byte.
    # Its field-format input can set the literal caret without changing flags.
    # This is generated locally on the carrier, never a foreign stty -g snapshot.
    local -a original=("$@") translated=()
    for ((i=0;i<${#original[@]};i++)); do
      if [[ ${original[i+1]-} == 0x5e ]]; then
        case ${original[i]} in
          intr|quit|erase|kill|eof|eol|eol2|start|stop|susp|reprint|werase|lnext|discard)
            translated+=("gfmt1:${original[i]}=5e"); ((i++)); continue;;
        esac
      fi
      translated+=("${original[i]}")
    done
    stty "${translated[@]}"
  else
    local pendin= arg
    local -a translated=()
    for arg in "$@"; do
      case $arg in
        pendin) pendin=1;; -pendin) pendin=0;;
        reprint) translated+=(rprnt);;
        *) translated+=("$arg");;
      esac
    done
    if ((${#translated[@]}>0)); then stty "${translated[@]}" || return 125; fi
    if [[ -n $pendin ]]; then _pt_linux_pendin "$pendin"; fi
  fi
}
_pt_run() {
  set +m
  trap - CHLD INT QUIT
  trap ':' INT QUIT
  exec 0</dev/tty 1>/dev/tty 2>/dev/tty
  stty "$_pt_saved" || exit 125
  if ((${#_pt_modes[@]}>0)); then _pt_stty_modes "${_pt_modes[@]}" || exit 125; fi
  if [[ -n $_pt_columns ]]; then stty rows "$_pt_rows" cols "$_pt_columns" || exit 125; fi
  coproc _PT_JOBS { trap '' INT QUIT; exec cat 2>/dev/null; }
  local metadata_pid=$_PT_JOBS_PID
  exec 3<&${_PT_JOBS[0]} 4>&${_PT_JOBS[1]}
  eval "exec ${_PT_JOBS[0]}<&- ${_PT_JOBS[1]}>&-"
  exec 2>&4
  # Bash 5.2 read -t can report a timeout after consuming the delimiter. Poll
  # metadata with -t 0 and sleep with timed reads on this never-written pipe.
  coproc _PT_IDLE { trap '' INT QUIT TSTP TTIN TTOU; exec cat 3<&- 4>&- 2>/dev/null; }
  local idle_pid=$_PT_IDLE_PID
  exec 5<&${_PT_IDLE[0]} 6>&${_PT_IDLE[1]}
  eval "exec ${_PT_IDLE[0]}<&- ${_PT_IDLE[1]}>&-"
  trap 'jobs -l >&4' CHLD
  coproc _PT_CMD {
    IFS= read -r start || exit 125
    trap - INT QUIT PIPE
    exec 3<&- 4>&- 5<&- 6>&-
    if [[ $_pt_lc_set == x ]]; then export LC_ALL=$_pt_old_lc; else unset LC_ALL; fi
    exec "$_pt_shell" -c "$_pt_command" </dev/tty >/dev/tty 2>/dev/tty
  }
  local command_pid=$_PT_CMD_PID command_in=${_PT_CMD[1]} command_out=${_PT_CMD[0]}
  _pt_child=$command_pid
  # Readiness follows restored terminal modes; input cannot race stty's flush.
  printf '%sTR\037' "$_pt_prefix" || exit 125
  printf 'start\n' >&$command_in || exit 125
  eval "exec $command_in>&- $command_out<&-"
  local kind=unknown line state status refreshed=0 unknown_since=-1
  while [[ $kind == unknown ]]; do
    line=
    if IFS= read -r -t 0 <&3; then
      # This shell wrote each notice completely before the pipe exposed it.
      IFS= read -r line <&3 || exit 125
      if [[ $line =~ ^\[[0-9]+\][+-]?[[:space:]]+([0-9]+)[[:space:]]+(.*)$ && ${BASH_REMATCH[1]} == "$command_pid" ]]; then
        state=${BASH_REMATCH[2]}
        case $state in Done*|'Exit '*) kind=exit;; Running*|Stopped*) :;; *) kind=signal;; esac
      elif [[ $line =~ ^.*:[[:space:]]+line[[:space:]]+[0-9]+:[[:space:]]+([0-9]+)[[:space:]]+(.*)$ && ${BASH_REMATCH[1]} == "$command_pid" ]]; then
        kind=signal
      fi
    else
      IFS= read -r -t 0.05 line <&5
      (($?>128)) || exit 125
    fi
    ((${#line}<=4096)) || exit 125
    if ! kill -0 "$command_pid" 2>/dev/null; then
      if ((refreshed==0)); then jobs -l >&4; refreshed=1; fi
      ((unknown_since>=0)) || unknown_since=$SECONDS
      ((SECONDS-unknown_since<5)) || exit 125
    fi
  done
  wait "$command_pid"; status=$?
  _pt_child= # reaped: never signal a possibly reused PID
  trap - CHLD
  exec 2>/dev/null 4>&- 5<&- 6>&-
  kill "$metadata_pid" "$idle_pid" 2>/dev/null
  wait "$metadata_pid" "$idle_pid" 2>/dev/null
  if [[ $kind == signal ]]; then
    ((status>128 && status<=255)) || exit 125
    status=255
  else
    ((status>=0 && status<=255)) || exit 125
  fi
  # Result framing is emitted only after classification and predictable modes.
  stty raw -echo || exit 125
  _pt_complete=1
  printf '%sTX:%d\037' "$_pt_prefix" "$status"
  exit "$status"
}
_pt_main() {
  set +e; set +m
  local _pt_saved=$_ps_tty_saved _pt_old_lc=$_ps_old_lc _pt_lc_set=$_ps_lc_set
  local _pt_prefix=$_ps_prefix _pt_shell=${SHELL:-/bin/sh}
  local _pt_columns=$_ps_columns _pt_rows=$_ps_rows
  local -a _pt_modes=("${_ps_modes[@]}")
  local _pt_command=$_ps_command _pt_complete=0 _pt_owner=$$
  local _pt_child=
  # Bash -im acquired the dedicated carrier group before disabling monitor.
  # Never allow PID 1 to become kill's special process-selection operand -1.
  ((_pt_owner>1)) || exit 125
  _pt_abort() {
    trap - EXIT CHLD HUP TERM
    # A job-control shell (the login shell) leaves this group. Like sshd's
    # session leader it gets HUP (and CONT), which it resends to its jobs.
    if [[ $_pt_child =~ ^[0-9]+$ ]] && ((_pt_child>1)); then
      kill -HUP -- "$_pt_child" 2>/dev/null; kill -CONT -- "$_pt_child" 2>/dev/null
    fi
    # This is the fresh carrier's group, not an externally selected process.
    # The self PID fallback terminates the controller if group setup failed.
    kill -KILL -- "-$_pt_owner" "$_pt_owner" 2>/dev/null
    exit 125
  }
  trap _pt_abort HUP TERM
  trap '[[ $_pt_complete == 1 ]] || _pt_abort' EXIT
  # One supervisor stays in the carrier group with the command. Introducing a
  # separate foreground worker changes orphan-group TSTP/TTIN/TTOU semantics.
  _pt_run
}
_pt_main
