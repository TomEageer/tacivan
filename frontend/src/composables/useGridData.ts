import { ref, shallowRef, computed } from 'vue'
import * as api from '../api'
import type { ColumnMeta, Page, Row } from '../types'

/** 前端一次向后端请求的行数。 */
const CHUNK = 200
/** 前端最多缓存的块数；超出后按最久未访问丢弃。 */
const MAX_CHUNKS = 14

/**
 * 网格数据源。
 *
 * 后端保证内存有界，前端这一侧同样要有界——否则用户滚过一百万行之后，
 * 数据全堆在 JS 堆里，一样会把内存吃穿。这里用与后端同构的策略：
 * 按块取、按块缓存、超出预算就丢最久未用的块，需要时重新取。
 */
export function useGridData() {
  const columns = shallowRef<ColumnMeta[]>([])
  const chunks = new Map<number, Row[]>()
  /** 块的最近访问序号，用于 LRU。 */
  const chunkClock = new Map<number, number>()
  let clock = 0

  const resultId = ref('')
  /** 已从服务端拉取的行数。 */
  const loaded = ref(0)
  /** 精确总行数；-1 表示尚未确定。 */
  const total = ref(-1)
  const complete = ref(false)
  const limitReached = ref(false)
  /** 用户主动中断了取数：行是真的，只是没读完。 */
  const stopped = ref(false)
  const loading = ref(false)
  const error = ref('')
  /** 触发重渲染的版本号：行数据存在非响应式的 Map 里，靠它通知视图。 */
  const version = ref(0)

  const inflight = new Map<number, Promise<void>>()

  /** 可滚动的行数：未读完时在已加载行后留出一段，让用户能继续往下滚触发加载。 */
  const scrollRows = computed(() => {
    if (total.value >= 0) return total.value
    if (complete.value) return loaded.value
    // 一行都还没取到时不预留滚动空间。否则结果为空或尚在加载时，
    // 网格会凭空画出一屏带行号的空行，看起来像「查出来是空的」。
    if (loaded.value === 0) return 0
    return loaded.value + CHUNK
  })

  function reset(id: string, firstPage: Page | null) {
    resultId.value = id
    chunks.clear()
    chunkClock.clear()
    inflight.clear()
    loaded.value = 0
    total.value = -1
    complete.value = false
    limitReached.value = false
    stopped.value = false
    error.value = ''
    if (firstPage) applyPage(firstPage)
    version.value++
  }

  function applyPage(page: Page) {
    if (page.columns?.length) columns.value = page.columns
    loaded.value = page.loaded
    total.value = page.total
    complete.value = page.complete
    limitReached.value = page.limitReached
    if (page.stopped) stopped.value = true
    error.value = page.error ?? ''

    // 按块切分写入缓存。首屏的 offset 通常是 0，但分页请求可能落在块中间。
    const rows = page.rows ?? []
    for (let i = 0; i < rows.length; i++) {
      const absolute = page.offset + i
      const ci = Math.floor(absolute / CHUNK)
      let chunk = chunks.get(ci)
      if (!chunk) {
        chunk = new Array(CHUNK)
        chunks.set(ci, chunk)
      }
      chunk[absolute - ci * CHUNK] = rows[i]
      chunkClock.set(ci, ++clock)
    }
    evict()
    version.value++
  }

  function evict() {
    if (chunks.size <= MAX_CHUNKS) return
    const entries = [...chunkClock.entries()].sort((a, b) => a[1] - b[1])
    for (const [ci] of entries) {
      if (chunks.size <= MAX_CHUNKS) break
      chunks.delete(ci)
      chunkClock.delete(ci)
    }
  }

  /** 取一行；未加载时返回 undefined 并在后台发起请求。 */
  function rowAt(index: number): Row | undefined {
    if (index < 0) return undefined
    const ci = Math.floor(index / CHUNK)
    const chunk = chunks.get(ci)
    if (chunk) {
      chunkClock.set(ci, ++clock)
      const r = chunk[index - ci * CHUNK]
      if (r) return r
    }
    void ensureChunk(ci)
    return undefined
  }

  function ensureChunk(ci: number): Promise<void> {
    if (!resultId.value) return Promise.resolve()
    if (chunks.has(ci) && chunks.get(ci)![0]) return Promise.resolve()
    const pending = inflight.get(ci)
    if (pending) return pending

    const p = (async () => {
      loading.value = true
      try {
        const page = await api.FetchRows(resultId.value, ci * CHUNK, CHUNK)
        applyPage(page)
      } catch (e) {
        error.value = e instanceof Error ? e.message : String(e)
      } finally {
        loading.value = false
        inflight.delete(ci)
      }
    })()
    inflight.set(ci, p)
    return p
  }

  /** 预取一段范围，滚动时提前把即将进入视口的块拉回来。 */
  async function ensureRange(start: number, end: number) {
    const from = Math.floor(Math.max(0, start) / CHUNK)
    const to = Math.floor(Math.max(0, end) / CHUNK)
    const tasks: Promise<void>[] = []
    for (let ci = from; ci <= to; ci++) {
      if (!chunks.has(ci) || !chunks.get(ci)![0]) tasks.push(ensureChunk(ci))
    }
    if (tasks.length) await Promise.all(tasks)
  }

  /** 让指定行重新从服务端取（保存之后刷新当前行）。 */
  function invalidateRow(index: number) {
    const ci = Math.floor(index / CHUNK)
    chunks.delete(ci)
    chunkClock.delete(ci)
    version.value++
  }

  function invalidateAll() {
    chunks.clear()
    chunkClock.clear()
    version.value++
  }

  async function countExact() {
    if (!resultId.value) return
    loading.value = true
    try {
      const n = await api.CountResultRows(resultId.value)
      total.value = n
      loaded.value = n
      complete.value = true
      version.value++
    } finally {
      loading.value = false
    }
  }

  /** 当前缓存占用的块数，显示在状态栏用于印证内存策略。 */
  const cachedChunks = computed(() => {
    void version.value
    return chunks.size
  })

  return {
    columns,
    resultId,
    loaded,
    total,
    complete,
    limitReached,
    stopped,
    loading,
    error,
    version,
    scrollRows,
    cachedChunks,
    CHUNK,
    reset,
    applyPage,
    rowAt,
    ensureRange,
    invalidateRow,
    invalidateAll,
    countExact,
  }
}

export type GridData = ReturnType<typeof useGridData>
