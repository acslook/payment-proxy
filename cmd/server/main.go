package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"payment-proxy/internal/payments/entities"
	"sync"
	"syscall"
	"time"

	"net"

	jsoniter "github.com/json-iterator/go"
	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/reuseport"
)

// JSON mais rápido
var json = jsoniter.ConfigFastest

// Conexão UDP persistente
var udpConn *net.UDPConn
var udpAddr *net.UDPAddr

// Pool de buffers para reduzir GC
var bufPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 8192)
	},
}

func main() {
	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	initUDP()

	// Router
	requestHandler := func(ctx *fasthttp.RequestCtx) {
		switch string(ctx.Path()) {
		case "/payments":
			handlePayments(ctx)
		case "/payments-summary":
			handlePaymentsSummary(ctx)
		case "/purge-payments":
			handlePurgePayments(ctx)
		case "/health":
			handleHealth(ctx)
		default:
			ctx.SetStatusCode(fasthttp.StatusNotFound)
		}
	}

	// Wrap com recover
	handler := recoverMiddleware(requestHandler)

	// Servidor
	ln, err := reuseport.Listen("tcp4", ":9999")
	if err != nil {
		slog.Error("failed to listen", "error", err)
		return
	}

	server := &fasthttp.Server{
		Handler: handler,
		Name:    "payment-proxy",
	}

	go func() {
		slog.Info("server started on :9999")
		if err := server.Serve(ln); err != nil {
			slog.Error("failed to start server", "error", err)
		}
	}()

	<-ctx.Done()
	stop()
	slog.Info("shutting down server...")

	if err := server.Shutdown(); err != nil {
		slog.Error("error shutting down server", "error", err)
	}
}

func initUDP() {
	var err error
	udpAddr, err = net.ResolveUDPAddr("udp", "172.25.0.12:9000")
	if err != nil {
		log.Fatalf("Erro ao resolver endereço UDP: %v", err)
	}
	udpConn, err = net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		log.Fatalf("Erro ao conectar ao servidor UDP: %v", err)
	}
}

func recoverMiddleware(next fasthttp.RequestHandler) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered", "error", rec)
				ctx.SetStatusCode(fasthttp.StatusInternalServerError)
				ctx.SetBody([]byte("internal server error"))
			}
		}()
		next(ctx)
	}
}

func handlePayments(ctx *fasthttp.RequestCtx) {
	//start := time.Now()
	if !ctx.IsPost() {
		ctx.SetStatusCode(fasthttp.StatusMethodNotAllowed)
		return
	}

	var payment entities.Payment
	if err := json.Unmarshal(ctx.PostBody(), &payment); err != nil {
		ctx.SetContentType("application/json")
		ctx.SetStatusCode(fasthttp.StatusBadRequest)
		ctx.SetBody([]byte(`{"error":"invalid request"}`))
		return
	}

	// Envio assíncrono
	go sendTestPayment(payment)

	ctx.SetStatusCode(fasthttp.StatusCreated)
	//slog.Info("payment sended in", "time", time.Since(start).Milliseconds())
}

func handlePaymentsSummary(ctx *fasthttp.RequestCtx) {
	start := time.Now()
	ctx.SetContentType("application/json")

	queryArgs := ctx.QueryArgs()

	var from, to *time.Time
	if fromStr := queryArgs.Peek("from"); len(fromStr) > 0 {
		if t, err := time.ParseInLocation(time.RFC3339, string(fromStr), time.UTC); err == nil {
			from = &t
		} else {
			ctx.SetStatusCode(fasthttp.StatusBadRequest)
			ctx.SetBody([]byte(`{"error":"invalid 'from' date"}`))
			return
		}
	}

	if toStr := queryArgs.Peek("to"); len(toStr) > 0 {
		if t, err := time.ParseInLocation(time.RFC3339, string(toStr), time.UTC); err == nil {
			to = &t
		} else {
			ctx.SetStatusCode(fasthttp.StatusBadRequest)
			ctx.SetBody([]byte(`{"error":"invalid 'to' date"}`))
			return
		}
	}

	summary, err := getSummary(from, to)
	if err != nil {
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBody([]byte(`{"error":"failed to get summary"}`))
		return
	}

	response, _ := json.Marshal(summary)
	slog.Info("handlePaymentsSummary", "time", time.Since(start).Milliseconds())
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBody(response)
}

func handlePurgePayments(ctx *fasthttp.RequestCtx) {
	if !ctx.IsPost() {
		ctx.SetStatusCode(fasthttp.StatusMethodNotAllowed)
		return
	}

	purge()

	ctx.SetContentType("application/json")
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBody([]byte(`{"message": "payments purged"}`))
}

func handleHealth(ctx *fasthttp.RequestCtx) {
	ctx.SetContentType("application/json")
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBody([]byte(`{"status": "ok"}`))
}

// ----------- Funções de backend otimizado -----------

func sendTestPayment(payment entities.Payment) {
	req := map[string]interface{}{
		"action":  "insert",
		"payment": payment,
	}
	data, _ := json.Marshal(req)
	udpConn.Write(data)
}

func getSummary(from, to *time.Time) (entities.AggregatedSummary, error) {
	req := map[string]interface{}{
		"action": "get",
		"from":   from,
		"to":     to,
	}
	reqBytes, _ := json.Marshal(req)
	udpConn.Write(reqBytes)

	buf := bufPool.Get().([]byte)
	defer bufPool.Put(buf)

	udpConn.SetReadDeadline(time.Now().Add(1 * time.Second)) // timeout menor
	n, _, err := udpConn.ReadFromUDP(buf)
	if err != nil {
		slog.Error("erro ao ler resposta", "error", err)
		return entities.AggregatedSummary{}, err
	}

	var summary entities.AggregatedSummary
	if err := json.Unmarshal(buf[:n], &summary); err != nil {
		slog.Error("erro ao converter resposta", "error", err)
		return entities.AggregatedSummary{}, err
	}

	return summary, nil
}

func purge() {
	req := map[string]interface{}{
		"action": "purge",
	}
	data, _ := json.Marshal(req)
	udpConn.Write(data)
}
