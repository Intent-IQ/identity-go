package aerospike

import (
	"context"
	"reflect"
	"testing"
	"time"

	as "github.com/aerospike/aerospike-client-go/v7"
)

type stubClient struct {
	records        map[string]as.BinMap
	lastExpiration uint32
	lastAction     as.RecordExistsAction
	closeCalls     int
	getErr         as.Error
	putErr         as.Error
	nilRecord      bool
}

func newStubClient() *stubClient { return &stubClient{records: make(map[string]as.BinMap)} }

func (client *stubClient) Get(_ *as.BasePolicy, key *as.Key, _ ...string) (*as.Record, as.Error) {
	if client.getErr != nil {
		return nil, client.getErr
	}
	if client.nilRecord {
		return nil, nil
	}
	bins, ok := client.records[key.String()]
	if !ok {
		return nil, as.ErrKeyNotFound
	}
	return &as.Record{Bins: bins}, nil
}

func (client *stubClient) Put(policy *as.WritePolicy, key *as.Key, bins as.BinMap) as.Error {
	if client.putErr != nil {
		return client.putErr
	}
	client.lastExpiration = policy.Expiration
	client.lastAction = policy.RecordExistsAction
	client.records[key.String()] = bins
	return nil
}

func TestStoreMissAndErrors(t *testing.T) {
	client := newStubClient()
	store := NewWithClient(client, "ns", "identity")
	client.nilRecord = true
	if value, err := store.Get(t.Context(), "key"); err != nil || value != nil {
		t.Fatalf("nil record Get() = (%q, %v)", value, err)
	}
	client.nilRecord = false
	client.getErr = as.ErrServerNotAvailable
	if _, err := store.Get(t.Context(), "key"); err == nil {
		t.Fatal("Get() did not propagate backend error")
	}
	client.getErr = nil
	client.putErr = as.ErrServerNotAvailable
	if err := store.Put(t.Context(), "key", []byte("value"), time.Minute); err == nil {
		t.Fatal("Put() did not propagate backend error")
	}
	client.putErr = nil
	aerospikeKey, _ := as.NewKey("ns", "identity", "invalid")
	client.records[aerospikeKey.String()] = as.BinMap{valueBin: 123}
	if value, err := store.Get(t.Context(), "invalid"); err != nil || value != nil {
		t.Fatalf("non-string bin Get() = (%q, %v)", value, err)
	}
}

func (client *stubClient) Close() { client.closeCalls++ }

func TestStoreContractAndLegacyEncoding(t *testing.T) {
	client := newStubClient()
	store := NewWithClient(client, "ns", "identity")
	ctx := context.Background()
	missing, err := store.Get(ctx, "missing")
	if err != nil || missing != nil {
		t.Fatalf("missing Get() = (%q, %v)", missing, err)
	}
	if err := store.Put(ctx, "key", []byte("first"), 1500*time.Millisecond); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if client.lastExpiration != 2 || client.lastAction != as.REPLACE {
		t.Fatalf("write policy = expiration %d action %v", client.lastExpiration, client.lastAction)
	}
	value, err := store.Get(ctx, "key")
	if err != nil || !reflect.DeepEqual(value, []byte("first")) {
		t.Fatalf("Get() = (%q, %v)", value, err)
	}
	if err := store.Put(ctx, "key", []byte("second"), time.Minute); err != nil {
		t.Fatalf("replacement Put() error = %v", err)
	}
	value, _ = store.Get(ctx, "key")
	if string(value) != "second" {
		t.Fatalf("replacement value = %q", value)
	}
	aerospikeKey, _ := as.NewKey("ns", "identity", "legacy")
	client.records[aerospikeKey.String()] = as.BinMap{valueBin: "old-string-entry"}
	value, err = store.Get(ctx, "legacy")
	if err != nil || string(value) != "old-string-entry" {
		t.Fatalf("legacy Get() = (%q, %v)", value, err)
	}
	storedKey, _ := as.NewKey("ns", "identity", "key")
	if _, ok := client.records[storedKey.String()][valueBin].(string); !ok {
		t.Fatalf("new entry type = %T, want string", client.records[storedKey.String()][valueBin])
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := store.Close(); err != nil || client.closeCalls != 1 {
		t.Fatalf("second Close() = %v, close calls=%d", err, client.closeCalls)
	}
}

func TestSharedClientIsNotClosed(t *testing.T) {
	client := newStubClient()
	store := NewWithSharedClient(client, "ns", "identity")

	if err := store.Put(t.Context(), "key", []byte("value"), time.Minute); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	value, err := store.Get(t.Context(), "key")
	if err != nil || string(value) != "value" {
		t.Fatalf("Get() = (%q, %v)", value, err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if client.closeCalls != 0 {
		t.Fatalf("shared client close calls = %d, want 0", client.closeCalls)
	}
}

func TestExpirationAndClientPolicyCompatibility(t *testing.T) {
	for _, test := range []struct {
		ttl  time.Duration
		want uint32
	}{{0, 1}, {-time.Second, 1}, {500 * time.Millisecond, 1}, {1500 * time.Millisecond, 2}, {30 * time.Second, 30}} {
		if got := expirationSeconds(test.ttl); got != test.want {
			t.Fatalf("expirationSeconds(%v) = %d, want %d", test.ttl, got, test.want)
		}
	}
	defaults := clientPolicy(ClientPolicy{})
	if defaults.ConnectionQueueSize != 1024 || defaults.MinConnectionsPerNode != 3 || !defaults.LimitConnectionsToQueueSize {
		t.Fatalf("default policy = %#v", defaults)
	}
	overrides := clientPolicy(ClientPolicy{ConnectionQueueSize: 64, MinConnectionsPerNode: 8, ConnectTimeoutMs: 250, IdleTimeoutMs: 5000})
	if overrides.ConnectionQueueSize != 64 || overrides.MinConnectionsPerNode != 8 || overrides.Timeout != 250*time.Millisecond || overrides.IdleTimeout != 5*time.Second {
		t.Fatalf("override policy = %#v", overrides)
	}
}
