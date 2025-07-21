package payment

import (
	"context"
	"fmt"
	"payment-proxy/internal/payment/entities"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type status struct {
	healthy         bool
	minResponseTime int
	lastCheck       time.Time
}

type GatewayManager struct {
	cache    *redis.Client
	gateways []PaymentGateway
	statuses map[entities.GatewayType]*status
	mu       sync.RWMutex
}

func NewGatewayManager(cache *redis.Client, gateways ...PaymentGateway) *GatewayManager {
	m := &GatewayManager{
		cache:    cache,
		gateways: gateways,
		statuses: make(map[entities.GatewayType]*status),
	}

	go m.monitorHealth()
	return m
}

func (m *GatewayManager) monitorHealth() {
	ticker := time.NewTicker(6 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		go m.checkGateway()
	}
}

func (m *GatewayManager) checkGateway() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, gw := range m.gateways {
		health, minResponseTime := gw.HealthCheck(context.Background())
		if health {
			fmt.Printf("[health] %v UP (%v)ms\n", gw.GetType(), minResponseTime)
			m.statuses[gw.GetType()] = &status{healthy: true, minResponseTime: minResponseTime, lastCheck: time.Now()}
		} else {
			fmt.Printf("[health] %v DOWN (%v)ms\n", gw.GetType(), minResponseTime)
			m.statuses[gw.GetType()] = &status{healthy: false, minResponseTime: minResponseTime, lastCheck: time.Now()}
		}
	}

	var theBest PaymentGateway
	def := m.statuses[entities.DefaultGateway]
	fb := m.statuses[entities.FallbackGateway]

	if fb.healthy && def.healthy && float64(def.minResponseTime) >= 1.9*float64(fb.minResponseTime) {
		theBest = m.gateways[entities.FallbackGateway]
	}

	if theBest == nil && def.healthy {
		theBest = m.gateways[entities.DefaultGateway]
	}

	if theBest == nil && fb.healthy {
		theBest = m.gateways[entities.FallbackGateway]
	}

	if theBest == nil {
		err := m.cache.Set(context.Background(), "the_best_gw", "", 0).Err()
		if err != nil {
			fmt.Printf("[ERROR] Failed to update the best gateway in cache: %v\n", err)
		}
	} else {
		err := m.cache.Set(context.Background(), "the_best_gw", fmt.Sprint(theBest.GetType()), 0).Err()
		if err != nil {
			fmt.Printf("[ERROR] Failed to update the best gateway in cache: %v\n", err)
		}
	}

}

func (m *GatewayManager) GetTheBest() PaymentGateway {
	theBest := m.cache.Get(context.Background(), "the_best_gw").Val()
	for _, gw := range m.gateways {
		if fmt.Sprint(gw.GetType()) == theBest {
			return gw
		}
	}
	return nil
}
