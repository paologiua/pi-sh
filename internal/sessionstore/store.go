// Package sessionstore keeps the Raspberry Pi Connect login sessions saved by
// the pi-sh CLIs in one encrypted file, protected by a single passphrase
// shared by all saved sessions, much like an OpenSSH private key protected by
// its passphrase.
//
// The file is sealed with XChaCha20-Poly1305 under a key derived from the
// passphrase with Argon2id; the header carrying the KDF parameters, salt and
// nonce is authenticated as additional data. Session data is opaque here. The
// package never prompts: callers supply the passphrase. Every change is a
// read-modify-write under an exclusive lock on a sibling lock file, written
// to a temporary file and renamed into place, so concurrent pi-sh processes
// saving different accounts keep each other's entries.
package sessionstore

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"pi-sh/internal/account"
)

// FileName is the name of the sessions file inside the pi-sh configuration
// directory; the lock file is FileName + ".lock".
const FileName = "sessions"

var (
	// ErrPassphrase means the file could not be authenticated with the given
	// passphrase (or vault key): a wrong passphrase, a passphrase changed by
	// another process, or tampering with authenticated content.
	ErrPassphrase = errors.New("incorrect passphrase for saved sessions")
	// ErrCorrupt means the file is not a well-formed pi-sh sessions file or
	// declares unacceptable parameters.
	ErrCorrupt = errors.New("saved sessions file is corrupt or has an unsupported format")
	// ErrExists is returned by Create when the file already exists.
	ErrExists = fmt.Errorf("saved sessions file already exists: %w", fs.ErrExist)
	// ErrNotExist is returned when the file does not exist.
	ErrNotExist = fmt.Errorf("no saved sessions file: %w", fs.ErrNotExist)
	// ErrEmptyPassphrase rejects an empty new passphrase.
	ErrEmptyPassphrase = errors.New("saved sessions passphrase must not be empty")
	// ErrClosed is returned by a Vault after Close.
	ErrClosed = errors.New("saved sessions vault is closed")

	errNoConfigDir = errors.New("cannot locate the pi-sh configuration directory: no home directory and no absolute XDG_CONFIG_HOME")
)

// Entry is one saved login session. Account is the Raspberry Pi ID email,
// compared case-insensitively. Data is opaque to this package.
type Entry struct {
	Account string
	Saved   time.Time
	Used    time.Time
	Data    []byte
}

func (e Entry) clone() Entry {
	e.Data = bytes.Clone(e.Data)
	return e
}

// Store names a sessions file. It holds no secrets.
type Store struct {
	path string
}

// DefaultPath reports the sessions file in the pi-sh configuration
// directory (see account.ConfigDir), or "" when it cannot be located.
func DefaultPath() string {
	dir := account.ConfigDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, FileName)
}

// Default returns the Store at DefaultPath.
func Default() (*Store, error) {
	path := DefaultPath()
	if path == "" {
		return nil, errNoConfigDir
	}
	return Open(path), nil
}

// Open returns the Store for path. Nothing is read or created.
func Open(path string) *Store {
	return &Store{path: path}
}

// Path reports the sessions file path.
func (s *Store) Path() string { return s.path }

// Exists reports whether the sessions file exists. It does not check its
// permissions or contents.
func (s *Store) Exists() (bool, error) {
	_, err := os.Lstat(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("saved sessions file: %w", err)
	}
	return true, nil
}

// Create writes a new, empty sessions file protected by passphrase and
// returns its unlocked Vault. It fails with ErrExists if the file exists.
func (s *Store) Create(passphrase []byte) (*Vault, error) {
	if len(passphrase) == 0 {
		return nil, ErrEmptyPassphrase
	}
	var v *Vault
	err := s.withLock(func() error {
		if ok, err := s.Exists(); err != nil {
			return err
		} else if ok {
			return ErrExists
		}
		nv, err := newVault(s, passphrase)
		if err != nil {
			return err
		}
		if err := nv.save(nil); err != nil {
			nv.Close()
			return err
		}
		v = nv
		return nil
	})
	return v, err
}

// Unlock derives the key from passphrase and checks it against the file. It
// returns ErrPassphrase for a wrong passphrase and ErrNotExist when there is
// no file.
func (s *Store) Unlock(passphrase []byte) (*Vault, error) {
	var v *Vault
	err := s.withLock(func() error {
		file, err := s.read()
		if err != nil {
			return err
		}
		h, sealed, err := parseHeader(file)
		if err != nil {
			return err
		}
		nv := &Vault{store: s, salt: bytes.Clone(h.salt), params: h.params}
		nv.key = deriveKey(passphrase, nv.salt, nv.params)
		plain, err := open(nv.key, h, sealed)
		if err != nil {
			nv.Close()
			return err
		}
		_, err = decodePayload(plain)
		zero(plain)
		if err != nil {
			nv.Close()
			return err
		}
		v = nv
		return nil
	})
	return v, err
}

// Remove deletes the sessions file, for example after the last logout or a
// forgotten passphrase. A missing file is not an error. The lock file is
// left in place, since removing it could let two processes lock different
// files.
func (s *Store) Remove() error {
	return s.withLock(func() error {
		err := os.Remove(s.path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove saved sessions file: %w", err)
		}
		return nil
	})
}

// withLock runs fn while holding the exclusive lock on the sibling lock
// file, creating the directory (0700) and lock file (0600) when needed.
func (s *Store) withLock(fn func() error) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create configuration directory: %w", err)
	}
	f, err := os.OpenFile(s.path+".lock", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("open saved sessions lock file: %w", err)
	}
	defer f.Close()
	if err := lockFile(f); err != nil {
		return fmt.Errorf("lock saved sessions file: %w", err)
	}
	defer unlockFile(f)
	return fn()
}

// read returns the raw file after checking that it is a private regular
// file of bounded size.
func (s *Store) read() ([]byte, error) {
	info, err := os.Lstat(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotExist
	}
	if err != nil {
		return nil, fmt.Errorf("saved sessions file: %w", err)
	}
	if err := checkMode(s.path, info); err != nil {
		return nil, err
	}
	f, err := os.Open(s.path)
	if err != nil {
		return nil, fmt.Errorf("saved sessions file: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("saved sessions file: %w", err)
	}
	if len(data) > maxFileBytes {
		return nil, ErrCorrupt
	}
	return data, nil
}

func checkMode(path string, info fs.FileInfo) error {
	if !info.Mode().IsRegular() {
		return fmt.Errorf("saved sessions file %s is not a regular file", path)
	}
	// Windows reports synthetic permission bits; rely on the profile ACL.
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("saved sessions file %s is accessible by other users (mode %04o); run chmod 600 on it", path, info.Mode().Perm())
	}
	return nil
}

// write atomically replaces the file: a 0600 temporary file in the same
// directory is written, synced and renamed over it.
func (s *Store) write(data []byte) (err error) {
	dir := filepath.Dir(s.path)
	f, err := os.CreateTemp(dir, "."+FileName+"-*.tmp")
	if err != nil {
		return fmt.Errorf("write saved sessions file: %w", err)
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(tmp)
		}
	}()
	// CreateTemp already uses 0600; make it explicit regardless of umask.
	if runtime.GOOS != "windows" {
		if err = f.Chmod(0o600); err != nil {
			return fmt.Errorf("write saved sessions file: %w", err)
		}
	}
	if _, err = f.Write(data); err != nil {
		return fmt.Errorf("write saved sessions file: %w", err)
	}
	if err = f.Sync(); err != nil {
		return fmt.Errorf("write saved sessions file: %w", err)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("write saved sessions file: %w", err)
	}
	if err = os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("write saved sessions file: %w", err)
	}
	if runtime.GOOS != "windows" {
		// Best effort: persist the rename itself.
		if d, derr := os.Open(dir); derr == nil {
			d.Sync()
			d.Close()
		}
	}
	return nil
}

// Vault is an unlocked sessions file. It keeps the derived key, salt and KDF
// parameters in memory, never the passphrase. Every operation re-reads the
// file under the lock, so changes made by other processes are seen. A Vault
// is safe for concurrent use.
type Vault struct {
	mu     sync.Mutex
	store  *Store
	key    []byte
	salt   []byte
	params kdfParams
}

func newVault(s *Store, passphrase []byte) (*Vault, error) {
	salt, err := randomBytes(saltLen)
	if err != nil {
		return nil, err
	}
	p := defaultParams
	return &Vault{store: s, salt: salt, params: p, key: deriveKey(passphrase, salt, p)}, nil
}

// Close zeroes the derived key (best effort). Later calls fail with
// ErrClosed.
func (v *Vault) Close() {
	v.mu.Lock()
	defer v.mu.Unlock()
	zero(v.key)
	v.key = nil
}

// load reads and decrypts the file. The lock must be held. A file
// re-encrypted under another salt or parameters (a passphrase change by
// another process) yields ErrPassphrase, so it is never overwritten.
func (v *Vault) load() ([]Entry, error) {
	if v.key == nil {
		return nil, ErrClosed
	}
	file, err := v.store.read()
	if err != nil {
		return nil, err
	}
	h, sealed, err := parseHeader(file)
	if err != nil {
		return nil, err
	}
	if h.params != v.params || !bytes.Equal(h.salt, v.salt) {
		return nil, ErrPassphrase
	}
	plain, err := open(v.key, h, sealed)
	if err != nil {
		return nil, err
	}
	defer zero(plain)
	return decodePayload(plain)
}

// save encrypts entries with a fresh nonce and writes them. The lock must be
// held.
func (v *Vault) save(entries []Entry) error {
	if v.key == nil {
		return ErrClosed
	}
	plain, err := encodePayload(entries)
	if err != nil {
		return err
	}
	defer zero(plain)
	sealed, err := seal(v.key, v.salt, v.params, plain)
	if err != nil {
		return err
	}
	return v.store.write(sealed)
}

// update runs fn on the current entries under the lock and saves the result
// when fn reports a change.
func (v *Vault) update(fn func([]Entry) ([]Entry, bool)) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.store.withLock(func() error {
		entries, err := v.load()
		if err != nil {
			return err
		}
		entries, changed := fn(entries)
		if !changed {
			return nil
		}
		return v.save(entries)
	})
}

// List returns all entries in file order.
func (v *Vault) List() ([]Entry, error) {
	var out []Entry
	err := v.update(func(entries []Entry) ([]Entry, bool) {
		out = entries
		return entries, false
	})
	return out, err
}

// Get returns the entry for account, compared case-insensitively.
func (v *Vault) Get(account string) (Entry, bool, error) {
	entries, err := v.List()
	if err != nil {
		return Entry{}, false, err
	}
	if i := find(entries, account); i >= 0 {
		return entries[i], true, nil
	}
	return Entry{}, false, nil
}

// Put inserts e, or replaces the entry whose account matches
// case-insensitively. The account must be a valid email address.
func (v *Vault) Put(e Entry) error {
	if err := account.Validate(e.Account); err != nil {
		return err
	}
	e = e.clone()
	return v.update(func(entries []Entry) ([]Entry, bool) {
		if i := find(entries, e.Account); i >= 0 {
			entries[i] = e
		} else {
			entries = append(entries, e)
		}
		return entries, true
	})
}

// Delete removes the entry for account and reports whether it existed. The
// file is kept even when it becomes empty; use Store.Remove to delete it.
func (v *Vault) Delete(account string) (bool, error) {
	var found bool
	err := v.update(func(entries []Entry) ([]Entry, bool) {
		i := find(entries, account)
		if i < 0 {
			return entries, false
		}
		found = true
		return append(entries[:i], entries[i+1:]...), true
	})
	return found, err
}

// ChangePassphrase re-encrypts every entry under a key derived from
// newPassphrase with a new salt and the default KDF parameters. Other Vaults
// holding the old key then fail with ErrPassphrase.
func (v *Vault) ChangePassphrase(newPassphrase []byte) error {
	if len(newPassphrase) == 0 {
		return ErrEmptyPassphrase
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.store.withLock(func() error {
		entries, err := v.load()
		if err != nil {
			return err
		}
		nv, err := newVault(v.store, newPassphrase)
		if err != nil {
			return err
		}
		if err := nv.save(entries); err != nil {
			zero(nv.key)
			return err
		}
		zero(v.key)
		v.key, v.salt, v.params = nv.key, nv.salt, nv.params
		return nil
	})
}

func find(entries []Entry, account string) int {
	for i, e := range entries {
		if strings.EqualFold(e.Account, account) {
			return i
		}
	}
	return -1
}
