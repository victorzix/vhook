package main

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func TestGenkeyPrintsA32ByteBase64Key(t *testing.T) {
	var out bytes.Buffer
	if err := genkey(&out); err != nil {
		t.Fatalf("genkey() error = %v", err)
	}

	encoded := strings.TrimSpace(out.String())
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("saída não é base64 padrão: %v — %q", err, encoded)
	}
	// 32 bytes é o que apikey.NewHasher exige e o que o AES-256 de
	// endpoints.secret já usa.
	if len(raw) != 32 {
		t.Errorf("chave tem %d bytes, queria 32", len(raw))
	}
}

func TestGenkeyDoesNotRepeat(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		var out bytes.Buffer
		if err := genkey(&out); err != nil {
			t.Fatalf("genkey() error = %v", err)
		}
		key := strings.TrimSpace(out.String())
		if seen[key] {
			t.Fatalf("chave mestra repetida na iteração %d", i)
		}
		seen[key] = true
	}
}

func TestRunRejectsAnUnknownSubcommand(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"naoexiste"}, &out); err == nil {
		t.Error("subcomando desconhecido devia falhar")
	}
}

func TestRunWithNoSubcommandFails(t *testing.T) {
	var out bytes.Buffer
	if err := run(nil, &out); err == nil {
		t.Error("sem subcomando devia falhar")
	}
}
