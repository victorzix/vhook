package httpauth_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/victorzix/vhook/internal/errs"
	"github.com/victorzix/vhook/internal/httpauth"
)

const expected = "s3cr3t-admin-token"

func request(header string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/v1/applications/app_1/endpoints", nil)
	if header != "" {
		r.Header.Set("Authorization", header)
	}
	return r
}

func TestCheckAdminTokenAcceptsTheRightToken(t *testing.T) {
	if err := httpauth.CheckAdminToken(request("Bearer "+expected), expected); err != nil {
		t.Errorf("error = %v, queria nil", err)
	}
}

// Todas as recusas devolvem o MESMO erro. Distinguir "ausente" de "errado"
// informaria a um atacante que o formato do envio já está certo.
func TestEveryRejectionIsIndistinguishable(t *testing.T) {
	for _, tt := range []struct{ name, header string }{
		{"sem header", ""},
		{"header vazio", " "},
		{"sem o esquema Bearer", expected},
		{"esquema errado", "Basic " + expected},
		{"token errado", "Bearer errado"},
		{"token vazio", "Bearer "},
		{"prefixo certo do token", "Bearer s3cr3t"},
		{"token com sufixo a mais", "Bearer " + expected + "x"},
		{"caixa diferente no esquema", "bearer " + expected},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := httpauth.CheckAdminToken(request(tt.header), expected)
			if !errors.Is(err, errs.InvalidCredentials) {
				t.Errorf("error = %v, queria errs.InvalidCredentials", err)
			}
		})
	}
}

// Um token esperado vazio nunca pode aceitar nada: seria uma API aberta por
// causa de uma variável de ambiente não preenchida.
func TestAnEmptyExpectedTokenAcceptsNothing(t *testing.T) {
	for _, header := range []string{"", "Bearer ", "Bearer qualquercoisa"} {
		if err := httpauth.CheckAdminToken(request(header), ""); !errors.Is(err, errs.InvalidCredentials) {
			t.Errorf("header %q: error = %v, queria errs.InvalidCredentials", header, err)
		}
	}
}
