package payment_processor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"payment-proxy/internal/payments/entities"
	"time"
)

type PaymentGateway interface {
	ProcessPayment(p entities.Payment) error
	HealthCheck(ctx context.Context) (health bool, minResponseTime int)
	GetType() entities.GatewayType
}

type PaymentsGateway struct {
	baseURL         string
	client          *http.Client
	gatewayType     entities.GatewayType
	healthy         bool
	minResponseTime int
}

type HealthCheckResponse struct {
	Failing         bool `json:"failing"`
	MinResponseTime int  `json:"minResponseTime"`
}

func NewPaymentGateway(baseURL string, gatewayType entities.GatewayType) *PaymentsGateway {
	return &PaymentsGateway{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		gatewayType: gatewayType,
	}
}

func (g *PaymentsGateway) ProcessPayment(p entities.Payment) error {
	payload, _ := json.Marshal(p)

	req, err := http.NewRequest("POST", g.baseURL+"/payments", bytes.NewBuffer(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("invalid response from gateway: %v", body)
	}

	return nil
}

func (g *PaymentsGateway) HealthCheck(ctx context.Context) (health bool, minResponseTime int) {
	req, err := http.NewRequestWithContext(ctx, "GET", g.baseURL+"/payments/service-health", nil)
	if err != nil {
		return false, 0
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return false, 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, 0
	}

	var healthCheckResponse HealthCheckResponse
	if err := json.NewDecoder(resp.Body).Decode(&healthCheckResponse); err != nil {
		return false, 0
	}
	minRespTime := healthCheckResponse.MinResponseTime
	if minRespTime <= 0 {
		minRespTime = 1
	}

	g.healthy = !healthCheckResponse.Failing
	g.minResponseTime = minRespTime

	return g.healthy, g.minResponseTime
}

func (g *PaymentsGateway) GetType() entities.GatewayType {
	return g.gatewayType
}
