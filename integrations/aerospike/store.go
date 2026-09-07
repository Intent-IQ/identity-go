package aerospike

import (
	"context"
	"errors"
	"sync"
	"time"

	identitycache "github.com/Intent-IQ/identity-go/cache"
	as "github.com/aerospike/aerospike-client-go/v7"
)

const valueBin = "v"

const (
	defaultConnectionQueueSize   = 1024
	defaultMinConnectionsPerNode = 3
)

type Client interface {
	Get(policy *as.BasePolicy, key *as.Key, binNames ...string) (*as.Record, as.Error)
	Put(policy *as.WritePolicy, key *as.Key, bins as.BinMap) as.Error
	Close()
}

type Store struct {
	client     Client
	namespace  string
	set        string
	ownsClient bool
	closeOnce  sync.Once
}

func New(config Config) (*Store, error) {
	client, err := as.NewClientWithPolicy(clientPolicy(config.Policy), config.Host, config.Port)
	if err != nil {
		return nil, err
	}
	client.DisableMetrics()
	return NewWithClient(client, config.Namespace, config.Set), nil
}

func NewWithClient(client Client, namespace, set string) *Store {
	return &Store{client: client, namespace: namespace, set: set, ownsClient: true}
}

// NewWithSharedClient creates a Store over a client owned by the host. Closing
// the Store does not close the shared client.
func NewWithSharedClient(client Client, namespace, set string) *Store {
	return &Store{client: client, namespace: namespace, set: set}
}

func (store *Store) Close() error {
	if store.ownsClient {
		store.closeOnce.Do(store.client.Close)
	}
	return nil
}

func (store *Store) Get(_ context.Context, key string) ([]byte, error) {
	aerospikeKey, err := as.NewKey(store.namespace, store.set, key)
	if err != nil {
		return nil, err
	}
	record, aerospikeError := store.client.Get(nil, aerospikeKey, valueBin)
	if aerospikeError != nil {
		if errors.Is(aerospikeError, as.ErrKeyNotFound) {
			return nil, nil
		}
		return nil, aerospikeError
	}
	if record == nil {
		return nil, nil
	}
	switch value := record.Bins[valueBin].(type) {
	case string:
		return []byte(value), nil
	case []byte:
		return append([]byte(nil), value...), nil
	default:
		return nil, nil
	}
}

func (store *Store) Put(_ context.Context, key string, value []byte, ttl time.Duration) error {
	aerospikeKey, err := as.NewKey(store.namespace, store.set, key)
	if err != nil {
		return err
	}
	policy := as.NewWritePolicy(0, expirationSeconds(ttl))
	policy.RecordExistsAction = as.REPLACE
	return store.client.Put(policy, aerospikeKey, as.BinMap{valueBin: string(value)})
}

func clientPolicy(config ClientPolicy) *as.ClientPolicy {
	policy := as.NewClientPolicy()
	policy.ConnectionQueueSize = orDefault(config.ConnectionQueueSize, defaultConnectionQueueSize)
	policy.MinConnectionsPerNode = orDefault(config.MinConnectionsPerNode, defaultMinConnectionsPerNode)
	policy.LimitConnectionsToQueueSize = true
	if config.ConnectTimeoutMs > 0 {
		policy.Timeout = time.Duration(config.ConnectTimeoutMs) * time.Millisecond
	}
	if config.IdleTimeoutMs > 0 {
		policy.IdleTimeout = time.Duration(config.IdleTimeoutMs) * time.Millisecond
	}
	return policy
}

func orDefault(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func expirationSeconds(ttl time.Duration) uint32 {
	seconds := int64(ttl / time.Second)
	if ttl%time.Second != 0 {
		seconds++
	}
	if seconds < 1 {
		seconds = 1
	}
	return uint32(seconds)
}

var _ identitycache.Store = (*Store)(nil)
