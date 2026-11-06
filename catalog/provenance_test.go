package catalog

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func TestSnapshotProvenance(t *testing.T) {
	b, e := os.ReadFile("PROVENANCE.json")
	if e != nil {
		t.Fatal(e)
	}
	var p struct {
		Snapshots []struct {
			Snapshot string         `json:"snapshot"`
			SHA      string         `json:"snapshot_sha256"`
			Counts   map[string]int `json:"counts"`
		} `json:"snapshots"`
	}
	if e = json.Unmarshal(b, &p); e != nil {
		t.Fatal(e)
	}
	if len(p.Snapshots) != 2 {
		t.Fatal("expected two sources")
	}
	a, e := Embedded()
	if e != nil {
		t.Fatal(e)
	}
	l, e := EmbeddedLiteLLM()
	if e != nil {
		t.Fatal(e)
	}
	for _, x := range p.Snapshots {
		switch x.Snapshot {
		case "snapshot.json.gz":
			if x.SHA != fmt.Sprintf("%x", sha256.Sum256(snapshot)) || x.Counts["text_models"] != len(a.Models) || x.Counts["audio_token_components"] != len(a.AudioPrices) || x.Counts["text_components"] != len(a.ExtraPrices) {
				t.Fatal("models.dev provenance mismatch")
			}
		case "litellm.json.gz":
			text, gen := 0, 0
			for _, v := range l.Prices {
				if v.Modality == "text" {
					text++
				} else {
					gen++
				}
			}
			if x.SHA != fmt.Sprintf("%x", sha256.Sum256(liteSnapshot)) || x.Counts["generation_components"] != gen || x.Counts["text_components"] != text || x.Counts["text_models"] != len(l.TextPrices) {
				t.Fatal("LiteLLM provenance mismatch")
			}
		default:
			t.Fatal("unknown snapshot")
		}
	}
}
