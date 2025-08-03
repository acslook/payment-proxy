package payment_processor

import (
	"context"
	"fmt"
	"log"
	"os"
	"payment-proxy/internal/payments/entities"
	"strconv"
	"time"

	"payment-proxy/internal/redis"

	"github.com/go-redsync/redsync/v4"
)

type GatewayManager struct {
	redis    *redis.Client
	gateways map[entities.GatewayType]PaymentGateway
}

func NewGatewayManager(redis *redis.Client) *GatewayManager {
	gatewayDefaultUrl := os.Getenv("GATEWAY_DEFAULT_URL")
	if gatewayDefaultUrl == "" {
		log.Fatal("GATEWAY_DEFAULT_URL not defined")
	}
	gatewayDefault := NewPaymentGateway(gatewayDefaultUrl, entities.DefaultGateway)
	gatewayFallbackUrl := os.Getenv("GATEWAY_FALLBACK_URL")
	if gatewayDefaultUrl == "" {
		log.Fatal("GATEWAY_FALLBACK_URL not defined")
	}
	gatewayFallback := NewPaymentGateway(gatewayFallbackUrl, entities.FallbackGateway)

	gatewaysMap := make(map[entities.GatewayType]PaymentGateway)
	gatewaysMap[entities.DefaultGateway] = gatewayDefault
	gatewaysMap[entities.FallbackGateway] = gatewayFallback

	m := &GatewayManager{
		redis:    redis,
		gateways: gatewaysMap,
	}

	return m
}

func (m *GatewayManager) MonitorHealth() {
	ctx := context.Background()
	mutex := m.redis.Lock.NewMutex("heath-check-pp", redsync.WithExpiry(60*time.Second))
	if err := mutex.LockContext(ctx); err != nil {
		return
	}

	for _, gw := range m.gateways {
		health, minResponseTime := gw.HealthCheck(context.Background())
		if health {
			fmt.Printf("[health] %v UP (%v)ms\n", gw.GetType(), minResponseTime)
		} else {
			fmt.Printf("[health] %v DOWN (%v)ms\n", gw.GetType(), minResponseTime)
		}
	}

	var theBest PaymentGateway
	def := m.gateways[entities.DefaultGateway].(*PaymentsGateway)
	fb := m.gateways[entities.FallbackGateway].(*PaymentsGateway)

	if fb.healthy && def.healthy && float64(def.minResponseTime) >= 2*float64(fb.minResponseTime) {
		theBest = m.gateways[entities.FallbackGateway]
	}

	if theBest == nil && def.healthy {
		theBest = m.gateways[entities.DefaultGateway]
	}

	if theBest == nil && fb.healthy {
		theBest = m.gateways[entities.FallbackGateway]
	}

	if theBest == nil {
		err := m.redis.Client.Set(context.Background(), "the_best_gw", "", 0).Err()
		if err != nil {
			fmt.Printf("[ERROR] Failed to update the best gateway in cache: %v\n", err)
		}
	} else {
		err := m.redis.Client.Set(context.Background(), "the_best_gw", fmt.Sprint(theBest.GetType()), 0).Err()
		if err != nil {
			fmt.Printf("[ERROR] Failed to update the best gateway in cache: %v\n", err)
		}
	}

	if ok, err := mutex.UnlockContext(ctx); !ok || err != nil {
		fmt.Println("[ERROR] Unlock error:", err)
	}
}

func (m *GatewayManager) GetTheBest() PaymentGateway {
	theBest := m.redis.Client.Get(context.Background(), "the_best_gw").Val()
	if theBest == "" {
		return nil
	}

	i, err := strconv.Atoi(theBest)
	if err != nil {
		fmt.Printf("[ERROR] Failed to convert the best gateway type from cache: %v\n", err)
		return nil
	}

	gateway, exists := m.gateways[entities.GatewayType(i)]
	if !exists {
		fmt.Printf("[ERROR] Gateway %s not found in the manager\n", theBest)
	}
	return gateway
}
