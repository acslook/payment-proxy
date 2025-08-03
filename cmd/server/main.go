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
	"time"

	"github.com/NYTimes/gziphandler"
)

func main() {
	// Graceful shutdown
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

	// Roteador http
	mux := http.NewServeMux()

	mux.HandleFunc("/payments", createPaymentHandler.Handle)

	mux.HandleFunc("/payments-summary", getPaymentsSummaryHandler.Handle)

	mux.HandleFunc("/purge-payments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		repo.Purge(r.Context())
		redisQueue.ClearStream(r.Context())

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message": "payments purged"}`))
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status": "ok"}`))
	})

	server := &http.Server{
		Addr:    ":9999",
		Handler: gziphandler.GzipHandler(recoverMiddleware(mux)),
	}

	go func() {
		slog.Info("server started on :9999")
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("failed to start server", "error", err)
		}
	}()

	<-ctx.Done()
	stop()
	slog.Info("shutting down server...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server.Shutdown(shutdownCtx)
}

func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered", "error", rec)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
