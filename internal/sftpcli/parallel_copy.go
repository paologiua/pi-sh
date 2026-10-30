package sftpcli

import (
	"context"
	"errors"
	"io"
	"pi-sh/internal/progress"
	"sync"
)

// parallelCopy uses bounded windows and explicit offsets. It avoids the
// dependency's ReaderFrom path that can lose a final short WRITE error.
// Only the contiguous acknowledged prefix contributes to the returned length.
func parallelCopy(ctx context.Context, dst io.WriterAt, src io.ReaderAt, size, offset int64, chunk, requests int, meter *progress.Transfer) (int64, error) {
	var copied int64
	type result struct {
		n   int
		err error
	}
	for base := offset; base < size; {
		if err := ctx.Err(); err != nil {
			return copied, err
		}
		count := int(min(int64(requests), (size-base-1)/int64(chunk)+1))
		results := make([]result, count)
		var workers sync.WaitGroup
		for i := 0; i < count; i++ {
			position := base + int64(i)*int64(chunk)
			length := int(min(int64(chunk), size-position))
			workers.Add(1)
			go func(i int, position int64, length int) {
				defer workers.Done()
				if err := ctx.Err(); err != nil {
					results[i].err = err
					return
				}
				buf := make([]byte, length)
				n, readErr := src.ReadAt(buf, position)
				if readErr == io.EOF && n == length {
					readErr = nil
				}
				if n < length && readErr == nil {
					readErr = io.ErrUnexpectedEOF
				}
				var err error
				if n > 0 {
					results[i].n, err = dst.WriteAt(buf[:n], position)
					meter.Add(results[i].n)
					if results[i].n < n && err == nil {
						err = io.ErrShortWrite
					}
				}
				results[i].err = errors.Join(readErr, err)
			}(i, position, length)
		}
		workers.Wait()
		var failure error
		contiguous := true
		for _, r := range results {
			if contiguous {
				copied += int64(r.n)
			}
			if r.err != nil {
				failure = errors.Join(failure, r.err)
				contiguous = false
			}
		}
		if failure != nil {
			return copied, failure
		}
		base = offset + copied
	}
	return copied, nil
}
