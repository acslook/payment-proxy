package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"payment-proxy/internal/infra"
	"payment-proxy/internal/payment_processor"
	"payment-proxy/internal/payments"
	"payment-proxy/internal/redis"
	"syscall"
	"time"
)

func main() {
	// Carregar contexto e configurar graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	redisClient := redis.NewClient()
	gatewayManager := payment_processor.NewGatewayManager(redisClient)

	repo, err := payments.NewPaymentPostgresRepository(ctx)
	if err != nil {
		slog.Error("failed to connect to PostgreSQL", "error", err)
		panic(err)
	}
	defer repo.Close()

	service := payments.NewPaymentService(repo)
	redisQueue := infra.NewPaymentQueue(ctx, redisClient.Client, service, gatewayManager)

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				gatewayManager.MonitorHealth()
			}
		}
	}()

	redisQueue.StartConsumer()
}
