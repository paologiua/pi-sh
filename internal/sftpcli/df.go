package sftpcli

import (
	"context"
	"fmt"
	"github.com/pkg/sftp"
	"io"
	"os"
	"pi-sh/internal/pathglob"
	"strings"
)

func (c *Console) diskFree(ctx context.Context, args []string) error {
	human, inodes := false, false
	for len(args) > 0 && strings.HasPrefix(args[0], "-") && args[0] != "-" {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		for _, flag := range args[0][1:] {
			switch flag {
			case 'h':
				human = true
			case 'i':
				inodes = true
			default:
				return shown(os.ErrInvalid, "Invalid flag -%c", flag)
			}
		}
		args = args[1:]
	}
	if len(args) > 1 {
		return shown(os.ErrInvalid, "Too many arguments")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if version, ok := c.Client.HasExtension("statvfs@openssh.com"); !ok || version != "2" {
		return shown(os.ErrInvalid, "Server does not support statvfs@openssh.com extension")
	}
	target := c.RemoteDir
	if len(args) == 1 {
		target = c.remote(pathglob.Unescape(args[0]))
	}
	info, err := c.Client.StatVFS(target)
	if err != nil {
		return shown(err, "")
	}
	return printDiskFree(c.Output, info, human, inodes)
}

func percentUsed(total, free uint64) string {
	if total == 0 {
		return "ERR"
	}
	return fmt.Sprintf("%3d%%", 100*(total-free)/total)
}

// scaledSize reproduces the compact binary-unit notation used by OpenSSH's df.
func scaledSize(n uint64) string {
	scale := uint64(1)
	unit := byte('B')
	for _, u := range []byte("KMGTPE") {
		if n/scale < 1024 {
			break
		}
		scale *= 1024
		unit = u
	}
	if scale == 1 {
		return fmt.Sprintf("%d", n)
	}
	whole, rest := n/scale, n%scale
	if whole >= 10 {
		if rest >= scale/2 {
			whole++
		}
		return fmt.Sprintf("%d%c", whole, unit)
	}
	tenth := (rest*10 + scale/2) / scale
	if tenth == 10 {
		whole++
		tenth = 0
	}
	return fmt.Sprintf("%d.%d%c", whole, tenth, unit)
}

func printDiskFree(out io.Writer, s *sftp.StatVFS, human, inodes bool) error {
	var err error
	if inodes {
		if _, err = fmt.Fprintln(out, "     Inodes        Used       Avail      (root)    %Capacity"); err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "%11d %11d %11d %11d         %s\n", s.Files, s.Files-s.Ffree, s.Favail, s.Ffree, percentUsed(s.Files, s.Ffree))
	} else if human {
		if _, err = fmt.Fprintln(out, "    Size     Used    Avail   (root)    %Capacity"); err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "%7sB %7sB %7sB %7sB         %s\n", scaledSize(s.Blocks*s.Frsize), scaledSize((s.Blocks-s.Bfree)*s.Frsize), scaledSize(s.Bavail*s.Frsize), scaledSize(s.Bfree*s.Frsize), percentUsed(s.Blocks, s.Bfree))
	} else {
		if _, err = fmt.Fprintln(out, "        Size         Used        Avail       (root)    %Capacity"); err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "%12d %12d %12d %12d         %s\n", s.Blocks*s.Frsize/1024, (s.Blocks-s.Bfree)*s.Frsize/1024, s.Bavail*s.Frsize/1024, s.Bfree*s.Frsize/1024, percentUsed(s.Blocks, s.Bfree))
	}
	return err
}
