// Package transferprobe runs synthetic transfer checks only in an explicitly
// approved, newly-created scratch directory. It never removes unknown entries.
package transferprobe

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"pi-sh/internal/copycli"
	"pi-sh/internal/sftpcli"
	"pi-sh/internal/sftpclient"
)

var rootPattern = regexp.MustCompile(`^/tmp/pi-sh-probe-[0-9a-f]{32}$`)

func NewRoot() (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", errors.New("scratch-name generation failed")
	}
	return "/tmp/pi-sh-probe-" + hex.EncodeToString(nonce[:]), nil
}
func ValidateRoot(root string) error {
	if !rootPattern.MatchString(root) {
		return errors.New("scratch root must be /tmp/pi-sh-probe- followed by 32 lowercase hex digits")
	}
	return nil
}

type Event struct {
	Check           string
	Passed, Skipped bool
}
type node struct {
	suffix    string
	directory bool
}

var nodes = []node{{"", true}, {"empty", false}, {"binary", false}, {"large", false}, {"preserved", false}, {"resume", false}, {"flushed", false}, {"tree", true}, {"tree/empty", true}, {"tree/nested", true}, {"tree/nested/leaf ' 世界.bin", false}}

// Plan is printable without authentication or a connection. Supplying a root or
// execute flag is not itself human authorization; the caller must obtain that.
func Plan(root string) (string, error) {
	if err := ValidateRoot(root); err != nil {
		return "", err
	}
	return fmt.Sprintf("Scratch root: %s\nCreate exclusively; require private directory mode; never reuse an existing root.\nChecks: empty/64KiB binary/1MiB roundtrip, ordinary modes, -p remote times and local mtime, reput/reget, advertised fsync, recursive tree, missing-source failure.\nAt most 1,314,816 bytes of synthetic remote file contents, plus filesystem overhead.\nCleanup: inventory only known entries, remove leaves before directories, verify root absent.\nOn lost transport or unexpected entries, report cleanup unconfirmed; never claim success or replay automatically.\nNo remote installs, persistent scripts, configuration/service changes, or SSH transport.\n", root) + "Known child entries:\n" + strings.Join(KnownChildren(), "\n") + "\n", nil
}

func emit(report func(Event), name string, pass, skip bool) {
	if report != nil {
		report(Event{name, pass, skip})
	}
}
func phaseError(name string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", name, sftpclient.SafeError(err))
}

// Run requires the ordinary suite's private server umask setup. Only fixed child
// names are used. An existing root is never adopted, overwritten or cleaned up.
func Run(ctx context.Context, client *sftp.Client, root string, mask os.FileMode, report func(Event)) (result error) {
	if err := ValidateRoot(root); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if client == nil {
		return errors.New("SFTP client unavailable")
	}
	local, err := os.MkdirTemp("", "pi-sh-transfer-probe-")
	if err != nil {
		return errors.New("local scratch creation failed")
	}
	defer func() { result = errors.Join(result, phaseError("local cleanup", os.RemoveAll(local))) }()
	if err := client.Mkdir(root); err != nil {
		return phaseError("exclusive scratch creation", err)
	}
	defer func() {
		err := Cleanup(client, root)
		emit(report, "cleanup", err == nil, false)
		result = errors.Join(result, phaseError("cleanup unconfirmed", err))
	}()
	info, err := client.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return errors.New("scratch root private mode not verified")
	}
	emit(report, "private-root", true, false)

	for _, fixture := range []struct {
		name string
		size int
	}{{"empty", 0}, {"binary", 64 << 10}, {"large", 1 << 20}, {"prefix", 1024}} {
		if err := writeFixture(local+"/"+fixture.name, fixture.size); err != nil {
			return phaseError("local fixture", err)
		}
	}
	var warnings bytes.Buffer
	console := sftpcli.Console{Client: client, RemoteUmask: &mask, Errors: &warnings, Program: "transfer-probe"}
	copyFile := func(upload, preserve, recursive bool, source, target string) error {
		args := []string{source, "probe:" + target}
		if !upload {
			args = []string{"probe:" + source, target}
		}
		plan, err := copycli.New(args)
		if err != nil {
			return err
		}
		plan.Preserve, plan.Recursive, plan.Errors = preserve, recursive, &warnings
		return plan.Run(ctx, client, &mask)
	}
	check := func(name string, run func() error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		warnings.Reset()
		err := run()
		if err == nil && warnings.Len() != 0 {
			err = errors.New("transfer reported an advisory warning")
		}
		emit(report, name, err == nil, false)
		return phaseError(name, err)
	}
	for _, name := range []string{"empty", "binary", "large"} {
		if err := check(name+"-roundtrip", func() error {
			if err := check(name+"-upload", func() error {
				return copyFile(true, false, false, local+"/"+name, root+"/"+name)
			}); err != nil {
				return err
			}
			if err := check(name+"-mode", func() error {
				info, err := client.Stat(root + "/" + name)
				if err != nil {
					return err
				}
				if info.Mode().Perm() != 0600&^mask {
					return errors.New("ordinary creation mode differs")
				}
				return nil
			}); err != nil {
				return err
			}
			if err := check(name+"-download", func() error {
				return copyFile(false, false, false, root+"/"+name, local+"/"+name+"-down")
			}); err != nil {
				return err
			}
			return check(name+"-integrity", func() error {
				return sameContents(local+"/"+name, local+"/"+name+"-down")
			})
		}); err != nil {
			return err
		}
	}
	if err := check("preservation", func() error {
		atime, mtime := time.Unix(1600000001, 0), time.Unix(1600000013, 0)
		if err := os.Chtimes(local+"/binary", atime, mtime); err != nil {
			return err
		}
		if err := copyFile(true, true, false, local+"/binary", root+"/preserved"); err != nil {
			return err
		}
		info, err := client.Stat(root + "/preserved")
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*sftp.FileStat)
		if !ok || info.Mode().Perm() != 0600 || stat.AccessTime().Unix() != atime.Unix() || info.ModTime().Unix() != mtime.Unix() {
			return errors.New("remote preserved metadata differs")
		}
		if err := copyFile(false, true, false, root+"/preserved", local+"/preserved-down"); err != nil {
			return err
		}
		down, err := os.Stat(local + "/preserved-down")
		if err != nil {
			return err
		}
		if down.Mode().Perm() != 0600 || down.ModTime().Unix() != mtime.Unix() {
			return errors.New("local preserved mode/mtime differs")
		}
		return sameContents(local+"/binary", local+"/preserved-down")
	}); err != nil {
		return err
	}
	if err := check("resume", func() error {
		if err := copyFile(true, false, false, local+"/prefix", root+"/resume"); err != nil {
			return err
		}
		if _, err := console.Execute(ctx, []string{"reput", "--", local + "/binary", root + "/resume"}); err != nil {
			return err
		}
		if err := writeFixture(local+"/resume-down", 1024); err != nil {
			return err
		}
		if _, err := console.Execute(ctx, []string{"reget", "--", root + "/resume", local + "/resume-down"}); err != nil {
			return err
		}
		return sameContents(local+"/binary", local+"/resume-down")
	}); err != nil {
		return err
	}
	if version, ok := client.HasExtension("fsync@openssh.com"); ok && version == "1" {
		if err := check("advertised-flush", func() error {
			if _, err := console.Execute(ctx, []string{"put", "-f", "--", local + "/binary", root + "/flushed"}); err != nil {
				return err
			}
			if _, err := console.Execute(ctx, []string{"get", "-f", "--", root + "/flushed", local + "/flushed-down"}); err != nil {
				return err
			}
			return sameContents(local+"/binary", local+"/flushed-down")
		}); err != nil {
			return err
		}
	} else {
		emit(report, "advertised-flush", false, true)
	}
	if err := check("recursive", func() error {
		for _, name := range []string{"tree", "tree/empty", "tree/nested"} {
			if err := os.Mkdir(local+"/"+name, 0700); err != nil {
				return err
			}
		}
		leaf := "tree/nested/leaf ' 世界.bin"
		if err := writeFixture(local+"/"+leaf, 4096); err != nil {
			return err
		}
		if err := copyFile(true, false, true, local+"/tree", root+"/tree"); err != nil {
			return err
		}
		if err := copyFile(false, false, true, root+"/tree", local+"/tree-down"); err != nil {
			return err
		}
		info, err := os.Stat(local + "/tree-down/empty")
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return errors.New("empty directory missing")
		}
		return sameContents(local+"/"+leaf, local+"/tree-down/nested/leaf ' 世界.bin")
	}); err != nil {
		return err
	}
	return check("missing-source-failure", func() error {
		err := copyFile(false, false, false, root+"/missing", local+"/must-not-exist")
		if !errors.Is(err, os.ErrNotExist) {
			return errors.New("missing source was not a no-such-file failure")
		}
		if _, err := os.Stat(local + "/must-not-exist"); !errors.Is(err, os.ErrNotExist) {
			return errors.New("missing-source download created a destination")
		}
		return nil
	})
}

// Cleanup is explicit and can be invoked again only after owner authorization.
// It never recursively removes a tree or follows a symlink. Unknown entries or
// changed types refuse cleanup before any deletion. Stable private trees remain
// required: the protocol offers no atomic directory-inventory/delete transaction.
func Cleanup(client *sftp.Client, root string) error {
	if err := ValidateRoot(root); err != nil {
		return err
	}
	if client == nil {
		return errors.New("SFTP client unavailable")
	}
	existing := map[string]bool{}
	for _, entry := range nodes {
		name := root
		if entry.suffix != "" {
			name += "/" + entry.suffix
		}
		info, err := client.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.IsDir() != entry.directory || (!entry.directory && !info.Mode().IsRegular()) || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("scratch entry type changed; cleanup refused")
		}
		existing[name] = true
	}
	if !existing[root] {
		return nil
	}
	for _, entry := range nodes {
		name := root
		if entry.suffix != "" {
			name += "/" + entry.suffix
		}
		if !entry.directory || !existing[name] {
			continue
		}
		if err := client.ReadDirEach(context.Background(), name, func(info os.FileInfo) error {
			child := name + "/" + info.Name()
			if !existing[child] {
				return errors.New("unknown scratch entry; cleanup refused")
			}
			return nil
		}); err != nil {
			return err
		}
	}
	for i := len(nodes) - 1; i >= 0; i-- {
		entry := nodes[i]
		name := root
		if entry.suffix != "" {
			name += "/" + entry.suffix
		}
		if !existing[name] {
			continue
		}
		var err error
		if entry.directory {
			err = client.RemoveDirectory(name)
		} else {
			err = client.Remove(name)
		}
		if err != nil {
			return err
		}
	}
	if _, err := client.Lstat(root); !errors.Is(err, os.ErrNotExist) {
		return errors.New("scratch absence not verified")
	}
	return nil
}

func writeFixture(name string, size int) (result error) {
	f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, f.Close()) }()
	var pattern [4096]byte
	for i := range pattern {
		pattern[i] = byte(i)
	}
	for remaining := size; remaining > 0; {
		n := min(remaining, len(pattern))
		written, err := f.Write(pattern[:n])
		if err != nil {
			return err
		}
		if written != n {
			return io.ErrShortWrite
		}
		remaining -= n
	}
	return nil
}
func digest(name string) ([sha256.Size]byte, error) {
	var sum [sha256.Size]byte
	f, err := os.Open(name)
	if err != nil {
		return sum, err
	}
	hash := sha256.New()
	_, err = io.CopyBuffer(hash, f, make([]byte, 32<<10))
	err = errors.Join(err, f.Close())
	if err == nil {
		copy(sum[:], hash.Sum(nil))
	}
	return sum, err
}
func sameContents(a, b string) error {
	first, err := digest(a)
	if err != nil {
		return err
	}
	second, err := digest(b)
	if err != nil {
		return err
	}
	if first != second {
		return errors.New("synthetic content checksum differs")
	}
	return nil
}

// KnownChildren documents the cleanup allowlist without exposing mutable state.
func KnownChildren() []string {
	names := make([]string, 0, len(nodes)-1)
	for _, entry := range nodes {
		if entry.suffix != "" {
			names = append(names, entry.suffix)
		}
	}
	return names
}
