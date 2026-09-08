package valkey

import (
	"context"
	"net"
	"strconv"
	"sync"
	"time"

	identitycache "github.com/Intent-IQ/identity-go/cache"
	govalkey "github.com/valkey-io/valkey-go"
)

type Store struct {
	client    govalkey.Client
	closeOnce sync.Once
}

func New(config Config) (*Store, error) {
	client, err := govalkey.NewClient(govalkey.ClientOption{
		InitAddress:  []string{net.JoinHostPort(config.Host, strconv.Itoa(config.Port))},
		Password:     config.Password,
		Dialer:       net.Dialer{Timeout: config.ConnectTimeout()},
		DisableCache: true,
		DisableRetry: true,
	})
	if err != nil {
		return nil, err
	}
	return &Store{client: client}, nil
}

func NewWithClient(client govalkey.Client) *Store {
	return &Store{client: client}
}

func (store *Store) Close() error {
	store.closeOnce.Do(store.client.Close)
	return nil
}

func (store *Store) Get(ctx context.Context, key string) ([]byte, error) {
	value, err := store.client.Do(ctx, store.client.B().Get().Key(key).Build()).AsBytes()
	if govalkey.IsValkeyNil(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), value...), nil
}

func (store *Store) Put(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	set := store.client.B().Set().Key(key).Value(string(value))
	if ttl <= 0 {
		return store.client.Do(ctx, set.Build()).Error()
	}
	return store.client.Do(ctx, set.Px(ttl).Build()).Error()
}

var _ identitycache.Store = (*Store)(nil)
