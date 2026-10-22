package sftpcli

import (
	"os"
	"syscall"
	"time"
)

func localAccessTime(info os.FileInfo) (time.Time, bool) {
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return time.Time{}, false
	}
	return time.Unix(int64(s.Atim.Sec), int64(s.Atim.Nsec)), true
}
