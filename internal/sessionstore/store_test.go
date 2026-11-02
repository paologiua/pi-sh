package sessionstore

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	// Keep tests fast; production defaults are checked in TestDefaultParams.
	defaultParams = kdfParams{Time: 1, MemoryKiB: 64, Threads: 1}
	os.Exit(m.Run())
}

func TestDefaultParams(t *testing.T) {
	// The RFC 9106 second recommendation must remain the production default.
	want := kdfParams{Time: 3, MemoryKiB: 64 * 1024, Threads: 4}
	if !want.valid() {
		t.Fatal("RFC 9106 parameters rejected")
	}
	src, err := os.ReadFile("format.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "var defaultParams = kdfParams{Time: 3, MemoryKiB: 64 * 1024, Threads: 4}") {
		t.Fatal("production KDF defaults changed")
	}
}

func newStore(t *testing.T) *Store {
	t.Helper()
	return Open(filepath.Join(t.TempDir(), "cfg", "pi-sh", FileName))
}

func entry(account, data string) Entry {
	now := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	return Entry{Account: account, Saved: now, Used: now.Add(time.Minute), Data: []byte(data)}
}

func TestRoundTrip(t *testing.T) {
	s := newStore(t)
	if ok, err := s.Exists(); ok || err != nil {
		t.Fatalf("Exists = %v, %v", ok, err)
	}
	if _, err := s.Unlock([]byte("pw")); !errors.Is(err, ErrNotExist) {
		t.Fatalf("Unlock missing: %v", err)
	}
	v, err := s.Create([]byte("correct horse"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create([]byte("x")); !errors.Is(err, ErrExists) {
		t.Fatalf("second Create: %v", err)
	}
	want := entry("a@example.com", "cookie-jar-1")
	if err := v.Put(want); err != nil {
		t.Fatal(err)
	}
	if err := v.Put(entry("b@example.com", "cookie-jar-2")); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(s.Path())
	if bytes.Contains(raw, []byte("cookie-jar")) || bytes.Contains(raw, []byte("example.com")) {
		t.Fatal("plaintext visible in file")
	}
	if !bytes.HasPrefix(raw, []byte(magic)) {
		t.Fatal("missing magic")
	}

	v2, err := s.Unlock([]byte("correct horse"))
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := v2.Get("A@Example.COM")
	if err != nil || !ok {
		t.Fatalf("Get = %v, %v", ok, err)
	}
	if got.Account != want.Account || !got.Saved.Equal(want.Saved) || !got.Used.Equal(want.Used) || string(got.Data) != "cookie-jar-1" {
		t.Fatalf("Get = %+v", got)
	}
	list, err := v2.List()
	if err != nil || len(list) != 2 || list[0].Account != "a@example.com" || list[1].Account != "b@example.com" {
		t.Fatalf("List = %+v, %v", list, err)
	}
	if err := v.Put(entry("bad", "x")); err == nil {
		t.Fatal("invalid account accepted")
	}
	if _, err := s.Create(nil); !errors.Is(err, ErrEmptyPassphrase) {
		t.Fatalf("empty passphrase: %v", err)
	}
}

func TestNonceFreshOnEveryWrite(t *testing.T) {
	s := newStore(t)
	v, err := s.Create([]byte("pw"))
	if err != nil {
		t.Fatal(err)
	}
	nonces := map[string]bool{}
	for i := 0; i < 5; i++ {
		if err := v.Put(entry("a@example.com", "same")); err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(s.Path())
		h, _, err := parseHeader(raw)
		if err != nil {
			t.Fatal(err)
		}
		nonces[string(h.nonce)] = true
	}
	if len(nonces) != 5 {
		t.Fatalf("nonce reused: %d distinct of 5", len(nonces))
	}
}

func TestWrongPassphrase(t *testing.T) {
	s := newStore(t)
	if _, err := s.Create([]byte("right")); err != nil {
		t.Fatal(err)
	}
	_, err := s.Unlock([]byte("wrong"))
	if !errors.Is(err, ErrPassphrase) {
		t.Fatalf("Unlock wrong = %v", err)
	}
	if strings.Contains(err.Error(), "wrong") || strings.Contains(err.Error(), "right") {
		t.Fatal("passphrase in error")
	}
}

func TestTamper(t *testing.T) {
	s := newStore(t)
	v, err := s.Create([]byte("pw"))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Put(entry("a@example.com", "data")); err != nil {
		t.Fatal(err)
	}
	orig, _ := os.ReadFile(s.Path())
	saltOff := len(magic) + 11
	cases := map[string]func([]byte) []byte{
		"magic":      func(b []byte) []byte { b[0] ^= 1; return b },
		"kdf id":     func(b []byte) []byte { b[len(magic)] = 2; return b },
		"time":       func(b []byte) []byte { b[len(magic)+4] ^= 1; return b }, // 1 -> 0 (invalid) or 2
		"memory":     func(b []byte) []byte { b[len(magic)+8] ^= 0x40; return b },
		"salt":       func(b []byte) []byte { b[saltOff] ^= 1; return b },
		"nonce":      func(b []byte) []byte { b[saltOff+saltLen] ^= 1; return b },
		"ciphertext": func(b []byte) []byte { b[len(b)-20] ^= 1; return b },
		"tag":        func(b []byte) []byte { b[len(b)-1] ^= 1; return b },
		"truncated":  func(b []byte) []byte { return b[:saltOff+saltLen+10] },
		"empty":      func(b []byte) []byte { return nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(s.Path(), mutate(bytes.Clone(orig)), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Unlock([]byte("pw")); !errors.Is(err, ErrPassphrase) && !errors.Is(err, ErrCorrupt) {
				t.Fatalf("Unlock = %v", err)
			}
			if _, err := v.List(); !errors.Is(err, ErrPassphrase) && !errors.Is(err, ErrCorrupt) {
				t.Fatalf("List = %v", err)
			}
		})
	}
}

func TestAbsurdParamsRejectedBeforeKDF(t *testing.T) {
	saved := deriveKey
	defer func() { deriveKey = saved }()
	calls := 0
	deriveKey = func(p, s []byte, k kdfParams) []byte { calls++; return saved(p, s, k) }

	s := newStore(t)
	if _, err := s.Create([]byte("pw")); err != nil {
		t.Fatal(err)
	}
	orig, _ := os.ReadFile(s.Path())
	h, sealed, err := parseHeader(orig)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []kdfParams{
		{Time: 1, MemoryKiB: 2 << 20, Threads: 1}, // 2 GiB
		{Time: 17, MemoryKiB: 64, Threads: 1},
		{Time: 0, MemoryKiB: 64, Threads: 1},
		{Time: 1, MemoryKiB: 64, Threads: 0},
		{Time: 1, MemoryKiB: 1024, Threads: 17},
		{Time: 1, MemoryKiB: 4, Threads: 1},
	} {
		h.params = p
		file := append(h.encode(), sealed...)
		if err := os.WriteFile(s.Path(), file, 0o600); err != nil {
			t.Fatal(err)
		}
		calls = 0
		if _, err := s.Unlock([]byte("pw")); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("%+v: Unlock = %v", p, err)
		}
		if calls != 0 {
			t.Fatalf("%+v: KDF ran", p)
		}
	}
}

func TestPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions")
	}
	s := newStore(t)
	if _, err := s.Create([]byte("pw")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(s.Path())
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("file mode %v, %v", fi.Mode(), err)
	}
	di, err := os.Stat(filepath.Dir(s.Path()))
	if err != nil || di.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v, %v", di.Mode(), err)
	}
	li, err := os.Stat(s.Path() + ".lock")
	if err != nil || li.Mode().Perm() != 0o600 {
		t.Fatalf("lock mode %v, %v", li.Mode(), err)
	}
	if err := os.Chmod(s.Path(), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Unlock([]byte("pw")); err == nil || !strings.Contains(err.Error(), "other users") {
		t.Fatalf("0644 accepted: %v", err)
	}
	if err := os.Chmod(s.Path(), 0o600); err != nil {
		t.Fatal(err)
	}

	// Symlinks and directories are not regular files.
	link := Open(filepath.Join(filepath.Dir(s.Path()), "link"))
	if err := os.Symlink(s.Path(), link.Path()); err != nil {
		t.Fatal(err)
	}
	if _, err := link.Unlock([]byte("pw")); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("symlink accepted: %v", err)
	}
	dir := Open(t.TempDir())
	if _, err := dir.Unlock([]byte("pw")); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("directory accepted: %v", err)
	}
}

func TestConcurrentPut(t *testing.T) {
	s := newStore(t)
	if _, err := s.Create([]byte("pw")); err != nil {
		t.Fatal(err)
	}
	// Two independent Vaults stand in for two processes: each has its own
	// lock file descriptor, and only the file lock serializes them.
	v1, err := Open(s.Path()).Unlock([]byte("pw"))
	if err != nil {
		t.Fatal(err)
	}
	v2, err := Open(s.Path()).Unlock([]byte("pw"))
	if err != nil {
		t.Fatal(err)
	}
	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, 2*n)
	for i, v := range []*Vault{v1, v2} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < n; j++ {
				errs <- v.Put(entry(fmt.Sprintf("u%d-%d@example.com", i, j), "x"))
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	list, err := v1.List()
	if err != nil || len(list) != 2*n {
		t.Fatalf("List has %d entries, want %d (%v)", len(list), 2*n, err)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(s.Path()), ".sessions-*.tmp")); len(left) != 0 {
		t.Fatalf("temporary files left: %v", left)
	}
}

func TestChangePassphrase(t *testing.T) {
	s := newStore(t)
	v, err := s.Create([]byte("old"))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Put(entry("a@example.com", "d")); err != nil {
		t.Fatal(err)
	}
	other, err := s.Unlock([]byte("old"))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(s.Path())
	if err := v.ChangePassphrase([]byte("new")); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(s.Path())
	hb, _, _ := parseHeader(before)
	ha, _, _ := parseHeader(after)
	if bytes.Equal(hb.salt, ha.salt) {
		t.Fatal("salt not renewed")
	}
	// The stale vault must refuse to read or overwrite.
	if err := other.Put(entry("b@example.com", "d")); !errors.Is(err, ErrPassphrase) {
		t.Fatalf("stale Put = %v", err)
	}
	if now, _ := os.ReadFile(s.Path()); !bytes.Equal(now, after) {
		t.Fatal("stale vault overwrote the file")
	}
	if _, err := s.Unlock([]byte("old")); !errors.Is(err, ErrPassphrase) {
		t.Fatalf("old passphrase = %v", err)
	}
	nv, err := s.Unlock([]byte("new"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := nv.Get("a@example.com"); !ok || err != nil {
		t.Fatalf("entry lost: %v %v", ok, err)
	}
	// The changing vault keeps working with the new key.
	if err := v.Put(entry("c@example.com", "d")); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteAndCase(t *testing.T) {
	s := newStore(t)
	v, err := s.Create([]byte("pw"))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Put(entry("Owner@Example.com", "1")); err != nil {
		t.Fatal(err)
	}
	if err := v.Put(entry("owner@example.COM", "2")); err != nil {
		t.Fatal(err)
	}
	list, _ := v.List()
	if len(list) != 1 || string(list[0].Data) != "2" || list[0].Account != "owner@example.COM" {
		t.Fatalf("case-insensitive replace: %+v", list)
	}
	if ok, err := v.Delete("nobody@example.com"); ok || err != nil {
		t.Fatalf("Delete missing = %v, %v", ok, err)
	}
	if ok, err := v.Delete("OWNER@EXAMPLE.COM"); !ok || err != nil {
		t.Fatalf("Delete = %v, %v", ok, err)
	}
	if list, _ := v.List(); len(list) != 0 {
		t.Fatalf("after Delete: %+v", list)
	}
	if err := s.Remove(); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.Exists(); ok {
		t.Fatal("Remove left the file")
	}
	if err := s.Remove(); err != nil {
		t.Fatalf("second Remove: %v", err)
	}
	if _, err := v.List(); !errors.Is(err, ErrNotExist) {
		t.Fatalf("List after Remove = %v", err)
	}
	v.Close()
	if _, err := v.List(); !errors.Is(err, ErrClosed) {
		t.Fatalf("List after Close = %v", err)
	}
}

func TestDataIsCopied(t *testing.T) {
	s := newStore(t)
	v, err := s.Create([]byte("pw"))
	if err != nil {
		t.Fatal(err)
	}
	e := entry("a@example.com", "abc")
	if err := v.Put(e); err != nil {
		t.Fatal(err)
	}
	e.Data[0] = 'X'
	got, _, _ := v.Get("a@example.com")
	if string(got.Data) != "abc" {
		t.Fatalf("Data aliased: %q", got.Data)
	}
}

func TestDefaultPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	s, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "pi-sh", "sessions"); s.Path() != want {
		t.Fatalf("Default = %s, want %s", s.Path(), want)
	}
}
