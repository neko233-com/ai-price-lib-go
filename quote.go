package aiprice

import (
	"fmt"
	"math/big"
)

func textCharge(p Price, input, output int64) *big.Rat {
	in, _ := decimal(p.Input)
	out, _ := decimal(p.Output)
	n := new(big.Rat).Add(new(big.Rat).Mul(in, new(big.Rat).SetInt64(input)), new(big.Rat).Mul(out, new(big.Rat).SetInt64(output)))
	return n.Quo(n, big.NewRat(1000000, 1))
}
func mediaCharge(p MediaPrice, quantity string) *big.Rat {
	q, _ := decimal(quantity)
	amount, _ := decimal(p.Amount)
	per, _ := decimal(p.Per)
	return new(big.Rat).Quo(new(big.Rat).Mul(q, amount), per)
}

// QuoteText returns an estimate using the model's current price and configured exchange rates.
func (c *Config) QuoteText(model string, input, output int64, currency string) (string, error) {
	if input < 0 || output < 0 {
		return "", fmt.Errorf("token counts must be non-negative")
	}
	p, ok := c.Models[model]
	if !ok {
		return "", fmt.Errorf("model %q missing", model)
	}
	if err := c.validatePrice(p); err != nil {
		return "", err
	}
	v, err := c.convert(textCharge(p, input, output), p.Currency, currency)
	if err != nil {
		return "", err
	}
	return format(v), nil
}

// QuoteMedia returns an estimate using the current price per configured unit.
func (c *Config) QuoteMedia(id, quantity, currency string) (string, error) {
	p, ok := c.MediaPrices[id]
	if !ok {
		return "", fmt.Errorf("media price %q missing", id)
	}
	if err := c.validateMediaPrice(p); err != nil {
		return "", err
	}
	if _, err := decimal(quantity); err != nil {
		return "", err
	}
	v, err := c.convert(mediaCharge(p, quantity), p.Currency, currency)
	if err != nil {
		return "", err
	}
	return format(v), nil
}
