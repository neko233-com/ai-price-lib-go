package catalog

import (
	"strings"
	"testing"
	"time"
)

func TestLiteLLMMediaPrices(t *testing.T) {
	fixture := `{"dashscope/wan":{"mode":"video_generation","output_cost_per_second":0.2,"output_cost_per_second_720p":0.05},"dashscope/tts":{"mode":"audio_speech","input_cost_per_character":1e-5,"output_cost_per_audio_token":0.0002},"ignore":{"mode":"chat","output_cost_per_second":5}}`
	c, e := ParseLiteLLM(strings.NewReader(fixture), time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if len(c.Prices) != 4 {
		t.Fatalf("got %d", len(c.Prices))
	}
	p := c.Prices["litellm/dashscope/wan#output-second-720p"]
	if p.Modality != "video" || p.Unit != "second" || p.Amount != "0.05" || p.Per != "1" {
		t.Fatalf("%+v", p)
	}
	p = c.Prices["litellm/dashscope/tts#input-character"]
	if p.Unit != "character" || p.Amount != "0.00001" {
		t.Fatalf("%+v", p)
	}
	embedded, e := EmbeddedLiteLLM()
	if e != nil || len(embedded.Prices) < 100 {
		t.Fatalf("embedded: %v", e)
	}
}
func TestLiteLLMTextPriceAndComponents(t *testing.T) {
	fixture := `{"tencent/model":{"mode":"chat","litellm_provider":"tencent","input_cost_per_token":1.5e-6,"output_cost_per_token":0.000002,"cache_read_input_token_cost":0.0000001,"input_cost_per_token_batches":0.00000075}}`
	c, e := ParseLiteLLM(strings.NewReader(fixture), time.Now())
	if e != nil {
		t.Fatal(e)
	}
	p := c.TextPrices["litellm/tencent/tencent/model"]
	if p.Input != "1.5" || p.Output != "2" || p.Currency != "USD" {
		t.Fatalf("%+v", p)
	}
	if len(c.Prices) != 2 {
		t.Fatalf("components %d", len(c.Prices))
	}
	v := c.Prices["litellm/tencent/tencent/model#cache-read-input-token"]
	if v.Modality != "text" || v.Amount != "0.0000001" {
		t.Fatalf("%+v", v)
	}
}
