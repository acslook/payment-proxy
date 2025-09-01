package payments

import (
	"context"
	"errors"
	"payment-proxy/internal/payment_processor"
	"payment-proxy/internal/payments/entities"
	"payment-proxy/internal/payments/repository"
	"time"
)

type Service struct {
	paymentRepository repository.Payment
}

func NewPaymentService(repo repository.Payment) *Service {
	return &Service{paymentRepository: repo}
}

func (s *Service) ProcessPayment(ctx context.Context, gw payment_processor.PaymentGateway, payment entities.Payment) (entities.Payment, error) {
	if payment.CorrelationID == "" {
		return payment, errors.New("correlation ID is required")
	}

	if payment.Amount <= 0 {
		return payment, errors.New("invalid payment amount")
	}
	payment.RequestedAt = time.Now().UTC()
	if gw == nil {
		return payment, errors.New("no healthy gateways available")
	}

	err := gw.ProcessPayment(payment)
	if err != nil {
		return payment, err
	}

	payment.PaymentGatewayType = gw.GetType()
	s.paymentRepository.Save(ctx, &payment)

	return payment, nil
}

func (s *Service) GetPaymentsSummary(ctx context.Context, from, to *time.Time) (entities.AggregatedSummary, error) {
	summary, err := s.paymentRepository.GetByDateRange(ctx, from, to)
	if err != nil {
		return entities.AggregatedSummary{}, err
	}
	return summary, nil
}
