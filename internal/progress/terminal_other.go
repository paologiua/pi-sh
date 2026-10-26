//go:build !darwin && !linux

package progress

import "os"

// These platforms lack the Unix controlling-terminal process-group check.
func foreground(int) bool                      { return true }
func resizeEvents() (<-chan os.Signal, func()) { return nil, func() {} }
