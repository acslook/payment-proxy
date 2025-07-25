# Etapa 1: build da aplicação
FROM golang:alpine3.22 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o worker ./cmd/worker

# Etapa 2: imagem final enxuta
FROM alpine:latest

WORKDIR /app

COPY /containers/app/docker-entrypoint.sh ./docker-entrypoint.sh
RUN chmod +x /app/docker-entrypoint.sh


COPY --from=builder /app/server ./server
COPY --from=builder /app/worker ./worker

ENTRYPOINT ["/app/docker-entrypoint.sh"]