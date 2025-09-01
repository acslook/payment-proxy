package infra

import (
	"context"
	"fmt"
	"log"
	"payment-proxy/internal/payment_processor"
	"payment-proxy/internal/payments"
	"payment-proxy/internal/payments/entities"
	"runtime"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// PaymentsQueue gerencia consumo e retry de pagamentos
type PaymentsQueue struct {
	redisClient    *redis.Client
	service        *payments.Service
	gatewayManager *payment_processor.GatewayManager

	// canais internos (não usar ponteiro para canal)
	paymentChan chan entities.Payment
	retryChan   chan retryJob

	// sync
	wg     sync.WaitGroup
	once   sync.Once
	closed int32
}

type retryJob struct {
	payment  entities.Payment
	attempts int
}

// Configuráveis
const (
	defaultPaymentChanBuf = 50000
	defaultRetryBuf       = 16384
	maxRetries            = 10000
	baseRetryDelay        = 100 * time.Millisecond
)

// NewPaymentQueue cria uma PaymentsQueue pronta para StartConsumer
func NewPaymentQueue(ctx context.Context, service *payments.Service, gatewayManager *payment_processor.GatewayManager) *PaymentsQueue {
	return &PaymentsQueue{
		service:        service,
		gatewayManager: gatewayManager,
		paymentChan:    make(chan entities.Payment, defaultPaymentChanBuf),
		retryChan:      make(chan retryJob, defaultRetryBuf),
	}
}

// StartConsumer inicia workers que processam pagamentos vindos de inputChan.
// inputChan normalmente é o canal que recebe pagamentos (ex: do UDP listener).
func (q *PaymentsQueue) StartConsumer(ctx context.Context, inputChan <-chan entities.Payment) {
	numWorkers := runtime.NumCPU() * 4
	log.Printf("[INFO] Starting PaymentsQueue with %d workers", numWorkers)

	// start workers
	for i := 0; i < numWorkers; i++ {
		q.wg.Add(1)
		go q.startWorker(ctx, i, inputChan)
	}

	// // start a small number of retry workers (can reuse same workers, but separamos para controle)
	// q.wg.Add(1)
	// go q.retryWorker(ctx)
}

// startWorker lê tanto de inputChan (novos) quanto de retryChan e processa
func (q *PaymentsQueue) startWorker(ctx context.Context, id int, inputChan <-chan entities.Payment) {
	defer q.wg.Done()
	log.Printf("[worker %d] started", id)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[worker %d] ctx done, exiting", id)
			return

		case p, ok := <-inputChan:
			if !ok {
				// input channel fechado; drain retry until ctx done
				log.Printf("[worker %d] input channel closed", id)
				return
			}
			q.processWithRetry(ctx, p, 0)

		case r, ok := <-q.retryChan:
			if !ok {
				// retry channel fechado
				return
			}
			q.processWithRetry(ctx, r.payment, r.attempts)
		}
	}
}

// processWithRetry tenta processar o pagamento e, em caso de falha, agenda retry com backoff
func (q *PaymentsQueue) processWithRetry(ctx context.Context, p entities.Payment, attempts int) {
	// obter gateway
	gateway := q.gatewayManager.GetTheBest()
	if gateway == nil {
		// sem gateway disponível -> requeue com backoff imediato
		q.enqueueRetry(p, attempts)
		return
	}

	_, err := q.service.ProcessPayment(ctx, gateway, p)
	if err != nil {
		// se falhar, schedule retry
		log.Printf("[WARN] process payment failed (attempt %d) CorrelationID=%s err=%v", attempts+1, p.CorrelationID, err)
		q.enqueueRetry(p, attempts)
		return
	}

	// sucesso
	// opcional: métricas aqui
}

// enqueueRetry coloca o job no retryChan com attempts+1 usando backoff não-bloqueante
func (q *PaymentsQueue) enqueueRetry(p entities.Payment, attempts int) {
	attempts++
	// if attempts > maxRetries {
	// 	log.Printf("[ERROR] max retries reached for payment %s — dropping", p.CorrelationID)
	// 	return
	// }

	// calcula delay exponencial
	//delay := time.Duration(1<<uint(attempts-1)) * baseRetryDelay
	delay := baseRetryDelay

	// Para não bloquear o caller, tentamos enfileirar com goroutine que aguarda o delay
	select {
	case q.retryChan <- retryJob{payment: p, attempts: attempts}:
		// enfileirou imediatamente — sem delay (já será processado conforme ordem).
		// Podemos optar por não fazer isto e sempre esperar, mas manter simples aqui.
	default:
		// quando retryChan estiver cheio, enfileiramos com espera assíncrona
		go func(p entities.Payment, attempts int, delay time.Duration) {
			time.Sleep(delay)
			// tentativa de colocar no canal (bloqueia se estiver cheio)
			select {
			case q.retryChan <- retryJob{payment: p, attempts: attempts}:
				// ok
			default:
				// se ainda estiver cheio, aguarda por um curto período e retenta, para evitar perder
				select {
				case q.retryChan <- retryJob{payment: p, attempts: attempts}:
				case <-time.After(200 * time.Millisecond):
					log.Printf("[ERROR] retryChan full, dropping payment %s after wait", p.CorrelationID)
				}
			}
		}(p, attempts, delay)
	}
}

// retryWorker apenas consome retryChan e re-enfileira no fluxo de processamento (a startWorker já lê retryChan)
// Aqui nos limitamos a manter o canal ativo e visível; poderia agregar métricas/ordenar por timestamp
// func (q *PaymentsQueue) retryWorker(ctx context.Context) {
// 	defer q.wg.Done()
// 	log.Printf("[retry worker] started")
// 	<-ctx.Done()
// 	log.Printf("[retry worker] ctx done, exiting")
// }

// ClearQueue esvazia os canais (drain) de forma segura. NÃO recria canais.
func (q *PaymentsQueue) ClearQueue() {
	// Drain paymentChan
	for {
		select {
		case <-q.paymentChan:
			// descarte
		default:
			goto drainedPayments
		}
	}
drainedPayments:

	// Drain retryChan
	for {
		select {
		case <-q.retryChan:
			// descarte
		default:
			break
		}
		// safety break (redundante)
		if len(q.retryChan) == 0 {
			break
		}
	}
	log.Printf("[info] queues cleared (paymentChan len=%d retryChan len=%d)", len(q.paymentChan), len(q.retryChan))
}

// Stop cancela os workers (assumindo que o ctx passado a StartConsumer será cancelado) e espera wg
// Se você quiser um Stop explícito sem depender do ctx, pode implementá-lo para fechar channels e aguardar wg.
func (q *PaymentsQueue) Stop() {
	q.once.Do(func() {
		// fechar canais pode ser perigoso se houver produtores externos; aqui assumimos produtores externos (UDP)
		// preferimos somente aguardar wg se o contexto externo for cancelado.
	})
}

// Expor um helper para que outros componentes possam enviar payments ao queue interno de forma segura
// (por exemplo, do UDP listener)
func (q *PaymentsQueue) Enqueue(p entities.Payment) error {
	select {
	case q.paymentChan <- p:
		return nil
	default:
		// canal cheio: podemos optar por bloquear por um curto timeout, ou retornar erro
		select {
		case q.paymentChan <- p:
			return nil
		case <-time.After(100 * time.Millisecond):
			return fmt.Errorf("payment queue full")
		}
	}
}
