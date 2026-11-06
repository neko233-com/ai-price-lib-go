# ai-price-lib-go

基于 Go 1.27 的本地 AI 用量账本和价格计算库。通过 JSON 配置维护 Agent、模型价格、汇率及用量，提供 Go API 和命令行。无第三方依赖。

从 [GitHub Releases](https://github.com/neko233-com/ai-price-lib-go/releases) 下载对应系统的压缩包；Go 库使用 `go get github.com/neko233-com/ai-price-lib-go@v0.1.0`。构建项目需要 Go 1.27。

## 计费规则

- 模型价格是每 100 万输入 / 输出 token 的单价。每条记录分别填写 token 数量并保存创建或更新时的价格快照；后续调整或删除模型价格不会改变旧记录。
- 汇率配置表示 **1 单位 base_currency 可兑换多少单位目标货币**。例如 USD 为基准，CNY 为 `7.2`：1 USD = 7.2 CNY；跨币种按基准币间接换算。
- 价格和汇率使用非负十进制字符串，汇率须大于零。内部使用精确有理数累计，最终结果四舍五入到最多 12 位小数；无需逐条取整。
- 报表使用当前配置汇率，不保存历史汇率；它是估算，不自动获取实时汇率、供应商价格或实际账单。示例值仅用于演示。
- 金额为成本而非代币余额；调用方可以记录实际用量或手动修改。缓存 token、不同计费层级、折扣、税费及额度上限需要调用方自行建模。

## 开始使用

安装 Go 1.27 后，在此目录执行：

```powershell
go test ./...
go build -o aiprice.exe ./cmd/aiprice
.\aiprice.exe -config ledger.json init USD
'"7.2"' | .\aiprice.exe -config ledger.json put rate CNY
'{"name":"我的 Agent"}' | .\aiprice.exe -config ledger.json put agent assistant
'{"currency":"USD","input_per_million":"2.5","output_per_million":"10"}' | .\aiprice.exe -config ledger.json put model example-model
'{"agent":"assistant","model":"example-model","input_tokens":100000,"output_tokens":20000,"time":"2026-11-06T12:00:00+08:00"}' | .\aiprice.exe -config ledger.json put usage request-001
.\aiprice.exe -config ledger.json report CNY assistant
.\aiprice.exe -config ledger.json convert 1 USD CNY
```

示例报表是 `{"currency":"CNY","total":"3.24","entries":1}`。也可以复制 [示例配置](examples/aiprice.json) 后按需编辑；所有时间使用 RFC 3339。币种使用大写三字母代码。模型 ID、Agent ID 和用量 ID 由使用方自行命名，不能有首尾空格。

```powershell
# 查询全部或指定项；put 同一 ID 即更新
.\aiprice.exe -config ledger.json get usage
.\aiprice.exe -config ledger.json get usage request-001
.\aiprice.exe -config ledger.json delete usage request-001
.\aiprice.exe -config ledger.json delete model example-model
```

`agent`、`model`、`rate`、`usage` 均支持 `put`、`get`、`delete`。`put` 从标准输入读取一个 JSON 值，`rate` 是 JSON 字符串，其余是 JSON 对象。`put usage` 更新时会重新按该模型的当前价格生成快照。删除被记录引用的 Agent 或仍被模型 / 记录引用的币种会被拒绝；先调整或删除引用项。程序校验完整配置，拒绝未知字段和不合法数据，写文件前先写入同目录临时文件再替换。请自行序列化多进程读改写操作，当前没有跨进程锁；外部手动编辑也应遵守这一限制。

## Go API

在 Go 项目中引用：

```go
package main

import (
    "fmt"
    price "github.com/neko233-com/ai-price-lib-go"
)

func main() {
    c, err := price.Load("ledger.json")
    if err != nil { panic(err) }
    report, err := c.Total("assistant", "CNY") // 空 Agent ID 统计全部
    if err != nil { panic(err) }
    fmt.Println(report.Total, report.Currency)
    // 写入使用 c.SetAgent、SetModel、SetRate、SetUsage、Delete，最后 c.Save。
}
```

公开字段适合读取和自定义配置；直接修改字段后，调用 `Validate` 或 `Save` 才会校验。文件保存不是数据库事务或多进程并发账本，不适合未经外部同步的并发记账。

## 模型目录

内置 [Models.dev](https://models.dev/) 公开数据的离线快照，采集时间 **2026-11-06 10:17:37 UTC+8**。此次快照包含 7,987 个供应商模型报价，跳过 956 个缺少输入或输出报价、无效或输入输出均为零的条目；免费模型可能因此被排除。数据遵循上游 MIT 许可，见 [NOTICE](NOTICE) 和 [上游许可证](THIRD_PARTY_MODELS_DEV_LICENSE)。

```powershell
# 查询内置快照，ID 为供应商/模型，价格是 USD 每百万 token
.\aiprice.exe catalog search openai/gpt
# 导入快照，不覆盖已有模型
.\aiprice.exe -config ledger.json catalog import
# 下载当前目录并导入，默认仍保留已有价格
.\aiprice.exe -config ledger.json catalog import online
# 显式覆盖同 ID 的现有模型价格；历史用量快照不变
.\aiprice.exe -config ledger.json catalog import online overwrite
.\aiprice.exe -config ledger.json get model openai/gpt-5.3-codex
```

如果账本的基准币不是 USD，先配置 USD 汇率（1 单位基准币等于多少 USD），再导入。导入结果包含数据来源、抓取时间、模型数量、跳过数量和分层模型数量。也可通过 `catalog.Embedded`、`catalog.Fetch`、`Catalog.Search` 和 `Config.ImportModels` 在 Go 程序中调用。

**价格边界：**目录仅导入基础输入和输出 token 的美元单价。`catalog search` 的 `tiered` 提示模型存在上下文价格层级，账本不会自动切换层级；缓存读写、音频、图片、推理、批量、折扣及其他特殊计费同样不会单独计算。目录是社区数据，可能过时或与服务商实际账单不同；应核对服务商价格并按需手动修改。没有实时汇率或自动刷新服务，在线刷新由用户主动触发。
