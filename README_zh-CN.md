# ai-price-lib-go

[English](README.md)

面向第三方 Go 项目的 AI 价格与用量库，支持 Agent、多币种、文本模型及音频／视频生成计费。用户通过 JSON 保存价格、汇率和用量，也可以使用附带的命令行工具。本库负责费用估算和记账，不调用模型供应商，也不生成音频或视频。

需要 Go 1.27 或更高版本。在项目中安装：

```sh
go get github.com/neko233-com/ai-price-lib-go@v0.2.0
```

Windows、Linux、macOS 命令行程序可从 [Releases](https://github.com/neko233-com/ai-price-lib-go/releases) 下载。文件锁依赖 `gofrs/flock` 和 `golang.org/x/sys`，许可证见 [licenses](licenses/)。

## 服务端接入：首次生成与热更新

在服务启动时调用 `store.Open`。默认仅在进程**当前工作目录**下不存在 `aiprice.json` 时创建文件，并导入内置目录；已有文件不会覆盖。随后每秒轮询配置。初始化是显式调用的，单纯 import 不会产生文件。默认只配置 USD，使用其他货币前需自行添加汇率。

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

服务运行期间保留 Store；通过 `store.Options{Path: ..., PollInterval: ...}` 配置路径和间隔，通过 `s.Update(ctx, func(c *price.Config) error { ... })` 协调写入。`s.QuoteText`、`s.QuoteMedia` 和 `s.Total` 使用当前有效快照，`s.Snapshot` 返回独立副本。外部修改在成功轮询后生效；文件缺失、写入不完整或校验失败时保留上一份有效快照，错误通过 `s.Status().LastError` 提供，文件不会被静默重建。请对该状态接入监控。`Close` 停止轮询，传入 `Open` 的 Context 取消也会停止。`Options.BaseCurrency` 和 `Rates` 只在首次创建时生效；基准币不是 USD 时需同时提供 USD 汇率，以导入目录。

包含 `#context-<阈值>-...` 的价格 ID 需按上下文大小自行选择。缓存读取和推理 token 可能包含在供应商的输入／输出总数中，调用方应按供应商定义传入互不重叠的数量，避免重复计算。生成的价格和用量均可编辑。

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

`MediaPrice.Modality` 为 `text`、`audio` 或 `video`；文本计费项包括缓存和上下文分层 token 单价。单位使用小写标识符，例如 `second`、`character`、`token`、`request`；`Amount` 表示 `Per` 个单位的价格，`Quantity` 使用相同单位。调用方负责选择正确的模型版本、计费单位、实际数量并拆分计费项。库不会自动处理分层、分辨率、缓存、计费单位进位、税费、折扣或免费额度。

## 配置与并发写入

Go API 提供 `Load`、`Save`、`SetAgent`、`SetModel`、`SetRate`、`SetUsage`、`SetMediaPrice`、`SetMediaUsage`、`Delete` 和 `ImportModels`。`Delete` 支持 `agent`、`model`、`rate`、`usage`、`media-price`、`media-usage`。JSON 格式见[文本示例](examples/aiprice.json)和[音视频示例](examples/media.json)；音视频字段可省略，因此 v0.1.0 文件可以读取。v0.1.0 程序无法读取包含新音视频字段的文件，升级前请备份，也不要混用不同版本写同一账本。

多个 v0.2.0 或更新版本进程协作写同一文件时，应使用 `UpdateFile`。它依次加锁、读取、执行回调、校验并替换文件；回调报错时不会保存。CLI 文件操作使用相同的锁，等待上限为 30 秒。协作读取使用 `LoadFile(ctx, path)`；未加锁的 `Load` 或直接编辑可能在 Windows 上干扰并发文件替换。

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

内置 [Models.dev](https://models.dev/)（2026-11-06 12:02:56，UTC+8）和 [LiteLLM](https://github.com/BerriAI/litellm/blob/main/model_prices_and_context_window.json)（2026-11-06 12:05:40，UTC+8）快照，包含 **11,378 个带供应商标识的文本模型条目**、**16,344 个文本缓存／推理／上下文分层／批量价格项**、**228 个音频 token 价格项**和 **366 个音视频生成价格项**，包括视频不同分辨率的价格。来源涉及 OpenAI、Anthropic、Google、DeepSeek、阿里、字节／火山等有公开数据的供应商。这是快照的实际覆盖，不保证包含每家供应商、所有模型、地区版本、计费层级或全部价格；价格会变化，使用前请核对供应商现行规则。

Models.dev 文本目录排除了 999 条缺失、无效、双零或非文本记录；免费模型可能需要自行添加。文本基础价格为美元／百万 token。Models.dev 文本附加项和音频 token 项保存在 `media_prices`，单位为美元／百万 token，上下文阈值包含在价格 ID 中。LiteLLM 文本 ID 使用 `litellm/<provider>/<source-model-id>`（来源模型 ID 本身可能已含供应商前缀）。其他 LiteLLM 计费项使用 `litellm/` 前缀，根据来源分别按秒、字符、图片或 token 计价；例如 `litellm/dashscope/wan3.0-video#output-second-720p` 与 1080p 价格分开。报价可能对应不同供应商、地区或中转平台，不应混用。

Go 端可调用 `catalog.Embedded()`／`catalog.Fetch` 与 `catalog.EmbeddedLiteLLM()`／`catalog.FetchLiteLLM`；通过 `ImportModels` 导入 `cat.Prices()` 和 `lite.TextPrices`，通过 `ImportMediaPrices` 分别导入 `cat.AudioPrices`、`cat.ExtraPrices` 和 `lite.Prices`。需要一起写入时放入同一个 `UpdateFile` 回调。`false` 保留已有 ID，`true` 覆盖当前单价。导入不会删除旧数据；使用前请检查 v0.2.0 之前导入的模型是否属于预期模态。`store.Open` 仅首次创建时离线导入；在线刷新须显式调用，价格和汇率不会自动联网刷新。

`QuoteText` 使用基础输入／输出单价，不会自动选择分层、分辨率、缓存或特殊费用。附加价格项供调用方显式选择，完整度依赖来源；部分批量价格、折扣、税费、免费额度、最小计费、模态和地区报价可能缺失。快照时间、来源数据和压缩快照的 SHA-256 见 [catalog/PROVENANCE.json](catalog/PROVENANCE.json)。来源及许可见 [NOTICE](NOTICE)、[Models.dev 许可证](THIRD_PARTY_MODELS_DEV_LICENSE)和 [LiteLLM 许可证](THIRD_PARTY_LITELLM_LICENSE)。

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
.\aiprice.exe catalog search-components openai/gpt-realtime
.\aiprice.exe catalog search-components dashscope/wan
.\aiprice.exe -config ledger.json catalog import
.\aiprice.exe -config ledger.json catalog import online
# 显式覆盖相同 ID 的单价；历史用量快照不变
.\aiprice.exe -config ledger.json catalog import online overwrite
```

`put KIND ID` 从标准输入读取一个 JSON 值（`rate` 为字符串，其余为对象）；`get KIND [ID]` 查询单项或全部；`delete KIND ID` 删除。被用量引用的 Agent 和被价格／用量引用的币种不能删除。基准币不是 USD 时，导入目录前需要配置 USD 汇率。汇率表示 1 单位基准币可兑换多少目标币：基准为 USD、CNY 为 `7.2` 时，1 USD = 7.2 CNY。时间使用 RFC 3339。

## 验证

运行 `go test ./...` 和 `go vet ./...`。CI 覆盖 Linux、Windows、macOS，并在 Linux 运行 race detector。命令行压缩包包含两种语言的 README 和依赖许可证。模型报价和手工记录的用量均为估算，不能替代供应商账单核对。

从干净检出构建发布包：运行 `go run ./cmd/releasebuild -out <新目录>`。输出包括六个平台压缩包和 `SHA256SUMS.txt`；目标目录必须尚不存在。
