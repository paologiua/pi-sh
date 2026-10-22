package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"pi-sh/internal/commandrun"
	"pi-sh/internal/shellbroker"
	"pi-sh/internal/transport"
)

// Classify sentinel errors only. Neither remote messages nor error strings,
// paths, descriptions or underlying network endpoints enter this report.
func failureClass(err error) string {
	if err == nil {
		return "none"
	}
	for _, entry := range []struct {
		err  error
		name string
	}{
		{commandrun.ErrAuthorization, "session-check"},
		{context.DeadlineExceeded, "deadline"},
		{context.Canceled, "canceled"},
		{transport.ErrReceiveOverflow, "receive-overflow"},
		{transport.ErrMessageSize, "message-size"},
		{transport.ErrPeer, "peer"},
		{transport.ErrClosed, "transport-closed"},
		{shellbroker.ErrProtocol, "broker-protocol"},
		{io.ErrClosedPipe, "pipe-closed"},
		{io.ErrUnexpectedEOF, "unexpected-eof"},
		{io.EOF, "eof"},
		{shellbroker.ErrCleanupUnconfirmed, "cleanup-unconfirmed"},
	} {
		if errors.Is(err, entry.err) {
			return entry.name
		}
	}
	return "other"
}

func reportBroker(w io.Writer, code int, err error, elapsed time.Duration) error {
	_, writeErr := fmt.Fprintf(w, "broker-completion: code=%d failure=%s cleanup-unconfirmed=%t elapsed-ms=%d\n",
		code, failureClass(err), errors.Is(err, shellbroker.ErrCleanupUnconfirmed), elapsed.Milliseconds())
	if writeErr != nil {
		return errors.Join(err, errors.New("cannot write broker verification report"))
	}
	return err
}
