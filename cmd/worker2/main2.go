package main

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"os"
	"os/signal"
	"payment-proxy/internal/payments"
	"payment-proxy/internal/payments/entities"
	"sync"
	"syscall"
	"time"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db := payments.NewInMemoryPaymentDB()

	paymentChan := make(chan entities.Payment, 1000) // buffer razoável para não travar

	// Worker único (0,2 CPU limitado), processa pagamentos
	go func() {
		for payment := range paymentChan {
			// Salva no DB reaproveitando o pool internamente
			db.Save(ctx, &payment)
			// Simula envio para gateway
			processPaymentGateway(payment)
		}
	}()

	// UDP server para receber pacotes
	addr, _ := net.ResolveUDPAddr("udp", ":9000")
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatalf("Erro ao iniciar UDP server: %v", err)
	}
	defer conn.Close()

	// Buffer e pool para reduzir alocação
	var bufferPool = sync.Pool{
		New: func() interface{} { return make([]byte, 8192) },
	}

	for {
		buf := bufferPool.Get().([]byte)
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Printf("Erro ao ler pacote UDP: %v", err)
			bufferPool.Put(buf)
			continue
		}

		var req struct {
			Action  string           `json:"action"`
			Payment entities.Payment `json:"payment"`
			From    *time.Time       `json:"from"`
			To      *time.Time       `json:"to"`
		}

		if err := json.Unmarshal(buf[:n], &req); err != nil {
			log.Printf("Erro ao parsear JSON: %v", err)
			bufferPool.Put(buf)
			continue
		}

		bufferPool.Put(buf)

		switch req.Action {
		case "insert":
			select {
			case paymentChan <- req.Payment:
			default:
				log.Println("Fila cheia, descartando pagamento")
			}
		case "get":
			summary, err := db.GetByDateRange(ctx, req.From, req.To)
			if err != nil {
				log.Printf("Erro na consulta: %v", err)
				continue
			}
			resp, _ := json.Marshal(summary)
			conn.WriteToUDP(resp, remoteAddr)
		case "purge":
			db.Purge(ctx)
		default:
			log.Printf("Ação desconhecida: %s", req.Action)
		}
	}
}

func processPaymentGateway(payment entities.Payment) {
	// Simule aqui sua lógica real de gateway com rate limit, retries etc
	// Para não travar a CPU, faça tudo síncrono e leve
	time.Sleep(10 * time.Millisecond)
}
