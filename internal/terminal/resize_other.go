//go:build !unix

package terminal

import "time"

// Windows console windows have no SIGWINCH. Poll only while the session owns
// raw mode, with constant space and no background process or console helper.
func resizeEvents() (<-chan time.Time, func()) {
	t := time.NewTicker(200 * time.Millisecond)
	return t.C, t.Stop
}
