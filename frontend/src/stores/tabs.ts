import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import * as api from '../api'
import type { Engine, FilterGroup, ObjectRef, OrderTerm } from '../types'

export type TabType = 'table' | 'query' | 'designer' | 'redis' | 'ddl' | 'monitor'

export interface Tab {
  id: string
  type: TabType
  title: string
  connId: string
  connName: string
  engine: Engine
  database: string
  /** 有未保存内容时标题上显示圆点。 */
  dirty: boolean

  // 表数据 / 设计器
  ref?: ObjectRef
  /** 对象树上已知的估算行数，打开表数据时直接复用，省一次统计信息查询。 */
  estimatedRows?: number
  where?: string
  filter?: FilterGroup
  orderBy?: OrderTerm[]
  /** 表数据的取数上限；0 用设置默认值，-1 不限。 */
  rowLimit?: number

  // SQL 编辑器
  sql?: string
  /** 关联的磁盘文件路径，用于「保存」。 */
  filePath?: string

  // Redis
  pattern?: string
  selectedKey?: string

  /** 该标签页持有的结果集，关闭时要一并释放。 */
  resultIds: string[]
}

let tabSeq = 0

export const useTabsStore = defineStore('tabs', () => {
  const tabs = ref<Tab[]>([])
  const activeId = ref('')

  const active = computed(() => tabs.value.find((t) => t.id === activeId.value))

  function nextId(prefix: string) {
    return `${prefix}-${++tabSeq}`
  }

  function activate(id: string) {
    if (tabs.value.some((t) => t.id === id)) activeId.value = id
  }

  /** 已存在同一对象的标签页时直接激活，避免重复打开。 */
  function findExisting(type: TabType, connId: string, ref?: ObjectRef) {
    if (!ref) return undefined
    return tabs.value.find(
      (t) =>
        t.type === type &&
        t.connId === connId &&
        t.ref?.name === ref.name &&
        (t.ref?.database ?? '') === (ref.database ?? '') &&
        (t.ref?.schema ?? '') === (ref.schema ?? ''),
    )
  }

  function open(
    tab: Omit<Tab, 'id' | 'resultIds' | 'dirty'> & Partial<Pick<Tab, 'id' | 'dirty'>>,
  ): Tab {
    if (tab.id && tabs.value.some((t) => t.id === tab.id)) {
      activate(tab.id)
      return tabs.value.find((t) => t.id === tab.id)!
    }
    const existing = findExisting(tab.type, tab.connId, tab.ref)
    if (existing) {
      activate(existing.id)
      return existing
    }
    // 恢复草稿时要沿用原来的 ID，草稿文件是按它命名的；
    // 同时把序号推过去，免得之后新开的标签页撞上恢复出来的。
    const id = tab.id ?? nextId(tab.type)
    if (tab.id) {
      const n = Number(tab.id.split('-').pop())
      if (Number.isFinite(n) && n > tabSeq) tabSeq = n
    }
    const t: Tab = {
      ...tab,
      id,
      dirty: tab.dirty ?? false,
      resultIds: [],
    }
    tabs.value.push(t)
    activeId.value = t.id
    return t
  }

  /** 记录标签页持有的结果集；替换时释放旧的。 */
  function setResult(tabId: string, resultId: string, keepPrevious = false) {
    const t = tabs.value.find((x) => x.id === tabId)
    if (!t) return
    if (!keepPrevious) releaseResults(t.resultIds)
    t.resultIds = keepPrevious ? [...t.resultIds, resultId] : [resultId]
  }

  function addResult(tabId: string, resultId: string) {
    const t = tabs.value.find((x) => x.id === tabId)
    if (t) t.resultIds.push(resultId)
  }

  function releaseResults(ids: string[]) {
    for (const id of ids) {
      // 释放失败不影响界面，后端最终也会在连接关闭时回收。
      api.CloseResult(id).catch(() => {})
    }
  }

  /**
   * 关闭标签页。
   *
   * discardDraft 区分「用户关掉的」和「被动没了的」：前者说明内容不要了，
   * 草稿该删；后者（比如连接断开导致标签页一起消失）内容还是有价值的，
   * 草稿必须留着，否则断个连接就把没存的 SQL 弄丢了——
   * 那正是自动恢复要防的事。
   */
  async function close(id: string, discardDraft = true) {
    const i = tabs.value.findIndex((t) => t.id === id)
    if (i < 0) return
    const t = tabs.value[i]
    releaseResults(t.resultIds)
    if (t.type === 'query' && discardDraft) void api.DeleteDraft(t.id).catch(() => {})
    tabs.value.splice(i, 1)
    if (activeId.value === id) {
      // 关掉当前页后激活右边的，没有则激活左边的，与浏览器行为一致。
      const next = tabs.value[i] ?? tabs.value[i - 1]
      activeId.value = next ? next.id : ''
    }
  }

  function closeOthers(id: string) {
    for (const t of [...tabs.value]) if (t.id !== id) close(t.id)
  }

  function closeAll() {
    for (const t of [...tabs.value]) close(t.id)
  }

  /** 连接断开时关掉它名下的所有标签页；草稿留着。 */
  function closeByConnection(connId: string) {
    for (const t of [...tabs.value]) if (t.connId === connId) close(t.id, false)
  }

  function setTitle(id: string, title: string) {
    const t = tabs.value.find((x) => x.id === id)
    if (t) t.title = title
  }

  function setDirty(id: string, dirty: boolean) {
    const t = tabs.value.find((x) => x.id === id)
    if (t) t.dirty = dirty
  }

  function activateNext(delta: number) {
    if (tabs.value.length === 0) return
    const i = tabs.value.findIndex((t) => t.id === activeId.value)
    const next = (i + delta + tabs.value.length) % tabs.value.length
    activeId.value = tabs.value[next].id
  }

  return {
    tabs,
    activeId,
    active,
    open,
    activate,
    activateNext,
    close,
    closeOthers,
    closeAll,
    closeByConnection,
    setResult,
    addResult,
    setTitle,
    setDirty,
  }
})
