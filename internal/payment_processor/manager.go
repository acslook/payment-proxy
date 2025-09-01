package payment_processor

import (
	"context"
	"fmt"
	"log"
	"os"
	"payment-proxy/internal/payments/entities"
	"sync"
)

type GatewayManager struct {
	gateways    map[entities.GatewayType]PaymentGateway
	bestGateway entities.GatewayType
	mu          sync.RWMutex
}

func NewGatewayManager() *GatewayManager {
	gatewayDefaultUrl := os.Getenv("GATEWAY_DEFAULT_URL")
	if gatewayDefaultUrl == "" {
		log.Fatal("GATEWAY_DEFAULT_URL not defined")
	}
	gatewayDefault := NewPaymentGateway(gatewayDefaultUrl, entities.DefaultGateway)
	gatewayFallbackUrl := os.Getenv("GATEWAY_FALLBACK_URL")
	if gatewayFallbackUrl == "" {
		log.Fatal("GATEWAY_FALLBACK_URL not defined")
	}
	gatewayFallback := NewPaymentGateway(gatewayFallbackUrl, entities.FallbackGateway)

	gatewaysMap := make(map[entities.GatewayType]PaymentGateway)
	gatewaysMap[entities.DefaultGateway] = gatewayDefault
	gatewaysMap[entities.FallbackGateway] = gatewayFallback

	return &GatewayManager{
		gateways:    gatewaysMap,
		bestGateway: entities.GatewayType(-1), // Nenhum inicialmente
	}
}

func (m *GatewayManager) MonitorHealth() {
	// Monitoramento simples sem lock distribuído, pois está tudo em memória

	for _, gw := range m.gateways {
		gw.HealthCheck(context.Background())
	}

	var theBest PaymentGateway
	def := m.gateways[entities.DefaultGateway].(*PaymentsGateway)
	fb := m.gateways[entities.FallbackGateway].(*PaymentsGateway)

	// if fb.healthy && def.healthy && float64(def.minResponseTime) >= 3*float64(fb.minResponseTime) {
	// 	theBest = m.gateways[entities.FallbackGateway]
	// }

	if theBest == nil && def.healthy {
		theBest = m.gateways[entities.DefaultGateway]
	}

	if theBest == nil && fb.healthy {
		theBest = m.gateways[entities.FallbackGateway]
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if theBest == nil {
		m.bestGateway = entities.GatewayType(-1)
		fmt.Println("[INFO] No healthy gateway found")
	} else {
		m.bestGateway = theBest.GetType()
		//fmt.Printf("[INFO] Best gateway updated to: %d\n", theBest)
	}
}

func (m *GatewayManager) GetTheBest() PaymentGateway {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.bestGateway == entities.GatewayType(-1) {
		return nil
	}

	gateway, exists := m.gateways[m.bestGateway]
	if !exists {
		fmt.Printf("[ERROR] Gateway %d not found in the manager\n", m.bestGateway)
		return nil
	}
	return gateway
}
