package sftpcli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"pi-sh/internal/pathglob"
	"pi-sh/internal/sftpclient"
)

// shownError is an error whose text is safe to print: fixed wording plus paths
// passed through display. It still unwraps to the classified underlying error.
type shownError struct {
	text string
	err  error
}

func (e *shownError) Error() string { return e.text }
func (e *shownError) Unwrap() error { return e.err }

func shown(err error, format string, args ...any) error {
	return &shownError{text: fmt.Sprintf(format, args...), err: err}
}

// safeError keeps presentable errors and otherwise applies the backend's
// redacting classification.
func safeError(err error) error {
	var s *shownError
	if errors.As(err, &s) {
		return err
	}
	return sftpclient.SafeError(err)
}

// failures reports every failed glob match except the last, which the caller
// returns, so a multi-match command prints one line per failure like OpenSSH.
type failures struct {
	c    *Console
	last error
}

func (f *failures) add(err error) {
	if err == nil {
		return
	}
	if f.last != nil {
		f.c.report(f.last)
	}
	f.last = err
}

func (c *Console) report(err error) {
	if c.Errors == nil {
		return
	}
	program := c.Program
	if program == "" {
		program = "pi-sftp"
	}
	fmt.Fprintf(c.Errors, "%s: %v\n", program, err)
}

// matchError labels a per-match failure with its path when several paths were
// expanded; a single match keeps the plain classified error.
func matchError(several bool, name string, err error) error {
	if err == nil || !several || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var s *shownError
	if errors.As(err, &s) {
		return err
	}
	safe := sftpclient.SafeError(err)
	return shown(safe, "%s: %v", display(name), safe)
}

// remotePattern makes an sftp argument absolute like OpenSSH's
// make_absolute_pwd_glob: the working directory is escaped, the argument is not.
func (c *Console) remotePattern(arg string) string {
	if path.IsAbs(arg) || c.RemoteDir == "" {
		return arg
	}
	return strings.TrimSuffix(pathglob.Escape(c.RemoteDir), "/") + "/" + arg
}

func (c *Console) remoteGlob(ctx context.Context, pattern string, opt pathglob.Options) (pathglob.Result, error) {
	opt.Limit = true
	return pathglob.Glob(ctx, pathglob.Remote(c.Client), pattern, opt)
}

// localGlob expands a put source with OpenSSH's GLOB_NOCHECK|GLOB_MARK. On
// Windows '\' is a path separator, so the unescaped argument is used literally.
func (c *Console) localGlob(ctx context.Context, arg string) (pathglob.Result, error) {
	if runtime.GOOS == "windows" {
		return pathglob.Result{Paths: []pathglob.Match{{Path: c.local(pathglob.Unescape(arg))}}}, nil
	}
	pattern := arg
	if !filepath.IsAbs(arg) && c.LocalDir != "" {
		pattern = strings.TrimSuffix(pathglob.Escape(c.LocalDir), "/") + "/" + arg
	}
	return pathglob.Glob(ctx, pathglob.Local(), pattern, pathglob.Options{Mark: true, NoCheck: true})
}

func unmarked(m pathglob.Match) string {
	if m.Marked {
		return strings.TrimSuffix(m.Path, "/")
	}
	return m.Path
}

func (c *Console) globGet(ctx context.Context, src string, dst *string, options transferOptions) error {
	pattern := c.remotePattern(src)
	res, err := c.remoteGlob(ctx, pattern, pathglob.Options{Mark: true})
	switch {
	case errors.Is(err, pathglob.ErrTooMany):
		return shown(err, "Too many files match \"%s\"", display(pathglob.Unescape(pattern)))
	case errors.Is(err, pathglob.ErrNoMatch):
		return shown(os.ErrNotExist, "File \"%s\" not found.", display(pathglob.Unescape(pattern)))
	case err != nil:
		return err
	}
	several := res.Matches > 1
	if several && dst != nil {
		if info, err := os.Stat(c.local(*dst)); err != nil || !info.IsDir() {
			return shown(os.ErrInvalid, "Multiple source paths, but destination \"%s\" is not a directory", display(*dst))
		}
	}
	f := failures{c: c}
	for _, m := range res.Paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		remote := unmarked(m)
		if m.Marked {
			f.add(shown(os.ErrInvalid, "download %s: not a regular file", display(m.Path)))
			continue
		}
		var local string
		if dst != nil {
			local = c.local(*dst)
		} else {
			base, err := localBase(remote)
			if err != nil {
				f.add(matchError(several, remote, err))
				continue
			}
			local = c.local(base)
		}
		f.add(matchError(several, remote, c.get(remote, local, options)))
	}
	return f.last
}

func (c *Console) globPut(ctx context.Context, src string, dst *string, options transferOptions) error {
	res, err := c.localGlob(ctx, src)
	if err != nil {
		return err
	}
	several := res.Matches > 1
	if several && dst != nil {
		if info, err := c.Client.Stat(c.remote(*dst)); err != nil || !info.IsDir() {
			return shown(os.ErrInvalid, "Multiple paths match, but destination \"%s\" is not a directory", display(c.remote(*dst)))
		}
	}
	f := failures{c: c}
	for _, m := range res.Paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		local := unmarked(m)
		if m.Marked {
			f.add(shown(os.ErrInvalid, "local \"%s\" is not a regular file", display(m.Path)))
			continue
		}
		remote := filepath.Base(local)
		if dst != nil {
			remote = *dst
		}
		f.add(matchError(several, local, c.put(local, c.remote(remote), options)))
	}
	return f.last
}

// eachRemote applies op to every GLOB_NOCHECK expansion of arg (rm, chmod).
// Unlike OpenSSH, which ignores remote_glob failure and processes a partial
// list, exceeding GLOB_LIMIT fails before any change. A batch without the '-'
// prefix stops at the first failure, as OpenSSH's err_abort does.
func (c *Console) eachRemote(ctx context.Context, arg string, op func(string) error) error {
	pattern := c.remotePattern(arg)
	res, err := c.remoteGlob(ctx, pattern, pathglob.Options{NoCheck: true})
	if errors.Is(err, pathglob.ErrTooMany) {
		return shown(err, "Too many matches for \"%s\"; nothing was changed", display(pathglob.Unescape(pattern)))
	}
	if err != nil {
		return err
	}
	several := len(res.Paths) > 1
	f := failures{c: c}
	for _, m := range res.Paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := matchError(several, m.Path, op(m.Path))
		f.add(err)
		if err != nil && c.abort {
			break
		}
	}
	return f.last
}

// removeFile sends only a file removal, like OpenSSH's do_rm. pkg/sftp's
// Remove would also remove an empty directory, so directories are refused after
// lstat; a directory substituted after that check remains a race.
func (c *Console) removeFile(name string) error {
	if info, err := c.Client.Lstat(name); err == nil && info.IsDir() {
		return shown(os.ErrInvalid, "remote delete %s: is a directory; use rmdir", display(name))
	}
	return c.Client.Remove(name)
}

// stripPath is OpenSSH's path_strip for ls output of relative arguments.
func stripPath(name, prefix string) string {
	if prefix == "" || !strings.HasPrefix(name, prefix) {
		return name
	}
	n := len(prefix)
	if !strings.HasSuffix(prefix, "/") && len(name) > n && name[n] == '/' {
		n++
	}
	return name[n:]
}

func (c *Console) globList(ctx context.Context, arg string, unsorted bool) error {
	pattern := c.remotePattern(arg)
	res, err := c.remoteGlob(ctx, pattern, pathglob.Options{Mark: true, NoCheck: true, Brace: true, NoSort: true})
	if errors.Is(err, pathglob.ErrTooMany) {
		return shown(err, "Can't ls: Too many matches for \"%s\"", display(pathglob.Unescape(pattern)))
	}
	if err != nil {
		return err
	}
	if res.Matches == 0 {
		return shown(os.ErrNotExist, "Can't ls: \"%s\" not found", display(pathglob.Unescape(pattern)))
	}
	if first := res.Paths[0]; res.Matches == 1 && first.Info != nil && (first.Info.IsDir() || first.Marked) {
		return c.listDirectory(ctx, unmarked(first), unsorted)
	}
	strip := ""
	if !path.IsAbs(arg) {
		strip = c.RemoteDir
	}
	if !unsorted {
		sort.SliceStable(res.Paths, func(i, j int) bool { return res.Paths[i].Path < res.Paths[j].Path })
	}
	var missing error
	for _, m := range res.Paths {
		if m.Info == nil {
			// A brace alternative that matched nothing: OpenSSH 10.2 crashes on
			// this mixture; report it instead.
			if missing == nil {
				missing = shown(os.ErrNotExist, "Can't ls: \"%s\" not found", display(m.Path))
			}
			continue
		}
		if _, err := fmt.Fprintln(c.Output, display(stripPath(m.Path, strip))); err != nil {
			return err
		}
	}
	return missing
}

// listDirectory prints a directory's entry basenames: sorted, or with -f in
// server order through the bounded incremental reader.
func (c *Console) listDirectory(ctx context.Context, target string, unsorted bool) error {
	if unsorted {
		return c.Client.ReadDirEach(ctx, target, func(info os.FileInfo) error {
			_, err := fmt.Fprintln(c.Output, display(info.Name()))
			return err
		})
	}
	files, err := c.Client.ReadDirContext(ctx, target)
	if err != nil {
		return err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })
	for _, f := range files {
		if _, err := fmt.Fprintln(c.Output, display(f.Name())); err != nil {
			return err
		}
	}
	return nil
}

// CopyFile copies one literal regular file for pi-cp. Paths are not globbed;
// an existing destination directory receives the source basename.
func (c *Console) CopyFile(upload bool, source, target string) error {
	options := transferOptions{flush: c.Flush, preserve: c.Preserve}
	if upload {
		return sftpclient.SafeError(c.put(source, target, options))
	}
	return sftpclient.SafeError(c.get(source, target, options))
}
