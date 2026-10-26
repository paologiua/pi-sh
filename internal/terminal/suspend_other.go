//go:build !darwin && !linux

package terminal

const canStopProcess = false

func stopProcess() error { return ErrSuspendUnsupported }

// WatchSuspendSignals has no external suspend signal on this platform.
func WatchSuspendSignals() (<-chan struct{}, func()) { return nil, func() {} }
