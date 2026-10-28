# Independently authored pi-sh in-memory forwarding adapter. No files, named
# pipes, here-documents, here-strings, process substitution or Python. It runs
# connector programs that already exist on the device; Go chooses the argv.
# A stream is raw (the connector's stdio carries the connection's bytes) or
# hex (a pi-sh Perl program reads hex lines and writes "length\nhex\n"
# records itself, so Bash never handles single bytes).
set +m
set +e
trap '' PIPE
_fw_emit() { printf '%s%s\n' "$_ps_prefix" "$*"; }
_fw_fail() { _fw_emit 'Z protocol'; exit 125; }
declare -a _fw_all=() S_pid=() S_wpid=() S_epid=() S_rpid=() S_out=() S_tok=() S_err=() S_wi=() S_wa=() S_mode=()
declare -a S_oseq=() S_oack=() S_oeof=() S_eeof=() S_iseq=() S_ifwd=() S_iack=() S_ifin=() S_iclosed=() S_waeof=() S_ebytes=() S_grace=() S_q=()
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
  for p in "${S_pid[id]}" "${S_wpid[id]}" "${S_epid[id]}" "${S_rpid[id]}"; do
    [[ $p =~ ^[0-9]+$ ]] && kill -KILL "$p" 2>/dev/null
  done
}
_fw_unqueue() { local id=$1 k; for ((k=id*8;k<id*8+8;k++)); do unset "S_q[k]"; done; }
_fw_drop() {
  local id=$1
  _fw_kill_stream "$id"
  _fw_close "${S_out[id]}"; _fw_close "${S_tok[id]}"; _fw_close "${S_err[id]}"; _fw_close "${S_wi[id]}"; _fw_close "${S_wa[id]}"
  wait "${S_pid[id]}" "${S_wpid[id]}" "${S_epid[id]}" ${S_rpid[id]} 2>/dev/null
  _fw_unqueue "$id"
  unset "S_pid[id]" "S_wpid[id]" "S_epid[id]" "S_rpid[id]" "S_out[id]" "S_tok[id]" "S_err[id]" "S_wi[id]" "S_wa[id]" "S_mode[id]" \
    "S_oseq[id]" "S_oack[id]" "S_oeof[id]" "S_eeof[id]" "S_iseq[id]" "S_ifwd[id]" "S_iack[id]" "S_ifin[id]" "S_iclosed[id]" \
    "S_waeof[id]" "S_ebytes[id]" "S_grace[id]"
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
  [[ $h != *[!0123456789abcdef]* && $((${#h}%2)) == 0 && ${#h} -le 4096 && -n $h ]] || _fw_fail
  for ((i=0;i<${#h};i+=2)); do e+="\\x${h:i:2}"; done
  printf -v REPLY '%b' "$e"
}
_fw_detect() {
  local t out line n
  for t in nc ncat socat perl; do
    command -v "$t" >/dev/null 2>&1 || continue
    case $t in
      nc) out=$(nc -h 2>&1 </dev/null) ;;
      ncat) out=$(ncat --version 2>&1 </dev/null) ;;
      socat) out=$(socat -V 2>&1 </dev/null) ;;
      perl) out=$(perl -MSocket=getaddrinfo,pack_sockaddr_un -e 'print "pi-sh perl sockets\n"' 2>&1 </dev/null) ;;
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
# Input writer: receives one "seq length" line and its hex at a time and
# acknowledges after writing it to the connector. A raw stream decodes the hex
# (one pattern substitution on Bash >=5.2); a hex stream passes it through.
_fw_writer() {
  local fd=$1 mode=$2 seq n hex e i fast=0
  exec 2>/dev/null
  if [[ $mode == r ]] && ((BASH_VERSINFO[0]>5||(BASH_VERSINFO[0]==5&&BASH_VERSINFO[1]>=2))); then
    shopt -s patsub_replacement && fast=1
  fi
  while IFS=' ' read -r seq n; do
    if [[ $seq == F ]]; then exec {fd}>&-; exit 0; fi
    [[ $seq =~ ^[0-9]+$ && $n =~ ^[0-9]{1,4}$ ]] && ((n>0 && n<=4096 && n%2==0)) || exit 125
    IFS= read -r -N $((n+1)) hex || exit 125
    hex=${hex:0:n}
    if [[ $mode == h ]]; then
      printf '%s\n' "$hex" >&"$fd" || { printf 'K\n'; exit 0; }
    else
      if ((fast)); then
        e=${hex//??/\\x&}
      else
        e=
        for ((i=0;i<n;i+=2)); do e+="\\x${hex:i:2}"; done
      fi
      printf '%b' "$e" >&"$fd" || { printf 'K\n'; exit 0; }
    fi
    printf 'A %s\n' "$seq"
  done
  exit 0
}
# Raw output reader (as the broker's): each newline token permits one record.
# Bash cannot learn how many pipe bytes are available and no timed read may
# consume data (Bash 5.2), so bytes after the first are polled with -t 0 and
# read singly; the record is encoded by one printf ("'" alone encodes NUL).
_fw_reader() {
  local fd=$1 c a IFS=
  exec 2>/dev/null
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
_fw_open() {
  local id=$1 mode=$2 w er ew co ci wi wa ro rt
  shift 2
  [[ $id =~ ^[1-9][0-9]{0,9}$ && -z ${S_pid[id]} && ( $mode == r || $mode == h ) ]] || _fw_fail
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
  coproc _FW_W { _fw_closeall "$ci"; _fw_writer "$ci" "$mode"; } 2>/dev/null
  S_wpid[id]=$_FW_W_PID
  exec {wa}<&${_FW_W[0]} {wi}>&${_FW_W[1]}
  eval "exec ${_FW_W[0]}<&- ${_FW_W[1]}>&-"
  _fw_close "$ci"
  _fw_all[wa]=1; _fw_all[wi]=1
  if [[ $mode == r ]]; then
    coproc _FW_R { _fw_closeall "$co"; _fw_reader "$co"; } 2>/dev/null
    S_rpid[id]=$_FW_R_PID
    exec {ro}<&${_FW_R[0]} {rt}>&${_FW_R[1]}
    eval "exec ${_FW_R[0]}<&- ${_FW_R[1]}>&-"
    _fw_close "$co"
    _fw_all[ro]=1; _fw_all[rt]=1
    printf '\n\n\n\n\n\n\n\n' >&"$rt"
    co=$ro S_tok[id]=$rt
  fi
  S_out[id]=$co; S_err[id]=$er; S_wi[id]=$wi; S_wa[id]=$wa; S_mode[id]=$mode
  S_oseq[id]=0; S_oack[id]=0; S_oeof[id]=0; S_eeof[id]=0; S_iseq[id]=0; S_ifwd[id]=0; S_iack[id]=0; S_ifin[id]=0
  S_iclosed[id]=0; S_waeof[id]=0; S_ebytes[id]=0
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
_fw_inclosed() { S_iclosed[$1]=1; _fw_unqueue "$1"; _fw_emit "k $1"; }
# Hand the writer the next queued input record (it holds one at a time, so
# the parent never blocks on its pipe), or EOF once every record is written.
_fw_pump() {
  local id=$1 seq h
  ((S_iclosed[id]==0 && S_ifwd[id]==S_iack[id])) || return 0
  if ((S_ifwd[id]<S_iseq[id])); then
    seq=$((S_ifwd[id]+1)) h=${S_q[id*8+(S_ifwd[id]+1)%8]}
    printf '%s %s\n%s\n' "$seq" "${#h}" "$h" >&"${S_wi[id]}" || { _fw_inclosed "$id"; return 0; }
    S_ifwd[id]=$seq
  elif ((S_ifin[id])); then
    printf 'F\n' >&"${S_wi[id]}"
    _fw_close "${S_wi[id]}"; S_wi[id]=; S_iclosed[id]=1; _fw_unqueue "$id"
  fi
}
# A malformed record from a hex connector ends that stream only.
_fw_badout() { local id=$1; _fw_kill_stream "$id"; S_oeof[id]=1; _fw_close "${S_out[id]}"; S_out[id]=; _fw_emit "f $id"; }
_fw_service() {
  local id=$1 line status n hex
  local wa=${S_wa[id]} out=${S_out[id]} err=${S_err[id]}
  if ((S_waeof[id]==0)); then
    while IFS= read -r -t 0 -u"$wa"; do
      _fw_busy=1
      if IFS= read -r -u"$wa" line; then
        case $line in
          'A '*)
            line=${line#A }
            ((line==S_iack[id]+1 && line==S_ifwd[id])) || _fw_fail
            S_iack[id]=$line; unset "S_q[id*8+line%8]"; _fw_emit "a $id $line"; _fw_pump "$id" ;;
          K) ((S_iclosed[id])) || _fw_inclosed "$id" ;;
          *) _fw_fail ;;
        esac
      else
        S_waeof[id]=1; _fw_close "$wa"; S_wa[id]=
        if ((S_iclosed[id]==0)); then _fw_close "${S_wi[id]}"; S_wi[id]=; _fw_inclosed "$id"; fi
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
  while ((S_oeof[id]==0 && S_oseq[id]-S_oack[id]<8)) && IFS= read -r -t 0 -u"$out"; do
    _fw_busy=1
    if IFS= read -r -u"$out" n && [[ $n != F ]]; then
      if [[ $n =~ ^[1-9][0-9]{0,3}$ ]] && ((n%2==0 && n<=4096)) && IFS= read -r -N $((n+1)) -u"$out" hex &&
        [[ ${hex:n} == $'\n' && ${hex:0:n} != *[!0123456789abcdef]* ]]; then
        ((S_oseq[id]++)); _fw_emit "o $id ${S_oseq[id]} ${hex:0:n}"
      else
        _fw_badout "$id"
      fi
    else
      S_oeof[id]=1; _fw_close "$out"; S_out[id]=; _fw_close "${S_tok[id]}"; S_tok[id]=; _fw_emit "f $id"
    fi
  done
  # A gracefully closed stream gets ten seconds to write its queued input.
  if [[ -n ${S_grace[id]} ]] && ((SECONDS>=S_grace[id])); then _fw_kill_stream "$id"; S_grace[id]=; fi
  if ((S_oeof[id] && S_eeof[id])) && ! kill -0 "${S_pid[id]}" 2>/dev/null; then
    wait "${S_pid[id]}"; status=$?
    _fw_emit "x $id $status"
    _fw_drop "$id"
    _fw_busy=1
  fi
}
# "I id seq length hex": up to eight unacknowledged records of at most 2048
# bytes per stream, queued here and written one at a time.
_fw_input() {
  local id=$1 seq=$2 hex=$3
  [[ -n ${S_pid[id]} ]] || return 0
  ((S_iclosed[id])) && return 0
  ((seq==S_iseq[id]+1 && S_iseq[id]-S_iack[id]<8 && S_ifin[id]==0)) || _fw_fail
  S_iseq[id]=$seq; S_q[id*8+seq%8]=$hex
  _fw_pump "$id"
}
_fw_record() {
  local record=$1 id seq
  case $record in
    'O '*) _fw_open ${record#O } ;;
    'F '*)
      id=${record#F }
      [[ $id =~ ^[0-9]+$ ]] || _fw_fail
      [[ -n ${S_pid[id]} ]] || return 0
      ((S_iclosed[id])) && return 0
      S_ifin[id]=1; _fw_pump "$id"
      ;;
    'A '*)
      record=${record#A }; id=${record%% *}; seq=${record#* }
      [[ $id =~ ^[0-9]+$ && $seq =~ ^[0-9]+$ ]] || _fw_fail
      [[ -n ${S_pid[id]} ]] || return 0
      ((seq==S_oack[id]+1 && seq<=S_oseq[id])) || _fw_fail
      S_oack[id]=$seq
      [[ -n ${S_tok[id]} ]] && { printf '\n' >&"${S_tok[id]}" || :; }
      ;;
    # G: both directions ended; let the connector take queued input and exit.
    'G '*)
      id=${record#G }
      [[ $id =~ ^[0-9]+$ ]] || _fw_fail
      if [[ -n ${S_pid[id]} ]]; then
        S_ifin[id]=1; _fw_pump "$id"; S_grace[id]=$((SECONDS+10))
      else
        _fw_emit "d $id"
      fi
      ;;
    'C '*)
      id=${record#C }
      [[ $id =~ ^[0-9]+$ ]] || _fw_fail
      [[ -n ${S_pid[id]} ]] && _fw_drop "$id"
      _fw_emit "d $id"
      ;;
    *) _fw_fail ;;
  esac
}
_fw_main() {
  local head line id seq n plen=${#_ps_prefix}
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
  while :; do
    _fw_busy=0
    # The client sends whole records, so a started record completes promptly;
    # read -N fetches an exact length in few syscalls, never more.
    if IFS= read -r -t 0; then
      _fw_busy=1
      IFS= read -r -N $((plen+2)) head || exit 125
      [[ ${head:0:plen} == "$_ps_prefix" ]] || _fw_fail
      head=${head:plen}
      case $head in
        'I ')
          IFS= read -r -d ' ' id || exit 125
          IFS= read -r -d ' ' seq || exit 125
          IFS= read -r -d ' ' n || exit 125
          [[ $id =~ ^[0-9]{1,10}$ && $seq =~ ^[0-9]{1,10}$ && $n =~ ^[0-9]{1,4}$ ]] && ((n>0 && n<=4096 && n%2==0)) || _fw_fail
          IFS= read -r -N $((n+1)) line || exit 125
          [[ ${line:n} == $'\n' && ${line:0:n} != *[!0123456789abcdef]* ]] || _fw_fail
          _fw_input "$id" "$seq" "${line:0:n}"
          ;;
        $'Q\n') _fw_cleanup; _fw_emit D; exit 0 ;;
        [OFACG]' ')
          IFS= read -r line || exit 125
          ((${#line}<=4224)) || _fw_fail
          _fw_record "$head$line"
          ;;
        *) _fw_fail ;;
      esac
    fi
    for id in "${!S_pid[@]}"; do _fw_service "$id"; done
    if ((_fw_busy==0)); then
      IFS= read -r -t 0.002 -u"$idle" line
      (($?>128)) || exit 125
    fi
  done
}
_fw_main
