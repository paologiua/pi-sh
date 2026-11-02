package sessionstore

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"time"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

// File layout (all integers big-endian):
//
//	magic    "pi-sh-sessions-v1" (17 bytes)
//	kdf      1 byte, 1 = Argon2id
//	time     uint32, Argon2 passes
//	memory   uint32, Argon2 memory in KiB
//	threads  uint8, Argon2 parallelism
//	saltLen  uint8, followed by the salt
//	nonce    24 bytes, XChaCha20-Poly1305 nonce
//	sealed   XChaCha20-Poly1305 ciphertext and tag of the JSON payload
//
// Every byte before the ciphertext is the AEAD additional data, so the KDF
// parameters, salt and nonce cannot be altered without failing
// authentication.
const magic = "pi-sh-sessions-v1"

const (
	kdfArgon2id   = 1
	keyLen        = chacha20poly1305.KeySize
	saltLen       = 16
	minSaltLen    = 16
	maxSaltLen    = 64
	maxTime       = 16
	maxMemoryKiB  = 1 << 20 // 1 GiB
	maxThreads    = 16
	maxFileBytes  = 16 << 20
	payloadFormat = 1
)

// kdfParams are the Argon2id cost parameters stored in the header.
type kdfParams struct {
	Time      uint32
	MemoryKiB uint32
	Threads   uint8
}

// defaultParams is the RFC 9106 second recommended option (t=3, m=64 MiB,
// p=4). Tests lower it through this variable; production code never does.
var defaultParams = kdfParams{Time: 3, MemoryKiB: 64 * 1024, Threads: 4}

// deriveKey is a variable so tests can prove that rejected parameters never
// reach the KDF.
var deriveKey = func(pass, salt []byte, p kdfParams) []byte {
	return argon2.IDKey(pass, salt, p.Time, p.MemoryKiB, p.Threads, keyLen)
}

func (p kdfParams) valid() bool {
	return p.Time >= 1 && p.Time <= maxTime &&
		p.Threads >= 1 && p.Threads <= maxThreads &&
		p.MemoryKiB >= 8*uint32(p.Threads) && p.MemoryKiB <= maxMemoryKiB
}

// header is the parsed, unauthenticated prefix of the file.
type header struct {
	params kdfParams
	salt   []byte
	nonce  []byte
	raw    []byte // the exact header bytes, used as additional data
}

func (h header) encode() []byte {
	b := make([]byte, 0, len(magic)+11+len(h.salt)+len(h.nonce))
	b = append(b, magic...)
	b = append(b, kdfArgon2id)
	b = binary.BigEndian.AppendUint32(b, h.params.Time)
	b = binary.BigEndian.AppendUint32(b, h.params.MemoryKiB)
	b = append(b, h.params.Threads, byte(len(h.salt)))
	b = append(b, h.salt...)
	b = append(b, h.nonce...)
	return b
}

// parseHeader splits a file into header and ciphertext. It checks only the
// format and parameter bounds; authenticity is established by the AEAD.
func parseHeader(file []byte) (header, []byte, error) {
	var h header
	rest, ok := bytes.CutPrefix(file, []byte(magic))
	if !ok || len(rest) < 11 || rest[0] != kdfArgon2id {
		return h, nil, ErrCorrupt
	}
	h.params = kdfParams{
		Time:      binary.BigEndian.Uint32(rest[1:5]),
		MemoryKiB: binary.BigEndian.Uint32(rest[5:9]),
		Threads:   rest[9],
	}
	n := int(rest[10])
	rest = rest[11:]
	if !h.params.valid() || n < minSaltLen || n > maxSaltLen ||
		len(rest) < n+chacha20poly1305.NonceSizeX+chacha20poly1305.Overhead {
		return h, nil, ErrCorrupt
	}
	h.salt = rest[:n:n]
	h.nonce = rest[n : n+chacha20poly1305.NonceSizeX : n+chacha20poly1305.NonceSizeX]
	sealed := rest[n+chacha20poly1305.NonceSizeX:]
	h.raw = file[:len(file)-len(sealed)]
	return h, sealed, nil
}

func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

// seal encrypts plaintext under key with a fresh random nonce.
func seal(key, salt []byte, params kdfParams, plaintext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	nonce, err := randomBytes(chacha20poly1305.NonceSizeX)
	if err != nil {
		return nil, err
	}
	head := header{params: params, salt: salt, nonce: nonce}.encode()
	return aead.Seal(head, nonce, plaintext, head), nil
}

// open authenticates and decrypts a parsed file.
func open(key []byte, h header, sealed []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	plain, err := aead.Open(nil, h.nonce, sealed, h.raw)
	if err != nil {
		return nil, ErrPassphrase
	}
	return plain, nil
}

// payload is the JSON plaintext.
type payload struct {
	Format  int         `json:"format"`
	Entries []jsonEntry `json:"entries"`
}

type jsonEntry struct {
	Account string    `json:"account"`
	Saved   time.Time `json:"saved"`
	Used    time.Time `json:"used"`
	Data    []byte    `json:"data"`
}

func encodePayload(entries []Entry) ([]byte, error) {
	p := payload{Format: payloadFormat, Entries: make([]jsonEntry, len(entries))}
	for i, e := range entries {
		p.Entries[i] = jsonEntry(e)
	}
	return json.Marshal(p)
}

func decodePayload(plain []byte) ([]Entry, error) {
	var p payload
	if err := json.Unmarshal(plain, &p); err != nil || p.Format != payloadFormat {
		return nil, ErrCorrupt
	}
	entries := make([]Entry, len(p.Entries))
	for i, e := range p.Entries {
		if e.Account == "" {
			return nil, ErrCorrupt
		}
		entries[i] = Entry(e)
	}
	return entries, nil
}

// zero overwrites b. It is best effort: the runtime and the JSON codec may
// have made copies that are not reachable from here.
func zero(b []byte) {
	clear(b)
}
