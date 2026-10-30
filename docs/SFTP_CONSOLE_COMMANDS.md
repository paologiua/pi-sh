# Console command increments — 2026-10-30

All new commands are developed on the Mac only, with essential targeted checks.
No new Pi session/write/listener or full/stress campaign is authorized by this
work. The historical local WebRTC `RESULT fail=1` stays open. Linux/386 build
checks do not prove iSH runtime compatibility. Each command has its own commit;
the preceding recursion milestone was recorded separately before these changes.

| Command | Behavior and essential local evidence | Native / remaining limits |
| --- | --- | --- |
| version | `SFTP protocol version 3`, byte-for-byte against the installed OpenSSH output; `TestVersionConsoleOutput` | v3 is the only version negotiated by the pinned backend; no new native check |

Command names are case-insensitive (`TestCommandsIgnoreCase`), matching OpenSSH.

Reference baseline: installed macOS OpenSSH 10.2p1 and local sftp-server,
plus the pinned upstream sftp.c. Output and operations are reproduced by an
independently authored implementation; no upstream implementation is copied.
