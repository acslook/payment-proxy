package payment

import (
	"context"
	"encoding/json"
	"fmt"
	"payment-proxy/internal/payment/entities"

	"github.com/redis/go-redis/v9"
)

type PaymentRedisRepository struct {
	client *redis.Client
	ctx    context.Context
}

func NewPaymentRedisRepository(addr string) *PaymentRedisRepository {
	rdb := redis.NewClient(&redis.Options{
		Addr: addr,
	})
	return &PaymentRedisRepository{
		client: rdb,
		ctx:    context.Background(),
	}
}

func (r *PaymentRedisRepository) Save(payment entities.Payment) error {
	data, err := json.Marshal(payment)
	if err != nil {
		return err
	}

	key := fmt.Sprintf("payment:%s", payment.CorrelationID)

	// Salva os dados como JSON em uma string
	if err := r.client.Set(r.ctx, key, data, 0).Err(); err != nil {
		return err
	}

	// Indexa por data no ZSET
	score := float64(payment.RequestedAt.Unix())

	if err := r.client.ZAdd(r.ctx, "payment:index_by_date", redis.Z{
		Score:  score,
		Member: payment.CorrelationID,
	}).Err(); err != nil {
		return err
	}

	return nil
}

func (r *PaymentRedisRepository) Get(correlationID string) (entities.Payment, bool) {
	key := fmt.Sprintf("payment:%s", correlationID)

	data, err := r.client.Get(r.ctx, key).Result()
	if err == redis.Nil {
		return entities.Payment{}, false
	}
	if err != nil {
		return entities.Payment{}, false
	}

	var payment entities.Payment
	if err := json.Unmarshal([]byte(data), &payment); err != nil {
		return entities.Payment{}, false
	}
	return payment, true
}

func (r *PaymentRedisRepository) GetAll() []entities.Payment {
	var payments []entities.Payment

	ids, err := r.client.ZRange(r.ctx, "payment:index_by_date", 0, -1).Result()
	if err != nil {
		return payments
	}

	for _, id := range ids {
		payment, ok := r.Get(id)
		if ok {
			payments = append(payments, payment)
		}
	}

	return payments
}

func (r *PaymentRedisRepository) Purge() {
	if err := r.client.FlushDB(r.ctx).Err(); err != nil {
		fmt.Println("Error purging Redis database:", err)
	}
}
