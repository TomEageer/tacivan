package app

import (
	"os"
	"os/exec"
	"strings"
)

// LanguagePreference 返回语言设置：system | zh-CN | en-US。
func (a *App) LanguagePreference() string {
	l := a.store.Settings().Language
	switch l {
	case "zh-CN", "en-US", "system":
		return l
	}
	return "system"
}

// ResolvedLanguage 把设置解析成实际语言，供原生菜单在启动时取用。
//
// 原生菜单是在 wails.Run 之前一次性建好的，之后改不了——
// 所以语言切换要等重启才在菜单上生效，界面里的其余部分是即时的。
func (a *App) ResolvedLanguage() string {
	if l := a.LanguagePreference(); l != "system" {
		return l
	}
	return systemLanguage()
}

// bundleID 与 build/darwin/Info.plist 里的 com.wails.{{safeBundleID .Name}} 一致。
const bundleID = "com.wails.Tacivan"

// applyLanguageToBundle 把语言设置写成按应用的 AppleLanguages 覆盖。
//
// 这是 macOS 自己的机制：AppKit 的关于/隐藏/退出、角色菜单、系统对话框
// 都按它取文案。不写它的话，界面切了英文，菜单栏还是系统语言，又变成半中半英。
// 「跟随系统」就把覆盖删掉。生效要重启，和菜单本身一样。
func applyLanguageToBundle(pref string) {
	switch pref {
	case "zh-CN":
		_ = exec.Command("defaults", "write", bundleID, "AppleLanguages", "-array", "zh-Hans").Run()
	case "en-US":
		_ = exec.Command("defaults", "write", bundleID, "AppleLanguages", "-array", "en").Run()
	default:
		_ = exec.Command("defaults", "delete", bundleID, "AppleLanguages").Run()
	}
}

// systemLanguage 读系统语言。
//
// 只区分中文和英文：其余语言都还没有译文，落到英文比落到中文更通用。
func systemLanguage() string {
	if v := firstNonEmpty(os.Getenv("LC_ALL"), os.Getenv("LC_MESSAGES"), os.Getenv("LANG")); v != "" {
		return normalizeLang(v)
	}
	// macOS 的图形会话里通常没有 LANG，要问 defaults。
	out, err := exec.Command("defaults", "read", "-g", "AppleLanguages").Output()
	if err == nil {
		return normalizeLang(strings.TrimSpace(string(out)))
	}
	return "zh-CN"
}

func normalizeLang(v string) string {
	lower := strings.ToLower(v)
	// AppleLanguages 返回的是一个数组字面量，取第一个有意义的标签即可。
	lower = strings.TrimLeft(lower, "(\n\t \"")
	switch {
	case strings.HasPrefix(lower, "zh"):
		return "zh-CN"
	case strings.HasPrefix(lower, "en"):
		return "en-US"
	}
	return "zh-CN"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// TitleBarDoubleClickAction 读系统偏好「双击窗口标题栏时」：Maximize | Minimize | Fill | None。
//
// 标题栏是我们自己画的，系统不会替我们做这件事；前端拿到这个值自己做，
// 行为才和别的 Mac 应用一致。
func (a *App) TitleBarDoubleClickAction() string {
	out, err := exec.Command("defaults", "read", "-g", "AppleActionOnDoubleClick").Output()
	if err != nil {
		return "Maximize" // 没设置过就是系统默认
	}
	return strings.TrimSpace(string(out))
}
