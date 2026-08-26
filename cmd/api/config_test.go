package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/victorzix/vhook/internal/errs"
)

func TestLoadConfigReadsTheThreeVariables(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/vhook")
	t.Setenv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")
	t.Setenv("VHOOK_HTTP_ADDR", ":9090")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if cfg.httpAddr != ":9090" {
		t.Errorf("httpAddr = %q, want :9090", cfg.httpAddr)
	}
}

func TestLoadConfigDefaultsTheHTTPAddress(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/vhook")
	t.Setenv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")
	t.Setenv("VHOOK_HTTP_ADDR", "")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if cfg.httpAddr != ":8080" {
		t.Errorf("httpAddr = %q, want :8080", cfg.httpAddr)
	}
}

func TestLoadConfigFailsWhenASecretIsMissing(t *testing.T) {
	tests := []struct{ name, unset string }{
		{"sem banco", "DATABASE_URL"},
		{"sem fila", "RABBITMQ_URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/vhook")
			t.Setenv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")
			t.Setenv(tt.unset, "")

			_, err := loadConfig()
			if !errors.Is(err, errs.MissingConfig) {
				t.Errorf("error = %v, queria errs.MissingConfig", err)
			}
			// A mensagem precisa nomear a variável: sem isso o operador
			// descobre qual falta por tentativa e erro.
			if err == nil || !contains(err.Error(), tt.unset) {
				t.Errorf("error = %v, devia nomear %s", err, tt.unset)
			}
		})
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && strings.Contains(haystack, needle)
}
