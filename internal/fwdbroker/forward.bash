# Independently authored pi-sh in-memory forwarding adapter. No files, named
# pipes, here-documents, here-strings, process substitution or Python. It runs
# connector programs that already exist on the device; Go chooses the argv.
set +m
set +e
trap '' PIPE
_fw_emit() { printf '%s%s\n' "$_ps_prefix" "$*"; }
_fw_fail() { _fw_emit 'Z protocol'; exit 125; }
declare -a _fw_all=() S_pid=() S_wpid=() S_epid=() S_out=() S_err=() S_wi=() S_wa=()
declare -a S_oseq=() S_oack=() S_oeof=() S_eeof=() S_iseq=() S_ipend=() S_iclosed=() S_waeof=() S_ebytes=()
_fw_idle_pid= _fw_relay_pid=
_fw_closeall() {
  local f k
  for f in "${!_fw_all[@]}"; do
    for k; do [[ $f == "$k" ]] && continue 2; done
    exec {f}>&-
  done
}
_fw_close() { local f=$1; [[ -n $f ]] || return 0; exec {f}>&-; unset "_fw_all[$f]"; }
_fw_kill_stream() {
  local id=$1 p
  for p in "${S_pid[id]}" "${S_wpid[id]}" "${S_epid[id]}"; do
    [[ $p =~ ^[0-9]+$ ]] && kill -KILL "$p" 2>/dev/null
  done
}
_fw_drop() {
  local id=$1
  _fw_kill_stream "$id"
  _fw_close "${S_out[id]}"; _fw_close "${S_err[id]}"; _fw_close "${S_wi[id]}"; _fw_close "${S_wa[id]}"
  wait "${S_pid[id]}" "${S_wpid[id]}" "${S_epid[id]}" 2>/dev/null
  unset "S_pid[id]" "S_wpid[id]" "S_epid[id]" "S_out[id]" "S_err[id]" "S_wi[id]" "S_wa[id]" "S_oseq[id]" "S_oack[id]" \
    "S_oeof[id]" "S_eeof[id]" "S_iseq[id]" "S_ipend[id]" "S_iclosed[id]" "S_waeof[id]" "S_ebytes[id]"
}
_fw_cleanup() {
  trap - EXIT HUP TERM
  local id
  for id in "${!S_pid[@]}"; do _fw_kill_stream "$id"; done
  [[ $_fw_idle_pid =~ ^[0-9]+$ ]] && kill "$_fw_idle_pid" 2>/dev/null
  [[ $_fw_relay_pid =~ ^[0-9]+$ ]] && kill "$_fw_relay_pid" 2>/dev/null
  wait 2>/dev/null
  :
}
_fw_tohex() {
  local s=$1 i h
  REPLY=
  for ((i=0;i<${#s};i++)); do printf -v h '%02x' "'${s:i:1}"; REPLY+=$h; done
}
_fw_unhex() {
  local h=$1 e= i
  [[ $h != *[!0123456789abcdef]* && $((${#h}%2)) == 0 && ${#h} -le 2048 && -n $h ]] || _fw_fail
  for ((i=0;i<${#h};i+=2)); do e+="\\x${h:i:2}"; done
  printf -v REPLY '%b' "$e"
}
_fw_detect() {
  local t out line n
  for t in nc ncat socat; do
    command -v "$t" >/dev/null 2>&1 || continue
    case $t in
      nc) out=$(nc -h 2>&1 </dev/null) ;;
      ncat) out=$(ncat --version 2>&1 </dev/null) ;;
      socat) out=$(socat -V 2>&1 </dev/null) ;;
    esac
    n=0
    while ((n<3)) && [[ -n $out ]]; do
      line=${out%%$'\n'*}
      if [[ $out == *$'\n'* ]]; then out=${out#*$'\n'}; else out=; fi
      [[ -n $line ]] || continue
      _fw_tohex "${line:0:200}"
      _fw_emit "T $t $REPLY"
      ((n++))
    done
    _fw_emit "T $t"
  done
}
_fw_writer() {
  local fd=$1 line seq hex e i
  exec 2>/dev/null
  while IFS= read -r line; do
    if [[ $line == F ]]; then exec {fd}>&-; exit 0; fi
    seq=${line%% *}; hex=${line#* }
    [[ $seq =~ ^[0-9]+$ && $hex != *[!0123456789abcdef]* && $((${#hex}%2)) == 0 && ${#hex} -le 2048 ]] || exit 125
    e=
    for ((i=0;i<${#hex};i+=2)); do e+="\\x${hex:i:2}"; done
    if ! printf '%b' "$e" >&"$fd"; then printf 'K\n'; exit 0; fi
    printf 'A %s\n' "$seq"
  done
  exit 0
}
_fw_open() {
  local id=$1 w er ew co ci wi wa
  shift
  [[ $id =~ ^[1-9][0-9]{0,9}$ && -z ${S_pid[id]} ]] || _fw_fail
  ((${#S_pid[@]}<64 && $#>0)) || _fw_fail
  local -a argv=()
  for w; do _fw_unhex "$w"; argv+=("$REPLY"); done
  coproc _FW_E { _fw_closeall; exec cat; } 2>/dev/null
  S_epid[id]=$_FW_E_PID
  exec {er}<&${_FW_E[0]} {ew}>&${_FW_E[1]}
  eval "exec ${_FW_E[0]}<&- ${_FW_E[1]}>&-"
  _fw_all[er]=1; _fw_all[ew]=1
  # The connector waits for a start line so a fast exit cannot make Bash
  # dispose of the coproc descriptors before they are duplicated.
  coproc _FW_C { exec 2>&"$ew"; _fw_closeall; IFS= read -r _fw_go || exit 125; exec "${argv[@]}"; } 2>/dev/null
  S_pid[id]=$_FW_C_PID
  exec {co}<&${_FW_C[0]} {ci}>&${_FW_C[1]}
  eval "exec ${_FW_C[0]}<&- ${_FW_C[1]}>&-"
  _fw_close "$ew"
  _fw_all[co]=1; _fw_all[ci]=1
  printf 'go\n' >&"$ci"
  coproc _FW_W { _fw_closeall "$ci"; _fw_writer "$ci"; } 2>/dev/null
  S_wpid[id]=$_FW_W_PID
  exec {wa}<&${_FW_W[0]} {wi}>&${_FW_W[1]}
  eval "exec ${_FW_W[0]}<&- ${_FW_W[1]}>&-"
  _fw_close "$ci"
  _fw_all[wa]=1; _fw_all[wi]=1
  S_out[id]=$co; S_err[id]=$er; S_wi[id]=$wi; S_wa[id]=$wa
  S_oseq[id]=0; S_oack[id]=0; S_oeof[id]=0; S_eeof[id]=0; S_iseq[id]=0; S_ipend[id]=0; S_iclosed[id]=0; S_waeof[id]=0; S_ebytes[id]=0
}
# Read up to max bytes already available on fd as hex; sets REPLY and
# _fw_eof=1 at end of file.
_fw_take() {
  local fd=$1 max=$2 n ch h
  REPLY= _fw_eof=0
  for ((n=0;n<max;n++)); do
    IFS= read -r -t 0 -u"$fd" || break
    ch=
    if IFS= read -r -d '' -n1 -u"$fd" ch; then
      if [[ -z $ch ]]; then REPLY+=00; else printf -v h '%02x' "'$ch"; REPLY+=$h; fi
    else
      _fw_eof=1
      break
    fi
  done
}
_fw_service() {
  local id=$1 line status
  local wa=${S_wa[id]} out=${S_out[id]} err=${S_err[id]}
  if ((S_waeof[id]==0)); then
    while IFS= read -r -t 0 -u"$wa"; do
      _fw_busy=1
      if IFS= read -r -u"$wa" line; then
        case $line in
          'A '*) ((S_ipend[id]>0)) || _fw_fail; ((S_ipend[id]--)); _fw_emit "a $id ${line#A }" ;;
          K) S_iclosed[id]=1; S_ipend[id]=0; _fw_emit "k $id" ;;
          *) _fw_fail ;;
        esac
      else
        S_waeof[id]=1; _fw_close "$wa"; S_wa[id]=
        if ((S_iclosed[id]==0)); then S_iclosed[id]=1; _fw_close "${S_wi[id]}"; S_wi[id]=; _fw_emit "k $id"; fi
        break
      fi
    done
  fi
  if ((S_eeof[id]==0)); then
    _fw_take "$err" 256
    if [[ -n $REPLY ]]; then
      _fw_busy=1
      if ((S_ebytes[id]<4096)); then _fw_emit "e $id $REPLY"; fi
      S_ebytes[id]=$((S_ebytes[id]+${#REPLY}/2))
    fi
    if ((_fw_eof)); then S_eeof[id]=1; _fw_close "$err"; S_err[id]=; _fw_busy=1; fi
  fi
  if ((S_oeof[id]==0 && S_oseq[id]-S_oack[id]<8)); then
    _fw_take "$out" 512
    if [[ -n $REPLY ]]; then _fw_busy=1; ((S_oseq[id]++)); _fw_emit "o $id ${S_oseq[id]} $REPLY"; fi
    if ((_fw_eof)); then S_oeof[id]=1; _fw_close "$out"; S_out[id]=; _fw_busy=1; _fw_emit "f $id"; fi
  fi
  if ((S_oeof[id] && S_eeof[id])) && ! kill -0 "${S_pid[id]}" 2>/dev/null; then
    wait "${S_pid[id]}"; status=$?
    _fw_emit "x $id $status"
    _fw_drop "$id"
    _fw_busy=1
  fi
}
_fw_record() {
  local record=$1 id seq hex
  case $record in
    'O '*) _fw_open ${record#O } ;;
    'I '*)
      record=${record#I }; id=${record%% *}; record=${record#* }; seq=${record%% *}; hex=${record#* }
      [[ $id =~ ^[0-9]+$ && $seq =~ ^[0-9]+$ && $hex != *[!0123456789abcdef]* && $((${#hex}%2)) == 0 && ${#hex} -le 2048 && -n $hex ]] || _fw_fail
      [[ -n ${S_pid[id]} ]] || return 0
      ((S_iclosed[id])) && return 0
      ((seq==S_iseq[id]+1 && S_ipend[id]<4)) || _fw_fail
      S_iseq[id]=$seq; ((S_ipend[id]++))
      printf '%s %s\n' "$seq" "$hex" >&"${S_wi[id]}" || { S_iclosed[id]=1; S_ipend[id]=0; _fw_emit "k $id"; }
      ;;
    'F '*)
      id=${record#F }
      [[ $id =~ ^[0-9]+$ ]] || _fw_fail
      [[ -n ${S_pid[id]} ]] || return 0
      if ((S_iclosed[id]==0)); then printf 'F\n' >&"${S_wi[id]}"; _fw_close "${S_wi[id]}"; S_wi[id]=; S_iclosed[id]=1; fi
      ;;
    'A '*)
      record=${record#A }; id=${record%% *}; seq=${record#* }
      [[ $id =~ ^[0-9]+$ && $seq =~ ^[0-9]+$ ]] || _fw_fail
      [[ -n ${S_pid[id]} ]] || return 0
      ((seq==S_oack[id]+1 && seq<=S_oseq[id])) || _fw_fail
      S_oack[id]=$seq
      ;;
    'C '*)
      id=${record#C }
      [[ $id =~ ^[0-9]+$ ]] || _fw_fail
      [[ -n ${S_pid[id]} ]] && _fw_drop "$id"
      _fw_emit "d $id"
      ;;
    Q) _fw_cleanup; _fw_emit D; exit 0 ;;
    *) _fw_fail ;;
  esac
}
_fw_main() {
  local record
  exec 2>/dev/null
  trap _fw_cleanup EXIT
  trap 'exit 125' HUP TERM
  # Timed reads on this never-written pipe pace the loop; protocol data is
  # only polled with read -t 0 (Bash 5.2 timed reads can lose a newline).
  coproc _FW_IDLE { exec cat; }
  _fw_idle_pid=$_FW_IDLE_PID
  local idle idlew
  exec {idle}<&${_FW_IDLE[0]} {idlew}>&${_FW_IDLE[1]}
  eval "exec ${_FW_IDLE[0]}<&- ${_FW_IDLE[1]}>&-"
  _fw_all[idle]=1; _fw_all[idlew]=1
  # Relay the carrier through a pipe, which Bash reads unbuffered.
  local carrier
  exec {carrier}<&0
  coproc _FW_RELAY { exec cat <&"$carrier"; }
  _fw_relay_pid=$_FW_RELAY_PID
  exec 0<&${_FW_RELAY[0]} {carrier}<&-
  eval "exec ${_FW_RELAY[0]}<&- ${_FW_RELAY[1]}>&-"
  _fw_detect
  _fw_emit R
  local id
  while :; do
    _fw_busy=0
    if IFS= read -r -t 0; then
      IFS= read -r record || exit 125
      ((${#record}<=2304+${#_ps_prefix})) || _fw_fail
      [[ $record == "$_ps_prefix"* ]] || _fw_fail
      _fw_record "${record#"$_ps_prefix"}"
      _fw_busy=1
    fi
    for id in "${!S_pid[@]}"; do _fw_service "$id"; done
    if ((_fw_busy==0)); then
      IFS= read -r -t 0.002 -u"$idle" record
      (($?>128)) || exit 125
    fi
  done
}
_fw_main
