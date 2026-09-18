import { defineStore } from 'pinia'
import { ref, computed, watch } from 'vue'
import * as api from '../api'
import type { Settings, ResourceStats, AppInfo } from '../types'
import { tr, resolveLocale, setLocale, type Locale } from '../i18n'

/** 一条顶部通知。 */
export interface Toast {
  id: number
  kind: 'info' | 'success' | 'warning' | 'error'
  title: string
  detail?: string
  /** 毫秒；0 表示不自动消失。 */
  timeout: number
}

let toastSeq = 0

export const useAppStore = defineStore('app', () => {
  const settings = ref<Settings>({
    theme: 'system',
    language: 'system',
    gridPageSize: 200,
    rowLimit: 1000,
    memoryLimitMb: 512,
    cellPreviewLimit: 4096,
    maxResultRows: 5000000,
    autoCommit: true,
    confirmOnDelete: true,
    fontSize: 13,
    editorFont: 'SF Mono, Menlo, monospace',
    showSystemObjects: false,
    historyLimit: 500,
    diagnosticLog: true,
    slowQueryMs: 300,
  })
  const info = ref<AppInfo | null>(null)
  const stats = ref<ResourceStats | null>(null)
  const toasts = ref<Toast[]>([])

  /**
   * 系统是否处于深色模式。
   *
   * 必须是响应式的 ref，不能在 computed 里直接读 matchMedia().matches——
   * 那个值不是响应式数据源，computed 的依赖不会因为系统换主题而失效，
   * 于是缓存住旧值，监听到事件也白搭（之前就是这么坏的）。
   */
  // matchMedia 在部分宿主环境（测试用的 jsdom、精简版 WebView）里并不存在，
  // 取不到就按浅色处理，不让整个 store 初始化失败。
  const darkQuery =
    typeof window !== 'undefined' && typeof window.matchMedia === 'function'
      ? window.matchMedia('(prefers-color-scheme: dark)')
      : null
  const systemDark = ref(darkQuery?.matches ?? false)
  darkQuery?.addEventListener?.('change', (e) => {
    systemDark.value = e.matches
  })

  /** 当前生效的主题（把 system 解析成具体值）。 */
  const effectiveTheme = computed<'light' | 'dark'>(() => {
    if (settings.value.theme === 'light') return 'light'
    if (settings.value.theme === 'dark') return 'dark'
    return systemDark.value ? 'dark' : 'light'
  })

  /**
   * 生效的语言。和主题一样，system 要能实时跟随，所以依赖响应式来源。
   */
  const effectiveLocale = computed<Locale>(() => resolveLocale(settings.value.language))

  function applyLocale() {
    setLocale(effectiveLocale.value)
  }
  watch(effectiveLocale, applyLocale)

  function applyTheme() {
    document.documentElement.setAttribute('data-theme', effectiveTheme.value)
    document.documentElement.style.setProperty('--font-size-mono', `${settings.value.fontSize - 1}px`)
  }

  // 主题一变就落到 DOM 上，不依赖调用方记得手动调 applyTheme。
  watch(effectiveTheme, applyTheme)
  watch(() => settings.value.fontSize, applyTheme)

  /** 在「跟随系统 / 浅色 / 深色」之间轮换，供状态栏的快捷切换使用。 */
  async function cycleTheme() {
    const order = ['system', 'light', 'dark'] as const
    const next = order[(order.indexOf(settings.value.theme) + 1) % order.length]
    await saveSettings({ ...settings.value, theme: next }, true)
    const label = { system: tr('跟随系统'), light: tr('浅色'), dark: tr('深色') }[next]
    toast('info', tr('外观：{name}', { name: label }))
  }

  async function load() {
    settings.value = await api.GetSettings()
    info.value = await api.GetAppInfo()
    applyLocale()
    applyTheme()
  }

  async function saveSettings(next: Settings, silent = false) {
    settings.value = await api.SaveSettings(next)
    applyLocale()
    applyTheme()
    if (!silent) toast('success', tr('设置已保存'))
  }

  async function refreshStats() {
    stats.value = await api.GetResourceStats()
  }

  async function releaseMemory() {
    stats.value = await api.ReleaseMemory()
    toast('success', tr('已回收内存'), tr('当前占用 {mb} MB', { mb: stats.value.processHeapMb.toFixed(1) }))
  }

  function toast(kind: Toast['kind'], title: string, detail?: string) {
    const id = ++toastSeq
    // 错误信息通常需要读完整，不自动消失；其余几秒后淡出。
    const timeout = kind === 'error' ? 0 : 3200
    toasts.value.push({ id, kind, title, detail, timeout })
    if (timeout > 0) setTimeout(() => dismiss(id), timeout)
    return id
  }

  function dismiss(id: number) {
    const i = toasts.value.findIndex((t) => t.id === id)
    if (i >= 0) toasts.value.splice(i, 1)
  }

  /** 把后端返回的错误统一转成一条通知。 */
  function reportError(e: unknown, context?: string) {
    const msg = e instanceof Error ? e.message : String(e)
    toast('error', context || tr('操作失败'), msg)
  }

  return {
    settings,
    info,
    stats,
    toasts,
    effectiveTheme,
    effectiveLocale,
    applyLocale,
    systemDark,
    cycleTheme,
    load,
    applyTheme,
    saveSettings,
    refreshStats,
    releaseMemory,
    toast,
    dismiss,
    reportError,
  }
})
