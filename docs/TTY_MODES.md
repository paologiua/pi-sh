# Portable command terminal modes — 2026-10-21

Experimental command TTY now snapshots local macOS/Linux input modes **before**
local raw mode. `internal/terminal` translates kernel fields into the bounded
`internal/ttymode` vocabulary; `internal/shellbroker` stages validated stty
arguments as a Bash array. The worker applies them after restoring its original
carrier state and before TR/command startup. No OS termios structure or foreign
`stty -g` encoding crosses the connection. No additional Connect channel, remote
file, executable installation or persistent configuration is introduced.

This implements the following OpenSSH terminal-mode meanings for the Linux Pi
destination, rather than copying every local kernel flag indiscriminately:

| Family | Forwarded values |
| --- | --- |
| Local processing | ICANON, ISIG, IEXTEN, ECHO, ECHOE, ECHOK, ECHONL, ECHOCTL, ECHOKE, NOFLSH, TOSTOP, PENDIN |
| Input processing | IGNPAR, PARMRK, INPCK, ISTRIP, INLCR, IGNCR, ICRNL, IXON, IXOFF, IXANY, IMAXBEL, IUTF8 |
| Output processing | OPOST, ONLCR, OCRNL, ONOCR, ONLRET |
| Linux-only source flags | IUCLC, OLCUC, XCASE |
| Parity/character size | PARENB, PARODD; CS7/CS8 follow the reference's overlapping bit-presence interpretation |
| Control characters | VINTR, VQUIT, VERASE, VKILL, VEOF, VEOL, VEOL2, VSTART, VSTOP, VSUSP, VREPRINT, VWERASE, VLNEXT, VDISCARD |
| Speeds | Input/output metadata rates supported by OpenSSH 10.2p1 on Linux, through 230400; unsupported rates normalize to 9600 |

Controls use 255 for disabled, translated to the remote stty's `undef`. Local
macOS disable 255 and Linux disable 0 are normalized before staging. Literal
byte 255 consequently cannot be distinguished from disabled, as in the reference.
Control-byte values are bounded; unknown names/values and unsupported speeds fail
local validation before adapter execution. Names, not shell expressions, select
settings. Single quotes, whitespace and high bytes in values cannot escape the
staged array; source upload remains ASCII hex even when a control value is not
UTF-8.

The all-byte test found an actual utility difference: macOS BSD stty turns a bare
caret into NUL and a hexadecimal string into its first character, while GNU stty
supports numeric control values. Literal caret is represented by hex 0x5e; on a
carrier whose own stty reports the BSD gfmt1 format, the adapter emits the single
field gfmt1:name=5e instead. This does not import or replace a foreign termios
snapshot. Kernel checks verify that all other captured fields remain unchanged.

## GNU/Linux dialect correction and native checks

The first native local-mode request failed because GNU stty does not expose
pendin, and uses rprnt for the BSD reprint name. The GNU branch now translates
that name and applies ordinary arguments first. PENDIN is then changed in a
fresh snapshot from the same carrier: verified GNU coreutils, Linux on supported
ARM/x86 types, 36 hexadecimal fields (four flags plus32 controls), bounded widths,
and only bit0x4000 of the fourth/local-flags field. The resulting save string is
read back exactly. No client bitfield/save string crosses the connection; unknown
platforms, utilities, shapes or failed application/readback stop before the command.
The BSD gfmt1 path retains its own options and literal-caret conversion.

Native Pi browser-companion runs now verify captured ordinary modes, TERM=xterm,
initial91x29, one input line and exit23. Enabling local PENDIN also sets the remote
Linux bit and retains those results; a final binary without temporary diagnostics
passes. This establishes bit preservation, not additional kernel retyping semantics.
One earlier corrected-mode run ended with a WebRTC error after application output;
its TX receipt was unobserved and remains a robustness gap. See REAL_DEVICE_TESTS.md.

`TestGNUCarrierPendinPreservesFreshState` checks set/clear, fresh ordinary-mode
changes and rprnt translation against a GNU-contract fixture. Eight refusal cases
cover platform/utility, malformed/count/width and write/readback failures.
`TestGNUCarrierPendinChangesOnlyRequestedKernelBit` compares complete Linux kernel
termios snapshots on a real PTY; this Linux-only test is compile-checked here,
not executed on this Mac or installed on the Pi. Linux CI must run it.

## Evidence

- Seven paired local OpenSSH_10.2p1 / LibreSSL 3.3.6 and actual staged-adapter
  tests cover echo-off, custom EOF 7, interrupt 29 with a trap, canonical-off Ctrl-D
  data, no output CR/LF mapping, disabled interrupt and speed 19200. The reference
  client's temporary PTY is configured before exec; its own local close notice
  is suppressed with -q rather than stripping application output.
- The actual production stty dialect helper is checked against kernel state for
  all 256 control values on macOS, including caret and unrelated-mode preservation.
  This is BSD-path all-byte runtime evidence. The GNU path has the limited native
  evidence above; all256 GNU values still require Linux runtime/CI execution.
- Thirteen staged source cases include NUL/control notation, space, shell
  metacharacters, caret, DEL, non-UTF-8 and disabled values without command/source
  corruption. Invalid-vocabulary/value fixtures fail closed.
- The real Pion/PTY CLI coordinator now has an eighth case: a nondefault local
  echo-off/noncanonical/no-ISIG/no-OPOST/no-ICRNL terminal at 19200 is captured,
  Ctrl-D reaches the remote as byte 4, output has LF only, and exact original local
  state is restored. The earlier seven coordinator cases remain.
- Capture/restoration and these new adapter/coordinator cases pass three repeated
  race runs on macOS. Cross-compilation is separate from Linux/Windows/iSH runtime.

Automated terminal/process/file fixtures use local development resources. The
separate native mode subset above creates no files and establishes no content
transfer or new long-session proof.

## Remaining boundaries

Windows console bits have no implemented POSIX-mode translation; that client
retains carrier modes. Physical Windows/iSH and local Linux Go-client runtime remain unverified.
The native remote Linux subset above is separate evidence.
Forced TTY with pipe or StdinNull has no local terminal snapshot, retaining the
carrier modes, just as the reference sends no tcgetattr-derived modes without a
terminal fd. Ordinary interactive shells still use their earlier mode path.

BSD-only VDSUSP/VSTATUS have no Linux terminal slots and are omitted; VFLUSH and
VSWTCH are not available under those names in the supported source targets.
VMIN/VTIME, IGNBRK/BRKINT, line discipline and other non-inventory kernel flags
are deliberately not invented as OpenSSH mode requests. Baud is PTY metadata,
not bandwidth limiting; unusual hardware sizes/parity, independent Linux input
rates, custom-rate fallback and libc/kernel combinations need runtime comparison.
The pure-Go Linux reader decodes TCGETS CBAUD/CIBAUD, rather than trusting
platform-dependent Ispeed/Ospeed struct fields; Linux-only synthetic tests cover
independent/follow-output/unsupported codes but have not run on this Mac.

A remote stty failure stops before the user command and produces missing adapter
status/cleanup failure, rather than silently running with requested modes absent.
This is stricter than OpenSSH's advisory mode-application errors; exact failure
diagnostic/status parity is still open. Full suspension/stopped-job behavior,
Windows/generic borrowed-reader/writer cancellation, agent teardown and native terminal
programs remain open in TTY_COMMANDS.md. None is inferred from these mode tests.

Unix file-backed stream cancellation has separate evidence in LOCAL_IO.md.

Behavior references (implementation independently authored, no source copied):
[OpenSSH 10.2p1 mode inventory](https://github.com/openssh/openssh-portable/blob/V_10_2_P1/ttymodes.h),
[mode conversion](https://github.com/openssh/openssh-portable/blob/V_10_2_P1/ttymodes.c),
[RFC 4254 section 8](https://www.rfc-editor.org/rfc/rfc4254#section-8),
[Apple stty character parser](https://github.com/apple-oss-distributions/adv_cmds/blob/main/stty/cchar.c),
[GNU stty](https://github.com/coreutils/coreutils/blob/master/src/stty.c),
[glibc Linux speed handling](https://github.com/bminor/glibc/blob/master/sysdeps/unix/sysv/linux/speed.c).
The latter moving branches are observations from 2026-10-21, not pinned release
artifacts or new runtime dependencies. Original project licensing/publication
gates and existing attribution remain unchanged.
