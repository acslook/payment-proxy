package payments

import (
	"context"
	"fmt"
	"log"
	"os"
	"payment-proxy/internal/payments/entities"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PaymentPostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPaymentPostgresRepository(ctx context.Context) (*PaymentPostgresRepository, error) {
	connString := os.Getenv("CONN_STRING")
	if connString == "" {
		log.Fatal("CONN_STRING not defined")
	}

	dbpool, err := pgxpool.New(ctx, connString)
	if err != nil {
		log.Fatalf("failed to connect to PostgreSQL: %v", err)
	}

	repo := &PaymentPostgresRepository{
		pool: dbpool,
	}

	return repo, nil
}

func (r *PaymentPostgresRepository) Close() {
	if r.pool != nil {
		r.pool.Close()
	}
}

// func (r *PaymentPostgresRepository) createTableIfNotExists() error {
// 	_, err := r.pool.Exec(r.ctx, `
//         CREATE TABLE IF NOT EXISTS payments (
//             id VARCHAR PRIMARY KEY,
//             amount NUMERIC(18,2) NOT NULL,
//             gateway_type INTEGER NOT NULL,
//             requested_at TIMESTAMPTZ NOT NULL
//         )
//     `)
// 	fmt.Println("Table payments created or already exists")
// 	return err
// }

func (r *PaymentPostgresRepository) Save(ctx context.Context, payment entities.Payment) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO payments (correlationId, amount, gateway_type, requested_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (correlationId) DO UPDATE 
		SET amount = EXCLUDED.amount,
		    gateway_type = EXCLUDED.gateway_type,
		    requested_at = EXCLUDED.requested_at
	`, payment.CorrelationID, payment.Amount, payment.PaymentGatewayType, payment.RequestedAt)

	return err
}

func (r *PaymentPostgresRepository) Get(ctx context.Context, correlationID string) (entities.Payment, bool) {
	var p entities.Payment

	err := r.pool.QueryRow(ctx, `
		SELECT correlationId, amount, gateway_type, requested_at
		FROM payments
		WHERE correlationId = $1
	`, correlationID).Scan(&p.CorrelationID, &p.Amount, &p.PaymentGatewayType, &p.RequestedAt)

	if err != nil {
		return entities.Payment{}, false
	}
	return p, true
}

func (r *PaymentPostgresRepository) GetAll(ctx context.Context) []entities.Payment {
	rows, err := r.pool.Query(ctx, `
		SELECT correlationId, amount, gateway_type, requested_at
		FROM payments
		ORDER BY requested_at ASC
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var results []entities.Payment

	for rows.Next() {
		var p entities.Payment

		err := rows.Scan(&p.CorrelationID, &p.Amount, &p.PaymentGatewayType, &p.RequestedAt)
		if err == nil {
			results = append(results, p)
		}
	}

	return results
}

func (r *PaymentPostgresRepository) GetByDateRange(ctx context.Context, from, to *time.Time) (entities.AggregatedSummary, error) {
	query := `
		SELECT
			COUNT(*) FILTER (WHERE gateway_type = 0)  AS default_total_requests,
			COALESCE(SUM(amount) FILTER (WHERE gateway_type = 0), 0) AS default_total_amount,
			COUNT(*) FILTER (WHERE gateway_type = 1) AS fallback_total_requests,
			COALESCE(SUM(amount) FILTER (WHERE gateway_type = 1), 0) AS fallback_total_amount
		FROM payments
		WHERE ($1::timestamptz IS NULL OR requested_at >= $1)
		  AND ($2::timestamptz IS NULL OR requested_at <= $2)
	`

	var summary entities.AggregatedSummary

	err := r.pool.QueryRow(ctx, query, from, to).Scan(
		&summary.Default.TotalRequests,
		&summary.Default.TotalAmount,
		&summary.Fallback.TotalRequests,
		&summary.Fallback.TotalAmount,
	)
	if err != nil {
		return entities.AggregatedSummary{}, err
	}

	return summary, nil
}

func (r *PaymentPostgresRepository) Purge(ctx context.Context) {
	_, err := r.pool.Exec(ctx, `DELETE FROM payments`)
	if err != nil {
		fmt.Printf("[ERROR] Failed to purge payments: %v\n", err)
	}
}
