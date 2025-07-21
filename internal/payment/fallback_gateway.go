package payment

import (
	"fmt"
	"payment-proxy/internal/payment/entities"
)

type FallbackGateway struct {
	defaultGateway  PaymentGateway
	fallbackGateway PaymentGateway
}

func NewFallbackGateway(defaultGW, fallbackGW PaymentGateway) *FallbackGateway {
	return &FallbackGateway{
		defaultGateway:  defaultGW,
		fallbackGateway: fallbackGW,
	}
}

func (f *FallbackGateway) ProcessPayment(p entities.Payment) (entities.GatewayType, error) {
	err := f.defaultGateway.ProcessPayment(p)
	if err == nil {
		return entities.DefaultGateway, nil
	}

	fmt.Println("[WARN] Default gateway failed, trying fallback:", err)

	err = f.fallbackGateway.ProcessPayment(p)
	if err != nil {
		return entities.FallbackGateway, fmt.Errorf("both gateways failed: %v", err)
	}

	return entities.FallbackGateway, nil
}
