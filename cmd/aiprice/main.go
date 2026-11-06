package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	aiprice "github.com/neko233-com/ai-price-lib-go"
	"github.com/neko233-com/ai-price-lib-go/catalog"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "aiprice:", err)
		os.Exit(1)
	}
}
func readJSON(v any) error {
	d := json.NewDecoder(io.LimitReader(os.Stdin, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("expected one JSON value")
	}
	return nil
}
func printJSON(v any) error {
	e := json.NewEncoder(os.Stdout)
	e.SetIndent("", "  ")
	return e.Encode(v)
}
func run() error {
	fs := flag.NewFlagSet("aiprice", flag.ContinueOnError)
	path := fs.String("config", "aiprice.json", "configuration file")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: aiprice [-config file] init BASE | put KIND ID | get KIND [ID] | delete KIND ID | report CURRENCY [AGENT] | convert AMOUNT FROM TO | catalog search QUERY | catalog search-media QUERY | catalog import [embedded|online] [overwrite]\nput reads a JSON value from standard input; KIND is agent, model, rate, usage, media-price or media-usage")
	}
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	args := fs.Args()
	if len(args) == 0 {
		fs.Usage()
		return errors.New("missing command")
	}
	mutating := args[0] == "init" || args[0] == "put" || args[0] == "delete" || (args[0] == "catalog" && len(args) > 1 && args[1] == "import")
	if mutating {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return aiprice.WithFileLock(ctx, *path, func() error { return runCommand(*path, args) })
	}
	return runCommand(*path, args)
}

func runCommand(path string, args []string) error {
	if args[0] == "init" {
		if len(args) != 2 {
			return errors.New("init requires BASE")
		}
		if _, err := os.Stat(path); err == nil {
			return errors.New("configuration already exists")
		} else if !os.IsNotExist(err) {
			return err
		}
		return aiprice.New(args[1]).Save(path)
	}
	if args[0] == "catalog" && len(args) >= 2 && (args[1] == "search" || args[1] == "search-media") {
		if len(args) != 3 {
			return errors.New("catalog search requires QUERY")
		}
		cat, err := catalog.Embedded()
		if err != nil {
			return err
		}
		if args[1] == "search-media" {
			return printJSON(cat.SearchAudio(args[2]))
		}
		return printJSON(cat.Search(args[2]))
	}
	c, err := aiprice.Load(path)
	if err != nil {
		return err
	}
	switch args[0] {
	case "catalog":
		if len(args) < 2 || args[1] != "import" || len(args) > 4 {
			return errors.New("catalog import [embedded|online] [overwrite]")
		}
		source := "embedded"
		if len(args) >= 3 {
			source = args[2]
		}
		overwrite := false
		if len(args) == 4 {
			if args[3] != "overwrite" {
				return errors.New("expected overwrite")
			}
			overwrite = true
		}
		var cat *catalog.Catalog
		switch source {
		case "embedded":
			cat, err = catalog.Embedded()
		case "online":
			cat, err = catalog.Fetch(context.Background(), nil, catalog.Source)
		default:
			return errors.New("source must be embedded or online")
		}
		if err != nil {
			return err
		}
		added, updated, skipped, err := c.ImportModels(cat.Prices(), overwrite)
		if err != nil {
			return err
		}
		mediaAdded, mediaUpdated, mediaSkipped, err := c.ImportMediaPrices(cat.AudioPrices, overwrite)
		if err != nil {
			return err
		}
		if err = c.Save(path); err != nil {
			return err
		}
		tiered := 0
		for _, entry := range cat.Models {
			if entry.Tiered {
				tiered++
			}
		}
		return printJSON(map[string]any{"source": cat.Source, "retrieved_at": cat.RetrievedAt, "available": len(cat.Models), "source_skipped": cat.Skipped, "tiered_models": tiered, "added": added, "updated": updated, "existing_skipped": skipped, "audio_available": len(cat.AudioPrices), "audio_added": mediaAdded, "audio_updated": mediaUpdated, "audio_existing_skipped": mediaSkipped})
	case "put":
		if len(args) != 3 {
			return errors.New("put requires KIND ID")
		}
		switch args[1] {
		case "agent":
			var v aiprice.Agent
			if err = readJSON(&v); err == nil {
				err = c.SetAgent(args[2], v)
			}
		case "model":
			var v aiprice.Price
			if err = readJSON(&v); err == nil {
				err = c.SetModel(args[2], v)
			}
		case "rate":
			var v string
			if err = readJSON(&v); err == nil {
				err = c.SetRate(args[2], v)
			}
		case "media-price":
			var v aiprice.MediaPrice
			if err = readJSON(&v); err == nil {
				err = c.SetMediaPrice(args[2], v)
			}
		case "media-usage":
			var v struct {
				Agent    string `json:"agent"`
				PriceID  string `json:"price_id"`
				Quantity string `json:"quantity"`
				Time     string `json:"time"`
			}
			if err = readJSON(&v); err == nil {
				err = c.SetMediaUsage(args[2], aiprice.MediaUsage{Agent: v.Agent, PriceID: v.PriceID, Quantity: v.Quantity, Time: v.Time})
			}
		case "usage":
			var v struct {
				Agent        string `json:"agent"`
				Model        string `json:"model"`
				InputTokens  int64  `json:"input_tokens"`
				OutputTokens int64  `json:"output_tokens"`
				Time         string `json:"time"`
			}
			if err = readJSON(&v); err == nil {
				err = c.SetUsage(args[2], aiprice.Usage{Agent: v.Agent, Model: v.Model, InputTokens: v.InputTokens, OutputTokens: v.OutputTokens, Time: v.Time})
			}
		default:
			return fmt.Errorf("unknown kind %q", args[1])
		}
		if err != nil {
			return err
		}
		return c.Save(path)
	case "delete":
		if len(args) != 3 {
			return errors.New("delete requires KIND ID")
		}
		if err = c.Delete(args[1], args[2]); err != nil {
			return err
		}
		return c.Save(path)
	case "get":
		if len(args) < 2 || len(args) > 3 {
			return errors.New("get requires KIND [ID]")
		}
		var all any
		switch args[1] {
		case "agent":
			all = c.Agents
		case "model":
			all = c.Models
		case "rate":
			all = c.Rates
		case "usage":
			all = c.Usages
		case "media-price":
			all = c.MediaPrices
		case "media-usage":
			all = c.MediaUsages
		default:
			return errors.New("unknown kind")
		}
		if len(args) == 2 {
			return printJSON(all)
		}
		switch v := all.(type) {
		case map[string]aiprice.MediaPrice:
			if x, ok := v[args[2]]; ok {
				return printJSON(x)
			}
		case map[string]aiprice.MediaUsage:
			if x, ok := v[args[2]]; ok {
				return printJSON(x)
			}
		case map[string]aiprice.Agent:
			if x, ok := v[args[2]]; ok {
				return printJSON(x)
			}
		case map[string]aiprice.Price:
			if x, ok := v[args[2]]; ok {
				return printJSON(x)
			}
		case map[string]string:
			if x, ok := v[args[2]]; ok {
				return printJSON(x)
			}
		case map[string]aiprice.Usage:
			if x, ok := v[args[2]]; ok {
				return printJSON(x)
			}
		}
		return errors.New("record not found")
	case "report":
		if len(args) < 2 || len(args) > 3 {
			return errors.New("report requires CURRENCY [AGENT]")
		}
		agent := ""
		if len(args) == 3 {
			agent = args[2]
		}
		r, e := c.Total(agent, args[1])
		if e != nil {
			return e
		}
		return printJSON(r)
	case "convert":
		if len(args) != 4 {
			return errors.New("convert requires AMOUNT FROM TO")
		}
		v, e := c.Convert(args[1], strings.ToUpper(args[2]), strings.ToUpper(args[3]))
		if e != nil {
			return e
		}
		fmt.Println(v)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
