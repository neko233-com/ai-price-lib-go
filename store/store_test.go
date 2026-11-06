package store

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	price "github.com/neko233-com/ai-price-lib-go"
)

func waitFor(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for reload")
}
func TestFirstRunAndHotReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.json")
	s, err := Open(context.Background(), Options{Path: path, PollInterval: 25 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cfg, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Models) < 7000 || len(cfg.MediaPrices) < 400 {
		t.Fatalf("catalogs missing: %d text, %d media", len(cfg.Models), len(cfg.MediaPrices))
	}
	cfg.Models = map[string]price.Price{}
	again, err := s.Snapshot()
	if err != nil || len(again.Models) < 7000 {
		t.Fatal("snapshot was not independent")
	}
	const model = "example/video"
	p := price.MediaPrice{Modality: "video", Model: model, Currency: "USD", Unit: "second", Amount: "0.2", Per: "1"}
	if err = s.Update(context.Background(), func(c *price.Config) error { return c.SetMediaPrice(model, p) }); err != nil {
		t.Fatal(err)
	}
	v, err := s.QuoteMedia(model, "2.5", "USD")
	if err != nil || v != "0.5" {
		t.Fatalf("quote %q %v", v, err)
	}
	p.Amount = "0.4"
	if err = price.UpdateFile(context.Background(), path, func(c *price.Config) error { return c.SetMediaPrice(model, p) }); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { v, e := s.QuoteMedia(model, "2.5", "USD"); return e == nil && v == "1" })
	valid, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("{invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return s.Status().LastError != "" })
	if v, err = s.QuoteMedia(model, "2.5", "USD"); err != nil || v != "1" {
		t.Fatalf("bad reload replaced snapshot: %s %v", v, err)
	}
	if err = os.WriteFile(path, valid, 0600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return s.Status().LastError == "" })
	s.Close()
	s.Close()
	other, err := Open(context.Background(), Options{Path: path, PollInterval: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	v, err = other.QuoteMedia(model, "2.5", "USD")
	if err != nil || v != "1" {
		t.Fatalf("existing file overwritten: %s %v", v, err)
	}
}
func TestConcurrentQuotesAndUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.json")
	s, err := Open(context.Background(), Options{Path: path, PollInterval: 25 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				_, _ = s.QuoteText("openai/gpt-5.3-codex", 10, 10, "USD")
			}
		}()
	}
	for i := 0; i < 4; i++ {
		if err = s.Update(context.Background(), func(c *price.Config) error { return c.SetAgent("agent", price.Agent{Name: "test"}) }); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}
func TestDefaultPathHelper(t *testing.T) {
	if os.Getenv("AIPRICE_CWD_HELPER") != "1" {
		return
	}
	s, err := Open(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
}
func TestDefaultPathIsWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestDefaultPathHelper$")
	cmd.Env = append(os.Environ(), "AIPRICE_CWD_HELPER=1")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if _, err := price.Load(filepath.Join(dir, DefaultFile)); err != nil {
		t.Fatal(err)
	}
}
func TestOpenRejectsInvalidExistingAndInitialRates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.json")
	if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), Options{Path: path}); err == nil {
		t.Fatal("accepted invalid existing file")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "invalid" {
		t.Fatal("overwrote invalid file")
	}
	path = filepath.Join(t.TempDir(), "ledger.json")
	if _, err := Open(context.Background(), Options{Path: path, BaseCurrency: "CNY"}); err == nil {
		t.Fatal("imported USD without a USD rate")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("partial config created")
	}
}
func TestConcurrentFirstOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.json")
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := Open(context.Background(), Options{Path: path, PollInterval: time.Second})
			if err == nil {
				s.Close()
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	c, err := price.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Models) < 7000 || len(c.MediaPrices) < 10000 {
		t.Fatalf("missing prices: %d %d", len(c.Models), len(c.MediaPrices))
	}
}
func TestEmbeddedDomesticQuotes(t *testing.T) {
	s, err := Open(context.Background(), Options{Path: filepath.Join(t.TempDir(), "ledger.json")})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	checks := []struct{ id, quantity, want string }{{"litellm/dashscope/qwen3-tts-flash#input-character", "1500", "0.015"}, {"litellm/dashscope/wan3.0-video#output-second-720p", "2.5", "0.2062825"}}
	for _, c := range checks {
		got, err := s.QuoteMedia(c.id, c.quantity, "USD")
		if err != nil || got != c.want {
			t.Fatalf("%s got %s: %v", c.id, got, err)
		}
	}
	got, err := s.QuoteText("litellm/tencent/tencent/glm-5.3", 1000000, 1000000, "USD")
	if err != nil || got != "5.8" {
		t.Fatalf("tencent: %s %v", got, err)
	}
}
