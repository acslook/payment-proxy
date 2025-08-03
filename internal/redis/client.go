package redis

import (
	"log"
	"os"

	"github.com/go-redsync/redsync/v4"
	redsync_redis "github.com/go-redsync/redsync/v4/redis/goredis/v9"
	"github.com/redis/go-redis/v9"
)

type Client struct {
	Client *redis.Client
	Lock   *redsync.Redsync
}

func NewClient() *Client {
	redisUrl := os.Getenv("REDIS_URL")
	if redisUrl == "" {
		log.Fatal("CONN_STRING not defined")
	}
	client := redis.NewClient(&redis.Options{
		Addr:         redisUrl,
		PoolSize:     20,
		MinIdleConns: 5,
	})

	pool := redsync_redis.NewPool(client)
	RedSyncLock := redsync.New(pool)

	return &Client{
		Client: client,
		Lock:   RedSyncLock,
	}
}
