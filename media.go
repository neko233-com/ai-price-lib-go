package aiprice

import (
	"fmt"
	"regexp"
)

// MediaPrice defines a price per Per units for a text, audio, or video model.
// Multiple components of one request should use separate IDs and usage records.
type MediaPrice struct {
	Modality string `json:"modality"` // text, audio, or video
	Model    string `json:"model"`    // provider/model and optional variant
	Currency string `json:"currency"`
	Unit     string `json:"unit"` // e.g. second, character, token, request
	Amount   string `json:"amount"`
	Per      string `json:"per"`
}

// MediaUsage snapshots its price on creation or replacement.
type MediaUsage struct {
	Agent    string     `json:"agent"`
	PriceID  string     `json:"price_id"`
	Quantity string     `json:"quantity"`
	Time     string     `json:"time"`
	Price    MediaPrice `json:"price"`
}

var unitPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

func (c *Config) validateMediaPrice(p MediaPrice) error {
	if p.Modality != "text" && p.Modality != "audio" && p.Modality != "video" {
		return fmt.Errorf("modality must be text, audio or video")
	}
	if !validID(p.Model) || !unitPattern.MatchString(p.Unit) {
		return fmt.Errorf("invalid model or unit")
	}
	if _, ok := c.Rates[p.Currency]; !ok {
		return fmt.Errorf("rate for %q missing", p.Currency)
	}
	if _, err := decimal(p.Amount); err != nil {
		return err
	}
	per, err := decimal(p.Per)
	if err != nil || per.Sign() <= 0 {
		return fmt.Errorf("per must be a positive decimal")
	}
	return nil
}

// SetMediaPrice creates or replaces a unit price. It does not change recorded snapshots.
func (c *Config) SetMediaPrice(id string, p MediaPrice) error {
	return c.edit(func(x *Config) error {
		if x.MediaPrices == nil {
			x.MediaPrices = map[string]MediaPrice{}
		}
		x.MediaPrices[id] = p
		return nil
	})
}

// SetMediaUsage records a quantity and snapshots the current price identified by PriceID.
func (c *Config) SetMediaUsage(id string, u MediaUsage) error {
	return c.edit(func(x *Config) error {
		p, ok := x.MediaPrices[u.PriceID]
		if !ok {
			return fmt.Errorf("media price %q missing", u.PriceID)
		}
		u.Price = p
		if x.MediaUsages == nil {
			x.MediaUsages = map[string]MediaUsage{}
		}
		x.MediaUsages[id] = u
		return nil
	})
}

// ImportMediaPrices merges a batch without changing existing prices unless overwrite is true.
func (c *Config) ImportMediaPrices(prices map[string]MediaPrice, overwrite bool) (added, updated, skipped int, err error) {
	err = c.edit(func(x *Config) error {
		if x.MediaPrices == nil {
			x.MediaPrices = map[string]MediaPrice{}
		}
		for id, p := range prices {
			if _, exists := x.MediaPrices[id]; exists {
				if !overwrite {
					skipped++
					continue
				}
				updated++
			} else {
				added++
			}
			x.MediaPrices[id] = p
		}
		return nil
	})
	if err != nil {
		return 0, 0, 0, err
	}
	return
}
