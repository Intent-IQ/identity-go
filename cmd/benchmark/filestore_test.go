package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileStoreRoundTrip(t *testing.T) {
	store, err := newFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	want := []byte("cached identity")
	if err := store.Put(context.Background(), "key", want, time.Minute); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(context.Background(), "key")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("Get() = %q, want %q", got, want)
	}

	info, err := os.Stat(store.path("key"))
	if err != nil {
		t.Fatal(err)
	}
	if permissions := info.Mode().Perm(); permissions != 0o600 {
		t.Fatalf("cache permissions = %o, want 600", permissions)
	}
}

func TestFileStoreReportsTruncatedEntry(t *testing.T) {
	store, err := newFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := store.path("key")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Get(context.Background(), "key"); err == nil {
		t.Fatal("Get() error = nil, want truncated-entry error")
	}
}
