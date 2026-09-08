package valkey

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func newTestStore(t *testing.T) (*Store, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	port, err := strconv.Atoi(server.Port())
	if err != nil {
		t.Fatalf("port conversion error = %v", err)
	}
	store, err := New(Config{Host: server.Host(), Port: port})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, server
}

func TestStoreContract(t *testing.T) {
	store, server := newTestStore(t)
	ctx := context.Background()
	missing, err := store.Get(ctx, "missing")
	if err != nil || missing != nil {
		t.Fatalf("missing Get() = (%q, %v)", missing, err)
	}
	if err := store.Put(ctx, "key", []byte{0, 1, 2, 255}, 90*time.Second); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if server.TTL("key") != 90*time.Second {
		t.Fatalf("TTL = %v", server.TTL("key"))
	}
	value, err := store.Get(ctx, "key")
	if err != nil || string(value) != string([]byte{0, 1, 2, 255}) {
		t.Fatalf("Get() = (%v, %v)", value, err)
	}
	if err := store.Put(ctx, "key", []byte("replacement"), time.Minute); err != nil {
		t.Fatalf("replacement Put() error = %v", err)
	}
	value, _ = store.Get(ctx, "key")
	if string(value) != "replacement" {
		t.Fatalf("replacement value = %q", value)
	}
	if err := store.Put(ctx, "persistent", []byte("value"), 0); err != nil {
		t.Fatalf("zero-TTL Put() error = %v", err)
	}
	if server.TTL("persistent") != 0 {
		t.Fatalf("zero-TTL key TTL = %v", server.TTL("persistent"))
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestStoreErrorsPropagate(t *testing.T) {
	store, server := newTestStore(t)
	server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := store.Get(ctx, "key"); err == nil {
		t.Fatal("Get() error = nil after backend shutdown")
	}
	if err := store.Put(ctx, "key", []byte("value"), time.Minute); err == nil {
		t.Fatal("Put() error = nil after backend shutdown")
	}
}
