package handlers

import (
	"context"
	"net/http"
	"payment-proxy/internal/payment"
	"payment-proxy/internal/payment/entities"

	"github.com/labstack/echo/v4"
)

type CreatePaymentHandler struct {
	paymentQueue *payment.RedisQueue
}

func NewCreatePaymentHandler(q *payment.RedisQueue) *CreatePaymentHandler {
	return &CreatePaymentHandler{paymentQueue: q}
}

func (h *CreatePaymentHandler) Handle(c echo.Context) error {
	var payment entities.Payment
	if err := c.Bind(&payment); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}

	go h.paymentQueue.Enqueue(context.Background(), payment)

	return c.NoContent(http.StatusOK)
}
