# Incremental remote directory enumeration

Recursive `pi-cp -r` downloads now consume one SFTP NAME packet at a time, instead
of retaining the entire listing for each directory. Uploads keep their existing
64-entry local batches. The new `pi-sftp ls -f [path]` lists names in server order
with the same incremental reader; compact `-ff` and `--` are accepted. Unknown
listing flags fail before any listing/stat request. Existing
`ls` remains lexically sorted and still accumulates the entire directory: bounded
sorted enumeration and the remaining ls options are open work.

Incoming packet bodies remain limited to 256 KiB. Each active remote iterator
holds at most one such response plus one decoded entry and request state.
Recursive descent can retain one response per ancestor; the existing depth64
limit therefore bounds retained packet bodies to approximately16 MiB, plus
metadata, filenames, client queues and Go/runtime/server overhead. This is a
structural bound, not an iSH peak-memory measurement or a runtime-compatibility
claim. No listing is spooled to disk and filenames are not persisted by this
implementation.

The callback can issue requests through the same client, including child
iteration. It runs synchronously, checks cancellation between entries, and stops
on its first failure. Directory CLOSE is attempted once and its error is joined;
failed CLOSE cannot turn a completed listing into success. Malformed NAME,
STATUS and handle replies fail, and transport EOF is distinguished from a valid
EOF status. Raw basenames are retained and empty/slash/NUL names rejected before
callback invocation, rather than silently cleaning server-controlled names.
Only exact dot entries are skipped. Unknown attribute flags fail in this new
reader; this does not claim every upstream parser now has the same policy.

Protocol writes and background directory CLOSE retain the upstream transport
lifetime model. During normal CLI cancellation, the owning SFTP session closes
its private protocol pipes. A caller supplying arbitrary blocked streams must
close them itself; this iterator does not independently interrupt such streams.
There is no server snapshot or consistency guarantee while directories change.
Existing path-replacement, attribute-presence and partial-tree limitations remain.

## Dependency decision and provenance

The root module uses a local copy of pkg/sftp v1.13.11 with directory-iteration
and combined handle-metadata extensions, each with its tests. All65 copied upstream files are unchanged and hash-recorded
in [UPSTREAM.json](../third_party/sftp/UPSTREAM.json); original copyright/license
notices are retained. [PI_SH_CHANGES.md](../third_party/sftp/PI_SH_CHANGES.md)
records the upstream module checksum, additions, validation and review duties.
The root provenance test detects accidental source drift and unrecorded source
additions. This is not a signed manifest or a substitute for dependency review.

The official [v2 directory API](https://pkg.go.dev/github.com/pkg/sftp/v2#Dir.ReaddirContext)
was inspected at v2.0.0-alpha2 (the available versions were alpha and alpha2).
Its incremental reader is suitable in isolation, but migrating its broader
client API would change existing behaviors: the public Remove implementation
attempts both file and directory removal, and negotiated extensions are private.
See [the pinned client source](https://github.com/pkg/sftp/blob/v2.0.0-alpha2/client.go).
The small local v1 extension preserves the currently tested file/server API and
limits, rather than introducing that whole migration. No v2 dependency is linked
or added to go.mod/go.sum. This local fork adds a maintenance/review obligation;
`go mod verify` does not authenticate a local replacement, and vulnerability
scanners must still be evaluated against its upstream v1.13.11 identity.

## Evidence

- A generated10,000-entry in-memory server verifies full order, one-packet
  consumption before visitor failure/cancellation, reentrant stat requests and
  exactly one handle close. READDIR/CLOSE failures and joined failures stay errors.
- Eight raw-wire cases distinguish clean directory EOF, transport EOF, malformed
  NAME/count/name data, denied CLOSE, EOF CLOSE and malformed CLOSE.
- An actual local OpenSSH_10.2p1 `sftp -D` comparison verifies `ls -f` names/order.
  The reference uses `-1f` to force one name per line. Our console retains its
  documented basename presentation rather than OpenSSH's directory prefix.
- All48 recursive SCP reference cases and the existing transfer/error tests are
  rerun through the extension. These are local tests only, with temporary Mac
  files and no connection to the Pi.
- The new packet parser passed a15-second, two-worker fuzz run with1,844,993
  executions and no crash. This is limited evidence, not a complete security proof.

Full regression/build results are recorded in TESTING.md after completion.
No native Pi listing/recursive evidence is asserted for this new reader. No remote
write location has been approved; no Pi file/package/configuration was changed.

The recursion differential's metadata observer was corrected to capture directory
atime before enumerating its contents; filepath.Walk enumerates before invoking
its callback. This fixes a test-side measurement that could record the test's own
access time. A subsequent isolated regular-file atime discrepancy also occurred
in a full-suite run; it was not reproduced in eight uninstrumented repetitions
of all48 corrected cases or the final passing package run. Its exact source is
not retroactively proven; further stress/measurement evidence remains open.
No timestamp assertion was removed or weakened to obtain those passes.

The dependency audit also found a reachable Go1.27.2 x509 advisory. Go1.27.3 is
now the project minimum, with x/crypto0.57.0/x/net0.60.0 selected. The subsequent
native scan reports zero reachable/imported-package findings and one module-only
finding in unimported crypto/openpgp, without identified calls. This is separate from the parser fuzz evidence.

Final checks pass for all seven CGO-disabled builds and both modules' test
compilation, including Linux/386 softfloat. Root and extension parser/SFTP tests,
root vet, provenance and module verification pass. Full nested vet reports two
unchanged upstream ReadFrom signature warnings; its remaining analyzers pass
with only stdmethods disabled for that nested module. CI keeps root vet intact.
The final full Go race run failed a command-TTY early-completion case outside
the transfer changes; thirty immediate targeted repetitions then passed.
TTY_COMPLETION.md records this still-open regression. Do not read the successful
transfer checks or builds as a successful complete final regression.

The later TTY recurrence has since been reproduced as receive-frame exhaustion
from per-rune text input and corrected with available-input batching. The full
final race suite now passes, along with120 positive/120 no-status concurrent
cases; this supersedes the failed gate above. Transfer code and its timestamp
stress caveat are unchanged. See TTY_COMPLETION.md.

## Recursive atime discrepancy: environmental reader — 2026-10-22

On this Mac, files created under `$TMPDIR` and given an old atime are read by an
external process within about two seconds: without any read by the test, 132/200
and then 130/200 files had atime reset to the current time. A
`.metadata_never_index` marker did not change the rate, so Spotlight is unlikely.
The active Trend Micro Endpoint Security system extension is the probable reader;
the exact process was not identified (that needs privileged `fs_usage`). Such a
read after the copy sets atime, and before the snapshot, explains an isolated
regular-file atime mismatch without a pi-sh defect.

The assertion is unchanged: a missed preservation would also show a recent atime.
`TestRecursiveCopyAgainstLocalSCP` now keeps an untouched control file in each
case and reports its atime on failure; a control no longer at 1600000001 marks
external interference. The 48 cases then passed 10 consecutive runs (480 cases).
