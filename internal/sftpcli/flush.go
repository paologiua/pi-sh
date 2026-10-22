package sftpcli

import (
	"errors"
	"fmt"
	"strings"

	"pi-sh/internal/sftpclient"
)

// transferFlags stops at an operand or --; unsupported flags fail before I/O.
type transferOptions struct{ flush, resume, preserve bool }

func transferFlags(args []string, options transferOptions) ([]string, transferOptions, error) {
	for i, arg := range args {
		if arg == "--" {
			return args[i+1:], options, nil
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			return args[i:], options, nil
		}
		for _, option := range arg[1:] {
			switch option {
			case 'f':
				options.flush = true
			case 'a':
				options.resume = true
			case 'p':
				options.preserve = true
			default:
				return nil, transferOptions{}, errors.New("unsupported transfer command flag")
			}
		}
	}
	return nil, options, nil
}

type syncCloser interface {
	Sync() error
	Close() error
}

// finishTransfer always closes the destination. The caller decides whether to
// sync a partial transfer; advisory errors cannot erase the original copy error.
func finishTransfer(target syncCloser, copyErr error, flush bool, advisory func(error) error) error {
	err := copyErr
	if flush {
		if syncErr := target.Sync(); syncErr != nil {
			err = errors.Join(err, advisory(syncErr))
		}
	}
	return errors.Join(err, target.Close())
}

func (c *Console) syncWarning(where string, err error) {
	if c.Errors != nil {
		fmt.Fprintf(c.Errors, "pi-sftp: %s sync failed: %v\n", where, sftpclient.SafeError(err))
	}
}
