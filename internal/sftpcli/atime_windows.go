package sftpcli

import (
	"os"
	"syscall"
	"time"
)

func localAccessTime(info os.FileInfo) (time.Time, bool) {
	s, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return time.Time{}, false
	}
	ticks := uint64(s.LastAccessTime.HighDateTime)<<32 | uint64(s.LastAccessTime.LowDateTime)
	return time.Unix(int64(ticks/10000000)-11644473600, int64(ticks%10000000)*100), true
}
