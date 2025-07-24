package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"payment-proxy/internal/infra"
	"payment-proxy/internal/payment_processor"
	"payment-proxy/internal/payments"
	"payment-proxy/internal/payments/handlers"
	"payment-proxy/internal/redis"
	"syscall"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func main() {
	// Carregar contexto e configurar graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	repo, err := payments.NewPaymentPostgresRepository(ctx)
	if err != nil {
		slog.Error("failed to connect to PostgreSQL", "error", err)
		panic(err)
	}
	defer repo.Close()

	service := payments.NewPaymentService(repo)

	redisClient := redis.NewClient()
	gatewayManager := payment_processor.NewGatewayManager(redisClient)
	redisQueue := infra.NewPaymentQueue(ctx, redisClient.Client, service, gatewayManager)

	createPaymentHandler := handlers.NewCreatePaymentHandler(redisQueue)
	getPaymentsSummaryHandler := handlers.NewGetSummaryHandler(service)

	//Configure echo server
	e := echo.New()

	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	e.POST("/payments", createPaymentHandler.Handle)
	e.GET("/payments-summary", getPaymentsSummaryHandler.Handle)
	e.POST("/purge-payments", func(c echo.Context) error {
		repo.Purge(context.Background())
		redisQueue.ClearStream(context.Background())
		return c.JSON(http.StatusOK, map[string]string{"message": "payments purged"})
	})

	if err := e.Start(":9999"); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("failed to start server", "error", err)
	}
}
