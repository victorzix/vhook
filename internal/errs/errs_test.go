package errs_test

import (
	"errors"
	"net/http"
	"regexp"
	"testing"

	"github.com/victorzix/vhook/internal/errs"
)

var codeFormat = regexp.MustCompile(`^[A-Z]{3}-[A-Z]{3}-[0-9]{3}$`)

func TestEveryCodeMatchesTheFormat(t *testing.T) {
	for _, e := range errs.All() {
		if !codeFormat.MatchString(e.Code) {
			t.Errorf("código %q não casa com MOD-TYP-NNN", e.Code)
		}
	}
}

func TestNoDuplicateCodes(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range errs.All() {
		if seen[e.Code] {
			t.Errorf("código duplicado: %s", e.Code)
		}
		seen[e.Code] = true
	}
}

func TestEveryErrorHasALevel(t *testing.T) {
	for _, e := range errs.All() {
		if e.Level != errs.LevelWarn && e.Level != errs.LevelError {
			t.Errorf("%s: nível inválido %q", e.Code, e.Level)
		}
	}
}

func TestDeclaredOverrides(t *testing.T) {
	// As sobrescritas da spec 001. Se alguma mudar sem passar pela spec,
	// este teste é onde isso aparece.
	tests := []struct {
		err    *errs.Error
		level  errs.Level
		status int
	}{
		{errs.StorageUnavailable, errs.LevelError, http.StatusServiceUnavailable},
		{errs.QueueUnavailable, errs.LevelError, http.StatusServiceUnavailable},
		{errs.Draining, errs.LevelWarn, http.StatusServiceUnavailable},
		{errs.Internal, errs.LevelError, http.StatusInternalServerError},
		{errs.MissingConfig, errs.LevelError, 0},
	}
	for _, tt := range tests {
		t.Run(tt.err.Code, func(t *testing.T) {
			if tt.err.Level != tt.level {
				t.Errorf("Level = %q, want %q", tt.err.Level, tt.level)
			}
			if tt.err.HTTPStatus != tt.status {
				t.Errorf("HTTPStatus = %d, want %d", tt.err.HTTPStatus, tt.status)
			}
		})
	}
}

func TestErrorsIsWorksAgainstTheConstant(t *testing.T) {
	wrapped := errors.Join(errs.StorageUnavailable, errors.New("dial tcp: refused"))
	if !errors.Is(wrapped, errs.StorageUnavailable) {
		t.Error("errors.Is falhou contra a constante")
	}
	if errors.Is(wrapped, errs.QueueUnavailable) {
		t.Error("errors.Is casou com a constante errada")
	}
}

func TestMissingConfigHasNoHTTPStatus(t *testing.T) {
	// Falta de configuração mata o processo antes de a porta abrir; a
	// constante existe pelo nível, não pelo status.
	if errs.MissingConfig.HTTPStatus != 0 {
		t.Errorf("HTTPStatus = %d, want 0", errs.MissingConfig.HTTPStatus)
	}
}
