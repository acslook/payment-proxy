package payments

import (
	"context"
	"hash/fnv"
	"payment-proxy/internal/payments/entities"
	"runtime"
	"sync"
	"time"
)

const shardCount = 256

type paymentShard struct {
	sync.RWMutex
	store map[string]*entities.Payment
}

type InMemoryPaymentDB struct {
	shards [shardCount]*paymentShard
}

func NewInMemoryPaymentDB() *InMemoryPaymentDB {
	db := &InMemoryPaymentDB{}
	for i := 0; i < shardCount; i++ {
		db.shards[i] = &paymentShard{
			store: make(map[string]*entities.Payment, 1024), // capacidade inicial evita realocação
		}
	}
	return db
}

func (db *InMemoryPaymentDB) getShard(key string) *paymentShard {
	h := fnv.New32a()
	h.Write([]byte(key))
	return db.shards[h.Sum32()%shardCount]
}

// Save insere ou atualiza o pagamento no shard correspondente
func (db *InMemoryPaymentDB) Save(ctx context.Context, payment *entities.Payment) {
	shard := db.getShard(payment.CorrelationID)
	shard.Lock()
	shard.store[payment.CorrelationID] = payment
	shard.Unlock()
}

// GetByDateRange otimizado com paralelismo adaptativo
func (db *InMemoryPaymentDB) GetByDateRange(ctx context.Context, from, to *time.Time) (entities.AggregatedSummary, error) {
	var summary entities.AggregatedSummary
	var mu sync.Mutex

	// Detecta se vale a pena paralelizar
	totalRecords := 0
	for _, shard := range db.shards {
		shard.RLock()
		totalRecords += len(shard.store)
		shard.RUnlock()
	}

	// Caso pequeno → sem goroutines para evitar overhead
	if totalRecords < 10_000 {
		for _, shard := range db.shards {
			summary = aggregateShard(shard, summary, from, to)
		}
		return summary, nil
	}

	// Caso grande → paraleliza com limite baseado em CPUs
	workerLimit := runtime.NumCPU()
	wg := sync.WaitGroup{}
	wg.Add(shardCount)

	sem := make(chan struct{}, workerLimit)

	for _, shard := range db.shards {
		shard := shard
		go func() {
			defer wg.Done()
			sem <- struct{}{} // reserva slot
			localSummary := entities.AggregatedSummary{}
			localSummary = aggregateShard(shard, localSummary, from, to)
			mu.Lock()
			summary.Default.TotalAmount += localSummary.Default.TotalAmount
			summary.Default.TotalRequests += localSummary.Default.TotalRequests
			summary.Fallback.TotalAmount += localSummary.Fallback.TotalAmount
			summary.Fallback.TotalRequests += localSummary.Fallback.TotalRequests
			mu.Unlock()
			<-sem // libera slot
		}()
	}

	wg.Wait()
	return summary, nil
}

func aggregateShard(s *paymentShard, acc entities.AggregatedSummary, from, to *time.Time) entities.AggregatedSummary {
	s.RLock()
	defer s.RUnlock()

	for _, payment := range s.store {
		if from != nil && payment.RequestedAt.Before(*from) {
			continue
		}
		if to != nil && payment.RequestedAt.After(*to) {
			continue
		}
		if payment.PaymentGatewayType == entities.DefaultGateway {
			acc.Default.TotalAmount += payment.Amount
			acc.Default.TotalRequests++
		} else {
			acc.Fallback.TotalAmount += payment.Amount
			acc.Fallback.TotalRequests++
		}
	}
	return acc
}

// Purge sem realocação de mapa
func (db *InMemoryPaymentDB) Purge(ctx context.Context) {
	for _, shard := range db.shards {
		shard.Lock()
		for k := range shard.store {
			delete(shard.store, k)
		}
		shard.Unlock()
	}
}
