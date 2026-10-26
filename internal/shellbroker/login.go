package shellbroker

// LoginShell is the command text that both adapters run, through the carrier's
// $SHELL -c, when no remote command is given. Like sshd without a command it
// changes to $HOME and execs $SHELL as a login shell (argv0 "-" + basename).
// The outer command is plain `exec word 'quoted'`, accepted by POSIX, csh and
// fish command shells; only the inner text depends on the required Bash, whose
// exec -a sets argv0. Nothing is written remotely: this replaces the process.
const LoginShell = `exec bash -c 'cd -- "${HOME:-/}" 2>/dev/null || printf "Could not chdir to home directory %s\n" "${HOME-}" >&2; s=${SHELL:-/bin/sh}; exec -a "-${s##*/}" "$s"'`
