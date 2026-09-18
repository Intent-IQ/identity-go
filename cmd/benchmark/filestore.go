package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const expiryPrefixSize = 8

// fileStore is a cache.Store kept on disk so entries survive between replay runs.
// The in-process L1 cannot do that, and a warm re-run is the point of -cache.
type fileStore struct{ dir string }

func newFileStore(dir string) (*fileStore, error) {
	if err := ensurePrivateDir(dir); err != nil {
		return nil, err
	}
	return &fileStore{dir: dir}, nil
}

// path shards by the first byte of the key digest to keep directories small.
func (store *fileStore) path(key string) string {
	digest := sha256.Sum256([]byte(key))
	name := hex.EncodeToString(digest[:])
	return filepath.Join(store.dir, name[:2], name[2:])
}

// Get reports a miss as (nil, nil); an error would be recorded as an L2 fault.
func (store *fileStore) Get(_ context.Context, key string) ([]byte, error) {
	path := store.path(key)
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read cache entry: %w", err)
	}
	if len(contents) < expiryPrefixSize {
		return nil, fmt.Errorf("cache entry %q is truncated", path)
	}
	if time.Now().UnixNano() > int64(binary.BigEndian.Uint64(contents[:expiryPrefixSize])) {
		_ = os.Remove(path)
		return nil, nil
	}
	return contents[expiryPrefixSize:], nil
}

func (store *fileStore) Put(_ context.Context, key string, value []byte, ttl time.Duration) error {
	if ttl <= 0 {
		return nil
	}
	path := store.path(key)
	if err := ensurePrivateDir(filepath.Dir(path)); err != nil {
		return err
	}

	buffer := make([]byte, expiryPrefixSize, expiryPrefixSize+len(value))
	binary.BigEndian.PutUint64(buffer, uint64(time.Now().Add(ttl).UnixNano()))
	buffer = append(buffer, value...)

	// Workers share keys, so stage under a unique name before the atomic rename.
	staged, err := os.CreateTemp(filepath.Dir(path), "staging-")
	if err != nil {
		return err
	}
	stagedName := staged.Name()
	defer os.Remove(stagedName)
	if _, err := staged.Write(buffer); err != nil {
		staged.Close()
		return err
	}
	if err := staged.Close(); err != nil {
		return err
	}
	return os.Rename(stagedName, path)
}

func (store *fileStore) Close() error { return nil }
