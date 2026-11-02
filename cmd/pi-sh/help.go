package main

import "pi-sh/internal/connectcompat"

const clientHelp = `usage: pi-sh [--direct-login|--browser] [options] [email::]device [command ...]
       pi-sh [options] email [command ...]   (choose the device after sign-in)
       pi-sh -G [-F file|none] [-o option] [device]
       pi-sh --sessions | --logout email | --passphrase | --forget-passphrase |
             --reset-sessions
       pi-sh -V | --version | -Q query | --help

Unofficial SSH-like client over Raspberry Pi Connect (not the SSH protocol).
Commands require --experimental-command. Passwords and one-time codes are never
stored; signed-in sessions can be saved, encrypted with one passphrase.
` + connectcompat.Help + `
Connection:
  --direct-login       Raspberry Pi ID password/TOTP login (default)
  --browser            Use an authorized Connect browser companion
  -l email             Raspberry Pi ID account (not a remote Unix user)
  -o UseKeychain=yes   Keep the saved-sessions passphrase in the system keyring
  -F file|none         Explicit configuration file; no default SSH files read
  -o option            Configuration directive; repeatable, first value wins
  -P tag               Select a configuration tag

Saved sessions (direct login):
  --sessions           List saved sessions and check whether each is signed in
  --logout email       Sign the saved session out of Connect and delete it
  --passphrase         Set (first time) or change the saved-sessions passphrase
  --forget-passphrase  Remove that passphrase from the system keyring (asks it)
  --reset-sessions     Sign out and delete all saved sessions and the passphrase

Session and terminal:
  --experimental-command  Execute command with binary stdin and separate streams
  -n                   Supply stdin EOF (authentication can still prompt)
  -T                   Disable command terminal allocation
  -t, -tt              Request terminal; repeat to force
  -e char|none         Escape character (default ~); ~? shows session help
  -N                   Forwarding only, without a command or shell
  -a, -x               Agent/X11 forwarding disabled (always the case)
  --legacy-command     Older merged-output, null-input command adapter
  --legacy-shell       Raw Connect shell without broker exit status

Forwarding:
  -L [bind:]port:host:port   Local TCP forwarding
  -R [bind:]port:host:port   Remote TCP forwarding
  -D [bind:]port             Local SOCKS4/5 proxy
  -W host:port              Forward stdin/stdout to a remote TCP endpoint
  -g                        Allow non-loopback local forwarding binds
  Forwarding requires an existing compatible remote connector. See docs/FORWARDING.md.

Diagnostics and offline information:
  -v, -vv, -vvv        Increase connection diagnostics
  -q                   Suppress connection diagnostics
  -E file              Append diagnostics to file
  -y                   Send diagnostics to local syslog (Unix)
  -G                   Print supported effective configuration without login
  -Q query             List capabilities; use -Q help for query names
  -V, --version        Print client version without login or network access
  -h, --help           Show this help without login or network access

Recognized but refused SSH options:
  -4 -6 -A -B -b -C -c -f -I -i -J -K -k -M -m -O -p -S -s -w -X -Y
  SSH ports, keys, algorithms, compression, proxies, multiplexing, agent/X11
  forwarding and tunnels have no implemented Connect equivalent.

Short options accept clusters and attached arguments where applicable.
pi-sh options may follow the device until the first command word or --.
See docs/SSH_COMPATIBILITY.md for individual decisions and verification limits.
`
