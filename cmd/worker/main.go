package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"payment-proxy/internal/infra"
	"payment-proxy/internal/payment_processor"
	"payment-proxy/internal/payments"
	"payment-proxy/internal/payments/entities"
	"sync"
	"syscall"
	"time"

	jsoniter "github.com/json-iterator/go"
)

var json = jsoniter.ConfigFastest

// Configs ajustáveis
const (
	udpPort          = ":9000"
	udpReadTimeout   = 1 * time.Second
	paymentChanBuf   = 50000
	workerMultiplier = 2                      // workers = NumCPU * workerMultiplier
	batchSize        = 500                    // flush batch quando atingir
	batchMaxWait     = 200 * time.Millisecond // flush batch por timeout
	maxUDPPacketSize = 8192
	dropIfQueueFull  = false // se true, descarta pagamento quando channel cheio (evita bloquear UDP loop)
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	gatewayManager := payment_processor.NewGatewayManager()

	repo := payments.NewInMemoryPaymentDB()
	service := payments.NewPaymentService(repo)
	redisQueue := infra.NewPaymentQueue(ctx, service, gatewayManager)

	// start external consumers (seu código)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				gatewayManager.MonitorHealth()
			}
		}
	}()

	// start redis queue consumer (this will push to paymentChan)
	paymentChan := make(chan entities.Payment, paymentChanBuf)
	go redisQueue.StartConsumer(ctx, paymentChan)

	// UDP server
	addr, err := net.ResolveUDPAddr("udp", udpPort)
	if err != nil {
		log.Fatalf("Erro ao resolver endereço UDP: %v", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatalf("Erro ao iniciar servidor UDP: %v", err)
	}
	defer conn.Close()

	log.Println("Servidor UDP escutando na porta", udpPort)

	// Pool de buffers para leitura UDP
	var bufferPool = sync.Pool{
		New: func() interface{} {
			return make([]byte, maxUDPPacketSize)
		},
	}

	for {
		// Permite sair do loop quando ctx.Done() for fechado
		select {
		case <-ctx.Done():
			log.Println("context canceled, shutting down UDP loop")
			return
		default:
		}

		buf := bufferPool.Get().([]byte)
		_ = conn.SetReadDeadline(time.Now().Add(udpReadTimeout))
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			// se for timeout, apenas continue para checar ctx.Done()
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				bufferPool.Put(buf)
				continue
			}
			log.Printf("Erro ao ler pacote UDP: %v", err)
			bufferPool.Put(buf)
			continue
		}

		// Decodifica mensagem (não alocar além do necessário)
		var req PaymentRequest
		if err := json.Unmarshal(buf[:n], &req); err != nil {
			log.Printf("Erro ao parsear JSON: %v", err)
			bufferPool.Put(buf)
			continue
		}
		bufferPool.Put(buf)

		switch req.Action {
		case "insert":
			// Não bloquear o loop UDP: tenta enviar no channel sem bloquear
			select {
			case paymentChan <- req.Payment:
				// ok
			default:
				// canal cheio
				if dropIfQueueFull {
					// opcional: contabilizar dropped
					log.Println("payment channel full: dropping payment")
				} else {
					// bloqueia (com timeout) para evitar perder mensagens
					select {
					case paymentChan <- req.Payment:
					case <-time.After(100 * time.Millisecond):
						log.Println("payment channel still full after wait: dropping")
					}
				}
			}

		case "get":
			// Responder em goroutine para não travar leitura UDP
			go func(remote *net.UDPAddr, from, to *time.Time) {
				results, err := repo.GetByDateRange(context.Background(), from, to)
				if err != nil {
					log.Printf("Erro ao consultar repo: %v", err)
					return
				}
				respBytes, err := json.Marshal(results)
				if err != nil {
					log.Printf("Erro ao serializar resposta: %v", err)
					return
				}
				if _, err := conn.WriteToUDP(respBytes, remote); err != nil {
					log.Printf("Erro ao enviar resposta UDP: %v", err)
				}
			}(remoteAddr, req.From, req.To)

		case "purge":
			// Operação leve — pode executar synchronously
			repo.Purge(context.Background())
			redisQueue.ClearQueue()
		default:
			log.Printf("Ação desconhecida: %s", req.Action)
		}
	}
}

// PaymentRequest - estrutura para comunicação UDP (mantive do seu original)
type PaymentRequest struct {
	Action  string           `json:"action"`  // "insert" ou "get"
	Payment entities.Payment `json:"payment"` // usado apenas se Action == "insert"
	From    *time.Time       `json:"from"`    // usado apenas se Action == "get"
	To      *time.Time       `json:"to"`      // usado apenas se Action == "get"
}
