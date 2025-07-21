package payment

import (
	"context"
	"fmt"
	"os"
	"payment-proxy/internal/payment/entities"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisQueue struct {
	redisClient    *redis.Client
	service        *Service
	gatewayManager *GatewayManager
}

const (
	PaymentStream = "payment_stream"
	PaymentGroup  = "payment_group"
)

type Job struct {
	ID     string
	Values map[string]interface{}
}

func NewRedisQueue(ctx context.Context, redisClient *redis.Client, service *Service, gatewayManager *GatewayManager) *RedisQueue {
	redisQ := &RedisQueue{
		redisClient:    redisClient,
		service:        service,
		gatewayManager: gatewayManager,
	}

	go redisQ.Start()
	return redisQ
}

func (q *RedisQueue) Start() {
	q.ClearStream(context.Background())

	// Config
	numConsumers := 5
	numWorkers := runtime.NumCPU()
	fmt.Printf("[INFO] Starting Redis Queue with %d workers\n", numWorkers)

	// Canal de jobs
	jobChan := make(chan Job, 1000)

	// Workers
	var wg sync.WaitGroup
	for i := 1; i <= numWorkers; i++ {
		wg.Add(1)
		go q.startWorker(context.Background(), jobChan, i, q.redisClient, PaymentStream, PaymentGroup, &wg)
	}

	// Consumer
	hostname, err := os.Hostname()
	if err != nil {
		fmt.Println("Erro ao obter hostname:", err)
		return
	}
	for i := 1; i <= numConsumers; i++ {
		go q.startConsumer(context.Background(), q.redisClient, fmt.Sprintf("consumer-%d-%s", i, hostname), PaymentStream, PaymentGroup, jobChan)
	}

	// Reclaimer
	//go startReclaimer(q.redisClient, PaymentStream, PaymentGroup, jobChan)

	wg.Wait()
}

func (q *RedisQueue) startConsumer(ctx context.Context, rdb *redis.Client, consumerName, stream, group string, jobChan chan<- Job) {
	for {
		gateway := q.gatewayManager.GetTheBest()
		if gateway == nil {
			continue
		}

		entries, err := rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    group,
			Consumer: consumerName,
			Streams:  []string{stream, ">"},
			Block:    2 * time.Second,
			Count:    100,
		}).Result()

		if err != nil && err != redis.Nil {
			continue
		}

		for _, entry := range entries {
			for _, msg := range entry.Messages {
				jobChan <- Job{ID: msg.ID, Values: msg.Values}
			}
		}
	}
}

func (q *RedisQueue) startWorker(ctx context.Context, jobChan <-chan Job, id int, rdb *redis.Client, stream, group string, wg *sync.WaitGroup) {
	defer wg.Done()
	for job := range jobChan {
		fmt.Printf("[INFO][Worker-%d] Processando %s: %v\n", id, job.ID, job.Values)
		payment, _ := mapToPayment(job.Values)
		gateway := q.gatewayManager.GetTheBest()
		if gateway == nil {
			fmt.Println("[ERROR] No healthy gateways available")
			rdb.XAck(ctx, stream, group, job.ID)
			q.Enqueue(ctx, payment)
			continue
		}

		_, err := q.service.ProcessPayment(ctx, gateway, payment)
		if err != nil {
			fmt.Printf("[ERROR] Failed to process payment %v: %v\n", payment.CorrelationID, err)
			rdb.XAck(ctx, stream, group, job.ID)
			q.Enqueue(ctx, payment)
			continue
		}
		fmt.Printf("[INFO] Payment processed successfully: %v in Gateway:%v\n", payment.CorrelationID, payment.PaymentGatewayType)
	}
}

// func startReclaimer(rdb *redis.Client, stream, group string, jobChan chan<- Job) {
// 	for {
// 		entries, _ := rdb.XPendingExt(context.Background(), &redis.XPendingExtArgs{
// 			Stream: stream,
// 			Group:  group,
// 			Start:  "-",
// 			End:    "+",
// 			Count:  20,
// 		}).Result()

// 		for _, entry := range entries {
// 			if entry.Idle >= 10*time.Second {
// 				msgs, _ := rdb.XClaim(context.Background(), &redis.XClaimArgs{
// 					Stream:   stream,
// 					Group:    group,
// 					Consumer: "reclaimer",
// 					MinIdle:  10 * time.Second,
// 					Messages: []string{entry.ID},
// 				}).Result()

// 				for _, msg := range msgs {
// 					fmt.Printf("[Reclaim] Reprocessando: %s\n", msg.ID)
// 					jobChan <- Job{ID: msg.ID, Values: msg.Values}
// 				}
// 			}
// 		}
// 		time.Sleep(5 * time.Second)
// 	}
// }

func (q *RedisQueue) Enqueue(ctx context.Context, payment entities.Payment) {
	result := q.redisClient.XAdd(ctx, &redis.XAddArgs{
		Stream: PaymentStream,
		Values: structToMap(payment),
	})
	fmt.Println("[INFO] Enqueued payment:", result)
}

func (q *RedisQueue) ClearStream(ctx context.Context) {
	err := q.redisClient.Del(ctx, PaymentStream).Err()
	if err != nil {
		fmt.Printf("[ERROR] Failed to clear stream %s: %v\n", PaymentStream, err)
	} else {
		fmt.Printf("[INFO] Cleared stream %s\n", PaymentStream)
	}
	_ = q.redisClient.XGroupCreateMkStream(ctx, PaymentStream, PaymentGroup, "$")
}

func structToMap(v entities.Payment) map[string]interface{} {
	m := map[string]interface{}{
		"correlationId":      v.CorrelationID,
		"amount":             v.Amount,
		"requestedAt":        v.RequestedAt.Format(time.RFC3339),
		"paymentGatewayType": int(v.PaymentGatewayType),
	}

	return m
}

func mapToPayment(data map[string]interface{}) (entities.Payment, error) {
	var p entities.Payment

	// CorrelationID
	if v, ok := data["correlationId"]; ok {
		p.CorrelationID = fmt.Sprintf("%v", v)
	}

	// Amount
	if v, ok := data["amount"]; ok {
		switch val := v.(type) {
		case float64:
			p.Amount = val
		case string:
			if f, err := strconv.ParseFloat(val, 64); err == nil {
				p.Amount = f
			}
		}
	}

	// RequestedAt
	if v, ok := data["requestedAt"]; ok {
		if s, ok := v.(string); ok {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				p.RequestedAt = t
			}
		}
	}

	// PaymentGatewayType
	if v, ok := data["paymentGatewayType"]; ok {
		val, _ := strconv.Atoi(fmt.Sprintf("%v", v))
		p.PaymentGatewayType = entities.GatewayType(val)
	}

	return p, nil
}
