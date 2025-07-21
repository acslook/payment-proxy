package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"payment-proxy/internal/payment"
	"payment-proxy/internal/payment/entities"
	"payment-proxy/internal/payment/handlers"
	"syscall"

	"github.com/go-redsync/redsync/v4"
	redsync_redis "github.com/go-redsync/redsync/v4/redis/goredis/v9"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/redis/go-redis/v9"
)

func main() {
	// Carregar contexto e configurar graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	connString := os.Getenv("CONN_STRING")
	if connString == "" {
		log.Fatal("CONN_STRING not defined")
	}

	dbpool, err := pgxpool.New(ctx, connString)
	if err != nil {
		log.Fatalf("failed to connect to PostgreSQL: %v", err)
	}
	defer dbpool.Close()

	repo, err := payment.NewPaymentPostgresRepository(dbpool)
	if err != nil {
		slog.Error("failed to connect to PostgreSQL", "error", err)
		panic(err)
	}

	gatewayDefaultUrl := os.Getenv("GATEWAY_DEFAULT_URL")
	if gatewayDefaultUrl == "" {
		log.Fatal("GATEWAY_DEFAULT_URL not defined")
	}
	gatewayDefault := payment.NewPaymentGateway(gatewayDefaultUrl, entities.DefaultGateway, 0.05)
	gatewayFallbackUrl := os.Getenv("GATEWAY_FALLBACK_URL")
	if gatewayDefaultUrl == "" {
		log.Fatal("GATEWAY_FALLBACK_URL not defined")
	}
	gatewayFallback := payment.NewPaymentGateway(gatewayFallbackUrl, entities.FallbackGateway, 0.15)

	service := payment.NewPaymentService(repo)

	redisClient := QueueConfig(ctx)
	pool := redsync_redis.NewPool(redisClient)
	redsyncLock := redsync.New(pool)

	gatewayManager := payment.NewGatewayManager(redisClient, redsyncLock, gatewayDefault, gatewayFallback)

	redisQueue := payment.NewRedisQueue(ctx, redisClient, service, gatewayManager)

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

func QueueConfig(ctx context.Context) *redis.Client {
	redisUrl := os.Getenv("REDIS_URL")
	if redisUrl == "" {
		log.Fatal("CONN_STRING not defined")
	}
	rdb := redis.NewClient(&redis.Options{
		Addr: redisUrl,
	})

	return rdb
}
