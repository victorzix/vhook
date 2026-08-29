// Package tokens draws random opaque tokens. It exists apart from the
// credential packages that use it because more than one of them needs the same
// unbiased draw, and a package named after one credential would lie about the
// others.
package tokens

import (
	"crypto/rand"
	"errors"
	"fmt"
)

// Alphabet is Base62: no + or /, which break in URLs and in badly quoted
// environment variables.
const Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// maxUnbiased is the largest multiple of 62 that fits in a byte (62 × 4).
// Bytes at or above it are discarded rather than folded with %, which would
// make the first characters of the alphabet more likely.
const maxUnbiased = 248

// Random returns prefix followed by n characters drawn uniformly from Alphabet.
func Random(prefix string, n int) (string, error) {
	if n <= 0 {
		return "", errors.New("tokens: length must be positive")
	}

	body := make([]byte, 0, n)
	buf := make([]byte, n)
	for len(body) < n {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("tokens: read random: %w", err)
		}
		for _, b := range buf {
			if b >= maxUnbiased {
				continue
			}
			body = append(body, Alphabet[int(b)%len(Alphabet)])
			if len(body) == n {
				break
			}
		}
	}
	return prefix + string(body), nil
}
