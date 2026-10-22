//go:build !darwin && !linux && !windows

package sftpcli

import (
	"os"
	"time"
)

func localAccessTime(os.FileInfo) (time.Time, bool) { return time.Time{}, false }
