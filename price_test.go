package aiprice

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func sample(t *testing.T) *Config {
	t.Helper()
	c := New("USD")
	must(t, c.SetRate("CNY", "7.2"))
	must(t, c.SetRate("EUR", "0.9"))
	must(t, c.SetAgent("agent-1", Agent{Name: "测试 Agent"}))
	must(t, c.SetAgent("agent-2", Agent{Name: "Other"}))
	must(t, c.SetModel("model", Price{Currency: "USD", Input: "2.5", Output: "10"}))
	return c
}
func TestLedgerAndSnapshot(t *testing.T) {
	c := sample(t)
	must(t, c.SetUsage("u1", Usage{Agent: "agent-1", Model: "model", InputTokens: 1000000, OutputTokens: 500000, Time: "2026-11-06T12:00:00+08:00"}))
	must(t, c.SetModel("model", Price{Currency: "USD", Input: "100", Output: "100"}))
	must(t, c.SetUsage("u2", Usage{Agent: "agent-2", Model: "model", InputTokens: 1000, Time: "2026-11-06T04:00:00Z"}))
	r, e := c.Total("agent-1", "CNY")
	must(t, e)
	if r.Total != "54" || r.Entries != 1 {
		t.Fatalf("report = %+v", r)
	}
	r, e = c.Total("", "EUR")
	must(t, e)
	if r.Total != "6.84" || r.Entries != 2 {
		t.Fatalf("report = %+v", r)
	}
	must(t, c.Delete("model", "model"))
	r, e = c.Total("agent-1", "USD")
	must(t, e)
	if r.Total != "7.5" {
		t.Fatal(r)
	}
	if e = c.Delete("agent", "agent-1"); e == nil {
		t.Fatal("deleted referenced agent")
	}
	if _, ok := c.Agents["agent-1"]; !ok {
		t.Fatal("failed edit mutated config")
	}
	must(t, c.Delete("usage", "u1"))
	must(t, c.Delete("agent", "agent-1"))
}
func TestConversionAndRounding(t *testing.T) {
	c := sample(t)
	for _, tc := range []struct{ amount, from, to, want string }{{"1", "USD", "CNY", "7.2"}, {"7.2", "CNY", "EUR", "0.9"}, {"0.1", "USD", "CNY", "0.72"}, {"1", "CNY", "USD", "0.138888888889"}} {
		got, e := c.Convert(tc.amount, tc.from, tc.to)
		must(t, e)
		if got != tc.want {
			t.Fatalf("got %s want %s", got, tc.want)
		}
	}
	if _, e := c.Convert("-1", "USD", "EUR"); e == nil {
		t.Fatal("accepted negative amount")
	}
	if _, e := c.Convert("1", "USD", "JPY"); e == nil {
		t.Fatal("accepted missing rate")
	}
}
func TestSaveLoadAndValidation(t *testing.T) {
	c := sample(t)
	path := filepath.Join(t.TempDir(), "config.json")
	must(t, c.Save(path))
	must(t, c.SetRate("CNY", "8"))
	must(t, c.Save(path))
	loaded, e := Load(path)
	must(t, e)
	if loaded.Rates["CNY"] != "8" {
		t.Fatal(loaded.Rates)
	}
	before, e := os.ReadFile(path)
	must(t, e)
	c.Rates["USD"] = "0"
	if e = c.Save(path); e == nil {
		t.Fatal("saved invalid data")
	}
	after, e := os.ReadFile(path)
	must(t, e)
	if string(before) != string(after) {
		t.Fatal("invalid save changed file")
	}
	must(t, os.WriteFile(path, []byte(strings.Replace(string(before), `"base_currency"`, `"unexpected": 1, "base_currency"`, 1)), 0600))
	if _, e = Load(path); e == nil {
		t.Fatal("accepted unknown field")
	}
}
func TestRejectInvalidMutations(t *testing.T) {
	c := sample(t)
	for _, f := range []func() error{func() error { return c.SetRate("USD", "0") }, func() error { return c.SetRate("bad", "1") }, func() error { return c.SetRate("CNY", "-1") }, func() error { return c.SetModel("bad", Price{Currency: "JPY", Input: "1", Output: "2"}) }, func() error {
		return c.SetUsage("u", Usage{Agent: "agent-1", Model: "model", InputTokens: -1, Time: "2026-11-06T00:00:00Z"})
	}, func() error {
		return c.SetUsage("u", Usage{Agent: "missing", Model: "model", Time: "2026-11-06T00:00:00Z"})
	}, func() error { return c.Delete("rate", "USD") }} {
		if e := f(); e == nil {
			t.Fatal("accepted invalid mutation")
		}
	}
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
}
func TestNoIntermediateRounding(t *testing.T) {
	c := sample(t)
	must(t, c.SetModel("tiny", Price{Currency: "USD", Input: "0.0000001", Output: "0"}))
	for _, id := range []string{"a", "b", "c", "d", "e", "f"} {
		must(t, c.SetUsage(id, Usage{Agent: "agent-1", Model: "tiny", InputTokens: 1, Time: "2026-11-06T00:00:00Z"}))
	}
	r, e := c.Total("", "USD")
	must(t, e)
	if r.Total != "0.000000000001" {
		t.Fatal(r.Total)
	}
}
