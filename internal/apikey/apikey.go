// Package apikey generates and verifies application API keys.
//
// The stored value is HMAC-SHA256 of the key under a server-side pepper, not a
// plain digest and never a salted slow hash: the ingress must find the
// application by indexed lookup on every request, which needs determinism, and
// a 256-bit random key has no search space that a slow hash would protect.
// See ARCHITECTURE.md §4.33.
package apikey

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/victorzix/vhook/internal/errs"
)

// Prefix makes a key recognisable in a log, a support ticket or a .env, and is
// what a secret-scanning rule would match on.
const Prefix = "vhk_"

const (
	// alphabet is Base62: no + or /, which break in URLs and in badly quoted
	// environment variables.
	alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

	// keyLength is 43 because 43 × log2(62) = 256.0 bits. The number comes from
	// the entropy budget, not from taste.
	keyLength = 43

	// maxUnbiased is the largest multiple of 62 that fits in a byte (62 × 4).
	// Bytes at or above it are discarded rather than folded with %, which would
	// make the first characters of the alphabet more likely.
	maxUnbiased = 248

	// masterKeyLength matches the AES-256 key already used for endpoint secrets.
	masterKeyLength = 32
)

// Hasher holds the pepper. It is built once at boot so the master key is
// validated in one place, and so no call site can forget to pass it — a nil key
// would silently produce an HMAC nobody could reproduce.
type Hasher struct {
	key []byte
}

// NewHasher validates the master key. It reports errs.MissingConfig for any
// length other than 32 bytes: a short key accepted in silence would mean less
// entropy than advertised.
func NewHasher(masterKey []byte) (*Hasher, error) {
	if len(masterKey) != masterKeyLength {
		return nil, errors.Join(errs.MissingConfig,
			fmt.Errorf("apikey: master key must be %d bytes, got %d",
				masterKeyLength, len(masterKey)))
	}
	// Copy so a caller mutating its slice later cannot change our pepper.
	key := make([]byte, masterKeyLength)
	copy(key, masterKey)
	return &Hasher{key: key}, nil
}

// Generate returns a fresh key and its hash. The plaintext is returned once and
// never stored anywhere.
func (h *Hasher) Generate() (plain, hash string, err error) {
	body := make([]byte, 0, keyLength)
	buf := make([]byte, keyLength)

	for len(body) < keyLength {
		if _, err := rand.Read(buf); err != nil {
			return "", "", fmt.Errorf("apikey: read random: %w", err)
		}
		for _, b := range buf {
			if b >= maxUnbiased {
				continue
			}
			body = append(body, alphabet[int(b)%len(alphabet)])
			if len(body) == keyLength {
				break
			}
		}
	}

	plain = Prefix + string(body)
	return plain, h.Hash(plain), nil
}

// Hash is deterministic under a fixed master key: the ingress calls it on an
// incoming key to find the application with a single indexed lookup.
func (h *Hasher) Hash(plain string) string {
	mac := hmac.New(sha256.New, h.key)
	mac.Write([]byte(plain))
	return hex.EncodeToString(mac.Sum(nil))
}
