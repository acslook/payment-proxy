package handlers

import (
	"encoding/json"
	"net/http"
	"payment-proxy/internal/payments"
	"time"
)

type GetSummaryHandler struct {
	paymentService *payments.Service
}

func NewGetSummaryHandler(s *payments.Service) *GetSummaryHandler {
	return &GetSummaryHandler{paymentService: s}
}

func (h *GetSummaryHandler) Handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	query := r.URL.Query()
	fromStr := query.Get("from")
	toStr := query.Get("to")

	var from, to *time.Time
	var err error

	if fromStr != "" {
		t, err := time.ParseInLocation(time.RFC3339, fromStr, time.UTC)
		if err != nil {
			http.Error(w, `{"error":"invalid 'from' date"}`, http.StatusBadRequest)
			return
		}
		from = &t
	}

	if toStr != "" {
		t, err := time.ParseInLocation(time.RFC3339, toStr, time.UTC)
		if err != nil {
			http.Error(w, `{"error":"invalid 'to' date"}`, http.StatusBadRequest)
			return
		}
		to = &t
	}

	summary, err := h.paymentService.GetPaymentsSummary(r.Context(), from, to)
	if err != nil {
		http.Error(w, `{"error":"failed to get summary"}`, http.StatusInternalServerError)
		return
	}

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(summary); err != nil {
		http.Error(w, `{"error":"failed to encode response"}`, http.StatusInternalServerError)
	}
}
