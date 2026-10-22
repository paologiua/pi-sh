# pi-cp recursive copies

`pi-cp -r` now traverses literal directory trees in both directions through the
existing SFTP backend. This is a local implementation milestone; no recursive
transfer or content write has been performed on the Pi. An approved remote
scratch location is still required before such a test.

A missing destination is the new tree root. An existing directory receives the
source basename; a source ending in `/.` merges its contents into the destination.
A trailing `/` retains the basename. Multiple sources still require an existing
destination directory. File sources remain supported with `-r`.

Source symbolic links are followed, including links outside the source tree.
Their targets become ordinary copied files/directories. Links and hard-link
identity are not recreated. A dangling link fails the copy. Local ancestor
identity and remote REALPATH ancestor names detect directory cycles; depth is
limited to 64 directories. Cycles fail earlier than OpenSSH's depth-only guard.
FIFO/device/socket entries are rejected without deliberately opening them.

Child names are validated before use. Joining a child preserves the original
path components, so `link/../directory` is resolved by the filesystem rather than
lexically cleaned. Remote names that cannot be a local basename are rejected.
This is not a complete Windows reserved-name/normalization inventory.

Directory permissions have intentionally asymmetric reference behavior:

- Upload restores source ordinary directory permission bits even without `-p`,
  including an existing target directory. Newly created directories have temporary
  owner write/execute access until children finish. The suite's private server
  creation mask is checked before relaxing a new directory's mode.
- Download creates directories with source ordinary bits plus temporary owner
  write/execute access. Final chmod occurs only when these temporary bits were
  needed. An existing directory can retain its mode even with `-p`.
- `-p` sets directory access/modification timestamps after children. Regular-file
  metadata uses the existing tested backend. Metadata status failures are advisory;
  transport failures remain fatal. Ownership, ACLs and special bits are not
  preserved. All directory metadata operations are path-based and can race with
  path replacement. Source and destination trees must remain stable.

The reference is the installed OpenSSH_10.2p1 client and existing local OpenSSH
`sftp-server`, connected directly with `scp -D`; there is no SSH connection and
all test mutations are in temporary Mac directories. The
[OpenSSH 10.2 source](https://github.com/openssh/openssh-portable/blob/V_10_2_P1/sftp-client.c)
also identifies the depth limit and the different directory mode paths.

`TestRecursiveCopyAgainstLocalSCP` compares 48 cases: upload/download, `-p` on/off,
new/existing destinations, three source suffixes, and writable/read-only source
root directories. It checks tree shape, binary/empty/nested contents, ordinary
modes, preserved child atime/mtime, and root mode/mtime. Link targets are separate
from ordinary tree entries so enumerating the same source inode twice does not
make timestamp expectations depend on the reference's traversal order.

Additional tests exercise cycles, dangling links, conflicting files, cancellation
before mutation, nonrecursive refusal, raw symlink/parent traversal, private-mask
startup, unsafe child names and the depth guard. The CLI config rejection test
also accepts compact `-prrp` before rejecting an incompatible TTY configuration.

Open work remains:

- Remote enumeration now uses the local incremental pkg/sftp extension, retaining
  at most one packet per ancestor. Upload still enumerates64 entries at a time.
  See [SFTP_LISTING.md](SFTP_LISTING.md) for bounds, close/error tests, provenance
  and the still-unbounded default sorted console listing.
- The first child failure stops traversal and leaves partial results, while
  OpenSSH can continue sibling entries and report failure at the end. Existing
  destinations are not rolled back. No atomic tree snapshot/copy is promised.
- Concurrent path changes, absent attribute
  flags, disk-full/late-close interruptions and special directory bits need
  further evidence. Cancellation during an in-flight SFTP operation depends on
  the surrounding session's transport cancellation.
- SFTP console `get -r`/`put -r` is still unimplemented; it must not silently inherit
  SCP's link-following policy. Glob/tilde, sparse files, progress and rate control
  remain separate work.
- Native Pi, Windows/Linux runtime and physical iSH recursive checks are pending.
