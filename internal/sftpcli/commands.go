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
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/pkg/sftp"
	"pi-sh/internal/sftpclient"
)

type Console struct {
	Client              *sftp.Client
	Input               io.Reader
	Output, Errors      io.Writer
	Batch               bool
	Flush               bool
	Resume              bool
	Preserve            bool
	Program             string
	RemoteDir, LocalDir string
	// RemoteUmask comes from sftpclient's private-creation setup. Nil is for
	// embedding with an ordinary server and uses conservative chmod intersection.
	RemoteUmask *os.FileMode
}

func Split(line string) ([]string, error) {
	var words []string
	var word strings.Builder
	var quote byte
	started, escaped := false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == 0 {
			return nil, errors.New("NUL in command")
		}
		if escaped {
			word.WriteByte(c)
			started = true
			escaped = false
			continue
		}
		if c == '\\' && quote != '\'' {
			escaped = true
			started = true
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
			} else {
				word.WriteByte(c)
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			started = true
			continue
		}
		if c == '#' && !started {
			break
		}
		if c == ' ' || c == '\t' || c == '\r' {
			if started {
				words = append(words, word.String())
				word.Reset()
				started = false
			}
			continue
		}
		word.WriteByte(c)
		started = true
	}
	if quote != 0 || escaped {
		return nil, errors.New("unterminated quote or backslash")
	}
	if started {
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
	name, a := args[0], args[1:]
	unsortedListing := false
	options := transferOptions{flush: c.Flush, resume: c.Resume, preserve: c.Preserve}
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
		_, err = fmt.Fprintln(c.Output, "pwd cd ls get put reget reput mkdir rmdir rm rename chmod symlink readlink lpwd lcd quit exit bye help")
		if err == nil {
			_, err = fmt.Fprintln(c.Output, "get [-afp] remote [local]; put [-afp] local [remote]; reget/reput resume; -- before a leading-dash source")
			if err == nil {
				_, err = fmt.Fprintln(c.Output, "ls [-f] [path]; -f lists in server order with bounded memory")
			}
		}
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
		if !need(1) {
			return false, bad
		}
		target := c.local(a[0])
		var info os.FileInfo
		info, err = os.Stat(target)
		if err == nil && !info.IsDir() {
			err = errors.New("not a directory")
		}
		if err == nil {
			c.LocalDir = target
		}
	case "cd":
		if !need(1) {
			return false, bad
		}
		var target string
		target, err = c.Client.RealPath(c.remote(a[0]))
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
		target := c.RemoteDir
		if len(a) == 1 {
			target = c.remote(a[0])
		}
		var info os.FileInfo
		info, err = c.Client.Stat(target)
		if err == nil && !info.IsDir() {
			_, err = fmt.Fprintln(c.Output, display(path.Base(target)))
		} else if err == nil && unsortedListing {
			err = c.Client.ReadDirEach(ctx, target, func(info os.FileInfo) error {
				_, err := fmt.Fprintln(c.Output, display(info.Name()))
				return err
			})
		} else if err == nil {
			var files []os.FileInfo
			files, err = c.Client.ReadDirContext(ctx, target)
			sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })
			if err == nil {
				for _, f := range files {
					if _, err = fmt.Fprintln(c.Output, display(f.Name())); err != nil {
						break
					}
				}
			}
		}
	case "get", "put":
		if len(a) < 1 || len(a) > 2 {
			return false, bad
		}
		if name == "get" {
			dst, baseErr := localBase(a[0])
			if baseErr != nil && len(a) == 1 {
				return false, baseErr
			}
			if len(a) == 2 {
				dst = a[1]
			}
			err = c.get(c.remote(a[0]), c.local(dst), options)
		} else {
			dst := filepath.Base(a[0])
			if len(a) == 2 {
				dst = a[1]
			}
			err = c.put(c.local(a[0]), c.remote(dst), options)
		}
	case "mkdir", "rmdir", "rm", "readlink":
		if !need(1) {
			return false, bad
		}
		target := c.remote(a[0])
		switch name {
		case "mkdir":
			err = c.Client.Mkdir(target)
			if err == nil && c.RemoteUmask != nil {
				err = c.Client.Chmod(target, 0777&^*c.RemoteUmask)
			}
		case "rmdir":
			err = c.Client.RemoveDirectory(target)
		case "rm":
			err = c.Client.Remove(target)
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
		err = c.Client.Rename(c.remote(a[0]), c.remote(a[1]))
	case "symlink":
		if !need(2) {
			return false, bad
		}
		err = c.Client.Symlink(a[0], c.remote(a[1]))
	case "chmod":
		if !need(2) {
			return false, bad
		}
		var mode uint64
		mode, err = strconv.ParseUint(a[0], 8, 32)
		if err != nil || mode > 0777 {
			return false, errors.New("chmod requires an octal mode between 000 and 777")
		}
		err = c.Client.Chmod(c.remote(a[1]), os.FileMode(mode))
	default:
		return false, errors.New("unsupported SFTP command")
	}
	return false, sftpclient.SafeError(err)
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
	if info, err := os.Stat(local); err == nil && info.IsDir() {
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
			return errors.Join(errors.New("cannot resume download: invalid destination size or type"), target.Close())
		}
		offset = localInfo.Size()
		if _, err = target.Seek(offset, io.SeekStart); err == nil {
			_, err = source.Seek(offset, io.SeekStart)
		}
		if err != nil {
			return errors.Join(err, target.Close())
		}
	}
	n, err := copyFileData(target, source)
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
	if info, err := c.Client.Stat(remote); err == nil && info.IsDir() {
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
			return errors.New("cannot resume upload: destination must exist and be smaller than source")
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
	n, err := copyFileData(target, source)
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
