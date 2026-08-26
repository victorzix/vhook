package sqlc_test

import (
	"reflect"
	"testing"

	"github.com/victorzix/vhook/internal/store/sqlc"
)

// O sqlc gera os modelos a partir da migration. Este teste falha se alguém
// editar o gerado à mão ou se a migration mudar sem regenerar — que é o furo
// que "contrato como fonte única" existe para fechar.
func TestEventPayloadIsAStringAndNullable(t *testing.T) {
	field, ok := reflect.TypeOf(sqlc.Event{}).FieldByName("Payload")
	if !ok {
		t.Fatal("Event não tem campo Payload")
	}
	// pgtype.Text e não []byte nem um tipo de json: payload é text
	// byte-exato (§4.32), e é nullable porque NULL = expurgado.
	if got := field.Type.String(); got != "pgtype.Text" {
		t.Errorf("Payload é %s, queria pgtype.Text", got)
	}
}

func TestEveryTableGotAModel(t *testing.T) {
	models := []any{
		sqlc.Organization{}, sqlc.Application{}, sqlc.Endpoint{},
		sqlc.Event{}, sqlc.Delivery{}, sqlc.DeliveryAttempt{},
	}
	for _, m := range models {
		if reflect.TypeOf(m).NumField() == 0 {
			t.Errorf("%T não tem campo nenhum", m)
		}
	}
}
