package sftpcli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/term"
	"pi-sh/internal/pathglob"
)

type listOptions struct {
	one, long, numeric, human, all, reverse bool
	order                                   byte // n=name, t=time, s=size, zero=server order
}

func parseListing(args []string) ([]string, listOptions, error) {
	opt := listOptions{order: 'n'}
	for len(args) > 0 {
		word := args[0]
		if word == "--" {
			return args[1:], opt, nil
		}
		if len(word) < 2 || word[0] != '-' {
			break
		}
		for _, flag := range word[1:] {
			switch flag {
			case '1':
				opt.one = true
				opt.long = false
				opt.numeric = false
				opt.human = false
			case 'a':
				opt.all = true
			case 'f':
				opt.order = 0
			case 'h':
				opt.human = true
			case 'l':
				opt.long = true
				opt.one = false
			case 'n':
				opt.long = true
				opt.numeric = true
				opt.one = false
			case 'r':
				opt.reverse = true
			case 'S':
				opt.order = 's'
			case 't':
				opt.order = 't'
			default:
				return nil, opt, shown(os.ErrInvalid, "ls: Invalid flag -%c", flag)
			}
		}
		args = args[1:]
	}
	return args, opt, nil
}

// Kept for callers testing the legacy -f boundary.
func listingFlags(args []string) ([]string, bool, error) {
	rest, opt, err := parseListing(args)
	return rest, opt.order == 0, err
}

type listingEntry struct {
	name, raw string
	info      os.FileInfo
}

func (c *Console) consoleList(ctx context.Context, args []string, opt listOptions) error {
	if len(args) > 1 {
		return shown(os.ErrInvalid, "Too many arguments")
	}
	target := c.RemoteDir
	strip := c.RemoteDir
	var matches pathglob.Result
	if len(args) == 1 {
		if strings.HasPrefix(args[0], "/") {
			strip = ""
		}
		var err error
		pattern := c.remotePattern(args[0])
		matches, err = c.remoteGlob(ctx, pattern, pathglob.Options{Mark: true, NoCheck: true, Brace: true, NoSort: true})
		if err != nil {
			return err
		}
		if matches.Matches == 0 {
			return shown(os.ErrNotExist, "Can't ls: \"%s\" not found", display(pathglob.Unescape(pattern)))
		}
		if matches.Matches == 1 && matches.Paths[0].Info != nil && (matches.Paths[0].Info.IsDir() || matches.Paths[0].Marked) {
			target = unmarked(matches.Paths[0])
		} else {
			return c.printGlobListing(ctx, matches, strip, opt)
		}
	}
	var entries []listingEntry
	prefix := stripPath(strings.TrimSuffix(target, "/"), strip)
	nameFor := func(name string) string {
		if prefix == "" {
			return name
		}
		return treeAppend(prefix, name, false)
	}
	// -1f retains incremental directory consumption; columns/ordered/long views
	// require retained names or attributes to compute widths and sort.
	if opt.one && opt.order == 0 && !opt.all && !opt.long {
		return c.Client.ReadDirEach(ctx, target, func(info os.FileInfo) error {
			if strings.HasPrefix(info.Name(), ".") {
				return nil
			}
			_, err := fmt.Fprintln(c.Output, display(nameFor(info.Name())))
			return err
		})
	}
	if opt.all {
		for _, name := range []string{".", ".."} {
			info, err := c.Client.Lstat(treeAppend(target, name, false))
			if err != nil {
				return err
			}
			entries = append(entries, listingEntry{nameFor(name), nameFor(name), info})
		}
	}
	err := c.Client.ReadDirEach(ctx, target, func(info os.FileInfo) error {
		if !opt.all && strings.HasPrefix(info.Name(), ".") {
			return nil
		}
		entries = append(entries, listingEntry{nameFor(info.Name()), nameFor(info.Name()), info})
		return nil
	})
	if err != nil {
		return err
	}
	return c.printListing(ctx, entries, opt, false)
}

func (c *Console) printGlobListing(ctx context.Context, matches pathglob.Result, strip string, opt listOptions) error {
	entries := make([]listingEntry, 0, len(matches.Paths))
	var missing error
	for _, match := range matches.Paths {
		if match.Info == nil {
			missing = shown(os.ErrNotExist, "Can't ls: \"%s\" not found", display(match.Path))
			continue
		}
		entries = append(entries, listingEntry{stripPath(match.Path, strip), match.Path, match.Info})
	}
	return errors.Join(c.printListing(ctx, entries, opt, true), missing)
}

func (c *Console) printListing(ctx context.Context, entries []listingEntry, opt listOptions, glob bool) error {
	if opt.order != 0 {
		sort.SliceStable(entries, func(i, j int) bool {
			a, b := entries[i], entries[j]
			result := strings.Compare(a.name, b.name)
			if opt.order == 's' {
				result = 0
				if a.info.Size() > b.info.Size() {
					result = -1
				} else if a.info.Size() < b.info.Size() {
					result = 1
				}
			}
			if opt.order == 't' {
				result = 0
				if a.info.ModTime().After(b.info.ModTime()) {
					result = -1
				} else if a.info.ModTime().Before(b.info.ModTime()) {
					result = 1
				}
			}
			if opt.reverse {
				result = -result
			}
			return result < 0
		})
	}
	if opt.long {
		return c.printLongListing(ctx, entries, opt)
	}
	width := 80
	if columns, _, err := term.GetSize(int(os.Stdin.Fd())); err == nil && columns > 0 {
		width = columns
	}
	longest := 0
	for _, e := range entries {
		name := e.name
		if glob {
			name = e.raw
		}
		longest = max(longest, len(name))
	}
	columns := 1
	spacing := 0
	if !opt.one {
		columns = max(1, width/(longest+2))
		spacing = min(width, width/columns)
	}
	for i, e := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := display(e.name)
		pad := max(0, spacing-len(name))
		if _, err := fmt.Fprint(c.Output, name+strings.Repeat(" ", pad)); err != nil {
			return err
		}
		if (i+1)%columns == 0 || i == len(entries)-1 {
			if _, err := fmt.Fprintln(c.Output); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Console) printLongListing(ctx context.Context, entries []listingEntry, opt listOptions) error {
	users, groups := map[uint32]string{}, map[uint32]string{}
	if version, ok := c.Client.HasExtension("users-groups-by-id@openssh.com"); ok && version == "1" {
		for _, e := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			if stat, ok := e.info.Sys().(*sftp.FileStat); ok {
				_, u := users[stat.UID]
				_, g := groups[stat.GID]
				if !u || !g {
					names, gnames, err := c.Client.UserGroupNames([]uint32{stat.UID}, []uint32{stat.GID})
					if err != nil {
						return err
					}
					users[stat.UID] = names[0]
					groups[stat.GID] = gnames[0]
				}
			}
		}
	}
	_, canNames := c.Client.HasExtension("users-groups-by-id@openssh.com")
	now := time.Now()
	for _, e := range entries {
		if !canNames && !opt.numeric && !opt.human {
			if info, ok := e.info.(interface{ LongName() string }); ok && info.LongName() != "" {
				if _, err := fmt.Fprintln(c.Output, display(info.LongName())); err != nil {
					return err
				}
				continue
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		var uid, gid uint32
		if stat, ok := e.info.Sys().(*sftp.FileStat); ok {
			uid, gid = stat.UID, stat.GID
		}
		user, group := users[uid], groups[gid]
		if user == "" {
			user = strconv.FormatUint(uint64(uid), 10)
		}
		if group == "" {
			group = strconv.FormatUint(uint64(gid), 10)
		}
		user, group = display(user), display(group)
		stamp := e.info.ModTime().In(time.Local)
		date := stamp.Format("Jan _2  2006")
		if !stamp.After(now) && stamp.After(now.Add(-365*24*time.Hour/2)) {
			date = stamp.Format("Jan _2 15:04")
		}
		size := strconv.FormatInt(e.info.Size(), 10)
		if opt.human {
			size = scaledSize(uint64(e.info.Size()))
			if e.info.Size() < 1024 {
				size += "B"
			}
		}
		line := fmt.Sprintf("%s    ? %-*s %-*s %8s %s %s\n", unixMode(e.info.Mode()), max(8, len(user)), user, max(8, len(group)), group, size, date, display(e.name))
		if _, err := fmt.Fprint(c.Output, line); err != nil {
			return err
		}
	}
	return nil
}

func unixMode(mode os.FileMode) string {
	chars := []byte("----------")
	switch {
	case mode.IsDir():
		chars[0] = 'd'
	case mode&os.ModeSymlink != 0:
		chars[0] = 'l'
	case mode&os.ModeNamedPipe != 0:
		chars[0] = 'p'
	case mode&os.ModeSocket != 0:
		chars[0] = 's'
	case mode&os.ModeCharDevice != 0:
		chars[0] = 'c'
	case mode&os.ModeDevice != 0:
		chars[0] = 'b'
	}
	for i, ch := range []byte("rwxrwxrwx") {
		if mode.Perm()&(1<<uint(8-i)) != 0 {
			chars[i+1] = ch
		}
	}
	for _, v := range []struct {
		mask    os.FileMode
		index   int
		on, off byte
	}{{os.ModeSetuid, 3, 's', 'S'}, {os.ModeSetgid, 6, 's', 'S'}, {os.ModeSticky, 9, 't', 'T'}} {
		if mode&v.mask != 0 {
			if chars[v.index] == 'x' {
				chars[v.index] = v.on
			} else {
				chars[v.index] = v.off
			}
		}
	}
	return string(chars)
}
