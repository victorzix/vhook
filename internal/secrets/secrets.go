// Package secrets encrypts values that the system must be able to read back —
// unlike credentials it only needs to verify, which are hashed instead. The
// endpoint secret is the case: vhook needs it in the clear to sign outgoing
// deliveries, so hashing it would make delivery impossible. See §4.12.
//
// Named secrets and not crypto so it does not shadow the standard library
// package at call sites.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/victorzix/vhook/internal/errs"
)

// masterKeyLength selects AES-256.
const masterKeyLength = 32

// ErrDecrypt covers every failure to recover a plaintext: wrong key, wrong
// AAD, tampered bytes, truncated blob. They are deliberately indistinguishable
// — telling them apart would help an attacker probing which part is wrong.
var ErrDecrypt = errors.New("secrets: cannot decrypt")

// Cipher seals and opens values under the master key.
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher validates the master key once, at boot, so no call site can pass a
// nil key and silently produce output nobody can read back.
func NewCipher(masterKey []byte) (*Cipher, error) {
	if len(masterKey) != masterKeyLength {
		return nil, errors.Join(errs.MissingConfig,
			fmt.Errorf("secrets: master key must be %d bytes, got %d",
				masterKeyLength, len(masterKey)))
	}
	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, fmt.Errorf("secrets: new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: new gcm: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Seal returns nonce ‖ ciphertext. The nonce is not secret and is stored
// alongside; what matters is that it is never reused under the same key, so it
// comes from crypto/rand and is never derived or counted.
//
// aad is authenticated but not encrypted. Passing the owning row's identifier
// makes a blob moved to another row fail to open, instead of opening fine.
func (c *Cipher) Seal(plaintext, aad []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secrets: read nonce: %w", err)
	}
	// Sealing into nonce appends the ciphertext to it, giving nonce ‖ ct.
	return c.aead.Seal(nonce, nonce, plaintext, aad), nil
}

// Open reverses Seal. Every failure is ErrDecrypt.
func (c *Cipher) Open(blob, aad []byte) ([]byte, error) {
	n := c.aead.NonceSize()
	if len(blob) < n {
		return nil, ErrDecrypt
	}
	plaintext, err := c.aead.Open(nil, blob[:n], blob[n:], aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return plaintext, nil
}
