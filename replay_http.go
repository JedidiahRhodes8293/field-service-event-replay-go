package fieldservice

import (
	"encoding/json"
	"io"
	"net/http"
)

type HTTPHandler struct {
	service   *ReplayService
	webhookID string
	queue     string
}

func NewHTTPHandler(service *ReplayService, webhookID, queue string) *HTTPHandler {
	return &HTTPHandler{service: service, webhookID: webhookID, queue: queue}
}

func (h *HTTPHandler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /replay", h.handleReplay)
	mux.HandleFunc("POST /redrive", h.handleRedrive)
	mux.HandleFunc("POST /webhooks/fieldservice", h.handleIncomingWebhook)
	mux.HandleFunc("GET /state", h.handleState)
	return mux
}

func (h *HTTPHandler) handleReplay(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.ReplayFromWebhookDeliveries(r.Context(), h.webhookID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *HTTPHandler) handleRedrive(w http.ResponseWriter, r *http.Request) {
	if err := h.service.RedriveDeadLetters(r.Context(), h.queue); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"queue": h.queue, "redriven": true})
}

func (h *HTTPHandler) handleIncomingWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sig := r.Header.Get("X-FieldService-Signature")
	if err := h.service.ApplySignedEvent(body, sig); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true})
}

func (h *HTTPHandler) handleState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.service.store.Snapshot(0))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
