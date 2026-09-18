package main

// 原生菜单的英文文案，按中文原文索引——和前端用的是同一套做法。
//
// 菜单只有这几十条，单独放在这里比引一套 Go 的 i18n 库省事得多；
// 需要第三种语言时再考虑把两边合并成一份可共享的资源。
var menuEN = map[string]string{
	"文件":           "File",
	"新建连接...":      "New Connection…",
	"新建查询":         "New Query",
	"打开 SQL 脚本...": "Open SQL Script…",
	"保存":           "Save",
	"另存为...":       "Save As…",
	"导出结果集...":     "Export Result Set…",
	"关闭标签页":        "Close Tab",

	"查询":      "Query",
	"运行":      "Run",
	"运行当前语句":  "Run Current Statement",
	"停止":      "Stop",
	"解释执行计划":  "Explain Query Plan",
	"查找":      "Find",
	"格式化 SQL": "Format SQL",

	"对象":    "Object",
	"刷新":    "Refresh",
	"新建表":   "New Table",
	"设计表":   "Design Table",
	"打开表数据": "Open Table Data",

	"工具":         "Tools",
	"在数据库中查找...": "Find in Database…",
	"资源监视器":      "Resource Monitor",
	"查询历史":       "Query History",
	"服务器监控":      "Server Monitor",
}

// mt 取一条菜单文案。中文直接返回原文，缺译文也回落原文。
func mt(lang, s string) string {
	if lang != "en-US" {
		return s
	}
	if v, ok := menuEN[s]; ok {
		return v
	}
	return s
}
