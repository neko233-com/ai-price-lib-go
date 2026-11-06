# ai-price-lib-go

[English](README.md)

面向第三方 Go 项目的 AI 价格与用量库，支持 Agent、多币种、文本模型及音频／视频生成计费。用户通过 JSON 保存价格、汇率和用量，也可以使用附带的命令行工具。本库负责费用估算和记账，不调用模型供应商，也不生成音频或视频。

需要 Go 1.27 或更高版本。在项目中安装：

```sh
go get github.com/neko233-com/ai-price-lib-go@v0.2.0
```

Windows、Linux、macOS 命令行程序可从 [Releases](https://github.com/neko233-com/ai-price-lib-go/releases) 下载。文件锁依赖 `gofrs/flock` 和 `golang.org/x/sys`，许可证见 [licenses](licenses/)。

## Go 接入

以下价格均为虚构示例。音视频价格表示每 `Per` 个单位的费用；一次请求如果分别收取文本输入和音频输出等费用，应使用不同价格 ID 和用量记录。价格和数量均为十进制字符串。

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
    must(c.SetAgent("assistant", price.Agent{Name: "我的 Agent"}))
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
    fmt.Println(estimate) // 3.6 CNY；不会生成用量记录

    must(c.SetMediaUsage("video-request-1", price.MediaUsage{
        Agent: "assistant", PriceID: "video-720p", Quantity: "2.5",
        Time: "2026-11-06T12:00:00+08:00",
    }))
    report, err := c.Total("assistant", "CNY") // Agent 为空时统计全部
    must(err)
    fmt.Println(report.Total, report.Currency, report.Entries)
    must(c.Save("ledger.json")) // 首次保存；并发写入见下文
}
```

`QuoteText` 按每百万输入／输出 token 的当前价格估算；`QuoteMedia` 按当前音视频单价估算。`SetUsage` 和 `SetMediaUsage` 创建或替换用量时保存价格快照，后续修改或删除单价不会改变旧记录；替换用量记录会重新生成快照。`Total` 汇总两类记录，按**当前配置汇率**换算，在最终结果处四舍五入到 12 位小数；不保存历史汇率，也不能替代供应商账单。

`MediaPrice.Modality` 为 `audio` 或 `video`。单位使用小写标识符，例如 `second`、`character`、`token`、`request`；`Amount` 表示 `Per` 个单位的价格，`Quantity` 使用相同单位。调用方负责选择正确的模型版本、计费单位、实际数量并拆分计费项。库不会自动处理分层、分辨率、缓存、计费单位进位、税费、折扣或免费额度。

## 配置与并发写入

Go API 提供 `Load`、`Save`、`SetAgent`、`SetModel`、`SetRate`、`SetUsage`、`SetMediaPrice`、`SetMediaUsage`、`Delete` 和 `ImportModels`。`Delete` 支持 `agent`、`model`、`rate`、`usage`、`media-price`、`media-usage`。JSON 格式见[文本示例](examples/aiprice.json)和[音视频示例](examples/media.json)；音视频字段可省略，因此 v0.1.0 文件可以读取。v0.1.0 程序无法读取包含新音视频字段的文件，升级前请备份，也不要混用不同版本写同一账本。

多个 v0.2.0 或更新版本进程协作写同一文件时，应使用 `UpdateFile`。它依次加锁、读取、执行回调、校验并替换文件；回调报错时不会保存。CLI 写入使用相同的锁，等待上限为 30 秒。读取可使用 `Load`。

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

此片段另需导入 `context` 和 `time`。Context 只限制等待锁的时间，不限制回调执行时间。`Save` 和直接编辑 JSON **不会**加锁，所有写入方必须协作。保留账本旁的 `.lock` 文件；写入期间删除它会破坏协调。请使用本地文件系统和同一账本路径，避免硬链接；网络文件系统的锁语义不保证。加锁 API 拒绝账本文件本身为符号链接。保存会同步临时文件并重命名，但不是数据库事务，也不能保证所有断电场景的持久性；请单独备份，多主机或高并发记账应使用数据库。

## 模型目录

内置 [Models.dev](https://models.dev/) 快照，采集时间为 2026-11-06 11:56:26（UTC+8）：包含 **7,944 个文本到文本模型价格**和 **228 个明确标注的音频输入／输出 token 价格项**。999 条来源记录因价格缺失、无效、输入输出均为零或非文本类型而未进入文本目录；免费模型也可能被排除，可手动添加。音频价格 ID 例如 `openai/gpt-realtime-2.1#audio-output-token`，单位为每百万音频 token 的美元价格。来源目录未提供可靠的逐秒／逐次视频报价，因此视频价格需要自行配置。

Go 端可调用 `catalog.Embedded()` 或 `catalog.Fetch`，再调用 `Config.ImportModels(cat.Prices(), false)` 和 `Config.ImportMediaPrices(cat.AudioPrices, false)`。`false` 保留已有 ID，`true` 覆盖当前单价；如需一起持久化，请将两次导入放进同一个 `UpdateFile` 回调。`Search` 和 `SearchAudio` 可查询内置或在线目录。导入不会删除旧数据；使用前请检查 v0.2.0 之前导入的模型是否属于预期模态。`catalog.Fetch` 使用 nil 客户端时超时为 30 秒。

目录是社区维护的估算，可能过时，也不是实际账单。文本价格仅为基础输入／输出 token 单价；`tiered` 表示存在额外上下文计费层级，库不会自动切换。缓存、音视频、分辨率、服务商附加费用及税费均需另行处理。使用前请核对供应商价格。数据来源和许可见 [NOTICE](NOTICE) 与[上游许可证](THIRD_PARTY_MODELS_DEV_LICENSE)。

## 命令行

PowerShell 示例（Unix 使用 `./aiprice`，并按所用 shell 调整引号）：

```powershell
.\aiprice.exe -config ledger.json init USD
'"7.2"' | .\aiprice.exe -config ledger.json put rate CNY
'{"name":"我的 Agent"}' | .\aiprice.exe -config ledger.json put agent assistant
'{"modality":"video","model":"example/video-720p","currency":"USD","unit":"second","amount":"0.2","per":"1"}' | .\aiprice.exe -config ledger.json put media-price video-720p
'{"agent":"assistant","price_id":"video-720p","quantity":"2.5","time":"2026-11-06T12:00:00+08:00"}' | .\aiprice.exe -config ledger.json put media-usage request-1
.\aiprice.exe -config ledger.json report CNY assistant
.\aiprice.exe -config ledger.json get media-usage request-1
.\aiprice.exe catalog search openai/gpt
.\aiprice.exe catalog search-media openai/gpt-realtime
.\aiprice.exe -config ledger.json catalog import
.\aiprice.exe -config ledger.json catalog import online
# 显式覆盖相同 ID 的单价；历史用量快照不变
.\aiprice.exe -config ledger.json catalog import online overwrite
```

`put KIND ID` 从标准输入读取一个 JSON 值（`rate` 为字符串，其余为对象）；`get KIND [ID]` 查询单项或全部；`delete KIND ID` 删除。被用量引用的 Agent 和被价格／用量引用的币种不能删除。基准币不是 USD 时，导入目录前需要配置 USD 汇率。汇率表示 1 单位基准币可兑换多少目标币：基准为 USD、CNY 为 `7.2` 时，1 USD = 7.2 CNY。时间使用 RFC 3339。

## 验证

运行 `go test ./...` 和 `go vet ./...`。CI 覆盖 Linux、Windows、macOS，并在 Linux 运行 race detector。命令行压缩包包含两种语言的 README 和依赖许可证。模型报价和手工记录的用量均为估算，不能替代供应商账单核对。

从干净检出构建发布包：运行 `go run ./cmd/releasebuild -out <新目录>`。输出包括六个平台压缩包和 `SHA256SUMS.txt`；目标目录必须尚不存在。
