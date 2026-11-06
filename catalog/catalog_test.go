package catalog

import (
	"context"
	price "github.com/neko233-com/ai-price-lib-go"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseAndImport(t *testing.T) {
	fixture := `{"p1":{"models":{"m":{"name":"Model","cost":{"input":1e-4,"output":3.50,"tiers":[{"tier":1}]}},"zero":{"cost":{"input":0,"output":0}},"missing":{}}},"p2":{"models":{"m":{"name":"Other","cost":{"input":2,"output":5}}}}}`
	c, e := Parse(strings.NewReader(fixture), time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	if e != nil {
		t.Fatal(e)
	}
	if len(c.Models) != 2 || c.Skipped != 2 {
		t.Fatalf("%+v", c)
	}
	v := c.Models["p1/m"]
	if v.Price.Input != "0.0001" || v.Price.Output != "3.5" || !v.Tiered {
		t.Fatalf("%+v", v)
	}
	ledger := price.New("USD")
	if e = ledger.SetModel("p1/m", price.Price{Currency: "USD", Input: "9", Output: "9"}); e != nil {
		t.Fatal(e)
	}
	a, u, s, e := ledger.ImportModels(c.Prices(), false)
	if e != nil || a != 1 || u != 0 || s != 1 || ledger.Models["p1/m"].Input != "9" {
		t.Fatalf("%d %d %d %v", a, u, s, e)
	}
	a, u, s, e = ledger.ImportModels(c.Prices(), true)
	if e != nil || a != 0 || u != 2 || s != 0 || ledger.Models["p1/m"].Input != "0.0001" {
		t.Fatalf("%d %d %d %v", a, u, s, e)
	}
	other := price.New("CNY")
	if _, _, _, e = other.ImportModels(c.Prices(), false); e == nil || len(other.Models) != 0 {
		t.Fatal("missing USD rate must reject the entire import")
	}
}
func TestFetchFailures(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer s.Close()
	if _, e := Fetch(context.Background(), s.Client(), s.URL); e == nil {
		t.Fatal("accepted failure status")
	}
	if _, e := Parse(strings.NewReader(`{"p":{"models":{"bad":{"cost":{"input":0,"output":0}}}}}`), time.Now()); e == nil {
		t.Fatal("accepted empty price catalog")
	}
}
func TestEmbedded(t *testing.T) {
	c, e := Embedded()
	if e != nil {
		t.Fatal(e)
	}
	if len(c.Models) < 1000 || c.Source != Source {
		t.Fatalf("bad snapshot: %d", len(c.Models))
	}
	if _, ok := c.Search("openai/gpt")["openai/gpt-5.3-codex"]; !ok {
		t.Fatal("missing known model")
	}
}
