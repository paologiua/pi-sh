package sftpclient

import (
	"errors"
	"math/big"
	"strconv"
	"strings"
)

// The receiver rejects packets beyond 256 KiB. Leave room for the DATA packet
// header and extension framing, matching OpenSSH's maximum payload policy.
const MaxTransferBuffer = 256*1024 - 1024

func TransferBuffer(requested uint) int {
	if requested == 0 {
		return 32768
	}
	return int(min(requested, uint(MaxTransferBuffer)))
}

// ApplyTransferOption implements the OpenSSH -X buffer=/nrequests= syntax.
// Caller pointers are updated only after the entire value is valid.
func ApplyTransferOption(value string, buffer, requests *uint) error {
	name, raw, ok := strings.Cut(value, "=")
	if !ok {
		return errors.New("Invalid -X option")
	}
	switch name {
	case "nrequests":
		n, err := strconv.ParseUint(raw, 10, 32)
		if err != nil || n == 0 || n > 256*1024 {
			return errors.New("Invalid number of requests")
		}
		*requests = uint(n)
	case "buffer":
		if raw == "" {
			return errors.New("Invalid buffer size")
		}
		scale := int64(1)
		unit := raw[len(raw)-1]
		switch unit {
		case 'b', 'B':
			raw = raw[:len(raw)-1]
		case 'k', 'K':
			scale = 1024
			raw = raw[:len(raw)-1]
		case 'm', 'M':
			scale = 1024 * 1024
			raw = raw[:len(raw)-1]
		case 'g', 'G':
			scale = 1024 * 1024 * 1024
			raw = raw[:len(raw)-1]
		}
		number, ok := new(big.Rat).SetString(raw)
		if !ok || strings.ContainsAny(raw, "/eE") {
			return errors.New("Invalid buffer size")
		}
		number.Mul(number, new(big.Rat).SetInt64(scale))
		n := new(big.Int).Quo(number.Num(), number.Denom())
		if !n.IsInt64() || n.Sign() <= 0 || n.Int64() > 256*1024 {
			return errors.New("Invalid buffer size")
		}
		*buffer = uint(n.Int64())
	default:
		return errors.New("Invalid -X option")
	}
	return nil
}

func ParseBandwidth(value string) (uint64, error) {
	limit, err := strconv.ParseUint(value, 10, 64)
	if err != nil || limit == 0 || limit > 100*1024*1024 {
		return 0, errors.New("Invalid bandwidth limit")
	}
	return limit, nil
}
