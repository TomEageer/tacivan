# MCP 服务器作为连接

新建连接 → 引擎选「**MCP 服务器**」。不接大模型：树上列出 server 的**工具**和**资源**，
工具用表单手动调用，资源直接读。每一次调用都是人按下去的，"查错库"的责任链是清楚的。

## 两种传输

| 传输 | 填什么 | 对应 |
|---|---|---|
| **stdio** | 启动命令一行（`npx -y xxx` / `python -m yyy`），可选环境变量 | Claude Desktop 配置里的 `command` + `args` + `env` |
| **Streamable HTTP** | URL，可选请求头（`Authorization: Bearer …`） | 云端 MCP（如阿里云 DMS 的 `/sse` 地址） |

## 工具调用

参数表单按工具声明的 JSON Schema 生成：字符串 / 数字 / 布尔 / 枚举给对应控件，
对象和数组给 JSON 文本框，必填项带星号。

结果**长得像表**（对象数组，或 `{rows|items|data|results|list: [...]}`）就进网格——筛选、导出、单行视图和其它引擎一样；
其余当文本显示。结果集走同一套分块缓存和内存预算。

## 实现

`internal/mcp` 是一个最小客户端（`initialize` / `tools/list` / `tools/call` / `resources/list` / `resources/read`，协议 2025-06-18，
自动翻页），`internal/driver/mcpdrv` 把它包成引擎。对着官方 SDK 写的真实 server（FastMCP 1.28）验证过握手与列表；
`TACIVAN_TEST_MCP_CMD="…" go test ./internal/mcp/ -run TestRealServerHandshake -v` 可对任意本地 server 复跑。
