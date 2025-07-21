package entities

import "time"

type GatewayType int

const (
	DefaultGateway GatewayType = iota
	FallbackGateway
)

type Payment struct {
	CorrelationID      string      `json:"correlationId"`
	Amount             float64     `json:"amount"`
	RequestedAt        time.Time   `json:"requestedAt"`
	PaymentGatewayType GatewayType `json:"paymentGatewayType"`
}
