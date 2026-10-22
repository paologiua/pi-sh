//go:build !windows

package terminal

func prepareOutput(int) (func() error, error) {
	return func() error { return nil }, nil
}
