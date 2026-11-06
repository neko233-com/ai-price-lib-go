package aiprice

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Config is the entire portable ledger. Rates are units of each currency per one base unit.
type Config struct {
	BaseCurrency string            `json:"base_currency"`
	Rates        map[string]string `json:"rates"`
	Agents       map[string]Agent  `json:"agents"`
	Models       map[string]Price  `json:"models"`
	Usages       map[string]Usage  `json:"usages"`
}
type Agent struct {
	Name string `json:"name"`
}

// Price specifies the charge per one million tokens.
type Price struct {
	Currency string `json:"currency"`
	Input    string `json:"input_per_million"`
	Output   string `json:"output_per_million"`
}

// Usage stores a price snapshot, so later model price edits never change past charges.
type Usage struct {
	Agent        string `json:"agent"`
	Model        string `json:"model"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	Time         string `json:"time"`
	Price        Price  `json:"price"`
}
type Report struct {
	Currency string `json:"currency"`
	Total    string `json:"total"`
	Entries  int    `json:"entries"`
}

var decimalPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)
var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

func decimal(s string) (*big.Rat, error) {
	if len(s) > 128 || !decimalPattern.MatchString(s) {
		return nil, fmt.Errorf("invalid non-negative decimal %q", s)
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil, fmt.Errorf("invalid decimal %q", s)
	}
	return r, nil
}
func format(r *big.Rat) string {
	s := r.FloatString(12)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "" || s == "-0" {
		return "0"
	}
	return s
}
func validID(s string) bool { return s != "" && strings.TrimSpace(s) == s }
func New(base string) *Config {
	return &Config{BaseCurrency: base, Rates: map[string]string{base: "1"}, Agents: map[string]Agent{}, Models: map[string]Price{}, Usages: map[string]Usage{}}
}

func (c *Config) Validate() error {
	if !currencyPattern.MatchString(c.BaseCurrency) {
		return errors.New("base_currency must be a three-letter uppercase code")
	}
	if c.Rates == nil || c.Agents == nil || c.Models == nil || c.Usages == nil {
		return errors.New("rates, agents, models and usages must be objects")
	}
	for code, s := range c.Rates {
		if !currencyPattern.MatchString(code) {
			return fmt.Errorf("invalid currency %q", code)
		}
		r, e := decimal(s)
		if e != nil || r.Sign() <= 0 {
			return fmt.Errorf("rate %s must be a positive decimal", code)
		}
	}
	base, ok := c.Rates[c.BaseCurrency]
	if !ok {
		return errors.New("base rate missing")
	}
	r, _ := decimal(base)
	if r.Cmp(big.NewRat(1, 1)) != 0 {
		return errors.New("base rate must equal 1")
	}
	for id, a := range c.Agents {
		if !validID(id) || strings.TrimSpace(a.Name) == "" {
			return fmt.Errorf("invalid agent %q", id)
		}
	}
	for id, p := range c.Models {
		if !validID(id) {
			return fmt.Errorf("invalid model ID %q", id)
		}
		if e := c.validatePrice(p); e != nil {
			return fmt.Errorf("model %s: %w", id, e)
		}
	}
	for id, u := range c.Usages {
		if !validID(id) {
			return fmt.Errorf("invalid usage ID %q", id)
		}
		if _, ok := c.Agents[u.Agent]; !ok {
			return fmt.Errorf("usage %s: agent %q missing", id, u.Agent)
		}
		if !validID(u.Model) || u.InputTokens < 0 || u.OutputTokens < 0 {
			return fmt.Errorf("usage %s: invalid model or token count", id)
		}
		if _, e := time.Parse(time.RFC3339, u.Time); e != nil {
			return fmt.Errorf("usage %s: invalid RFC3339 time", id)
		}
		if e := c.validatePrice(u.Price); e != nil {
			return fmt.Errorf("usage %s: %w", id, e)
		}
	}
	return nil
}
func (c *Config) validatePrice(p Price) error {
	if _, ok := c.Rates[p.Currency]; !ok {
		return fmt.Errorf("rate for %q missing", p.Currency)
	}
	if _, e := decimal(p.Input); e != nil {
		return e
	}
	if _, e := decimal(p.Output); e != nil {
		return e
	}
	return nil
}

// Load rejects unknown fields and invalid data instead of silently dropping it on save.
func Load(path string) (*Config, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var c Config
	if e = dec.Decode(&c); e != nil {
		return nil, e
	}
	var extra any
	if e = dec.Decode(&extra); e != io.EOF {
		return nil, errors.New("configuration must contain one JSON value")
	}
	if e = c.Validate(); e != nil {
		return nil, e
	}
	return &c, nil
}

// Save replaces the file only after validation and a complete write in the same directory.
// Callers must serialize read-modify-write transactions across processes.
func (c *Config) Save(path string) error {
	if e := c.Validate(); e != nil {
		return e
	}
	b, e := json.MarshalIndent(c, "", "  ")
	if e != nil {
		return e
	}
	b = append(b, '\n')
	dir := filepath.Dir(path)
	f, e := os.CreateTemp(dir, ".aiprice-*.tmp")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp, path)
}
func (c *Config) edit(fn func(*Config) error) error {
	if e := c.Validate(); e != nil {
		return e
	}
	b, e := json.Marshal(c)
	if e != nil {
		return e
	}
	var copy Config
	if e = json.Unmarshal(b, &copy); e != nil {
		return e
	}
	if e = fn(&copy); e != nil {
		return e
	}
	if e = copy.Validate(); e != nil {
		return e
	}
	*c = copy
	return nil
}
func (c *Config) SetAgent(id string, a Agent) error {
	return c.edit(func(x *Config) error { x.Agents[id] = a; return nil })
}
func (c *Config) SetModel(id string, p Price) error {
	return c.edit(func(x *Config) error { x.Models[id] = p; return nil })
}
func (c *Config) SetRate(code, rate string) error {
	return c.edit(func(x *Config) error { x.Rates[code] = rate; return nil })
}

// SetUsage creates or replaces a record and snapshots the model's current price.
func (c *Config) SetUsage(id string, u Usage) error {
	return c.edit(func(x *Config) error {
		p, ok := x.Models[u.Model]
		if !ok {
			return fmt.Errorf("model %q missing", u.Model)
		}
		u.Price = p
		x.Usages[id] = u
		return nil
	})
}
func (c *Config) Delete(kind, id string) error {
	return c.edit(func(x *Config) error {
		var exists bool
		switch kind {
		case "agent":
			_, exists = x.Agents[id]
			delete(x.Agents, id)
		case "model":
			_, exists = x.Models[id]
			delete(x.Models, id)
		case "rate":
			_, exists = x.Rates[id]
			delete(x.Rates, id)
		case "usage":
			_, exists = x.Usages[id]
			delete(x.Usages, id)
		default:
			return fmt.Errorf("unknown kind %q", kind)
		}
		if !exists {
			return fmt.Errorf("%s %q not found", kind, id)
		}
		return nil
	})
}
func (c *Config) convert(amount *big.Rat, from, to string) (*big.Rat, error) {
	f, ok := c.Rates[from]
	if !ok {
		return nil, fmt.Errorf("rate for %s missing", from)
	}
	t, ok := c.Rates[to]
	if !ok {
		return nil, fmt.Errorf("rate for %s missing", to)
	}
	fr, e := decimal(f)
	if e != nil || fr.Sign() <= 0 {
		return nil, fmt.Errorf("invalid rate for %s", from)
	}
	tr, e := decimal(t)
	if e != nil || tr.Sign() <= 0 {
		return nil, fmt.Errorf("invalid rate for %s", to)
	}
	return new(big.Rat).Mul(new(big.Rat).Quo(amount, fr), tr), nil
}

// Convert rounds only the returned result to 12 decimal places.
func (c *Config) Convert(amount, from, to string) (string, error) {
	n, e := decimal(amount)
	if e != nil {
		return "", e
	}
	n, e = c.convert(n, from, to)
	if e != nil {
		return "", e
	}
	return format(n), nil
}

// Total computes exact rational charges and converts at the current configured rates.
// An empty agent includes all agents. Conversion occurs before rounding the total.
func (c *Config) Total(agent, currency string) (Report, error) {
	if e := c.Validate(); e != nil {
		return Report{}, e
	}
	if agent != "" {
		if _, ok := c.Agents[agent]; !ok {
			return Report{}, fmt.Errorf("agent %q missing", agent)
		}
	}
	if _, ok := c.Rates[currency]; !ok {
		return Report{}, fmt.Errorf("rate for %s missing", currency)
	}
	total := new(big.Rat)
	count := 0
	for _, u := range c.Usages {
		if agent != "" && u.Agent != agent {
			continue
		}
		in, _ := decimal(u.Price.Input)
		out, _ := decimal(u.Price.Output)
		charge := new(big.Rat).Add(new(big.Rat).Mul(in, new(big.Rat).SetInt64(u.InputTokens)), new(big.Rat).Mul(out, new(big.Rat).SetInt64(u.OutputTokens)))
		charge.Quo(charge, big.NewRat(1000000, 1))
		charge, e := c.convert(charge, u.Price.Currency, currency)
		if e != nil {
			return Report{}, e
		}
		total.Add(total, charge)
		count++
	}
	return Report{currency, format(total), count}, nil
}

// ImportModels merges a batch atomically in memory. Existing prices are retained unless overwrite is true.
func (c *Config) ImportModels(prices map[string]Price, overwrite bool) (added, updated, skipped int, err error) {
	err = c.edit(func(x *Config) error {
		for id, p := range prices {
			if _, exists := x.Models[id]; exists {
				if !overwrite {
					skipped++
					continue
				}
				updated++
			} else {
				added++
			}
			x.Models[id] = p
		}
		return nil
	})
	if err != nil {
		return 0, 0, 0, err
	}
	return
}
