package ids_test

import (
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/victorzix/vhook/internal/ids"
)

type vector struct {
	Name   string `json:"name"`
	UUID   string `json:"uuid"`
	Base32 string `json:"base32"`
}

func loadVectors(t *testing.T) []vector {
	t.Helper()
	raw, err := os.ReadFile("testdata/vectors.json")
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	var vs []vector
	if err := json.Unmarshal(raw, &vs); err != nil {
		t.Fatalf("parse vectors: %v", err)
	}
	if len(vs) == 0 {
		t.Fatal("no vectors")
	}
	return vs
}

func TestRenderMatchesVectors(t *testing.T) {
	for _, v := range loadVectors(t) {
		t.Run(v.Name, func(t *testing.T) {
			id := uuid.MustParse(v.UUID)
			if got := ids.Render(id); got != v.Base32 {
				t.Errorf("Render() = %q, want %q", got, v.Base32)
			}
		})
	}
}

func TestParseMatchesVectors(t *testing.T) {
	for _, v := range loadVectors(t) {
		t.Run(v.Name, func(t *testing.T) {
			got, err := ids.Parse(ids.Event, "evt_"+v.Base32)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if got != uuid.MustParse(v.UUID) {
				t.Errorf("Parse() = %v, want %v", got, v.UUID)
			}
		})
	}
}

func TestEncodeAddsPrefix(t *testing.T) {
	id := uuid.MustParse("018f4c2a-7b31-7c4e-9a2b-1f5c8d3e6b04")
	want := "dlv_01HX62MYSHFH79MARZBJ6KWTR4"
	if got := ids.Encode(ids.Delivery, id); got != want {
		t.Errorf("Encode() = %q, want %q", got, want)
	}
}

func TestParseAcceptsLowercase(t *testing.T) {
	got, err := ids.Parse(ids.Event, "evt_01hx62myshfh79marzbj6kwtr4")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if want := uuid.MustParse("018f4c2a-7b31-7c4e-9a2b-1f5c8d3e6b04"); got != want {
		t.Errorf("Parse() = %v, want %v", got, want)
	}
}

func TestParseAcceptsAmbiguousCrockford(t *testing.T) {
	// I e L valem 1, O vale 0 — é o ponto do alfabeto Crockford.
	got, err := ids.Parse(ids.Event, "evt_OIHX62MYSHFH79MARZBJ6KWTR4")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if want := uuid.MustParse("018f4c2a-7b31-7c4e-9a2b-1f5c8d3e6b04"); got != want {
		t.Errorf("Parse() = %v, want %v", got, want)
	}
}

func TestParseRejects(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  error
	}{
		{"prefixo de outro recurso", "dlv_01HX62MYSHFH79MARZBJ6KWTR4", ids.ErrWrongPrefix},
		{"sem prefixo", "01HX62MYSHFH79MARZBJ6KWTR4", ids.ErrWrongPrefix},
		{"curto demais", "evt_01HX62MYSHFH79MARZBJ6KWTR", ids.ErrMalformed},
		{"longo demais", "evt_01HX62MYSHFH79MARZBJ6KWTR44", ids.ErrMalformed},
		{"U não pertence ao alfabeto", "evt_01HX62MYSHFH79MARZBJ6KWTU4", ids.ErrMalformed},
		{"estouro de 128 bits", "evt_8ZZZZZZZZZZZZZZZZZZZZZZZZZ", ids.ErrMalformed},
		{"vazio", "", ids.ErrWrongPrefix},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ids.Parse(ids.Event, tt.input); !errorsIs(err, tt.want) {
				t.Errorf("Parse() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestNewProducesVersion7(t *testing.T) {
	id, err := ids.New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if id.Version() != 7 {
		t.Errorf("Version() = %d, want 7", id.Version())
	}
}

func TestRenderPreservesTimeOrder(t *testing.T) {
	early := ids.Render(uuid.MustParse("018f4c2a-7b31-7c4e-9a2b-1f5c8d3e6b04"))
	late := ids.Render(uuid.MustParse("01912d4e-8f00-7000-8000-000000000001"))
	if !(early < late) {
		t.Errorf("ordem lexicográfica quebrou: %q não é menor que %q", early, late)
	}
}

func errorsIs(got, want error) bool {
	return got != nil && want != nil && errors.Is(got, want)
}
