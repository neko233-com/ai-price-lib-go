# ai-price-lib-go

[简体中文](README_zh-CN.md)

A Go library and optional CLI for estimating and recording AI costs across agents, currencies, text models, and audio/video generation. It stores user-owned prices, exchange rates, and usage in JSON. Generating media or calling a provider is outside this library's scope.

Requires Go 1.27+. Install the library in your project:

```sh
go get github.com/neko233-com/ai-price-lib-go@v0.2.0
```

Download CLI binaries for Windows, Linux, and macOS from [Releases](https://github.com/neko233-com/ai-price-lib-go/releases). The library depends on `gofrs/flock` and `golang.org/x/sys` for file locking; see [licenses](licenses/).

## Server integration: initialize and reload

Call `store.Open` during service startup. With default options it creates `aiprice.json` in the **process working directory** only when absent, populates it from the embedded catalogs, and starts a one-second polling watcher. It never replaces an existing file. Initialization is explicit; merely importing the package has no side effects. The defaults use USD only, so add your own rates before quoting in other currencies.

```go
package main

import (
    "context"
    "fmt"
    "github.com/neko233-com/ai-price-lib-go/store"
)

func main() {
    s, err := store.Open(context.Background(), store.Options{})
    if err != nil { panic(err) }
    defer s.Close()
    amount, err := s.QuoteMedia("litellm/dashscope/wan3.0-video#output-second-720p", "2.5", "USD")
    if err != nil { panic(err) }
    fmt.Println(s.Status().Path, amount)
}
```

Keep the store open for the life of the server. Use `store.Options{Path: ..., PollInterval: ...}` to choose a path or interval, and `s.Update(ctx, func(c *price.Config) error { ... })` for coordinated writes. `s.QuoteText`, `s.QuoteMedia`, and `s.Total` read the live snapshot; `s.Snapshot` returns an independent copy. Changes made by another writer are visible after a successful poll. If a file is missing, partially written, or invalid, the last valid snapshot remains active and `s.Status().LastError` exposes the failure; the file is not silently recreated. Monitor this status. `Close` stops the watcher; cancelling the `Open` context also stops it. `Options.BaseCurrency` and `Rates` apply only on first creation; if the base is not USD, supply its USD rate so the catalog can be imported.

For model names containing `#context-<threshold>-...`, select the applicable context tier yourself. Token components such as cache-read or reasoning can overlap ordinary input/output counts: use provider-defined disjoint quantities to avoid double counting. Rates and usage remain editable in the generated file.

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

`MediaPrice.Modality` is `text`, `audio`, or `video`; text components include cache and tier-specific token rates. A unit is an application-defined lowercase identifier such as `second`, `character`, `token`, or `request`. `Amount` is the price for `Per` units; `Quantity` uses that same unit. The caller must select the correct model variant, billing unit, actual quantity and separate components. No automatic tier, resolution, cache, rounding-to-billable-unit, tax, discount, or free-tier logic is applied.

## Configuration and concurrent writes

`Load`, `Save`, `SetAgent`, `SetModel`, `SetRate`, `SetUsage`, `SetMediaPrice`, `SetMediaUsage`, `Delete`, and `ImportModels` are available to Go callers. `Delete` accepts `agent`, `model`, `rate`, `usage`, `media-price`, and `media-usage`. The JSON schemas are illustrated by [the text example](examples/aiprice.json) and [the media example](examples/media.json); media fields are optional for compatibility with v0.1.0 files. Older v0.1.0 binaries cannot read files containing the new media fields; back up before upgrading and do not mix versions on one ledger.

For concurrent writes by cooperating v0.2.0+ processes, use `UpdateFile`: it locks, loads, invokes the callback, validates and replaces the file. The CLI uses the same lock for file operations, with a 30-second acquisition timeout. Use `LoadFile(ctx, path)` for coordinated reads; uncoordinated `Load` or direct edits can interfere with a concurrent replacement on Windows. The callback error prevents saving.

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

Embedded snapshots from [Models.dev](https://models.dev/) (2026-11-06 04:02:56 UTC) and [LiteLLM](https://github.com/BerriAI/litellm/blob/main/model_prices_and_context_window.json) (2026-11-06 04:05:40 UTC) provide **11,378 provider-qualified text model entries**, **16,344 text cache/reasoning/context-tier/batch components**, **228 audio token components**, and **366 explicit audio/video generation components**, including resolution-specific video rates. Providers represented include OpenAI, Anthropic, Google, DeepSeek, Alibaba, ByteDance/Volcengine and others where data is published. This is the coverage of these snapshots, not a guarantee of every provider, model, regional variant, tier, or price. Prices can change; verify the provider's current billing terms.

The Models.dev text list skips 999 missing, invalid, both-zero, or non-text records; free entries may need to be configured manually. Text base prices are USD per million tokens. Models.dev text and audio token components use the `media_prices` table and USD per million tokens; context threshold is encoded in the component ID. LiteLLM text IDs use `litellm/<provider>/<source-model-id>` (the source model ID may itself include a provider prefix). Other LiteLLM components use `litellm/` IDs and their explicit per-second, per-character, per-image, or per-token rates. For example `litellm/dashscope/wan3.0-video#output-second-720p` is distinct from the 1080p variant. Prices may reflect a provider, region, or intermediary, and must not be substituted across them.

In Go use `catalog.Embedded()` / `catalog.Fetch` and `catalog.EmbeddedLiteLLM()` / `catalog.FetchLiteLLM`. Import `cat.Prices()` and `lite.TextPrices` with `ImportModels` and `cat.AudioPrices`, `cat.ExtraPrices`, and `lite.Prices` with `ImportMediaPrices`. Use one `UpdateFile` callback for an atomic file update. `false` preserves existing IDs; `true` overwrites current prices. Importing does not delete older entries; inspect pre-v0.2.0 entries before relying on their modality. `store.Open` performs the initial offline import only on first creation. Online refresh is explicit; neither prices nor FX rates refresh automatically.

`QuoteText` uses the base input/output prices; it does not automatically select tiers, resolution, cache or special charges. Component prices are provided for explicit selection, and completeness varies by source. Some batch variants, discounts, tax, free tiers, minimum billing, modalities and region-specific pricing may be absent. Snapshot timestamps, source and compressed-snapshot SHA-256 hashes are recorded in [catalog/PROVENANCE.json](catalog/PROVENANCE.json). The source and licenses are in [NOTICE](NOTICE), [Models.dev license](THIRD_PARTY_MODELS_DEV_LICENSE), and [LiteLLM license](THIRD_PARTY_LITELLM_LICENSE).

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
.\aiprice.exe catalog search-components openai/gpt-realtime
.\aiprice.exe catalog search-components dashscope/wan
.\aiprice.exe -config ledger.json catalog import
.\aiprice.exe -config ledger.json catalog import online
# Explicitly overwrite matching prices; old usage snapshots remain unchanged:
.\aiprice.exe -config ledger.json catalog import online overwrite
```

`put KIND ID` reads one JSON value from stdin (`rate` is a JSON string, others are objects). `get KIND [ID]` reads one or all; `delete KIND ID` removes an item. Referenced agents and currencies cannot be deleted. A non-USD base requires a configured USD rate before importing the catalog. Rates are currency units per one base unit: with USD base and CNY `7.2`, 1 USD = 7.2 CNY. Times use RFC 3339.

## Validation and releases

Run `go test ./...` and `go vet ./...`. CI tests Linux, Windows, and macOS; the Linux job also runs the race detector. CLI packages include both READMEs and dependency licenses. Model quotes and manually entered usage are estimates, not a replacement for reconciling provider invoices.

To build release archives from a clean checkout, run `go run ./cmd/releasebuild -out <new-directory>`. The output includes six platform archives and `SHA256SUMS.txt`; the target directory must not already exist.
