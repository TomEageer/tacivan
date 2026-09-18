// Navigo 是一个开源的图形化数据库客户端，界面与操作逻辑对齐 Navicat，
// 支持 MySQL / MariaDB / PostgreSQL / SQLite / Redis。
//
// 相对同类工具的主要差别在结果集引擎：行数据以块为单位缓存、受全局内存预算约束、
// 超限自动溢出到磁盘，因此打开超大表或执行宽结果集查询时内存占用是有界的。
package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"tacivan/internal/app"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	a, err := app.New()
	if err != nil {
		log.Fatalf("初始化失败: %v", err)
	}

	// 窗口背景与原生外观在创建时定死，这里先按设置/系统外观算好，
	// 免得深色下启动闪一帧白底。颜色与前端的 --c-canvas 保持一致。
	dark := a.InitialAppearance() == "dark"
	bg := &options.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 1}
	if dark {
		bg = &options.RGBA{R: 0x17, G: 0x18, B: 0x1c, A: 1}
	}
	appearance := mac.DefaultAppearance // 空值即跟随系统
	switch a.ThemePreference() {
	case "dark":
		appearance = mac.NSAppearanceNameDarkAqua
	case "light":
		appearance = mac.NSAppearanceNameAqua
	}

	err = wails.Run(&options.App{
		Title:     "Tacivan",
		Width:     1440,
		Height:    900,
		MinWidth:  1024,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		Menu:             buildMenu(a),
		OnStartup:        a.Startup,
		OnShutdown:       a.Shutdown,
		Bind:             []any{a},
		BackgroundColour: bg,
		Mac: &mac.Options{
			// 标题栏透明、内容延伸上去，红绿灯浮在顶栏左上角。
			//
			// 用系统标准标题栏时，那一条的底色由 macOS 决定，和下面工具栏的颜色
			// 永远对不齐，中间留着一道很显眼的缝；靠调色是补不平的。
			// 合并之后顶部整片都是同一块背景，前提是工具栏左侧给红绿灯留出位置——
			// 之前挤在一起是因为没留，不是这个方案本身不行。
			//
			// 用 TitleBarHidden 而不是 ...HiddenInset：Inset 那一版会挂一个
			// NSToolbar，标题栏被撑高，红绿灯跟着往下沉，和我自己画的 28px
			// 标题行怎么都对不齐。不挂工具栏时红绿灯回到标准位置，
			// 正好落在 28px 行的垂直中心上。
			TitleBar:             mac.TitleBarHidden(),
			Appearance:           appearance,
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			About: &mac.AboutInfo{
				Title:   "Tacivan",
				Message: "开源数据库客户端\n支持 MySQL / MariaDB / PostgreSQL / SQLite / Redis",
			},
		},
	})
	if err != nil {
		log.Fatalf("启动失败: %v", err)
	}
}

// emit 向前端发送一个事件，菜单项据此触发界面动作。
func emit(a *app.App, name string) func(*menu.CallbackData) {
	return func(*menu.CallbackData) {
		if ctx := a.Context(); ctx != nil {
			wruntime.EventsEmit(ctx, name)
		}
	}
}

// buildMenu 构造原生菜单栏，结构对齐 Navicat 的菜单组织。
func buildMenu(a *app.App) *menu.Menu {
	m := menu.NewMenu()
	// 菜单是在 wails.Run 之前一次性建好的，之后改不了，
	// 所以语言在这里定死；界面里切语言要重启才能带动菜单。
	lang := a.ResolvedLanguage()

	m.Append(menu.AppMenu())

	file := m.AddSubmenu(mt(lang, "文件"))
	file.AddText(mt(lang, "新建连接..."), keys.CmdOrCtrl("n"), emit(a, "menu:new-connection"))
	file.AddText(mt(lang, "新建查询"), keys.Combo("t", keys.CmdOrCtrlKey, keys.OptionOrAltKey), emit(a, "menu:new-query"))
	file.AddSeparator()
	file.AddText(mt(lang, "打开 SQL 脚本..."), keys.CmdOrCtrl("o"), emit(a, "menu:open-sql"))
	file.AddText(mt(lang, "保存"), keys.CmdOrCtrl("s"), emit(a, "menu:save"))
	file.AddText(mt(lang, "另存为..."), keys.Combo("s", keys.CmdOrCtrlKey, keys.ShiftKey), emit(a, "menu:save-as"))
	file.AddSeparator()
	file.AddText(mt(lang, "导出结果集..."), keys.Combo("e", keys.CmdOrCtrlKey, keys.ShiftKey), emit(a, "menu:export"))
	file.AddSeparator()
	file.AddText(mt(lang, "关闭标签页"), keys.CmdOrCtrl("w"), emit(a, "menu:close-tab"))

	// 编辑菜单整体用系统角色菜单：撤销/剪切/复制/粘贴都是 role 项，
	// 自己转发反而会丢掉输入框里的选区与原生行为，且它不接受追加子项。
	m.Append(menu.EditMenu())

	query := m.AddSubmenu(mt(lang, "查询"))
	query.AddText(mt(lang, "运行"), keys.CmdOrCtrl("r"), emit(a, "menu:run"))
	query.AddText(mt(lang, "运行当前语句"), keys.Combo("r", keys.CmdOrCtrlKey, keys.ShiftKey), emit(a, "menu:run-current"))
	query.AddText(mt(lang, "停止"), keys.CmdOrCtrl("."), emit(a, "menu:stop"))
	query.AddSeparator()
	query.AddText(mt(lang, "解释执行计划"), keys.CmdOrCtrl("e"), emit(a, "menu:explain"))
	query.AddSeparator()
	query.AddText(mt(lang, "查找"), keys.CmdOrCtrl("f"), emit(a, "menu:find"))
	query.AddText(mt(lang, "格式化 SQL"), keys.Combo("f", keys.CmdOrCtrlKey, keys.ShiftKey), emit(a, "menu:format-sql"))

	object := m.AddSubmenu(mt(lang, "对象"))
	object.AddText(mt(lang, "刷新"), keys.Key("f5"), emit(a, "menu:refresh"))
	object.AddSeparator()
	object.AddText(mt(lang, "新建表"), nil, emit(a, "menu:new-table"))
	object.AddText(mt(lang, "设计表"), nil, emit(a, "menu:design-table"))
	object.AddText(mt(lang, "打开表数据"), nil, emit(a, "menu:open-table"))

	tools := m.AddSubmenu(mt(lang, "工具"))
	tools.AddText(mt(lang, "在数据库中查找..."), keys.Combo("f", keys.CmdOrCtrlKey, keys.ShiftKey), emit(a, "menu:find-in-database"))
	tools.AddSeparator()
	tools.AddText(mt(lang, "资源监视器"), keys.Combo("m", keys.CmdOrCtrlKey, keys.ShiftKey), emit(a, "menu:resource-monitor"))
	tools.AddText(mt(lang, "查询历史"), keys.Combo("h", keys.CmdOrCtrlKey, keys.ShiftKey), emit(a, "menu:history"))
	tools.AddText(mt(lang, "服务器监控"), nil, emit(a, "menu:server-monitor"))

	m.Append(menu.WindowMenu())
	return m
}
