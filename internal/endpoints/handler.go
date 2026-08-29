package endpoints

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/victorzix/vhook/internal/errs"
	"github.com/victorzix/vhook/internal/ids"
	"github.com/victorzix/vhook/internal/obs"
	"github.com/victorzix/vhook/internal/openapi"
)

// Handler is the HTTP edge of this package. It does exactly three things —
// decode into the generated type, call the service, serialise the answer — and
// holds no rule at all.
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// parsePath turns the external identifiers into UUIDs. A malformed id is a
// 422, distinct from a well-formed id that does not exist, which is a 404.
//
// The generated path parameters are aliases of string, so the compiler will
// not stop an application id from being passed where an endpoint id belongs.
// The prefix check below is what catches it.
func parsePath(applicationID string, endpointID *string) (uuid.UUID, uuid.UUID, error) {
	app, err := ids.Parse(ids.Application, applicationID)
	if err != nil {
		return uuid.Nil, uuid.Nil, errs.MalformedID
	}
	if endpointID == nil {
		return app, uuid.Nil, nil
	}
	ept, err := ids.Parse(ids.Endpoint, *endpointID)
	if err != nil {
		return uuid.Nil, uuid.Nil, errs.MalformedID
	}
	return app, ept, nil
}

func (h *Handler) CreateEndpoint(w http.ResponseWriter, r *http.Request, applicationId openapi.ApplicationId) {
	appID, _, err := parsePath(applicationId, nil)
	if err != nil {
		writeErr(w, r, err)
		return
	}

	var body openapi.CreateEndpointRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, r, errs.InvalidEndpointURL)
		return
	}

	created, err := h.svc.Create(r.Context(), appID, body.Url)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toWithSecret(created))
}

func (h *Handler) ListEndpoints(w http.ResponseWriter, r *http.Request, applicationId openapi.ApplicationId) {
	appID, _, err := parsePath(applicationId, nil)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	list, err := h.svc.List(r.Context(), appID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	out := make([]openapi.Endpoint, 0, len(list))
	for _, e := range list {
		out = append(out, toAPI(e))
	}
	writeJSON(w, http.StatusOK, openapi.EndpointList{Endpoints: out})
}

func (h *Handler) GetEndpoint(w http.ResponseWriter, r *http.Request, applicationId openapi.ApplicationId, endpointId openapi.EndpointId) {
	appID, eptID, err := parsePath(applicationId, &endpointId)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	got, err := h.svc.Get(r.Context(), appID, eptID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toWithSecret(got))
}

func (h *Handler) UpdateEndpoint(w http.ResponseWriter, r *http.Request, applicationId openapi.ApplicationId, endpointId openapi.EndpointId) {
	appID, eptID, err := parsePath(applicationId, &endpointId)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	var body openapi.UpdateEndpointRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, r, errs.InvalidEndpointURL)
		return
	}
	updated, err := h.svc.UpdateURL(r.Context(), appID, eptID, body.Url)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPI(updated))
}

// toAPI leaves the secret out. The list and the update responses use it, and
// the secret belongs only to creation and to the detail route.
func toAPI(e Endpoint) openapi.Endpoint {
	return openapi.Endpoint{
		Id:        ids.Encode(ids.Endpoint, e.ID),
		Url:       e.URL,
		Status:    openapi.EndpointStatus(e.Status),
		CreatedAt: e.CreatedAt,
	}
}

func toWithSecret(e Endpoint) openapi.EndpointWithSecret {
	return openapi.EndpointWithSecret{
		Id:        ids.Encode(ids.Endpoint, e.ID),
		Url:       e.URL,
		Status:    openapi.EndpointStatus(e.Status),
		CreatedAt: e.CreatedAt,
		Secret:    e.Secret,
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeErr maps the registered constant to its status. The handler never picks
// a status ad hoc — that is how the same error returns 400 in one place and
// 422 in another.
func writeErr(w http.ResponseWriter, r *http.Request, err error) {
	var registered *errs.Error
	if !errors.As(err, &registered) {
		// An unregistered error never leaks detail to the client: it becomes
		// SYS-INT-001, and the original stays in the log.
		registered = errs.Internal
	}
	obs.WriteError(w, r, registered)
}
