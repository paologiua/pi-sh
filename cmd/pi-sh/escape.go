package main

import (
	"context"
	"fmt"

	"pi-sh/internal/forward"
	"pi-sh/internal/sshconfig"
)

// The input path carries UTF-8 text, so byte-valued escape characters outside
// ASCII cannot be supported without inventing a binary-input mapping.
func parseEscape(value string) (rune, error) {
	return sshconfig.Escape(value)
}

func escapeLabel(c rune) string {
	if c < 32 {
		return fmt.Sprintf("^%c", c+64)
	}
	if c == 127 {
		return "^?"
	}
	return string(c)
}

// sessionChannel is the ~# line of the session; ssh's parenthesized internal
// state (descriptors, flags) has no pi-sh counterpart.
const sessionChannel = "client-session (Connect shell channel)"

type channelsKey struct{}

// withChannels makes the forwarding channel table available to ~#.
func withChannels(ctx context.Context, c *forward.Channels) context.Context {
	return context.WithValue(ctx, channelsKey{}, c)
}

// openChannels is the ~# list: the table's channels, or only the session
// (#0, as in ssh without forwarding).
func openChannels(ctx context.Context) []string {
	if c, _ := ctx.Value(channelsKey{}).(*forward.Channels); c != nil {
		return c.List()
	}
	return []string{"  #0 " + sessionChannel}
}
