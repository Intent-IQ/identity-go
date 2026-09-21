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

type fileStore struct{ dir string }

func newFileStore(dir string) (*fileStore, error) {
	if err := ensurePrivateDir(dir); err != nil {
		return nil, err
	}
	return &fileStore{dir: dir}, nil
}

func (store *fileStore) path(key string) string {
	digest := sha256.Sum256([]byte(key))
	name := hex.EncodeToString(digest[:])
	return filepath.Join(store.dir, name[:2], name[2:])
}

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

	// Concurrent writes are committed atomically.
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

func ensurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return os.Chmod(path, 0o700)
}
