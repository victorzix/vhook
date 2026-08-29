package main

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/victorzix/vhook/internal/errs"
)

func validMasterKey() string {
	return base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
}

func TestLoadConfigReadsBothVariables(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/vhook")
	t.Setenv("VHOOK_MASTER_KEY", validMasterKey())

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if cfg.databaseURL == "" {
		t.Error("databaseURL vazio")
	}
	if len(cfg.masterKey) != 32 {
		t.Errorf("masterKey tem %d bytes, queria 32", len(cfg.masterKey))
	}
}

func TestLoadConfigRejectsBadInput(t *testing.T) {
	tests := []struct {
		name      string
		dbURL     string
		masterKey string
		named     string
	}{
		{"sem banco", "", validMasterKey(), "DATABASE_URL"},
		{"sem chave mestra", "postgres://x", "", "VHOOK_MASTER_KEY"},
		{"chave mestra não é base64", "postgres://x", "!!!nao-e-base64!!!", "VHOOK_MASTER_KEY"},
		{"chave mestra curta", "postgres://x", base64.StdEncoding.EncodeToString([]byte("curta")), "VHOOK_MASTER_KEY"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", tt.dbURL)
			t.Setenv("VHOOK_MASTER_KEY", tt.masterKey)

			_, err := loadConfig()
			if !errors.Is(err, errs.MissingConfig) {
				t.Fatalf("error = %v, queria errs.MissingConfig", err)
			}
			if !strings.Contains(err.Error(), tt.named) {
				t.Errorf("error = %v, devia nomear %s", err, tt.named)
			}
			// O valor da chave mestra nunca pode aparecer numa mensagem de erro.
			if tt.masterKey != "" && strings.Contains(err.Error(), tt.masterKey) {
				t.Error("o valor de VHOOK_MASTER_KEY vazou na mensagem de erro")
			}
		})
	}
}
