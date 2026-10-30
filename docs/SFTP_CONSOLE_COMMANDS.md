# Console command increments — 2026-10-30

All new commands are developed on the Mac only, with essential targeted checks.
No new Pi session/write/listener or full/stress campaign is authorized by this
work. The historical local WebRTC `RESULT fail=1` stays open. Linux/386 build
checks do not prove iSH runtime compatibility. Each command has its own commit;
the preceding recursion milestone was recorded separately before these changes.

| Command | Behavior and essential local evidence | Native / remaining limits |
| --- | --- | --- |
| chdir | Alias of cd; silent success, `TestChdirAlias` | Same remote path policy as cd; no new native check |
| lchdir | Alias of lcd; silent success, TestLchdirAlias | Local command; no remote mutation |
| dir | Alias of ls, identical output; TestDirAlias | Inherits current listing limits |
| cd | No argument returns to initial remote directory, silently; TestCdReturnsToInitialDirectory | Local fixture only, native pending |
| lcd | No argument selects local home, silently; TestLcdReturnsHome | Local command; user-home resolution uses the OS |
| ln | Hard link by default, -s symbolic link with literal target; silent success; TestLnAgainstLocalOpenSSH | hardlink extension required for hard links; native pending |
| chown | Numeric UID, remote globs, -h uses negotiated lsetstat v1; preserves GID; TestChownLocalServer | Status/permission failures retained; native pending; omitted attribute flags remain unavailable |
| chgrp | Numeric GID, remote globs and -h; preserves UID; TestChgrpLocalServer | Existing server authorization applies; native pending |
| chmod | Octal 0000–7777 including special bits; -h uses lsetstat v1; TestChmodSpecialBitsAndLink compares server outcomes and modes with OpenSSH | Link chmod may be refused by the server/filesystem, as in OpenSSH; native pending |
| lmkdir | Creates a literal local directory; silent success, existing directory is an error as OpenSSH 10.2; TestLmkdirLiteral | Local only; later OpenSSH versions add -p, outside this reference baseline |
| df | Default KiB columns, -h binary units, -i inodes; -i wins over -h; exact layout in TestDfOutput, four local-server forms in TestDfLocalServer | Requires advertised statvfs v2; native pending |
| copy | Literal remote-to-remote regular-file copy via copy-data v1; no client relay; TestRemoteCopyLocalServer compares binary contents/new and existing modes with OpenSSH | Requires negotiated copy-data; partial targets remain; native pending |
| cp | Alias of copy; TestCpAliasLocalServer | Same extension and error policy as copy |
| lumask | Octal mask with exact Local umask output; applies to new downloads, recursive directories and lmkdir without changing other Go goroutines; TestLumaskCreationAndOutput | Existing target modes retained; child-shell propagation added with shell command; Windows permission runtime unverified |
| ! | Raw command or interactive local shell, local cwd/mask, child stdio; exact output/status diagnostic compared in TestLocalShellAgainstOpenSSH | Demand-driven command reader stops read-ahead; context kills direct child, arbitrary descendant cleanup/platform behavior remains open |
| lls | Local /bin/ls through local shell; raw flags, quotes/globs and cwd; exact output in TestLlsAgainstOpenSSH | Uses existing local ls, not extra remote software; Windows dir adaptation unverified |
| progress | Exact enabled/disabled lines, default on interactively and off in batch/-q; same terminal meter as SCP; TestProgressToggleAgainstOpenSSH | Redirected output never renders a meter; existing meter layout tests reused; SFTP terminal native check pending |
| ls | -1afhlnrSt, columns, prefixes, long mode/date/size/ownership layout; 16 exact local OpenSSH comparisons in TestLsOptionsAgainstOpenSSH; -1f streams | Name lookup uses users-groups-by-id v1, older servers use their v3 longname for plain -l; locale/control-name/tie-order edge parity and native checks open |
| pi-sftp -N | Clears command quiet mode even with batch/-q; does not re-enable the progress meter; TestNoisyOverridesBatchNotices and frontend pre-auth parsing | Connect authentication diagnostics remain product-specific |
| pi-sftp -B | Sets copy and SFTP packet payload size; default 32768, maximum effective 256KiB minus packet overhead; TestTransferBufferSize and frontend parser case | Large requested sizes clamped to receiver bound; native tuning pending |
| version | `SFTP protocol version 3`, byte-for-byte against the installed OpenSSH output; `TestVersionConsoleOutput` | v3 is the only version negotiated by the pinned backend; no new native check |

Command names are case-insensitive (`TestCommandsIgnoreCase`), matching OpenSSH.

Reference baseline: installed macOS OpenSSH 10.2p1 and local sftp-server,
plus the pinned upstream sftp.c. Output and operations are reproduced by an
independently authored implementation; no upstream implementation is copied.
