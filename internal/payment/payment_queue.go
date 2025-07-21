package payment

import (
	"context"
	"fmt"
	"payment-proxy/internal/payment/entities"
	"sync"
)

type PaymentQueue struct {
	queue          []entities.Payment
	service        *Service
	mutex          sync.Mutex
	cond           *sync.Cond
	gatewayManager *GatewayManager
}

func NewPaymentQueue(service *Service, gatewayManager *GatewayManager) *PaymentQueue {
	paymentQueue := &PaymentQueue{
		queue:          make([]entities.Payment, 0),
		service:        service,
		gatewayManager: gatewayManager,
	}
	paymentQueue.cond = sync.NewCond(&paymentQueue.mutex)
	go paymentQueue.start()
	return paymentQueue
}

func (pq *PaymentQueue) Enqueue(payment entities.Payment) {
	pq.mutex.Lock()
	pq.queue = append(pq.queue, payment)
	pq.cond.Signal()
	pq.mutex.Unlock()
}

func (pq *PaymentQueue) start() {
	for {
		pq.mutex.Lock()
		for len(pq.queue) == 0 {
			pq.cond.Wait()
		}

		gateway := pq.gatewayManager.GetTheBest()
		if gateway == nil {
			pq.mutex.Unlock()
			continue
		}

		payment := pq.queue[0]
		pq.queue = pq.queue[1:]
		pq.mutex.Unlock()
		go pq.ProcessPaymentAsync(gateway, payment)
	}
}

func (pq *PaymentQueue) ProcessPaymentAsync(gateway PaymentGateway, payment entities.Payment) {
	payment, err := pq.service.ProcessPayment(context.Background(), gateway, payment)
	if err != nil {
		fmt.Printf("[ERROR] Failed to process payment %v: %v\n", payment.CorrelationID, err)
		pq.Enqueue(payment) // Re-enqueue the payment if processing fails
	}
	fmt.Printf("[INFO] Payment processed successfully: %v in Gateway:%v\n", payment.CorrelationID, payment.PaymentGatewayType)
}
