package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/cocina/server-mvp/types"
)

func requestID(r *http.Request) string {
	if id := r.Header.Get("X-Request-ID"); id != "" {
		return id
	}
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err == nil {
		return "req_" + hex.EncodeToString(bytes)
	}
	return "req_unknown"
}

func (h *APIHandler) writeData(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(types.APIEnvelope{Data: data})
}

func (h *APIHandler) writePaginated(w http.ResponseWriter, data interface{}, meta types.APIMeta) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(types.APIEnvelope{Data: data, Meta: &meta})
}

func (h *APIHandler) writeAPIError(w http.ResponseWriter, r *http.Request, code, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(types.APIErrorResponse{
		Error: types.APIErrorBody{
			Code:      code,
			Message:   message,
			RequestID: requestID(r),
		},
	})
}
