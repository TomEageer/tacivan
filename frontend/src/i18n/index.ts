/**
 * 轻量国际化。
 *
 * 没有引 vue-i18n：这里需要的只是「按 key 取字符串 + 插值 + 切换语言时重新渲染」，
 * 自己写不到一百行，多一个依赖就多一套要跟着升级的东西。
 *
 * key 直接用中文原文（gettext 的做法），不另造一套 `tab.close` 式的标识符。
 * 理由是这个项目的实际约束：
 *   - 少维护一份和源码一一对应的中文字典，那份东西纯属复制
 *   - 漏包一处 t() 时中文界面完全正常，只有英文界面露出中文——
 *     退化是渐进的，不会变成满屏 `tab.close`
 *   - 翻译文件长得就是「中文 → English」，给人看比 key → English 直观
 * 代价是改中文文案会断掉对应的译文，两种语言的规模下这个代价可以接受。
 */
import { computed, ref } from 'vue'
import enUS from './en-US'

export type Locale = 'zh-CN' | 'en-US'
/** 语言设置；system 表示跟随系统。 */
export type LanguagePreference = 'system' | Locale

export type Dict = Record<string, string>

/** zh-CN 不需要字典：源码里的中文就是它自己。 */
const DICTS: Partial<Record<Locale, Dict>> = { 'en-US': enUS }

export const LOCALE_NAMES: Record<Locale, string> = {
  'zh-CN': '简体中文',
  'en-US': 'English',
}

export const locale = ref<Locale>('zh-CN')

/**
 * 按系统语言挑一个。
 *
 * 只认语言主标签：zh-Hans-CN、zh-TW、zh 都走中文，其余一律英文。
 * 繁体单独做一份的价值不大，先合并，将来要拆也只是多一个字典。
 */
export function detectLocale(): Locale {
  const nav = typeof navigator === 'undefined' ? undefined : navigator
  const langs = nav ? (nav.languages ?? [nav.language]) : []
  for (const l of langs) {
    if (!l) continue
    const lower = l.toLowerCase()
    if (lower.startsWith('zh')) return 'zh-CN'
    if (lower.startsWith('en')) return 'en-US'
  }
  return 'zh-CN'
}

export function resolveLocale(pref: LanguagePreference | undefined): Locale {
  if (pref === 'zh-CN' || pref === 'en-US') return pref
  return detectLocale()
}

export function setLocale(l: Locale) {
  locale.value = l
  if (typeof document !== 'undefined') document.documentElement.lang = l
}

/**
 * 取一条文案。zh 为原文直返；其余语言查表，查不到回落原文。
 *
 * 插值写成 {name}，参数按名字替换；没给到的占位符原样留着，
 * 这样漏传参数是看得见的，而不是悄悄变成空白。
 */
export function t(key: string, params?: Record<string, string | number>): string {
  const dict = DICTS[locale.value]
  let s = dict?.[key] ?? key
  if (params) {
    s = s.replace(/\{(\w+)\}/g, (m, name: string) => (name in params ? String(params[name]) : m))
  }
  return s
}

/**
 * 组件里统一用 tr 这个名字，不用更顺口的 t。
 *
 * 原因很实际：代码里 `const t = tabs.active`、`for (const t of tabs)`
 * 这类局部变量到处都是，全局引入一个叫 t 的函数会和它们撞，
 * 撞了还不一定报错（比如 t 恰好是个对象）。tr 没有这个问题。
 */
export const tr = t

/** 在 <script setup> 里用；返回的 t 是响应式的，切换语言会重渲染。 */
export function useI18n() {
  return {
    t: (key: string, params?: Record<string, string | number>) => {
      // 读一下 locale，让计算属性与渲染函数把它记为依赖。
      void locale.value
      return t(key, params)
    },
    locale: computed(() => locale.value),
  }
}
