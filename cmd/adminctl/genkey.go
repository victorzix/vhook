package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

// masterKeyBytes matches what apikey.NewHasher requires and what the AES-256
// key for endpoint secrets already uses.
const masterKeyBytes = 32

// genkey prints a master key. It touches neither the database nor any file:
// whoever runs it decides where the key is going to live.
func genkey(out io.Writer) error {
	key := make([]byte, masterKeyBytes)
	if _, err := rand.Read(key); err != nil {
		return fmt.Errorf("genkey: read random: %w", err)
	}
	_, err := fmt.Fprintln(out, base64.StdEncoding.EncodeToString(key))
	return err
}
