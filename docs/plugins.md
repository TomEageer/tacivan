# 插件接口

插件是一个**独立进程**，用任何语言写，和 Tacivan 之间通过 stdin/stdout 上按行分隔的
JSON-RPC 2.0 通信（和 LSP、MCP 是同一种做法）。一个插件 = 一种新的连接类型：
装上之后「新建连接」的引擎列表里会多出一项，连接对话框按插件声明的字段渲染，
对象树、SQL 编辑器、数据网格、筛选器、全库搜索、导出全部照常可用。

不用 Go 的 `plugin` 包：它要求插件用完全相同的工具链编译，macOS 上还常年不稳定；
进程协议让插件崩了也只是重启一个子进程，拖不死主程序。

## 目录

```
~/Library/Application Support/Tacivan/plugins/<id>/plugin.json   # 用户安装
<仓库>/plugins/<id>/plugin.json                                    # 开发期，随源码
```

每个子目录一个插件，`plugin.json` 是清单。坏掉的清单会被跳过并记进诊断日志，
不会挡住其它插件加载。

## 清单 plugin.json

```jsonc
{
  "id": "ro-gateway",               // 唯一；引擎名 = "plugin:" + id
  "name": "只读查询网关",
  "version": "1.0.0",
  "exec": ["node", "./main.js"],    // 启动命令，相对路径按插件目录解析
  "dialect": "mysql",               // mysql | postgres | sqlite：决定标识符引用与筛选器写法
  "readOnly": true,                 // 为真时写路径根本不存在，不是拦一下
  "fields": [                       // 连接对话框按这个渲染
    { "key": "authFile", "label": "凭证文件", "type": "text", "required": true,
      "default": "~/.config/ro-gateway/auth.conf", "hint": "…" }
    // type: text | password | number | bool | select | textarea | connection；secret:true 的值进钥匙串
    // connection：让用户选一条已有连接，值是连接 id。key 为 metaConn 时主程序会把它当"结构来源"：
    //   库/表/结构从那条连接读、只展示它上面收藏过的库和表，查询仍交给插件
  ],
  "rules": {                        // 主程序在把 SQL 交给插件之前强制执行
    "selectOnly": true,             // 只放行 SELECT / WITH 开头
    "singleStatement": true,        // 拒绝分号堆叠
    "forceLimit": 200,              // 没写 LIMIT 补上；写了但更大就压下来
    "denyPatterns": ["information_schema", "@@"]   // 命中即拒（大小写不敏感）
  }
}
```

## 协议

主程序 → 插件的请求，每行一条：

```json
{"jsonrpc":"2.0","id":1,"method":"query","params":{"database":"shop_2026","sql":"select …","limit":200}}
```

插件 → 主程序的响应：

```json
{"jsonrpc":"2.0","id":1,"result":{"columns":[{"name":"OrderID","type":"varchar"}],"rows":[["2608…"]]}}
{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"查询失败：…"}}
```

| 方法 | 入参 | 返回 | 说明 |
|---|---|---|---|
| `initialize` | `{config}` | `{name, version}` | 连接建立时第一条；`config` 是清单 fields 的键值 |
| `ping` | — | `null` | 可不实现 |
| `serverInfo` | — | `{version, extra:{}}` | 可不实现 |
| `databases` | — | `[{name}]` | 不实现则用连接上填的默认库 |
| `objects` | `{database, kinds}` | `[{name, kind, comment}]` | kind 缺省为 table |
| `tableDefinition` | `{database, name}` | `{columns:[{name,type,nullable,comment,primaryKey,default}], comment, ddl}` | 列名与类型用于表头、筛选器、编辑判定 |
| `query` | `{database, sql, limit}` | `{columns:[{name,type}], rows:[[…]]}` | `rows` 里 `null` 即 NULL；数字直接给数字 |
| `actions` | `{scope, database?, name?}` | `[{id, label, danger, fields:[…]}]` | 节点右键时问插件有哪些自定义动作；`fields` 非空时主程序先弹表单 |
| `action` | `{id, node, params}` | `{message, level, refresh}` | 执行动作；`refresh` 为真主程序刷新该连接的树 |
| `shutdown` | — | `null` | 断开连接时；之后进程应自行退出 |

`databases` / `objects` 返回的节点可以带 `status` / `detail` / `color`：树上按 `color` 给名字着色、
`detail` 显示在右侧——"已核验 / 出错(标红)"这类业务状态全由插件决定，主程序只负责画。

### 插件 → 主程序（反向调用）

同一条管道，方法名以 `host.` 开头，主程序只暴露**读**能力：

| 方法 | 入参 | 返回 |
|---|---|---|
| `host.connections` | — | `[{id, name, engine, open}]`（不含插件连接） |
| `host.favorites` | `{connId}` | `[{database, schema, name, kind}]` |
| `host.databases` | `{connId}` | `[{name, …}]` |
| `host.objects` | `{connId, database}` | `[{name, kind, …}]` |
| `host.tableDefinition` | `{connId, database, name}` | `{columns:[…], ddl}` |

没打开的连接主程序会替插件打开。插件拿不到密码，也拿不到任何写入口。

`initialize` 的 `config` 里除了清单字段，还有 `__connId`（本连接 id）和 `__dataDir`
（插件私有数据目录，`<配置目录>/plugin-data/<id>/`，删插件不会删它）。

没实现的方法返回 `{"error":{"code":-32601}}`，主程序会降级（比如对象树为空）而不是报错。
`stderr` 原样进主程序的诊断日志，调试就往那儿打。

## 主程序替插件做的事

- **只读**：`readOnly` 的连接在主程序里没有 Exec 路径，数据网格不可编辑、结构变更按钮不出现
- **门禁**：`rules` 在主程序里先执行一遍；插件自己再校验是加分项，但主程序不依赖它
- **结果集**：插件一次性返回一页，主程序包成流，走和其它引擎相同的分块缓存/内存预算
- **进程**：插件崩了下一次调用会自动拉起并重新 `initialize`

## 示例：一个只读网关插件

假设公司有一条"HTTP 网关 → 生产库只读 SELECT"的通道，网关列不了库表、也不让碰
`information_schema`。用插件接进来，**所有业务都在插件里**，主程序不知道"生产库"这回事：

- 凭证只从插件自己指定的文件读，不进应用配置也不进钥匙串
- 库表由插件自己维护在 `__dataDir` 里：右键连接 →「从结构来源连接导入收藏」（借 `host.favorites` /
  `host.objects` 读另一条内网连接上收藏的库表）、「添加库…」、库上「添加表…」「核验」
- 核验 = 真发一条 `select 1` / `select * … limit 1` 走网关；失败的库/表标红并把错误写在右侧
- 表结构优先借 `host.tableDefinition` 从结构来源连接读真实结构，没有就 `select * limit 1` 推列名
- 分库路由：库名末尾四位年份 → 逻辑库名 + 日期参数；也可在 SQL 里写 `-- date: 2026-08-10`
- SQL 里不带库名前缀（主程序的插件方言保证），网关按参数路由到物理分片
- 行数上限按 表 > 库 > 默认 三级取，没配就是 1，并且不允许网格自动翻页

这类插件通常含公司内部地址与库名，**不要提交进公开仓库**：放在
`~/Library/Application Support/Tacivan/plugins/` 或任何私有目录，通过「设置 → 插件 → 导入」装入即可。
