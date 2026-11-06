// Package store provides opt-in initialization and hot reload of a file-backed ledger.
package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	price "github.com/neko233-com/ai-price-lib-go"
	"github.com/neko233-com/ai-price-lib-go/catalog"
)

const DefaultFile = "aiprice.json"

type Options struct {
	Path         string
	PollInterval time.Duration
	BaseCurrency string
	Rates        map[string]string
}
type Status struct {
	Path        string
	LastSuccess time.Time
	LastError   string
}
type Store struct {
	path      string
	interval  time.Duration
	mu        sync.RWMutex
	op        sync.Mutex
	current   *price.Config
	hash      [32]byte
	status    Status
	cancel    context.CancelFunc
	done      chan struct{}
	closeOnce sync.Once
}

// Open initializes a missing file using embedded catalogs, then starts a polling watcher.
// An empty Path resolves to aiprice.json in the process working directory at Open time.
// Existing files are never replaced. The caller must call Close to stop the watcher.
func Open(ctx context.Context, opts Options) (*Store, error) {
	if ctx == nil {
		return nil, errors.New("context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if opts.PollInterval < 0 {
		return nil, errors.New("poll interval cannot be negative")
	}
	if opts.PollInterval == 0 {
		opts.PollInterval = time.Second
	}
	if opts.Path == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		opts.Path = filepath.Join(wd, DefaultFile)
	}
	path, err := filepath.Abs(opts.Path)
	if err != nil {
		return nil, err
	}
	err = price.WithFileLock(ctx, path, func() error {
		_, err := os.Stat(path)
		if err == nil {
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		base := opts.BaseCurrency
		if base == "" {
			base = "USD"
		}
		c := price.New(base)
		for code, rate := range opts.Rates {
			if err = c.SetRate(code, rate); err != nil {
				return err
			}
		}
		cat, err := catalog.Embedded()
		if err != nil {
			return err
		}
		media, err := catalog.EmbeddedLiteLLM()
		if err != nil {
			return err
		}
		if _, _, _, err = c.ImportModels(cat.Prices(), false); err != nil {
			return err
		}
		if _, _, _, err = c.ImportModels(media.TextPrices, false); err != nil {
			return err
		}
		if _, _, _, err = c.ImportMediaPrices(cat.AudioPrices, false); err != nil {
			return err
		}
		if _, _, _, err = c.ImportMediaPrices(cat.ExtraPrices, false); err != nil {
			return err
		}
		if _, _, _, err = c.ImportMediaPrices(media.Prices, false); err != nil {
			return err
		}
		return c.Save(path)
	})
	if err != nil {
		return nil, err
	}
	s := &Store{path: path, interval: opts.PollInterval, done: make(chan struct{}), status: Status{Path: path}}
	if err = s.reloadContext(ctx); err != nil {
		return nil, err
	}
	watchCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	go s.watch(watchCtx)
	return s, nil
}
func (s *Store) watch(ctx context.Context) {
	defer close(s.done)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.reloadContext(ctx)
		}
	}
}

// Close stops background polling; it is safe to call more than once.
func (s *Store) Close() { s.closeOnce.Do(func() { s.cancel(); <-s.done }) }

// Reload keeps the last valid snapshot on read or validation failure.
func (s *Store) Reload() error { return s.reloadContext(context.Background()) }
func (s *Store) reloadContext(ctx context.Context) error {
	s.op.Lock()
	defer s.op.Unlock()
	return s.reloadLocked(ctx)
}
func (s *Store) reloadLocked(ctx context.Context) error {
	wait, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err := price.WithFileLock(wait, s.path, func() error { return s.readLocked() })
	if err != nil {
		s.setError(err)
	}
	return err
}
func (s *Store) readLocked() error {
	b, err := os.ReadFile(s.path)
	if err != nil {
		s.setError(err)
		return err
	}
	hash := sha256.Sum256(b)
	s.mu.RLock()
	unchanged := s.current != nil && s.hash == hash
	s.mu.RUnlock()
	if unchanged {
		s.mu.Lock()
		s.status.LastError = ""
		s.mu.Unlock()
		return nil
	}
	c, err := price.Decode(b)
	if err != nil {
		s.setError(err)
		return err
	}
	s.mu.Lock()
	s.current = c
	s.hash = hash
	s.status.LastSuccess = time.Now().UTC()
	s.status.LastError = ""
	s.mu.Unlock()
	return nil
}
func (s *Store) setError(err error) { s.mu.Lock(); s.status.LastError = err.Error(); s.mu.Unlock() }

// Status reports the last successful replacement and latest reload error.
func (s *Store) Status() Status { s.mu.RLock(); defer s.mu.RUnlock(); return s.status }

// Snapshot returns an independent copy; modifying it does not mutate the live store.
func (s *Store) Snapshot() (*price.Config, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, err := json.Marshal(s.current)
	if err != nil {
		return nil, err
	}
	var c price.Config
	if err = json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// QuoteText and QuoteMedia read the current snapshot without cloning the entire catalog.
func (s *Store) QuoteText(model string, input, output int64, currency string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current.QuoteText(model, input, output, currency)
}
func (s *Store) QuoteMedia(id, quantity, currency string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current.QuoteMedia(id, quantity, currency)
}
func (s *Store) Total(agent, currency string) (price.Report, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current.Total(agent, currency)
}

// Update serializes cooperating writers and publishes the resulting file immediately.
func (s *Store) Update(ctx context.Context, fn func(*price.Config) error) error {
	s.op.Lock()
	defer s.op.Unlock()
	if err := price.UpdateFile(ctx, s.path, fn); err != nil {
		return err
	}
	if err := s.reloadLocked(ctx); err != nil {
		return fmt.Errorf("update saved but reload failed: %w", err)
	}
	return nil
}
