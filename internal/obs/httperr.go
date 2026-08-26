package obs

import (
	"encoding/json"
	"net/http"

	"github.com/victorzix/vhook/internal/errs"
	"github.com/victorzix/vhook/internal/openapi"
)

// WriteError renders the project's error envelope: code, correlation id and
// optional per-field details — never a message. The dashboard translates the
// code through the i18n catalogue. See ARCHITECTURE.md §4.29.
func WriteError(w http.ResponseWriter, r *http.Request, e *errs.Error, details ...openapi.ErrorDetail) {
	body := openapi.Error{
		Error: openapi.ErrorBody{
			Code:          openapi.ErrorCode(e.Code),
			CorrelationId: CorrelationID(r.Context()),
		},
	}
	if len(details) > 0 {
		body.Error.Details = &details
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.HTTPStatus)
	_ = json.NewEncoder(w).Encode(body)
}
