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

const LiteLLMSource = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

//go:embed litellm.json.gz
var liteSnapshot []byte

// MediaCatalog contains explicit per-unit generation prices from LiteLLM.
type MediaCatalog struct {
	Source      string                      `json:"source"`
	RetrievedAt string                      `json:"retrieved_at"`
	Prices      map[string]price.MediaPrice `json:"prices"`
	TextPrices  map[string]price.Price      `json:"text_prices,omitempty"`
}

// ParseLiteLLM accepts explicitly specified audio speech and video generation rates.
// Each ID names the source, model, and price field; different resolution rates remain separate.
func ParseLiteLLM(r io.Reader, at time.Time) (*MediaCatalog, error) {
	var raw map[string]map[string]json.RawMessage
	d := json.NewDecoder(r)
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
	c := &MediaCatalog{Source: LiteLLMSource, RetrievedAt: at.UTC().Format(time.RFC3339), Prices: map[string]price.MediaPrice{}, TextPrices: map[string]price.Price{}}
	for model, fields := range raw {
		var mode, provider string
		_ = json.Unmarshal(fields["mode"], &mode)
		_ = json.Unmarshal(fields["litellm_provider"], &provider)
		if model == "" || strings.TrimSpace(model) != model {
			continue
		}
		if (mode == "chat" || mode == "completion" || mode == "responses") && provider != "" && strings.TrimSpace(provider) == provider && !strings.Contains(provider, "/") {
			var input, output json.Number
			if json.Unmarshal(fields["input_cost_per_token"], &input) == nil && json.Unmarshal(fields["output_cost_per_token"], &output) == nil {
				in, e1 := perMillion(input)
				out, e2 := perMillion(output)
				if e1 == nil && e2 == nil && (in != "0" || out != "0") {
					id := "litellm/" + provider + "/" + model
					c.TextPrices[id] = price.Price{Currency: "USD", Input: in, Output: out}
					specs := map[string]string{"cache_read_input_token_cost": "cache-read-input-token", "cache_creation_input_token_cost": "cache-write-input-token"}
					for _, variant := range []string{"batches", "flex", "priority"} {
						specs["input_cost_per_token_"+variant] = "input-token-" + variant
						specs["output_cost_per_token_"+variant] = "output-token-" + variant
						specs["cache_read_input_token_cost_"+variant] = "cache-read-input-token-" + variant
						specs["cache_creation_input_token_cost_"+variant] = "cache-write-input-token-" + variant
					}
					for field, suffix := range specs {
						var n json.Number
						if json.Unmarshal(fields[field], &n) != nil {
							continue
						}
						amount, e := exact(n)
						if e != nil || amount == "0" {
							continue
						}
						key := id + "#" + suffix
						c.Prices[key] = price.MediaPrice{Modality: "text", Model: id, Currency: "USD", Unit: "token", Amount: amount, Per: "1"}
					}
				}
			}
		}
		if mode != "audio_speech" && mode != "video_generation" {
			continue
		}
		modality := "audio"
		if mode == "video_generation" {
			modality = "video"
		}
		specs := map[string]struct{ suffix, unit string }{}
		if modality == "audio" {
			specs["input_cost_per_character"] = struct{ suffix, unit string }{"input-character", "character"}
			specs["output_cost_per_character"] = struct{ suffix, unit string }{"output-character", "character"}
			specs["output_cost_per_audio_token"] = struct{ suffix, unit string }{"output-audio-token", "token"}
			specs["output_cost_per_second"] = struct{ suffix, unit string }{"output-second", "second"}
			specs["input_cost_per_token"] = struct{ suffix, unit string }{"input-token", "token"}
			specs["output_cost_per_token"] = struct{ suffix, unit string }{"output-token", "token"}
			specs["cache_read_input_token_cost"] = struct{ suffix, unit string }{"cache-read-input-token", "token"}
			for _, variant := range []string{"batches", "flex", "priority"} {
				specs["input_cost_per_token_"+variant] = struct{ suffix, unit string }{"input-token-" + variant, "token"}
				specs["output_cost_per_token_"+variant] = struct{ suffix, unit string }{"output-token-" + variant, "token"}
				specs["cache_read_input_token_cost_"+variant] = struct{ suffix, unit string }{"cache-read-input-token-" + variant, "token"}
			}
		}
		if modality == "video" {
			specs["output_cost_per_second"] = struct{ suffix, unit string }{"output-second", "second"}
			for _, res := range []string{"480p", "720p", "768p", "1080p", "2k", "4k"} {
				specs["output_cost_per_second_"+res] = struct{ suffix, unit string }{"output-second-" + res, "second"}
			}
			specs["output_cost_per_video_per_second"] = struct{ suffix, unit string }{"output-video-second", "second"}
			specs["output_cost_per_video_token"] = struct{ suffix, unit string }{"output-video-token", "token"}
			specs["input_cost_per_image"] = struct{ suffix, unit string }{"input-image", "image"}
			specs["input_cost_per_video_per_second"] = struct{ suffix, unit string }{"input-video-second", "second"}
			specs["input_cost_per_video_token"] = struct{ suffix, unit string }{"input-video-token", "token"}
		}
		for field, spec := range specs {
			b, ok := fields[field]
			if !ok || bytes.Equal(b, []byte("null")) {
				continue
			}
			var n json.Number
			if err := json.Unmarshal(b, &n); err != nil {
				continue
			}
			amount, err := exact(n)
			if err != nil || amount == "0" {
				continue
			}
			id := "litellm/" + model + "#" + spec.suffix
			c.Prices[id] = price.MediaPrice{Modality: modality, Model: "litellm/" + model, Currency: "USD", Unit: spec.unit, Amount: amount, Per: "1"}
		}
	}
	if len(c.Prices) == 0 && len(c.TextPrices) == 0 {
		return nil, errors.New("catalog has no supported media prices")
	}
	return c, nil
}
func EmbeddedLiteLLM() (*MediaCatalog, error) {
	z, err := gzip.NewReader(bytes.NewReader(liteSnapshot))
	if err != nil {
		return nil, err
	}
	defer z.Close()
	var c MediaCatalog
	if err = json.NewDecoder(z).Decode(&c); err != nil {
		return nil, err
	}
	return &c, nil
}
func FetchLiteLLM(ctx context.Context, client *http.Client, url string) (*MediaCatalog, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalog HTTP status %d", resp.StatusCode)
	}
	return ParseLiteLLM(io.LimitReader(resp.Body, 32<<20), time.Now())
}
func (c *MediaCatalog) Search(query string) map[string]price.MediaPrice {
	out := map[string]price.MediaPrice{}
	q := strings.ToLower(query)
	for id, p := range c.Prices {
		if strings.Contains(strings.ToLower(id), q) {
			out[id] = p
		}
	}
	return out
}

// perMillion converts a USD-per-token JSON number to a USD-per-million decimal without floating point.
func perMillion(n json.Number) (string, error) {
	s := string(n)
	mantissa := s
	exp := 0
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		mantissa = s[:i]
		v, e := strconv.Atoi(s[i+1:])
		if e != nil {
			return "", e
		}
		exp = v
	}
	if exp > 94 || exp < -106 {
		return "", errors.New("invalid exponent")
	}
	return exact(json.Number(mantissa + "e" + strconv.Itoa(exp+6)))
}

// SearchText searches LiteLLM text model IDs.
func (c *MediaCatalog) SearchText(query string) map[string]Entry {
	out := map[string]Entry{}
	q := strings.ToLower(query)
	for id, p := range c.TextPrices {
		if strings.Contains(strings.ToLower(id), q) {
			out[id] = Entry{Provider: "litellm", Model: id, Name: id, Price: p}
		}
	}
	return out
}
