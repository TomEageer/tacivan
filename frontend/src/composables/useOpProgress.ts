import { onBeforeUnmount, ref } from 'vue'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import { tr } from '../i18n'

/**
 * 长操作的分步进度。
 *
 * 只有一个转圈图标等于什么都没说：用户看不出是在等服务端出数、
 * 在等表结构，还是已经卡死了。这里做两件事——
 * 一是显示后端播报的步骤，二是本地自己走秒。
 *
 * 走秒必须在本地做。后端卡住时事件是不会来的，而那恰恰是最需要看到
 * 时间还在涨的时刻；靠事件里的 elapsedMs 驱动，界面会停在最后一次
 * 播报的数字上，看着反倒像死了。
 */
export interface OpProgressEvent {
  token: string
  stage: string
  detail?: string
  elapsedMs: number
  done: boolean
}

let seq = 0

export function useOpProgress() {
  const token = `op${Date.now().toString(36)}${++seq}`
  const stage = ref('')
  const detail = ref('')
  const elapsedMs = ref(0)
  const running = ref(false)

  let startAt = 0
  let timer = 0

  const off = EventsOn(`op:progress`, (e: OpProgressEvent) => {
    if (!e || e.token !== token) return
    if (e.done) {
      stop()
      return
    }
    stage.value = e.stage
    detail.value = e.detail ?? ''
  })

  function start() {
    stage.value = ''
    detail.value = ''
    startAt = performance.now()
    elapsedMs.value = 0
    running.value = true
    if (timer) window.clearInterval(timer)
    // 100ms 一跳：秒位下面再显示一位小数，看得出一直在走
    timer = window.setInterval(() => {
      elapsedMs.value = performance.now() - startAt
    }, 100)
  }

  function stop() {
    running.value = false
    if (timer) {
      window.clearInterval(timer)
      timer = 0
    }
  }

  onBeforeUnmount(() => {
    stop()
    off?.()
  })

  return { token, stage, detail, elapsedMs, running, start, stop }
}

/** 把毫秒显示成人看的形式。 */
export function formatElapsed(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)} ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)} s`
  const m = Math.floor(ms / 60_000)
  return tr('{m} 分 {s} 秒', { m, s: Math.floor((ms % 60_000) / 1000) })
}
