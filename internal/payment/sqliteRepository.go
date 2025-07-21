package payment

import (
	"database/sql"
	"payment-proxy/internal/payment/entities"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type SQLiteRepository struct {
	db *sql.DB
	mu sync.Mutex
}

func NewSQLiteRepository(dataSourceName string) (*SQLiteRepository, error) {
	db, err := sql.Open("sqlite3", dataSourceName)
	if err != nil {
		return nil, err
	}
	// Cria a tabela se não existir
	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS payments (
            correlation_id TEXT PRIMARY KEY,
            amount REAL,
            requested_at TEXT,
            payment_gateway_type INTEGER
        )
    `)
	if err != nil {
		return nil, err
	}
	return &SQLiteRepository{db: db}, nil
}

func (r *SQLiteRepository) Save(p entities.Payment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, err := r.db.Exec(`
        INSERT OR REPLACE INTO payments (correlation_id, amount, requested_at, payment_gateway_type)
        VALUES (?, ?, ?, ?)
    `, p.CorrelationID, p.Amount, p.RequestedAt.Format("2006-01-02T15:04:05.000Z"), int(p.PaymentGatewayType))
	return err
}

func (r *SQLiteRepository) Get(correlationID string) (entities.Payment, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row := r.db.QueryRow(`
        SELECT correlation_id, amount, requested_at, payment_gateway_type
        FROM payments WHERE correlation_id = ?
    `, correlationID)

	var p entities.Payment
	var requestedAt string
	var gatewayType int
	err := row.Scan(&p.CorrelationID, &p.Amount, &requestedAt, &gatewayType)
	if err != nil {
		return p, false
	}
	p.RequestedAt, _ = parseTime(requestedAt)
	p.PaymentGatewayType = entities.GatewayType(gatewayType)
	return p, true
}

func (r *SQLiteRepository) GetAll() []entities.Payment {
	r.mu.Lock()
	defer r.mu.Unlock()
	rows, err := r.db.Query(`
        SELECT correlation_id, amount, requested_at, payment_gateway_type
        FROM payments
    `)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var payments []entities.Payment
	for rows.Next() {
		var p entities.Payment
		var requestedAt string
		var gatewayType int
		if err := rows.Scan(&p.CorrelationID, &p.Amount, &requestedAt, &gatewayType); err == nil {
			p.RequestedAt, _ = parseTime(requestedAt)
			p.PaymentGatewayType = entities.GatewayType(gatewayType)
			payments = append(payments, p)
		}
	}
	return payments
}

func (r *SQLiteRepository) Purge() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.db.Exec(`DELETE FROM payments`)
}

func parseTime(s string) (t time.Time, err error) {
	parsed, err := time.Parse("2006-01-02T15:04:05.000Z", s)
	if err != nil {
		return t, err
	}
	return parsed, nil
}
