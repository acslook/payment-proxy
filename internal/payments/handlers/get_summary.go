package handlers

import (
	"context"
	"net/http"
	"payment-proxy/internal/payments"
	"time"

	"github.com/labstack/echo/v4"
)

type GetSummaryHandler struct {
	paymentService *payments.Service
}

func NewGetSummaryHandler(s *payments.Service) *GetSummaryHandler {
	return &GetSummaryHandler{paymentService: s}
}

func (h *GetSummaryHandler) Handle(c echo.Context) error {
	fromStr := c.QueryParam("from")
	toStr := c.QueryParam("to")

	var from, to *time.Time

	if fromStr != "" {
		t, err := time.Parse(time.RFC3339, fromStr)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid 'from' date"})
		}
		from = &t
	}

	if toStr != "" {
		t, err := time.Parse(time.RFC3339, toStr)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid 'to' date"})
		}
		to = &t
	}

	summary, err := h.paymentService.GetPaymentsSummary(context.Background(), from, to)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to get summary"})
	}

	return c.JSON(http.StatusOK, summary)
}
