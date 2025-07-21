package payment

import (
	"context"
	"payment-proxy/internal/payment/entities"
	"sync"
	"time"
)

type PaymentRepository interface {
	Save(cxt context.Context, payment entities.Payment) error
	Get(cxt context.Context, correlationID string) (entities.Payment, bool)
	GetAll(cxt context.Context) []entities.Payment
	GetByDateRange(cxt context.Context, from, to *time.Time) (entities.AggregatedSummary, error)
}

type InMemoryRepository struct {
	data map[string]entities.Payment
	mu   sync.Mutex
}

func NewInMemoryRepository() *InMemoryRepository {
	return &InMemoryRepository{data: make(map[string]entities.Payment)}
}

func (r *InMemoryRepository) Save(p entities.Payment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[p.CorrelationID] = p
	return nil
}

func (r *InMemoryRepository) Get(correlationID string) (entities.Payment, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	payment, exists := r.data[correlationID]
	return payment, exists
}

func (r *InMemoryRepository) GetAll() []entities.Payment {
	r.mu.Lock()
	defer r.mu.Unlock()
	payments := make([]entities.Payment, 0, len(r.data))
	for _, payment := range r.data {
		payments = append(payments, payment)
	}
	return payments
}

func (r *InMemoryRepository) Purge() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data = make(map[string]entities.Payment)
}
