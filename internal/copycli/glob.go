package copycli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode"

	"github.com/pkg/sftp"
	"pi-sh/internal/pathglob"
	"pi-sh/internal/sftpcli"
)

// remotePath is scp's prepare_remote_path for SFTP mode: "~" and "~/" name
// the server's starting directory and "~/x" becomes the relative "x", so the
// server's starting directory must be the home directory, as for scp. "~user"
// needs the expand-path@openssh.com request, which this client does not send.
func remotePath(p string) (string, error) {
	if p == "" || p == "~" {
		return ".", nil
	}
	if !strings.HasPrefix(p, "~") {
		return p, nil
	}
	if strings.HasPrefix(p, "~/") {
		rest := strings.TrimLeft(p[2:], "/")
		if rest == "" {
			return ".", nil
		}
		return rest, nil
	}
	return "", errors.New("~user remote paths require expand-path@openssh.com, which pi-cp does not implement")
}

func display(s string) string {
	if strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return strconv.Quote(s)
	}
	return s
}

func (p *Plan) report(err error) {
	if p.Errors != nil {
		fmt.Fprintln(p.Errors, "pi-cp:", err)
	}
}

// download follows scp's SFTP-mode sink: the remote operand is a glob
// (GLOB_NOCHECK|GLOB_MARK); several matches need a local
// directory, which is created when the destination does not exist. Every
// match is attempted; earlier failures are printed and the last is returned.
func (p *Plan) download(ctx context.Context, client *sftp.Client, console *sftpcli.Console, source string) error {
	pattern, err := remotePath(source)
	if err != nil {
		return err
	}
	res, err := pathglob.Glob(ctx, pathglob.Remote(client), pattern, pathglob.Options{Mark: true, NoCheck: true})
	if errors.Is(err, pathglob.ErrTooMany) {
		return fmt.Errorf("%s: too many glob matches", display(source))
	}
	if err != nil {
		return err
	}
	if res.Matches == 0 {
		if _, err := client.Stat(res.Paths[0].Path); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("%s: %w", display(source), os.ErrNotExist)
		}
	}
	target := p.Target.Path
	if res.Matches > 1 {
		info, err := os.Stat(target)
		if err == nil && !info.IsDir() {
			return fmt.Errorf("multiple files match pattern, but destination \"%s\" is not a directory", display(target))
		}
		if err != nil {
			if err := os.Mkdir(target, 0777); err != nil {
				return errors.New("cannot create the local destination directory for multiple matches")
			}
		}
	}
	several := len(res.Paths) > 1
	var last error
	for _, m := range res.Paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := m.Path
		if m.Marked {
			name = strings.TrimSuffix(name, "/")
		}
		switch {
		case p.Recursive:
			err = console.CopyTree(ctx, false, name, target)
		case m.Marked:
			err = fmt.Errorf("download %s: not a regular file", display(m.Path))
			several = false
		default:
			err = console.CopyFile(false, name, target)
		}
		if err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil && several {
			err = fmt.Errorf("%s: %w", display(name), err)
		}
		several = len(res.Paths) > 1
		if err != nil {
			if last != nil {
				p.report(last)
			}
			last = err
		}
	}
	return last
}
