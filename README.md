# ai-price-lib-go

[简体中文](README_zh-CN.md)

A Go library and optional CLI for estimating and recording AI costs across agents, currencies, text models, and audio/video generation. It stores user-owned prices, exchange rates, and usage in JSON. Generating media or calling a provider is outside this library's scope.

Requires Go 1.27+. Install the library in your project:

```sh
go get github.com/neko233-com/ai-price-lib-go@v0.2.0
```

Download CLI binaries for Windows, Linux, and macOS from [Releases](https://github.com/neko233-com/ai-price-lib-go/releases). The library depends on `gofrs/flock` and `golang.org/x/sys` for file locking; see [licenses](licenses/).

## Integrate in Go

The prices below are fictional. A media price is an amount per specified number of units; use separate price IDs and usage records for separately billed components, such as text input and audio output. Quantities and prices are decimal strings.

```go
package main

import (
    "fmt"
    price "github.com/neko233-com/ai-price-lib-go"
)

func must(err error) {
    if err != nil { panic(err) }
}

func main() {
    c := price.New("USD")
    must(c.SetRate("CNY", "7.2"))
    must(c.SetAgent("assistant", price.Agent{Name: "My agent"}))
    must(c.SetModel("text", price.Price{Currency: "USD", Input: "2.5", Output: "10"}))
    must(c.SetMediaPrice("tts", price.MediaPrice{
        Modality: "audio", Model: "example/tts", Currency: "USD",
        Unit: "character", Amount: "0.015", Per: "1000",
    }))
    must(c.SetMediaPrice("video-720p", price.MediaPrice{
        Modality: "video", Model: "example/video-720p", Currency: "USD",
        Unit: "second", Amount: "0.2", Per: "1",
    }))
    estimate, err := c.QuoteMedia("video-720p", "2.5", "CNY")
    must(err)
    fmt.Println(estimate) // 3.6 CNY; no ledger entry is created

    must(c.SetMediaUsage("video-request-1", price.MediaUsage{
        Agent: "assistant", PriceID: "video-720p", Quantity: "2.5",
        Time: "2026-11-06T12:00:00+08:00",
    }))
    report, err := c.Total("assistant", "CNY") // empty agent means all agents
    must(err)
    fmt.Println(report.Total, report.Currency, report.Entries)
    must(c.Save("ledger.json")) // first save; see concurrent writes below
}
```

`QuoteText` estimates current text input/output pricing per million tokens. `QuoteMedia` estimates a configured media component. `SetUsage` and `SetMediaUsage` create or replace records and snapshot the current price; changing or deleting the price later does not change existing records. Replacing a usage record takes a new snapshot. `Total` sums both record types and converts at the **current** configured exchange rates, then rounds once to 12 decimal places. It is not a historical exchange-rate or provider-billing system.

`MediaPrice.Modality` is `audio` or `video`. A unit is an application-defined lowercase identifier such as `second`, `character`, `token`, or `request`. `Amount` is the price for `Per` units; `Quantity` uses that same unit. The caller must select the correct model variant, billing unit, actual quantity and separate components. No automatic tier, resolution, cache, rounding-to-billable-unit, tax, discount, or free-tier logic is applied.

## Configuration and concurrent writes

`Load`, `Save`, `SetAgent`, `SetModel`, `SetRate`, `SetUsage`, `SetMediaPrice`, `SetMediaUsage`, `Delete`, and `ImportModels` are available to Go callers. `Delete` accepts `agent`, `model`, `rate`, `usage`, `media-price`, and `media-usage`. The JSON schemas are illustrated by [the text example](examples/aiprice.json) and [the media example](examples/media.json); media fields are optional for compatibility with v0.1.0 files. Older v0.1.0 binaries cannot read files containing the new media fields; back up before upgrading and do not mix versions on one ledger.

For concurrent writes by cooperating v0.2.0+ processes, use `UpdateFile`: it locks, loads, invokes the callback, validates and replaces the file. The CLI uses the same lock for mutations, with a 30-second acquisition timeout. Readers can use `Load`. The callback error prevents saving.

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
err := price.UpdateFile(ctx, "ledger.json", func(c *price.Config) error {
    return c.SetMediaUsage("request-123", price.MediaUsage{
        Agent: "assistant", PriceID: "video-720p", Quantity: "2.5",
        Time: "2026-11-06T12:00:00+08:00",
    })
})
```

This snippet additionally imports `context` and `time`. Context limits lock acquisition, not callback execution. `Save` and direct JSON edits do **not** acquire a lock: all writers must cooperate, including other programs. Keep the adjacent `.lock` file; removing it while writers are running can break coordination. Use a local filesystem, the same ledger path, and avoid file hard links; network filesystem lock semantics are not guaranteed. The locking API rejects a ledger file that is a symbolic link. A temporary file is synced and renamed, but this is not a database transaction or a guarantee against every crash/power-loss scenario. Store backups separately and use a database for multi-host or high-concurrency accounting.

## Model catalog

The embedded [Models.dev](https://models.dev/) snapshot was retrieved on 2026-11-06 at 03:56:26 UTC. It includes **7,944 text-to-text model prices** and **228 explicit audio input/output token price components**; 999 source entries were skipped from the text list because of missing, invalid, both-zero or non-text pricing. Both-zero prices are excluded even if a model is free; add free models manually. Audio components use IDs such as `openai/gpt-realtime-2.1#audio-output-token`, in USD per million audio tokens. Video per-second or per-request prices are not inferred from this source: configure them explicitly.

In Go, use `catalog.Embedded()` or `catalog.Fetch`, then `Config.ImportModels(cat.Prices(), false)` and `Config.ImportMediaPrices(cat.AudioPrices, false)`. `false` preserves existing IDs; `true` overwrites current prices. Apply both operations inside one `UpdateFile` callback if they need to be persisted together. `Search` and `SearchAudio` search the embedded or fetched catalog. Importing does not remove older entries; inspect any pre-v0.2.0 catalog entries before relying on their modality. `catalog.Fetch` uses a 30-second timeout when given a nil client.

Catalog prices are community-maintained estimates, may be stale, and are not actual bills. Text prices cover the base input/output token rates; `tiered` marks models with additional context tiers, which are not applied automatically. Cached tokens, audio/video components, resolution, provider-specific charges, and taxes are separate. Verify the applicable provider price before use. The source and license are in [NOTICE](NOTICE) and [THIRD_PARTY_MODELS_DEV_LICENSE](THIRD_PARTY_MODELS_DEV_LICENSE).

## CLI

PowerShell example (the same commands work with `./aiprice` on Unix; use shell-appropriate quoting):

```powershell
.\aiprice.exe -config ledger.json init USD
'"7.2"' | .\aiprice.exe -config ledger.json put rate CNY
'{"name":"My agent"}' | .\aiprice.exe -config ledger.json put agent assistant
'{"modality":"video","model":"example/video-720p","currency":"USD","unit":"second","amount":"0.2","per":"1"}' | .\aiprice.exe -config ledger.json put media-price video-720p
'{"agent":"assistant","price_id":"video-720p","quantity":"2.5","time":"2026-11-06T12:00:00+08:00"}' | .\aiprice.exe -config ledger.json put media-usage request-1
.\aiprice.exe -config ledger.json report CNY assistant
.\aiprice.exe -config ledger.json get media-usage request-1
.\aiprice.exe catalog search openai/gpt
.\aiprice.exe catalog search-media openai/gpt-realtime
.\aiprice.exe -config ledger.json catalog import
.\aiprice.exe -config ledger.json catalog import online
# Explicitly overwrite matching prices; old usage snapshots remain unchanged:
.\aiprice.exe -config ledger.json catalog import online overwrite
```

`put KIND ID` reads one JSON value from stdin (`rate` is a JSON string, others are objects). `get KIND [ID]` reads one or all; `delete KIND ID` removes an item. Referenced agents and currencies cannot be deleted. A non-USD base requires a configured USD rate before importing the catalog. Rates are currency units per one base unit: with USD base and CNY `7.2`, 1 USD = 7.2 CNY. Times use RFC 3339.

## Validation and releases

Run `go test ./...` and `go vet ./...`. CI tests Linux, Windows, and macOS; the Linux job also runs the race detector. CLI packages include both READMEs and dependency licenses. Model quotes and manually entered usage are estimates, not a replacement for reconciling provider invoices.

To build release archives from a clean checkout, run `go run ./cmd/releasebuild -out <new-directory>`. The output includes six platform archives and `SHA256SUMS.txt`; the target directory must not already exist.
