package payment

import (
	"context"
	"errors"
	"payment-proxy/internal/payment/entities"
	"time"
)

type Service struct {
	paymentRepository PaymentRepository
}

func NewPaymentService(repo PaymentRepository) *Service {
	return &Service{paymentRepository: repo}
}

func (s *Service) ProcessPayment(ctx context.Context, gateway PaymentGateway, payment entities.Payment) (entities.Payment, error) {
	if payment.CorrelationID == "" {
		return payment, errors.New("correlation ID is required")
	}

	paymentDb, exists := s.paymentRepository.Get(ctx, payment.CorrelationID)
	if exists {
		return paymentDb, nil
	}

	if payment.Amount <= 0 {
		return payment, errors.New("invalid payment amount")
	}
	payment.RequestedAt = time.Now().UTC()
	if gateway == nil {
		return payment, errors.New("no healthy gateways available")
	}

	err := gateway.ProcessPayment(payment)
	if err != nil {
		return payment, err
	}

	payment.PaymentGatewayType = gateway.GetType()
	err = s.paymentRepository.Save(ctx, payment)
	if err != nil {
		return payment, err
	}

	return payment, nil
}

func (s *Service) GetPayment(ctx context.Context, correlationID string) (entities.Payment, bool) {
	payment, exists := s.paymentRepository.Get(ctx, correlationID)
	if !exists {
		return payment, false
	}
	return payment, true
}

func (s *Service) GetPaymentsSummary(ctx context.Context, from, to *time.Time) (entities.AggregatedSummary, error) {
	summary, err := s.paymentRepository.GetByDateRange(ctx, from, to)
	if err != nil {
		return entities.AggregatedSummary{}, err
	}
	return summary, nil
}
