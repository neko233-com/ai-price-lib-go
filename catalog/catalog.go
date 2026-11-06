// Package catalog imports base text token prices from models.dev.
package catalog

import (
	"bytes"
	"compress/gzip"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"net/http"
	"strconv"
	"strings"
	"time"

	price "github.com/neko233-com/ai-price-lib-go"
)

const Source = "https://models.dev/api.json"

//go:embed snapshot.json.gz
var snapshot []byte

type Entry struct {
	Provider string      `json:"provider"`
	Model    string      `json:"model"`
	Name     string      `json:"name"`
	Price    price.Price `json:"price"`
	Tiered   bool        `json:"tiered,omitempty"`
}
type Catalog struct {
	Source      string                      `json:"source"`
	RetrievedAt string                      `json:"retrieved_at"`
	Models      map[string]Entry            `json:"models"`
	Skipped     int                         `json:"skipped"`
	AudioPrices map[string]price.MediaPrice `json:"audio_prices,omitempty"`
	ExtraPrices map[string]price.MediaPrice `json:"extra_prices,omitempty"`
}
type rawModel struct {
	Name       string `json:"name"`
	Modalities struct {
		Input  []string `json:"input"`
		Output []string `json:"output"`
	} `json:"modalities"`
	Cost struct {
		Input       *json.Number    `json:"input"`
		Output      *json.Number    `json:"output"`
		InputAudio  *json.Number    `json:"input_audio"`
		OutputAudio *json.Number    `json:"output_audio"`
		CacheRead   *json.Number    `json:"cache_read"`
		CacheWrite  *json.Number    `json:"cache_write"`
		Reasoning   *json.Number    `json:"reasoning"`
		Tiers       json.RawMessage `json:"tiers"`
		Context     json.RawMessage `json:"context_over_200k"`
	} `json:"cost"`
}
type rawProvider struct {
	Models map[string]rawModel `json:"models"`
}

// exact converts JSON number notation into the decimal syntax accepted by the ledger.
func exact(n json.Number) (string, error) {
	s := string(n)
	mantissa := s
	exp := 0
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		mantissa = s[:i]
		v, e := strconv.Atoi(s[i+1:])
		if e != nil || v < -100 || v > 100 {
			return "", errors.New("invalid exponent")
		}
		exp = v
	}
	if strings.HasPrefix(mantissa, "-") || strings.HasPrefix(mantissa, "+") {
		return "", errors.New("negative price")
	}
	pieces := strings.Split(mantissa, ".")
	if len(pieces) > 2 || len(pieces[0]) == 0 {
		return "", errors.New("invalid price")
	}
	frac := 0
	if len(pieces) == 2 {
		frac = len(pieces[1])
	}
	digits := strings.Join(pieces, "")
	for _, c := range digits {
		if c < '0' || c > '9' {
			return "", errors.New("invalid price")
		}
	}
	pos := len(digits) - frac + exp
	var result string
	if pos <= 0 {
		result = "0." + strings.Repeat("0", -pos) + digits
	} else if pos >= len(digits) {
		result = digits + strings.Repeat("0", pos-len(digits))
	} else {
		result = digits[:pos] + "." + digits[pos:]
	}
	parts := strings.SplitN(result, ".", 2)
	parts[0] = strings.TrimLeft(parts[0], "0")
	if parts[0] == "" {
		parts[0] = "0"
	}
	result = parts[0]
	if len(parts) == 2 {
		f := strings.TrimRight(parts[1], "0")
		if f != "" {
			result += "." + f
		}
	}
	if len(result) > 128 {
		return "", errors.New("price too long")
	}
	return result, nil
}

// Parse reads the upstream provider catalog, skipping missing, invalid, or both-zero prices.
func Parse(r io.Reader, retrieved time.Time) (*Catalog, error) {
	var raw map[string]rawProvider
	d := json.NewDecoder(r)
	d.UseNumber()
	if err := d.Decode(&raw); err != nil {
		return nil, err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return nil, errors.New("invalid trailing JSON")
	}
	if len(raw) == 0 {
		return nil, errors.New("empty catalog")
	}
	c := &Catalog{Source: Source, RetrievedAt: retrieved.UTC().Format(time.RFC3339), Models: map[string]Entry{}, AudioPrices: map[string]price.MediaPrice{}, ExtraPrices: map[string]price.MediaPrice{}}
	for provider, p := range raw {
		for model, m := range p.Models {
			if provider != "" && model != "" && strings.TrimSpace(provider) == provider && strings.TrimSpace(model) == model {
				for _, component := range []struct {
					name  string
					value *json.Number
				}{{"input", m.Cost.InputAudio}, {"output", m.Cost.OutputAudio}} {
					if component.value == nil {
						continue
					}
					amount, err := exact(*component.value)
					if err != nil || amount == "0" {
						continue
					}
					key := provider + "/" + model + "#audio-" + component.name + "-token"
					if _, exists := c.AudioPrices[key]; exists {
						return nil, fmt.Errorf("duplicate media price ID %s", key)
					}
					c.AudioPrices[key] = price.MediaPrice{Modality: "audio", Model: provider + "/" + model, Currency: "USD", Unit: "token", Amount: amount, Per: "1000000"}
				}
			}
			if !contains(m.Modalities.Input, "text") || !contains(m.Modalities.Output, "text") {
				c.Skipped++
				continue
			}
			if provider == "" || model == "" || strings.TrimSpace(provider) != provider || strings.TrimSpace(model) != model || m.Cost.Input == nil || m.Cost.Output == nil {
				c.Skipped++
				continue
			}
			in, e1 := exact(*m.Cost.Input)
			out, e2 := exact(*m.Cost.Output)
			if e1 != nil || e2 != nil || (in == "0" && out == "0") {
				c.Skipped++
				continue
			}
			key := provider + "/" + model
			if _, exists := c.Models[key]; exists {
				return nil, fmt.Errorf("duplicate model ID %s", key)
			}
			tiered := (len(m.Cost.Tiers) > 0 && string(m.Cost.Tiers) != "null" && string(m.Cost.Tiers) != "[]") || (len(m.Cost.Context) > 0 && string(m.Cost.Context) != "null")
			c.Models[key] = Entry{Provider: provider, Model: model, Name: m.Name, Price: price.Price{Currency: "USD", Input: in, Output: out}, Tiered: tiered}
			addComponent := func(suffix string, n *json.Number) error {
				if n == nil {
					return nil
				}
				amount, e := exact(*n)
				if e != nil || amount == "0" {
					return nil
				}
				id := key + "#" + suffix + "-token"
				if _, exists := c.ExtraPrices[id]; exists {
					return fmt.Errorf("duplicate component ID %s", id)
				}
				c.ExtraPrices[id] = price.MediaPrice{Modality: "text", Model: key, Currency: "USD", Unit: "token", Amount: amount, Per: "1000000"}
				return nil
			}
			for _, part := range []struct {
				name  string
				value *json.Number
			}{{"cache-read", m.Cost.CacheRead}, {"cache-write", m.Cost.CacheWrite}, {"reasoning", m.Cost.Reasoning}} {
				if err := addComponent(part.name, part.value); err != nil {
					return nil, err
				}
			}
			if len(m.Cost.Tiers) > 0 && string(m.Cost.Tiers) != "null" {
				var tiers []struct {
					Tier struct {
						Type string      `json:"type"`
						Size json.Number `json:"size"`
					} `json:"tier"`
					Input      *json.Number `json:"input"`
					Output     *json.Number `json:"output"`
					CacheRead  *json.Number `json:"cache_read"`
					CacheWrite *json.Number `json:"cache_write"`
					Reasoning  *json.Number `json:"reasoning"`
				}
				if err := json.Unmarshal(m.Cost.Tiers, &tiers); err == nil {
					for _, tier := range tiers {
						size, e := strconv.ParseUint(string(tier.Tier.Size), 10, 64)
						if e != nil || size == 0 || tier.Tier.Type != "context" {
							continue
						}
						prefix := fmt.Sprintf("context-%d-", size)
						for _, part := range []struct {
							name  string
							value *json.Number
						}{{"input", tier.Input}, {"output", tier.Output}, {"cache-read", tier.CacheRead}, {"cache-write", tier.CacheWrite}, {"reasoning", tier.Reasoning}} {
							if err := addComponent(prefix+part.name, part.value); err != nil {
								return nil, err
							}
						}
					}
				}
			}

		}
	}
	if len(c.Models) == 0 && len(c.AudioPrices) == 0 {
		return nil, errors.New("catalog has no usable model prices")
	}
	return c, nil
}
func Embedded() (*Catalog, error) {
	z, e := gzip.NewReader(bytes.NewReader(snapshot))
	if e != nil {
		return nil, e
	}
	defer z.Close()
	var c Catalog
	if e = json.NewDecoder(z).Decode(&c); e != nil {
		return nil, e
	}
	return &c, nil
}

// Fetch downloads an upstream snapshot; the client should set a timeout.
func Fetch(ctx context.Context, client *http.Client, url string) (*Catalog, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if e != nil {
		return nil, e
	}
	resp, e := client.Do(req)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalog HTTP status %d", resp.StatusCode)
	}
	return Parse(io.LimitReader(resp.Body, 32<<20), time.Now())
}
func (c *Catalog) Prices() map[string]price.Price {
	out := make(map[string]price.Price, len(c.Models))
	for id, e := range c.Models {
		out[id] = e.Price
	}
	return out
}
func (c *Catalog) Search(query string) map[string]Entry {
	out := map[string]Entry{}
	query = strings.ToLower(query)
	for id, e := range c.Models {
		if strings.Contains(strings.ToLower(id), query) || strings.Contains(strings.ToLower(e.Name), query) {
			out[id] = e
		}
	}
	return out
}

func contains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

// SearchAudio searches the explicit audio token prices in the catalog.
func (c *Catalog) SearchAudio(query string) map[string]price.MediaPrice {
	out := map[string]price.MediaPrice{}
	query = strings.ToLower(query)
	for id, p := range c.AudioPrices {
		if strings.Contains(strings.ToLower(id), query) {
			out[id] = p
		}
	}
	return out
}

// SearchExtras returns explicit text cache, reasoning and context-tier token rates.
func (c *Catalog) SearchExtras(query string) map[string]price.MediaPrice {
	out := map[string]price.MediaPrice{}
	q := strings.ToLower(query)
	for id, p := range c.ExtraPrices {
		if strings.Contains(strings.ToLower(id), q) {
			out[id] = p
		}
	}
	return out
}
