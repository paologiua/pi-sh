package sftpclient

// The receiver rejects packets beyond 256 KiB. Leave room for the DATA packet
// header and extension framing, matching OpenSSH's maximum payload policy.
const MaxTransferBuffer = 256*1024 - 1024

func TransferBuffer(requested uint) int {
	if requested == 0 {
		return 32768
	}
	return int(min(requested, uint(MaxTransferBuffer)))
}
