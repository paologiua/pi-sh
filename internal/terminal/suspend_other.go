//go:build !darwin && !linux

package terminal

const canStopProcess = false

func stopProcess() error { return ErrSuspendUnsupported }
