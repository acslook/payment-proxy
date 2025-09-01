package handlers

import (
	"payment-proxy/internal/infra"
)

type CreatePaymentHandler struct {
	paymentQueue *infra.PaymentsQueue
}

func NewCreatePaymentHandler(q *infra.PaymentsQueue) *CreatePaymentHandler {
	return &CreatePaymentHandler{paymentQueue: q}
}

// func (h *CreatePaymentHandler) Handle(w http.ResponseWriter, r *http.Request) {
// 	if r.Method != http.MethodPost {
// 		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
// 		return
// 	}

// 	var payment entities.Payment
// 	if err := json.NewDecoder(r.Body).Decode(&payment); err != nil {
// 		w.Header().Set("Content-Type", "application/json")
// 		w.WriteHeader(http.StatusBadRequest)
// 		w.Write([]byte(`{"error":"invalid request"}`))
// 		return
// 	}

// 	go h.paymentQueue.Enqueue(context.Background(), payment)

// 	w.WriteHeader(http.StatusOK)
// }
