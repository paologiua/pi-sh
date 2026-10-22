// Package pathglob expands SFTP/SCP path patterns with the semantics of the
// OpenBSD glob(3) used by OpenSSH's sftp and scp: '*', '?', bracket sets and
// POSIX classes per path segment, backslash escapes, a leading '.' that must be
// matched literally, optional csh-style braces, GLOB_MARK, GLOB_NOCHECK and the
// GLOB_LIMIT resource bounds. Paths are byte strings; matching is per byte as in
// the C locale. Directories are enumerated through a caller-supplied FS, one
// entry at a time, so a remote listing is never retained by the expander.
//
// No path is ever passed to a shell. Tilde is not expanded here: OpenSSH's glob
// calls do not set GLOB_TILDE.
package pathglob

import (
	"context"
	"errors"
	"io/fs"
	"sort"
	"strings"
)

// FS is the directory view the expander walks. ReadDir must not report "." or
// "..": the expander supplies them itself, as a conventional SFTP server does.
// Errors are treated like glob(3) treats opendir/lstat failures (no match),
// unless wrapped with Fatal, in which case expansion stops with that error.
type FS interface {
	ReadDir(ctx context.Context, dir string, visit func(name string) error) error
	Lstat(ctx context.Context, name string) (fs.FileInfo, error)
	Stat(ctx context.Context, name string) (fs.FileInfo, error)
}

// Options selects glob(3) flags. Limit applies OpenSSH remote_glob's GLOB_LIMIT
// bounds; local OpenSSH globbing (sftp put) does not set it.
type Options struct {
	Mark    bool // GLOB_MARK: append '/' to directories (following symlinks).
	NoCheck bool // GLOB_NOCHECK: return the unescaped pattern when nothing matches.
	Brace   bool // GLOB_BRACE: csh-style {a,b} alternatives (sftp ls only).
	NoSort  bool // GLOB_NOSORT: keep enumeration order.
	Limit   bool // GLOB_LIMIT: bound directory reads, lstat calls and result bytes.
}

// The OpenBSD glob(3) GLOB_LIMIT values.
const (
	LimitMalloc  = 65536
	LimitStat    = 2048
	LimitReaddir = 16384
)

// Match is one expanded path. Info is the lstat result, nil for a NOCHECK
// pattern. Marked reports that Mark appended the trailing '/'.
type Match struct {
	Path   string
	Info   fs.FileInfo
	Marked bool
}

// Result mirrors glob_t: Paths holds matches and NOCHECK patterns; Matches is
// gl_matchc, the number of real matches.
type Result struct {
	Paths   []Match
	Matches int
}

var (
	// ErrNoMatch corresponds to GLOB_NOMATCH.
	ErrNoMatch = errors.New("no match")
	// ErrTooMany corresponds to GLOB_NOSPACE under Options.Limit.
	ErrTooMany = errors.New("too many matches")
)

type fatalError struct{ err error }

func (e fatalError) Error() string { return e.err.Error() }
func (e fatalError) Unwrap() error { return e.err }

// Fatal marks an FS error (for example a lost connection) that must stop the
// expansion instead of being read as "no such path".
func Fatal(err error) error {
	if err == nil {
		return nil
	}
	return fatalError{err}
}

// Character encoding of the OpenBSD implementation: a byte, plus a quote bit
// for compiled metacharacters or a protect bit for backslash-escaped input.
type char uint16

const (
	mQuote   char = 0x8000
	mProtect char = 0x4000
	mASCII   char = 0x00ff
	mAll          = mQuote | '*'
	mEnd          = mQuote | ']'
	mNot          = mQuote | '!'
	mOne          = mQuote | '?'
	mRng          = mQuote | '-'
	mSet          = mQuote | '['
	mClass        = mQuote | ':'
)

func ismeta(c char) bool { return c&mQuote != 0 }

var classes = []struct {
	name string
	is   func(byte) bool
}{
	{"alnum", func(c byte) bool { return isAlpha(c) || isDigit(c) }},
	{"alpha", isAlpha},
	{"blank", func(c byte) bool { return c == ' ' || c == '\t' }},
	{"cntrl", func(c byte) bool { return c < 0x20 || c == 0x7f }},
	{"digit", isDigit},
	{"graph", func(c byte) bool { return c > 0x20 && c < 0x7f }},
	{"lower", func(c byte) bool { return c >= 'a' && c <= 'z' }},
	{"print", func(c byte) bool { return c >= 0x20 && c < 0x7f }},
	{"punct", func(c byte) bool { return c > 0x20 && c < 0x7f && !isAlpha(c) && !isDigit(c) }},
	{"space", func(c byte) bool { return c == ' ' || (c >= '\t' && c <= '\r') }},
	{"upper", func(c byte) bool { return c >= 'A' && c <= 'Z' }},
	{"xdigit", func(c byte) bool { return isDigit(c) || (c|0x20 >= 'a' && c|0x20 <= 'f') }},
}

func isAlpha(c byte) bool { return (c|0x20) >= 'a' && (c|0x20) <= 'z' }
func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// HasMagic reports whether pattern contains an unescaped '*', '?' or '['.
// It is a quick test; an unterminated '[' is still literal during expansion.
func HasMagic(pattern string) bool {
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '\\':
			i++
		case '*', '?', '[':
			return true
		}
	}
	return false
}

// Escape protects every glob metacharacter and backslash so that the result
// matches s literally.
func Escape(s string) string {
	if !strings.ContainsAny(s, `\*?[`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if strings.IndexByte(`\*?[`, s[i]) >= 0 {
			b.WriteByte('\\')
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// Unescape is OpenSSH sftp's undo_glob_escape: it removes the backslash only
// before '?', '[', '*' and '\\', which is the escaping sftp's argument splitter
// adds. Other backslashes are kept, including a trailing one.
func Unescape(s string) string {
	if strings.IndexByte(s, '\\') < 0 {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && strings.IndexByte(`?[*\`, s[i+1]) >= 0 {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

type globber struct {
	ctx                   context.Context
	fs                    FS
	opt                   Options
	res                   Result
	readdirs, stats, heap int
}

// Glob expands pattern. ErrNoMatch is returned when nothing matches and
// NoCheck is unset; ErrTooMany when Limit is exceeded (partial results are
// discarded). Other errors are FS errors wrapped with Fatal, or ctx errors.
func Glob(ctx context.Context, fsys FS, pattern string, opt Options) (Result, error) {
	g := &globber{ctx: ctx, fs: fsys, opt: opt}
	// Protect backslash-escaped characters; a trailing backslash is literal.
	in := make([]char, 0, len(pattern))
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		if c == '\\' && i+1 < len(pattern) {
			i++
			in = append(in, char(pattern[i])|mProtect)
			continue
		}
		in = append(in, char(c))
	}
	var err error
	if opt.Brace {
		err = g.globexp1(in)
	} else {
		err = g.glob0(in)
	}
	if err != nil {
		var fatal fatalError
		if errors.As(err, &fatal) {
			return Result{}, fatal.err
		}
		if err == ErrNoMatch && opt.Brace {
			err = nil
		}
		if err != nil {
			return Result{}, err
		}
	}
	if len(g.res.Paths) == 0 && !opt.NoCheck {
		return Result{}, ErrNoMatch
	}
	return g.res, nil
}

func indexChar(s []char, c char) int {
	for i, v := range s {
		if v == c {
			return i
		}
	}
	return -1
}

// globexp1/globexp2 are OpenBSD's brace expansion, including its handling of
// unbalanced braces (only the prefix before the brace is globbed).
func (g *globber) globexp1(pattern []char) error {
	if len(pattern) == 2 && pattern[0] == '{' && pattern[1] == '}' {
		return g.glob0(pattern)
	}
	if i := indexChar(pattern, '{'); i >= 0 {
		return g.globexp2(i, pattern)
	}
	return g.glob0(pattern)
}

func (g *globber) globexp2(brace int, pattern []char) error {
	prefix := pattern[:brace]
	at := func(i int) char {
		if i < len(pattern) {
			return pattern[i]
		}
		return 0
	}
	depth, pe := 0, brace+1
	for ; pe < len(pattern); pe++ {
		if pattern[pe] == '[' {
			pm := pe
			for pe++; pe < len(pattern) && pattern[pe] != ']'; pe++ {
			}
			if pe >= len(pattern) {
				pe = pm
			}
		} else if pattern[pe] == '{' {
			depth++
		} else if pattern[pe] == '}' {
			if depth == 0 {
				break
			}
			depth--
		}
	}
	if depth != 0 || pe >= len(pattern) {
		return g.glob0(append([]char(nil), prefix...))
	}
	pl := brace + 1
	depth = 0
	for pm := brace + 1; pm <= pe; pm++ {
		switch at(pm) {
		case '[':
			start := pm
			for pm++; pm < len(pattern) && pattern[pm] != ']'; pm++ {
			}
			if pm >= len(pattern) {
				pm = start
			}
		case '{':
			depth++
		case '}', ',':
			if at(pm) == '}' && depth > 0 {
				depth--
				break
			}
			if depth > 0 && at(pm) == ',' {
				break
			}
			next := make([]char, 0, len(pattern))
			next = append(next, prefix...)
			next = append(next, pattern[pl:pm]...)
			next = append(next, pattern[pe+1:]...)
			if err := g.globexp1(next); err != nil && err != ErrNoMatch {
				return err
			}
			pl = pm + 1
		}
	}
	return nil
}

// glob0 compiles one brace-free pattern and expands it.
func (g *globber) glob0(pattern []char) error {
	old := len(g.res.Paths)
	buf := make([]char, 0, len(pattern))
	// at returns 0 (EOS) past the end, where the C code would stop.
	at := func(i int) char {
		if i < len(pattern) {
			return pattern[i]
		}
		return 0
	}
	for q := 0; q < len(pattern); {
		c := pattern[q]
		q++
		switch c {
		case '[':
			negate := at(q) == '!'
			if negate {
				q++
			}
			if q >= len(pattern) || indexChar(pattern[q+1:], ']') < 0 {
				buf = append(buf, '[')
				if negate {
					q--
				}
				break
			}
			buf = append(buf, mSet)
			if negate {
				buf = append(buf, mNot)
			}
			c = at(q)
			q++
			for q <= len(pattern) {
				if c == '[' && at(q) == ':' {
					invalid := false
					for c == '[' && at(q) == ':' {
						n, idx, status := charClass(pattern[q:])
						if status != 0 {
							invalid = status < 0
							break
						}
						buf = append(buf, mClass, char(idx))
						q += n
						c = at(q)
						q++
					}
					if invalid && !g.opt.NoCheck {
						return ErrNoMatch
					}
					if c == ']' {
						break
					}
				}
				buf = append(buf, c&mASCII)
				if at(q) == '-' && q+1 < len(pattern) && at(q+1) != ']' {
					buf = append(buf, mRng, at(q+1)&mASCII)
					q += 2
				}
				c = at(q)
				q++
				if c == ']' {
					break
				}
			}
			buf = append(buf, mEnd)
		case '?':
			buf = append(buf, mOne)
		case '*':
			if len(buf) == 0 || buf[len(buf)-1] != mAll {
				buf = append(buf, mAll)
			}
		default:
			buf = append(buf, c&mASCII)
		}
	}
	if err := g.glob2("", buf); err != nil {
		return err
	}
	if len(g.res.Paths) == old {
		if g.opt.NoCheck {
			literal := make([]byte, len(pattern))
			for i, c := range pattern {
				literal[i] = byte(c & mASCII)
			}
			return g.extend(Match{Path: string(literal)})
		}
		return ErrNoMatch
	}
	if !g.opt.NoSort {
		added := g.res.Paths[old:]
		sort.SliceStable(added, func(i, j int) bool { return added[i].Path < added[j].Path })
	}
	return nil
}

// charClass parses "[:name:]" starting at the ':' after '['. It returns the
// consumed length after that '[', the class index and 0; 1 when this is not a
// class expression; -1 for an unknown class name.
func charClass(p []char) (int, int, int) {
	body := p[1:]
	colon := indexChar(body, ':')
	if colon < 0 || colon+1 >= len(body) || body[colon+1] != ']' {
		return 0, 0, 1
	}
	name := make([]byte, colon)
	for i, c := range body[:colon] {
		if c > 0xff {
			return 0, 0, -1
		}
		name[i] = byte(c)
	}
	for idx, class := range classes {
		if class.name == string(name) {
			return colon + 3, idx, 0
		}
	}
	return 0, 0, -1
}

func (g *globber) glob2(path string, pattern []char) error {
	for {
		if err := g.ctx.Err(); err != nil {
			return err
		}
		if len(pattern) == 0 {
			return g.found(path)
		}
		end, meta := 0, false
		for end < len(pattern) && pattern[end] != '/' {
			meta = meta || ismeta(pattern[end])
			end++
		}
		if meta {
			return g.glob3(path, pattern[:end], pattern[end:])
		}
		var b strings.Builder
		b.WriteString(path)
		for _, c := range pattern[:end] {
			b.WriteByte(byte(c & mASCII))
		}
		pattern = pattern[end:]
		for len(pattern) > 0 && pattern[0] == '/' {
			b.WriteByte('/')
			pattern = pattern[1:]
		}
		path = b.String()
	}
}

func (g *globber) found(path string) error {
	if g.opt.Limit {
		if g.stats >= LimitStat {
			return ErrTooMany
		}
		g.stats++
	}
	info, err := g.fs.Lstat(g.ctx, path)
	if err != nil {
		return g.fsError(err)
	}
	m := Match{Path: path, Info: info}
	if g.opt.Mark && !strings.HasSuffix(path, "/") {
		dir := info.IsDir()
		if !dir && info.Mode()&fs.ModeSymlink != 0 {
			target, err := g.fs.Stat(g.ctx, path)
			if err == nil {
				dir = target.IsDir()
			} else if err := g.fsError(err); err != nil {
				return err
			}
		}
		if dir {
			m.Path += "/"
			m.Marked = true
		}
	}
	g.res.Matches++
	return g.extend(m)
}

func (g *globber) extend(m Match) error {
	if g.opt.Limit {
		pointers := 8 * (2 + len(g.res.Paths))
		if pointers+g.heap > LimitMalloc {
			return ErrTooMany
		}
		g.heap += len(m.Path) + 1
		if pointers+g.heap > LimitMalloc {
			return ErrTooMany
		}
	}
	g.res.Paths = append(g.res.Paths, m)
	return nil
}

// fsError keeps fatal and cancellation errors and maps every other FS error to
// "no match", as glob(3) does without an error callback.
func (g *globber) fsError(err error) error {
	if ctxErr := g.ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	var fatal fatalError
	if errors.As(err, &fatal) {
		return fatal
	}
	return nil
}

type stopError struct{ err error }

func (e stopError) Error() string { return e.err.Error() }

func (g *globber) glob3(path string, segment, rest []char) error {
	dir := path
	if dir == "" {
		dir = "."
	}
	var inner error
	visit := func(name string) error {
		if g.opt.Limit {
			if g.readdirs >= LimitReaddir {
				inner = ErrTooMany
				return stopError{ErrTooMany}
			}
			g.readdirs++
		}
		if name == "" || strings.ContainsAny(name, "/\x00") {
			return nil
		}
		if name[0] == '.' && segment[0] != '.' {
			return nil
		}
		if !match([]byte(name), segment) {
			return nil
		}
		if err := g.glob2(path+name, rest); err != nil {
			inner = err
			return stopError{err}
		}
		return nil
	}
	if segment[0] == '.' {
		for _, name := range []string{".", ".."} {
			if err := visit(name); err != nil {
				return inner
			}
		}
	}
	err := g.fs.ReadDir(g.ctx, dir, visit)
	if inner != nil {
		return inner
	}
	if err != nil {
		return g.fsError(err)
	}
	return nil
}

// match is OpenBSD glob's iterative matcher with single-star backtracking.
func match(name []byte, pat []char) bool {
	n, p := 0, 0
	nextN, nextP := -1, -1
	for p < len(pat) {
		c := pat[p]
		p++
		ok := true
		switch c {
		case mAll:
			for p < len(pat) && pat[p] == mAll {
				p++
			}
			if p == len(pat) {
				return true
			}
			if n >= len(name) {
				return false
			}
			nextN, nextP = n+1, p-1
		case mOne:
			if n >= len(name) {
				ok = false
				break
			}
			n++
		case mSet:
			if n >= len(name) {
				ok = false
				break
			}
			k := char(name[n])
			n++
			negate := p < len(pat) && pat[p] == mNot
			if negate {
				p++
			}
			found := false
			for p < len(pat) {
				c = pat[p]
				p++
				if c == mEnd {
					break
				}
				if c == mClass {
					if p < len(pat) && int(pat[p]) < len(classes) && classes[pat[p]].is(byte(k)) {
						found = true
					}
					p++
				}
				if p+1 < len(pat) && pat[p] == mRng {
					if c <= k && k <= pat[p+1] {
						found = true
					}
					p += 2
				} else if c == k {
					found = true
				}
			}
			ok = found != negate
		default:
			if n >= len(name) || char(name[n]) != c {
				ok = false
				break
			}
			n++
		}
		if !ok {
			if nextN < 0 {
				return false
			}
			p, n = nextP, nextN
			continue
		}
	}
	return n == len(name)
}
