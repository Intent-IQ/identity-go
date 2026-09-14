package redis

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"time"

	identitycache "github.com/Intent-IQ/identity-go/cache"
	goredis "github.com/redis/go-redis/v9"
)

type Store struct {
	client    *goredis.Client
	closeOnce sync.Once
	closeErr  error
}

func New(config Config) (*Store, error) {
	client := goredis.NewClient(&goredis.Options{
		Addr:        net.JoinHostPort(config.Host, strconv.Itoa(config.Port)),
		Password:    config.Password,
		DialTimeout: config.ConnectTimeout(),
	})
	ctx, cancel := context.WithTimeout(context.Background(), config.ConnectTimeout())
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	return NewWithClient(client), nil
}

func NewWithClient(client *goredis.Client) *Store {
	return &Store{client: client}
}

func (store *Store) Close() error {
	store.closeOnce.Do(func() { store.closeErr = store.client.Close() })
	return store.closeErr
}

func (store *Store) Get(ctx context.Context, key string) ([]byte, error) {
	value, err := store.client.Get(ctx, key).Bytes()
	if errors.Is(err, goredis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return value, nil
}

func (store *Store) Put(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return store.client.Set(ctx, key, value, ttl).Err()
}

var _ identitycache.Store = (*Store)(nil)
