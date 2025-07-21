# Etapa 1: build da aplicação
FROM golang:alpine3.22 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o server ./cmd/server

# Etapa 2: imagem final enxuta
FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/server .
# COPY --from=builder /app/config.yaml .

EXPOSE 9999

CMD ["./server"]