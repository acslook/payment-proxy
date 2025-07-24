package repository

import (
	"context"
	"payment-proxy/internal/payments/entities"
	"time"
)

type Payment interface {
	Save(cxt context.Context, payment entities.Payment) error
	Get(cxt context.Context, correlationID string) (entities.Payment, bool)
	GetAll(cxt context.Context) []entities.Payment
	GetByDateRange(cxt context.Context, from, to *time.Time) (entities.AggregatedSummary, error)
}
