// Package sftpcli implements explicit SFTP commands without evaluating paths in
// local or remote shells. Unsupported command flags fail before file operations.
package sftpcli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/pkg/sftp"
	"pi-sh/internal/pathglob"
	"pi-sh/internal/progress"
	"pi-sh/internal/sftpclient"
)

type Console struct {
	Client         *sftp.Client
	Input          io.Reader
	Output, Errors io.Writer
	Batch          bool
	Flush          bool
	Resume         bool
	Preserve       bool
	Recursive      bool
	Program        string
	// Progress is optional; pi-cp supplies scp's per-file terminal meter.
	Progress            *progress.Display
	RemoteDir, LocalDir string
	// RemoteUmask comes from sftpclient's private-creation setup. Nil is for
	// embedding with an ordinary server and uses conservative chmod intersection.
	RemoteUmask *os.FileMode
	// abort is OpenSSH's err_abort for the current batch line: multi-path rm
	// and chmod stop at their first failure.
	abort            bool
	initialRemoteDir string
}

// Split tokenizes a command line like OpenSSH sftp's makeargv. Quotes may join
// parts of one word; '#' outside quotes ends the line. Inside quotes, and when
// backslash-escaped outside them, the glob characters '*', '?' and '[' stay
// escaped with a backslash, as does an escaped backslash: commands that glob
// see them literally and the others remove that escaping (pathglob.Unescape).
// Inside quotes a backslash only escapes the enclosing quote character; other
// backslashes are kept for glob(3) to interpret, as in OpenSSH. A trailing
// backslash, an unterminated quote or a NUL is rejected.
func Split(line string) ([]string, error) {
	const (
		start = iota
		single
		double
		unquoted
	)
	var words []string
	var word strings.Builder
	state := start
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == 0 {
			return nil, errors.New("NUL in command")
		}
		quoted := state == single || state == double
		switch {
		case c == ' ' || (c >= '\t' && c <= '\r'):
			if state == unquoted {
				words = append(words, word.String())
				word.Reset()
				state = start
			} else if quoted {
				word.WriteByte(c)
			}
		case c == '\'' || c == '"':
			q := single
			if c == '"' {
				q = double
			}
			switch state {
			case start, unquoted:
				state = q
			case q:
				state = unquoted
			default:
				word.WriteByte(c)
			}
		case c == '\\':
			if i+1 >= len(line) {
				return nil, errors.New("unterminated quote or backslash")
			}
			i++
			next := line[i]
			if quoted {
				quote := byte('\'')
				if state == double {
					quote = '"'
				}
				switch {
				case next == quote:
				case strings.IndexByte("?[*", next) >= 0:
					// A quoted, escaped glob character is a literal backslash
					// followed by that literal character.
					word.WriteString(`\\\`)
				default:
					word.WriteByte('\\')
				}
				word.WriteByte(next)
				continue
			}
			state = unquoted
			if strings.IndexByte(`?[*\`, next) >= 0 {
				word.WriteByte('\\')
			}
			word.WriteByte(next)
		case c == '#' && !quoted:
			i = len(line)
		default:
			if state == start {
				state = unquoted
			}
			if quoted && strings.IndexByte("?[*", c) >= 0 {
				word.WriteByte('\\')
			}
			word.WriteByte(c)
		}
	}
	if state == single || state == double {
		return nil, errors.New("unterminated quote or backslash")
	}
	if state == unquoted {
		words = append(words, word.String())
	}
	return words, nil
}

func (c *Console) Run(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if c.Output == nil {
		c.Output = io.Discard
	}
	if c.Errors == nil {
		c.Errors = io.Discard
	}
	if c.RemoteDir == "" {
		wd, err := c.Client.Getwd()
		if err != nil {
			return sftpclient.SafeError(err)
		}
		c.RemoteDir = wd
	}
	if c.LocalDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return errors.New("local working directory unavailable")
		}
		c.LocalDir = wd
	}
	type lineResult struct {
		line string
		err  error
	}
	lines := make(chan lineResult, 1)
	reading, stop := context.WithCancel(ctx)
	defer stop()
	go func() {
		scanner := bufio.NewScanner(c.Input)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		for scanner.Scan() {
			select {
			case lines <- lineResult{line: scanner.Text()}:
			case <-reading.Done():
				return
			}
		}
		var err error
		if scanner.Err() != nil {
			err = errors.New("command input failed or exceeded line limit")
		}
		select {
		case lines <- lineResult{err: err}:
		case <-reading.Done():
		}
		close(lines)
	}()
	for {
		if !c.Batch {
			fmt.Fprint(c.Output, "pi-sftp> ")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case r, ok := <-lines:
			if !ok {
				return ctx.Err()
			}
			if r.err != nil {
				return r.err
			}
			line := strings.TrimSpace(r.line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			ignore, quiet := false, false
			if c.Batch {
				for len(line) > 0 && (line[0] == '-' || line[0] == '@') {
					if line[0] == '-' {
						ignore = true
					} else {
						quiet = true
					}
					line = line[1:]
				}
			}
			if c.Batch && !quiet {
				fmt.Fprintln(c.Output, "pi-sftp> "+line)
			}
			args, err := Split(line)
			done := false
			c.abort = c.Batch && !ignore
			if err == nil {
				done, err = c.Execute(ctx, args)
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				fmt.Fprintln(c.Errors, "pi-sftp:", err)
				if c.Batch && !ignore {
					return errors.New("batch command failed")
				}
			}
			if done {
				return nil
			}
		}
	}
}

func display(s string) string {
	if strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return strconv.Quote(s)
	}
	return s
}
func (c *Console) remote(s string) string {
	if path.IsAbs(s) {
		return s
	}
	if c.RemoteDir == "" {
		return s
	}
	return strings.TrimSuffix(c.RemoteDir, "/") + "/" + s
}
func (c *Console) local(s string) string {
	if filepath.IsAbs(s) {
		return s
	}
	if c.LocalDir == "" {
		return s
	}
	return c.LocalDir + string(os.PathSeparator) + s
}

func localBase(remote string) (string, error) {
	base := path.Base(remote)
	if base == "." || base == ".." || base == "/" || filepath.Base(base) != base || filepath.IsAbs(base) || filepath.VolumeName(base) != "" {
		return "", errors.New("remote basename cannot be used as a local filename; specify a safe destination")
	}
	return base, nil
}

func (c *Console) Execute(ctx context.Context, args []string) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if len(args) == 0 {
		return false, nil
	}
	if c.initialRemoteDir == "" && c.RemoteDir != "" {
		c.initialRemoteDir = c.RemoteDir
	}
	name, a := strings.ToLower(args[0]), args[1:]
	if name == "mget" || name == "mput" {
		name = strings.TrimPrefix(name, "m")
	}
	if name == "chdir" {
		name = "cd"
	}
	if name == "lchdir" {
		name = "lcd"
	}
	if name == "dir" {
		name = "ls"
	}
	symbolicLink := false
	unsortedListing := false
	options := transferOptions{flush: c.Flush, resume: c.Resume, preserve: c.Preserve, recursive: c.Recursive}
	if name == "reget" || name == "reput" {
		options.resume = true
		name = strings.TrimPrefix(name, "re")
	}
	if name == "get" || name == "put" {
		var err error
		a, options, err = transferFlags(a, options)
		if err != nil {
			return false, err
		}
	} else if name == "ln" {
		for len(a) > 0 && strings.HasPrefix(a[0], "-") {
			if a[0] == "--" {
				a = a[1:]
				break
			}
			if strings.Trim(a[0][1:], "s") != "" || a[0] == "-" {
				return false, errors.New("unsupported ln option")
			}
			symbolicLink = true
			a = a[1:]
		}
	} else if name == "ls" {
		var err error
		a, unsortedListing, err = listingFlags(a)
		if err != nil {
			return false, err
		}
	} else if len(a) > 0 && a[0] == "--" {
		a = a[1:]
	} else if len(a) > 0 && strings.HasPrefix(a[0], "-") {
		return false, errors.New("command flags are not implemented")
	}
	need := func(n int) bool { return len(a) == n }
	bad := errors.New("wrong number of command arguments")
	var err error
	switch name {
	case "quit", "exit", "bye":
		if !need(0) {
			return false, bad
		}
		return true, nil
	case "help", "?":
		if !need(0) {
			return false, bad
		}
		_, err = fmt.Fprintln(c.Output, "pwd cd ls get put mget mput reget reput mkdir rmdir rm rename chmod ln symlink readlink version lpwd lcd quit exit bye help")
		if err == nil {
			_, err = fmt.Fprintln(c.Output, "get/mget [-afpRr] remote [local]; put/mput [-afpRr] local [remote]; reget/reput resume; -- before a leading-dash source")
			if err == nil {
				_, err = fmt.Fprintln(c.Output, "ls [-f] [path]; -f lists in server order with bounded memory")
			}
			if err == nil {
				_, err = fmt.Fprintln(c.Output, "get, ls, rm and chmod expand remote globs; put expands local globs; quote or backslash-escape * ? [ to match them literally")
			}
		}
	case "version":
		if !need(0) {
			return false, bad
		}
		_, err = fmt.Fprintln(c.Output, "SFTP protocol version 3")
	case "pwd":
		if !need(0) {
			return false, bad
		}
		_, err = fmt.Fprintln(c.Output, display(c.RemoteDir))
	case "lpwd":
		if !need(0) {
			return false, bad
		}
		_, err = fmt.Fprintln(c.Output, display(c.LocalDir))
	case "lcd":
		if len(a) > 1 {
			return false, bad
		}
		var target string
		if len(a) == 0 || a[0] == "" {
			target, err = os.UserHomeDir()
			if err != nil {
				break
			}
		} else {
			target = c.local(pathglob.Unescape(a[0]))
		}
		var info os.FileInfo
		info, err = os.Stat(target)
		if err == nil && !info.IsDir() {
			err = errors.New("not a directory")
		}
		if err == nil {
			c.LocalDir = target
		}
	case "cd":
		if len(a) > 1 {
			return false, bad
		}
		var target string
		source := c.initialRemoteDir
		if len(a) == 1 && a[0] != "" {
			source = c.remote(pathglob.Unescape(a[0]))
		}
		if source == "" {
			source, err = c.Client.Getwd()
			if err != nil {
				break
			}
			c.initialRemoteDir = source
		}
		target, err = c.Client.RealPath(source)
		if err == nil {
			var info os.FileInfo
			info, err = c.Client.Stat(target)
			if err == nil && !info.IsDir() {
				err = errors.New("not a directory")
			}
			if err == nil {
				c.RemoteDir = target
			}
		}
	case "ls":
		if len(a) > 1 {
			return false, bad
		}
		if len(a) == 0 {
			err = c.listDirectory(ctx, c.RemoteDir, unsortedListing)
		} else {
			err = c.globList(ctx, a[0], unsortedListing)
		}
	case "get", "put":
		if len(a) < 1 || len(a) > 2 {
			return false, bad
		}
		// The source is a glob pattern; the destination is never globbed.
		var dst *string
		if len(a) == 2 {
			literal := pathglob.Unescape(a[1])
			dst = &literal
		}
		if name == "get" {
			err = c.globGet(ctx, a[0], dst, options)
		} else {
			err = c.globPut(ctx, a[0], dst, options)
		}
	case "rm":
		if !need(1) {
			return false, bad
		}
		err = c.eachRemote(ctx, a[0], c.removeFile)
	case "mkdir", "rmdir", "readlink":
		if !need(1) {
			return false, bad
		}
		target := c.remote(pathglob.Unescape(a[0]))
		switch name {
		case "mkdir":
			err = c.Client.Mkdir(target)
			if err == nil && c.RemoteUmask != nil {
				err = c.Client.Chmod(target, 0777&^*c.RemoteUmask)
			}
		case "rmdir":
			err = c.Client.RemoveDirectory(target)
		case "readlink":
			var value string
			value, err = c.Client.ReadLink(target)
			if err == nil {
				_, err = fmt.Fprintln(c.Output, display(value))
			}
		}
	case "rename":
		if !need(2) {
			return false, bad
		}
		err = c.Client.Rename(c.remote(pathglob.Unescape(a[0])), c.remote(pathglob.Unescape(a[1])))
	case "ln":
		if !need(2) {
			return false, bad
		}
		source := pathglob.Unescape(a[0])
		target := c.remote(pathglob.Unescape(a[1]))
		if symbolicLink {
			err = c.Client.Symlink(source, target)
		} else {
			err = c.Client.Link(c.remote(source), target)
		}
	case "symlink":
		if !need(2) {
			return false, bad
		}
		err = c.Client.Symlink(pathglob.Unescape(a[0]), c.remote(pathglob.Unescape(a[1])))
	case "chmod":
		if !need(2) {
			return false, bad
		}
		var mode uint64
		mode, err = strconv.ParseUint(a[0], 8, 32)
		if err != nil || mode > 0777 {
			return false, errors.New("chmod requires an octal mode between 000 and 777")
		}
		err = c.eachRemote(ctx, a[1], func(name string) error { return c.Client.Chmod(name, os.FileMode(mode)) })
	default:
		return false, errors.New("unsupported SFTP command")
	}
	return false, safeError(err)
}

func (c *Console) get(remote, local string, options transferOptions) (result error) {
	flush, resume := options.flush, options.resume
	source, err := c.Client.Open(remote)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, source.Close()) }()
	info, err := source.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("get requires a regular file")
	}
	if info, err := os.Stat(local); err == nil && info.IsDir() && options.named {
		return errors.New("local destination is a directory")
	} else if err == nil && info.IsDir() {
		base, err := localBase(remote)
		if err != nil {
			return err
		}
		local = local + string(os.PathSeparator) + base
	} else if err == nil && !info.Mode().IsRegular() {
		return errors.New("get requires a regular local destination")
	}
	flags := os.O_WRONLY | os.O_CREATE
	if !resume {
		flags |= os.O_TRUNC
	}
	target, err := os.OpenFile(local, flags, info.Mode().Perm())
	if err != nil {
		return err
	}
	var offset int64
	if resume {
		localInfo, statErr := target.Stat()
		if statErr != nil {
			return errors.Join(statErr, target.Close())
		}
		if !localInfo.Mode().IsRegular() || localInfo.Size() < 0 || localInfo.Size() > info.Size() {
			return errors.Join(shown(os.ErrInvalid, "cannot resume download: invalid destination size or type"), target.Close())
		}
		offset = localInfo.Size()
		if _, err = target.Seek(offset, io.SeekStart); err == nil {
			_, err = source.Seek(offset, io.SeekStart)
		}
		if err != nil {
			return errors.Join(err, target.Close())
		}
	}
	n, err := c.copyData(target, source, path.Base(remote), info.Size(), offset)
	if resume && err != nil {
		err = errors.Join(err, target.Truncate(offset+n))
	}
	if options.preserve && err == nil {
		c.preserveLocal(target, local, info)
	}
	return finishTransfer(target, err, flush && err == nil && (!resume || n > 0), func(err error) error { c.syncWarning("local", err); return nil })
}
func (c *Console) put(local, remote string, options transferOptions) (result error) {
	flush, resume := options.flush, options.resume
	source, err := os.Open(local)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, source.Close()) }()
	info, err := source.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("put requires a regular file")
	}
	var metadata preservedMetadata
	if options.preserve {
		var ok bool
		metadata.atime, ok = localAccessTime(info)
		if !ok {
			return errors.New("source access time unavailable for preservation")
		}
		metadata.mtime, metadata.mode = info.ModTime(), info.Mode().Perm()
	}
	if info, err := c.Client.Stat(remote); err == nil && info.IsDir() && options.named {
		return errors.New("remote destination is a directory")
	} else if err == nil && info.IsDir() {
		remote = strings.TrimSuffix(remote, "/") + "/" + filepath.Base(local)
	}
	destination, err := c.Client.Stat(remote)
	create := errors.Is(err, os.ErrNotExist)
	if err != nil && !create {
		return err
	}
	if !create && !destination.Mode().IsRegular() {
		return errors.New("put requires a regular remote destination")
	}
	flags := os.O_WRONLY | os.O_TRUNC
	var offset int64
	if resume {
		// OpenSSH upload resume requires an existing, strictly shorter target.
		// Its prefix is trusted, not compared with the source.
		if create || destination.Size() < 0 || destination.Size() >= info.Size() {
			return shown(os.ErrInvalid, "cannot resume upload: destination must exist and be smaller than source")
		}
		offset = destination.Size()
		if _, err := source.Seek(offset, io.SeekStart); err != nil {
			return err
		}
		flags = os.O_WRONLY | os.O_APPEND
	}
	if create {
		// Never fall back from exclusive creation to truncation: the destination
		// might have appeared since Stat, and its permissions are then unknown.
		flags |= os.O_CREATE | os.O_EXCL
	}
	target, err := c.Client.OpenFile(remote, flags)
	if err != nil {
		return err
	}
	if create {
		// The suite's server starts with umask 077, so the file is private from
		// creation. Apply source permissions filtered by its original mask before
		// sending contents. An ordinary embedded server uses conservative defaults.
		created, modeErr := target.Stat()
		if modeErr == nil {
			mode := created.Mode().Perm() & info.Mode().Perm()
			if c.RemoteUmask != nil {
				if created.Mode().Perm()&0077 != 0 {
					return errors.Join(errors.New("remote server did not honor private creation"), target.Close())
				}
				mode = info.Mode().Perm() &^ *c.RemoteUmask
			}
			modeErr = target.Chmod(mode)
		}
		if modeErr != nil {
			return errors.Join(modeErr, target.Close())
		}
	}
	if resume {
		if _, err := target.Seek(offset, io.SeekStart); err != nil {
			return errors.Join(err, target.Close())
		}
	}
	n, err := c.copyData(target, source, filepath.Base(local), info.Size(), offset)
	if resume && err != nil {
		err = errors.Join(err, target.Truncate(offset+n))
	}
	if options.preserve {
		err = errors.Join(err, c.preserveRemote(target, metadata))
	}
	version, supported := c.Client.HasExtension("fsync@openssh.com")
	return finishTransfer(target, err, flush && supported && version == "1", func(err error) error {
		// OpenSSH reports a server fsync status failure without changing the
		// transfer status. Lost/malformed protocol replies must remain fatal.
		var status *sftp.StatusError
		if errors.As(err, &status) || errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrNotExist) || errors.Is(err, io.EOF) {
			// pkg/sftp normalizes a protocol EOF status and stream EOF to the
			// same value. The mandatory subsequent Close reply distinguishes a
			// live status failure from a broken connection; lost EOF stays fatal.
			c.syncWarning("remote", err)
			return nil
		}
		return err
	})
}
