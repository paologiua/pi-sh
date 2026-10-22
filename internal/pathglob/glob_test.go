package pathglob

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

type memInfo struct {
	name string
	mode fs.FileMode
}

func (i memInfo) Name() string       { return i.name }
func (i memInfo) Size() int64        { return 0 }
func (i memInfo) Mode() fs.FileMode  { return i.mode }
func (i memInfo) ModTime() time.Time { return time.Time{} }
func (i memInfo) IsDir() bool        { return i.mode.IsDir() }
func (i memInfo) Sys() any           { return nil }

// memFS is a tree of absolute '/'-separated paths. A value of "dir" or "file"
// is the entry type; "->target" is a symbolic link. Paths must be clean.
type memFS struct {
	entries  map[string]string
	order    map[string][]string // optional directory enumeration order
	readdirs int
	failDir  string
	fatalDir string
}

func newMemFS(paths ...string) *memFS {
	m := &memFS{entries: map[string]string{"/": "dir"}}
	for _, p := range paths {
		kind := "file"
		if i := strings.Index(p, " ->"); i >= 0 {
			p, kind = p[:i], p[i+1:]
		}
		if strings.HasSuffix(p, "/") {
			p, kind = strings.TrimSuffix(p, "/"), "dir"
		}
		for dir := parent(p); dir != "/"; dir = parent(dir) {
			m.entries[dir] = "dir"
		}
		m.entries[p] = kind
	}
	return m
}

func parent(p string) string {
	i := strings.LastIndexByte(p, '/')
	if i <= 0 {
		return "/"
	}
	return p[:i]
}

// resolve follows links and validates intermediate directories like a kernel
// would for a path that may contain "." and "..", and a trailing slash.
func (m *memFS) resolve(p string, follow bool) (string, string, bool) {
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	trailing := strings.HasSuffix(p, "/") && p != "/"
	parts := strings.Split(strings.Trim(p, "/"), "/")
	cur := "/"
	for i, part := range parts {
		last := i == len(parts)-1
		switch part {
		case "", ".":
			continue
		case "..":
			cur = parent(cur)
			continue
		}
		next := strings.TrimSuffix(cur, "/") + "/" + part
		kind, ok := m.entries[next]
		if !ok {
			return "", "", false
		}
		for strings.HasPrefix(kind, "->") && (!last || follow || trailing) {
			target := kind[2:]
			if !strings.HasPrefix(target, "/") {
				target = cur + "/" + target
			}
			resolved, k, ok := m.resolve(target, true)
			if !ok {
				return "", "", false
			}
			next, kind = resolved, k
		}
		if !last && kind != "dir" {
			return "", "", false
		}
		cur = next
	}
	kind := m.entries[cur]
	if trailing && kind != "dir" {
		return "", "", false
	}
	return cur, kind, true
}

func (m *memFS) info(p string, follow bool) (fs.FileInfo, error) {
	_, kind, ok := m.resolve(p, follow)
	if !ok {
		return nil, fs.ErrNotExist
	}
	mode := fs.FileMode(0644)
	switch {
	case kind == "dir":
		mode = fs.ModeDir | 0755
	case strings.HasPrefix(kind, "->"):
		mode = fs.ModeSymlink | 0777
	}
	return memInfo{name: p, mode: mode}, nil
}

func (m *memFS) Lstat(_ context.Context, p string) (fs.FileInfo, error) { return m.info(p, false) }
func (m *memFS) Stat(_ context.Context, p string) (fs.FileInfo, error)  { return m.info(p, true) }
func (m *memFS) ReadDir(ctx context.Context, dir string, visit func(string) error) error {
	dir = strings.TrimSuffix(dir, "/")
	if m.fatalDir != "" && dir == m.fatalDir {
		return Fatal(errors.New("connection lost"))
	}
	if m.failDir != "" && dir == m.failDir {
		return fs.ErrPermission
	}
	resolved, kind, ok := m.resolve(dir, true)
	if !ok || kind != "dir" {
		return fs.ErrNotExist
	}
	m.readdirs++
	names := m.order[resolved]
	if names == nil {
		for p := range m.entries {
			if p != "/" && parent(p) == resolved {
				names = append(names, p[strings.LastIndexByte(p, '/')+1:])
			}
		}
		sort.Sort(sort.Reverse(sort.StringSlice(names)))
	}
	for _, name := range names {
		if err := visit(name); err != nil {
			return err
		}
	}
	return nil
}

func paths(r Result) []string {
	var out []string
	for _, m := range r.Paths {
		out = append(out, m.Path)
	}
	return out
}

func TestGlobMatchesOpenBSDSemantics(t *testing.T) {
	m := newMemFS("/r/a1", "/r/a2", "/r/b*x", "/r/.hid", "/r/q?", "/r/[x]", "/r/x", "/r/sub/s1",
		"/r/sp ace", `/r/back\slash`, "/r/A9", "/r/link ->sub", "/r/dangling ->missing", "/r/caf\xc3\xa9")
	for _, tc := range []struct {
		pattern string
		opt     Options
		want    []string
		matches int
		err     error
	}{
		{"/r/a*", Options{}, []string{"/r/a1", "/r/a2"}, 2, nil},
		{`/r/a\*`, Options{}, nil, 0, ErrNoMatch},
		{`/r/a\*`, Options{NoCheck: true}, []string{"/r/a*"}, 0, nil},
		{`/r/b\*x`, Options{}, []string{"/r/b*x"}, 1, nil},
		{"/r/b*x", Options{}, []string{"/r/b*x"}, 1, nil},
		{"/r/[x]", Options{}, []string{"/r/x"}, 1, nil},
		{`/r/\[x]`, Options{}, []string{"/r/[x]"}, 1, nil},
		{"/r/[]x]", Options{}, []string{"/r/x"}, 1, nil},
		{"/r/[!a]?", Options{}, []string{"/r/A9", "/r/q?"}, 2, nil},
		{"/r/[a-b]?", Options{}, []string{"/r/a1", "/r/a2"}, 2, nil},
		{"/r/[[:upper:]][[:digit:]]", Options{}, []string{"/r/A9"}, 1, nil},
		{"/r/[[:bogus:]]1", Options{}, nil, 0, ErrNoMatch},
		{"/r/[[:bogus:]]1", Options{NoCheck: true}, []string{"/r/[[:bogus:]]1"}, 0, nil},
		{"/r/a[", Options{NoCheck: true}, []string{"/r/a["}, 0, nil},
		{"/r/.*", Options{Mark: true}, []string{"/r/../", "/r/./", "/r/.hid"}, 3, nil},
		{`/r/\.h*`, Options{}, []string{"/r/.hid"}, 1, nil},
		{"/r/s*", Options{Mark: true}, []string{"/r/sp ace", "/r/sub/"}, 2, nil},
		{"/r/s*/", Options{Mark: true}, []string{"/r/sub/"}, 1, nil},
		{"/r/*/s1", Options{}, []string{"/r/link/s1", "/r/sub/s1"}, 2, nil},
		{"/r/l*", Options{Mark: true}, []string{"/r/link/"}, 1, nil},
		{"/r/d*", Options{Mark: true}, []string{"/r/dangling"}, 1, nil},
		{"/r/sub/../a1", Options{}, []string{"/r/sub/../a1"}, 1, nil},
		{"/r/a1/", Options{}, nil, 0, ErrNoMatch},
		{"/r/caf?", Options{}, nil, 0, ErrNoMatch},
		{"/r/caf??", Options{}, []string{"/r/caf\xc3\xa9"}, 1, nil},
		{"/r/***1", Options{}, []string{"/r/a1"}, 1, nil},
		{"/r/back*", Options{}, []string{`/r/back\slash`}, 1, nil},
		{`/r/back\\slash`, Options{}, []string{`/r/back\slash`}, 1, nil},
		{"/r/x\\", Options{NoCheck: true}, []string{`/r/x\`}, 0, nil},
		{"/r/{a1,x}", Options{}, nil, 0, ErrNoMatch},
		{"/r/{a1,x}", Options{Brace: true}, []string{"/r/a1", "/r/x"}, 2, nil},
		{"/r/{a1,{x,a2}}", Options{Brace: true, NoSort: true}, []string{"/r/a1", "/r/x", "/r/a2"}, 3, nil},
		{"/r/{zz,yy}", Options{Brace: true, NoCheck: true}, []string{"/r/zz", "/r/yy"}, 0, nil},
		{"/r/{zz,yy}", Options{Brace: true}, nil, 0, ErrNoMatch},
		{"/r/a{1", Options{Brace: true, NoCheck: true}, []string{"/r/a"}, 0, nil},
		{"/r/a{1,2}{", Options{Brace: true}, []string{"/r/a1", "/r/a2"}, 2, nil},
		{`/r/\{a1,x}`, Options{Brace: true, NoCheck: true}, []string{"/r/{a1,x}"}, 0, nil},
		{"/r/{}", Options{Brace: true, Mark: true}, []string{"/r/"}, 1, nil},
		{"{}", Options{Brace: true, NoCheck: true}, []string{"{}"}, 0, nil},
		{"/r/{[},]}x", Options{Brace: true, NoCheck: true}, []string{"/r/[},]x"}, 0, nil},
	} {
		got, err := Glob(context.Background(), m, tc.pattern, tc.opt)
		if !errors.Is(err, tc.err) || !reflect.DeepEqual(paths(got), tc.want) || got.Matches != tc.matches {
			t.Errorf("%q %+v: got %q matches %d err %v; want %q %d %v", tc.pattern, tc.opt, paths(got), got.Matches, err, tc.want, tc.matches, tc.err)
		}
	}
}

func TestGlobOrderMarkAndRelativePaths(t *testing.T) {
	m := newMemFS("/b", "/a/", "/c")
	m.order = map[string][]string{"/": {"c", "a", "b"}}
	got, err := Glob(context.Background(), m, "/*", Options{NoSort: true, Mark: true})
	if err != nil || !reflect.DeepEqual(paths(got), []string{"/c", "/a/", "/b"}) || !got.Paths[1].Marked || got.Paths[0].Marked {
		t.Fatal("server order or mark changed", paths(got), err)
	}
	got, err = Glob(context.Background(), m, "*", Options{})
	if err != nil || !reflect.DeepEqual(paths(got), []string{"a", "b", "c"}) {
		t.Fatal("relative pattern did not list the current directory", paths(got), err)
	}
	if got.Paths[0].Info == nil || !got.Paths[0].Info.IsDir() {
		t.Fatal("lstat information missing")
	}
}

func TestGlobErrorsAndLimits(t *testing.T) {
	m := newMemFS("/ok/a", "/denied/a", "/lost/a")
	m.failDir, m.fatalDir = "/denied", "/lost"
	if _, err := Glob(context.Background(), m, "/denied/*", Options{}); !errors.Is(err, ErrNoMatch) {
		t.Fatal("unreadable directory was not treated as no match", err)
	}
	if _, err := Glob(context.Background(), m, "/lost/*", Options{}); err == nil || errors.Is(err, ErrNoMatch) || err.Error() != "connection lost" {
		t.Fatal("fatal enumeration error lost", err)
	}
	if _, err := Glob(context.Background(), m, "/*/*", Options{}); err == nil || err.Error() != "connection lost" {
		t.Fatal("nested fatal enumeration error lost", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Glob(ctx, m, "/ok/*", Options{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled expansion succeeded", err)
	}

	many := newMemFS()
	for i := 0; i < LimitStat+1; i++ {
		many.entries[fmt.Sprintf("/d/%05d", i)] = "file"
	}
	many.entries["/d"] = "dir"
	if _, err := Glob(context.Background(), many, "/d/*", Options{Limit: true}); !errors.Is(err, ErrTooMany) {
		t.Fatal("stat limit not applied", err)
	}
	if got, err := Glob(context.Background(), many, "/d/*", Options{}); err != nil || len(got.Paths) != LimitStat+1 {
		t.Fatal("unlimited expansion truncated", err)
	}
	if got, err := Glob(context.Background(), many, "/d/0000?", Options{Limit: true}); err != nil || len(got.Paths) != 10 {
		t.Fatal("bounded expansion failed below limits", err)
	}
	long := newMemFS()
	long.entries["/d"] = "dir"
	name := strings.Repeat("n", 200)
	for i := 0; i < 400; i++ {
		long.entries[fmt.Sprintf("/d/%s%03d", name, i)] = "file"
	}
	if _, err := Glob(context.Background(), long, "/d/*", Options{Limit: true}); !errors.Is(err, ErrTooMany) {
		t.Fatal("result byte limit not applied", err)
	}
	wide := newMemFS()
	wide.entries["/d"] = "dir"
	for i := 0; i < LimitReaddir+1; i++ {
		wide.entries[fmt.Sprintf("/d/%05d", i)] = "file"
	}
	if _, err := Glob(context.Background(), wide, "/d/x*", Options{Limit: true}); !errors.Is(err, ErrTooMany) {
		t.Fatal("readdir limit not applied", err)
	}
}

func TestEscapeUnescapeAndMagic(t *testing.T) {
	for _, s := range []string{"plain", `a*b?c[d]\e`, `trailing\`, "", "{a,b}"} {
		m := newMemFS("/d/" + s)
		if s == "" {
			continue
		}
		got, err := Glob(context.Background(), m, "/d/"+Escape(s), Options{})
		if err != nil || !reflect.DeepEqual(paths(got), []string{"/d/" + s}) {
			t.Errorf("escaped %q did not match literally: %q %v", s, paths(got), err)
		}
		if HasMagic(Escape(s)) {
			t.Errorf("escaped %q reported magic", s)
		}
	}
	for in, want := range map[string]string{`a\*b`: "a*b", `a\b`: `a\b`, `a\\b`: `a\b`, `x\`: `x\`, `\?\[`: "?["} {
		if got := Unescape(in); got != want {
			t.Errorf("Unescape(%q) = %q, want %q", in, got, want)
		}
	}
	for pattern, want := range map[string]bool{"a*": true, `a\*`: false, "x?": true, "[": true, "{a,b}": false, `\\*`: true} {
		if HasMagic(pattern) != want {
			t.Errorf("HasMagic(%q) != %t", pattern, want)
		}
	}
}

func FuzzGlobDoesNotPanic(f *testing.F) {
	for _, seed := range []string{"/r/*", "[", "[!", "[[:", "[[:alpha:]", "{a,{b}", `\`, "[a-", "/r/[a/b]*", "{[}"} {
		f.Add(seed)
	}
	m := newMemFS("/r/a", "/r/b/c", "/r/.d")
	f.Fuzz(func(t *testing.T, pattern string) {
		for _, opt := range []Options{{}, {NoCheck: true, Mark: true, Brace: true, Limit: true}} {
			Glob(context.Background(), m, pattern, opt)
		}
	})
}
