package secrets_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/victorzix/vhook/internal/errs"
	"github.com/victorzix/vhook/internal/secrets"
)

var (
	masterA = []byte("0123456789abcdef0123456789abcdef")
	masterB = []byte("fedcba9876543210fedcba9876543210")
)

func newCipher(t *testing.T, master []byte) *secrets.Cipher {
	t.Helper()
	c, err := secrets.NewCipher(master)
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}
	return c
}

func TestSealThenOpenRoundTrips(t *testing.T) {
	c := newCipher(t, masterA)
	plaintext := []byte("whsec_zDccFjpqVDQHpyWI9SskzezueMASw60LLuaLOFjmD8H")
	aad := []byte("ept_01J4PMX3R0E008000000000003")

	blob, err := c.Seal(plaintext, aad)
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	got, err := c.Open(blob, aad)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("Open() = %q, want %q", got, plaintext)
	}
}

func TestCiphertextDoesNotContainThePlaintext(t *testing.T) {
	c := newCipher(t, masterA)
	plaintext := []byte("whsec_umsegredoquenaopodevazar")

	blob, err := c.Seal(plaintext, []byte("ept_1"))
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	if bytes.Contains(blob, plaintext) {
		t.Error("o texto em claro aparece dentro do blob cifrado")
	}
}

// Duas cifragens do mesmo texto têm de diferir: nonce reutilizado em AES-GCM
// é falha catastrófica, não estética — dois textos sob o mesmo nonce vazam o
// XOR entre eles.
func TestSealUsesAFreshNonceEveryTime(t *testing.T) {
	c := newCipher(t, masterA)
	plaintext := []byte("mesmo texto")
	aad := []byte("ept_1")

	first, err := c.Seal(plaintext, aad)
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	second, err := c.Seal(plaintext, aad)
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("duas cifragens produziram o mesmo blob: o nonce não está variando")
	}
}

// O teste central desta task. Sem ele, uma implementação que passasse nil como
// AAD passaria em todos os outros — e o ganho de vincular o ciphertext ao
// endpoint estaria perdido em silêncio.
func TestOpenWithADifferentAADFails(t *testing.T) {
	c := newCipher(t, masterA)

	blob, err := c.Seal([]byte("segredo"), []byte("ept_01J4PMX3R0E008000000000003"))
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}

	got, err := c.Open(blob, []byte("ept_01J4PMX3R0E008000000000009"))
	if !errors.Is(err, secrets.ErrDecrypt) {
		t.Fatalf("error = %v, queria secrets.ErrDecrypt — o AAD não está no cálculo", err)
	}
	if got != nil {
		t.Error("Open() devolveu dado apesar do erro")
	}
}

func TestOpenWithADifferentMasterKeyFails(t *testing.T) {
	blob, err := newCipher(t, masterA).Seal([]byte("segredo"), []byte("ept_1"))
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	if _, err := newCipher(t, masterB).Open(blob, []byte("ept_1")); !errors.Is(err, secrets.ErrDecrypt) {
		t.Errorf("error = %v, queria secrets.ErrDecrypt", err)
	}
}

func TestOpenRejectsATamperedBlob(t *testing.T) {
	c := newCipher(t, masterA)
	blob, err := c.Seal([]byte("segredo"), []byte("ept_1"))
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}

	tampered := bytes.Clone(blob)
	tampered[len(tampered)-1] ^= 0x01

	if _, err := c.Open(tampered, []byte("ept_1")); !errors.Is(err, secrets.ErrDecrypt) {
		t.Errorf("error = %v, queria secrets.ErrDecrypt", err)
	}
}

func TestOpenRejectsABlobShorterThanTheNonce(t *testing.T) {
	c := newCipher(t, masterA)
	if _, err := c.Open([]byte{1, 2, 3}, []byte("ept_1")); !errors.Is(err, secrets.ErrDecrypt) {
		t.Errorf("error = %v, queria secrets.ErrDecrypt", err)
	}
}

func TestNewCipherRejectsBadMasterKeys(t *testing.T) {
	for _, tt := range []struct {
		name   string
		master []byte
	}{
		{"nil", nil},
		{"vazia", []byte{}},
		{"curta", []byte("curta")},
		{"longa", []byte(strings.Repeat("x", 64))},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := secrets.NewCipher(tt.master); !errors.Is(err, errs.MissingConfig) {
				t.Errorf("error = %v, queria errs.MissingConfig", err)
			}
		})
	}
}
