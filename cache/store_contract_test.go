package cache

import (
	"bytes"
	"testing"
	"time"
)

type storeFactory func(*testing.T) Store

// runStoreContract is reusable by concrete backend integrations.
func runStoreContract(t *testing.T, factory storeFactory) {
	t.Helper()
	store := factory(t)
	missing, err := store.Get(t.Context(), "missing")
	if err != nil || missing != nil {
		t.Fatalf("missing Get() = (%q, %v), want (nil, nil)", missing, err)
	}

	first := []byte("first")
	if err := store.Put(t.Context(), "key", first, time.Minute); err != nil {
		t.Fatalf("first Put() error = %v", err)
	}
	got, err := store.Get(t.Context(), "key")
	if err != nil || !bytes.Equal(got, first) {
		t.Fatalf("Get() = (%q, %v), want %q", got, err, first)
	}

	replacement := []byte("replacement")
	if err := store.Put(t.Context(), "key", replacement, 2*time.Minute); err != nil {
		t.Fatalf("replacement Put() error = %v", err)
	}
	got, err = store.Get(t.Context(), "key")
	if err != nil || !bytes.Equal(got, replacement) {
		t.Fatalf("replacement Get() = (%q, %v), want %q", got, err, replacement)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestRecordingStoreContract(t *testing.T) {
	runStoreContract(t, func(*testing.T) Store { return newRecordingStore() })
}
