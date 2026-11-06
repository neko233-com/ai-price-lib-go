package aiprice

import (
	"path/filepath"
	"testing"
)

func TestMediaQuotesSnapshotsAndReports(t *testing.T) {
	c := sample(t)
	audio := MediaPrice{Modality: "audio", Model: "example/tts", Currency: "USD", Unit: "character", Amount: "0.015", Per: "1000"}
	video := MediaPrice{Modality: "video", Model: "example/video-720p", Currency: "USD", Unit: "second", Amount: "0.2", Per: "1"}
	must(t, c.SetMediaPrice("tts", audio))
	must(t, c.SetMediaPrice("video", video))
	if got, e := c.QuoteMedia("tts", "1500", "CNY"); e != nil || got != "0.162" {
		t.Fatalf("audio quote: %s %v", got, e)
	}
	if got, e := c.QuoteMedia("video", "2.5", "USD"); e != nil || got != "0.5" {
		t.Fatalf("video quote: %s %v", got, e)
	}
	if got, e := c.QuoteText("model", 100000, 20000, "CNY"); e != nil || got != "3.24" {
		t.Fatalf("text quote: %s %v", got, e)
	}
	now := "2026-11-06T04:00:00Z"
	must(t, c.SetMediaUsage("audio1", MediaUsage{Agent: "agent-1", PriceID: "tts", Quantity: "1500", Time: now}))
	must(t, c.SetMediaUsage("video1", MediaUsage{Agent: "agent-1", PriceID: "video", Quantity: "2.5", Time: now}))
	must(t, c.SetUsage("text", Usage{Agent: "agent-1", Model: "model", InputTokens: 100000, OutputTokens: 20000, Time: now}))
	video.Amount = "1"
	must(t, c.SetMediaPrice("video", video))
	r, e := c.Total("agent-1", "USD")
	must(t, e)
	if r.Total != "0.9725" || r.Entries != 3 {
		t.Fatalf("report: %+v", r)
	}
	if got, e := c.QuoteMedia("video", "2.5", "USD"); e != nil || got != "2.5" {
		t.Fatalf("updated quote %s %v", got, e)
	}
	must(t, c.Delete("media-price", "video"))
	r, e = c.Total("agent-1", "CNY")
	must(t, e)
	if r.Total != "7.002" {
		t.Fatalf("snapshot report %s", r.Total)
	}
	if e = c.Delete("agent", "agent-1"); e == nil {
		t.Fatal("removed referenced agent")
	}
	path := filepath.Join(t.TempDir(), "config.json")
	must(t, c.Save(path))
	loaded, e := Load(path)
	must(t, e)
	r, e = loaded.Total("agent-1", "USD")
	must(t, e)
	if r.Total != "0.9725" {
		t.Fatal(r)
	}
}
func TestMediaValidationAndLegacyFile(t *testing.T) {
	c, e := Load(filepath.Join("examples", "aiprice.json"))
	must(t, e)
	must(t, c.SetMediaPrice("audio", MediaPrice{Modality: "audio", Model: "example/audio", Currency: "USD", Unit: "token", Amount: "2", Per: "1000000"}))
	for _, f := range []func() error{func() error {
		return c.SetMediaPrice("bad", MediaPrice{Modality: "image", Model: "m", Currency: "USD", Unit: "second", Amount: "1", Per: "1"})
	}, func() error {
		return c.SetMediaPrice("bad", MediaPrice{Modality: "video", Model: "m", Currency: "USD", Unit: "second", Amount: "1", Per: "0"})
	}, func() error {
		return c.SetMediaUsage("bad", MediaUsage{Agent: "assistant", PriceID: "audio", Quantity: "-1", Time: "2026-11-06T00:00:00Z"})
	}, func() error { _, e := c.QuoteText("example-model", -1, 0, "USD"); return e }, func() error { _, e := c.QuoteMedia("audio", "1", "JPY"); return e }} {
		if err := f(); err == nil {
			t.Fatal("accepted invalid request")
		}
	}
}
