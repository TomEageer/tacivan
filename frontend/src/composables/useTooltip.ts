import { reactive } from 'vue'

/**
 * 全局提示层。
 *
 * 不用原生 title：系统要等 1~2 秒才弹，查数据时想快速确认某个字段是什么意思，
 * 等这么久已经打断思路了。这里统一走 150ms，并且内容可以排版
 * （字段的类型、可空、默认值、注释分行展示）。
 *
 * 做成单例而不是每个元素挂一个组件：列头几十上百个，
 * 每个都塞一个组件实例是纯浪费。
 */

/** 提示里的一行键值。 */
export interface TipRow {
  label: string
  value: string
  /** 值用等宽字体（类型、默认值这类）。 */
  mono?: boolean
  /** 强调色，用于主键、只读这类标记。 */
  tone?: 'meta' | 'warning' | 'danger' | 'success'
}

export interface TipContent {
  title?: string
  /** 标题右侧的小标签，如 PK、NOT NULL。 */
  tags?: { text: string; tone?: TipRow['tone'] }[]
  rows?: TipRow[]
  /** 大段文本，如字段注释。 */
  note?: string
}

export const tipState = reactive({
  visible: false,
  x: 0,
  y: 0,
  /** 触发元素的位置，用于避让。 */
  anchor: { top: 0, bottom: 0, left: 0 },
  content: {} as TipContent,
})

let showTimer: number | undefined
let hideTimer: number | undefined

/** 展示提示。delay 默认 150ms —— 快到几乎无感，又不会鼠标划过就乱闪。 */
export function showTip(el: HTMLElement, content: TipContent, delay = 150) {
  window.clearTimeout(hideTimer)
  window.clearTimeout(showTimer)
  if (!content.title && !content.rows?.length && !content.note) return

  showTimer = window.setTimeout(() => {
    const r = el.getBoundingClientRect()
    tipState.anchor = { top: r.top, bottom: r.bottom, left: r.left }
    tipState.x = r.left
    tipState.y = r.bottom + 6
    tipState.content = content
    tipState.visible = true
  }, delay)
}

/** 隐藏提示。留一点延迟，指针在相邻元素之间移动时不会闪烁。 */
export function hideTip(delay = 60) {
  window.clearTimeout(showTimer)
  window.clearTimeout(hideTimer)
  hideTimer = window.setTimeout(() => {
    tipState.visible = false
  }, delay)
}

/** 立即隐藏，用于滚动、点击这类必须马上收起的场景。 */
export function hideTipNow() {
  window.clearTimeout(showTimer)
  window.clearTimeout(hideTimer)
  tipState.visible = false
}
