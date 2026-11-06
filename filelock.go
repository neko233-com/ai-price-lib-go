package aiprice

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

// WithFileLock serializes cooperating writers of the same ledger on a local filesystem.
// The .lock file is permanent and must not be deleted while any writer may be running.
// The context controls lock acquisition, not the duration of fn. Do not call recursively.
// The callback must use the same path and finish all writes before returning.
func WithFileLock(ctx context.Context, path string, fn func() error) (err error) {
	if ctx == nil || fn == nil {
		return errors.New("context and callback are required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return fmt.Errorf("resolve ledger directory: %w", err)
	}
	resolved := filepath.Join(dir, filepath.Base(abs))
	lock := flock.New(resolved + ".lock")
	locked, err := lock.TryLockContext(ctx, 25*time.Millisecond)
	if err != nil {
		return fmt.Errorf("acquire ledger lock: %w", err)
	}
	if !locked {
		return errors.New("ledger lock not acquired")
	}
	defer func() { err = errors.Join(err, lock.Unlock()) }()
	info, e := os.Lstat(resolved)
	if e == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("ledger file must not be a symbolic link")
	}
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return e
	}
	return fn()
}

// UpdateFile performs a serialized read-modify-save transaction among cooperating writers.
// If fn fails or validation fails, the existing ledger is not replaced.
func UpdateFile(ctx context.Context, path string, fn func(*Config) error) error {
	if fn == nil {
		return errors.New("callback is required")
	}
	return WithFileLock(ctx, path, func() error {
		c, err := Load(path)
		if err != nil {
			return err
		}
		if err = fn(c); err != nil {
			return err
		}
		return c.Save(path)
	})
}

// LoadFile reads while holding the same lock used by UpdateFile and the Store watcher.
// Prefer this to Load when other processes may replace the file, especially on Windows.
func LoadFile(ctx context.Context, path string) (c *Config, err error) {
	err = WithFileLock(ctx, path, func() error { var e error; c, e = Load(path); return e })
	return
}
