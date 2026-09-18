<script setup lang="ts">
/**
 * 全库搜索。
 *
 * 两种模式代价差着数量级：搜对象名一个库一次查询就够，搜数据则要
 * 一张表一次全表扫描。所以范围、上限、进度、中断这四样都必须显式给到用户，
 * 而不是让他点一下之后干等着。
 */
import { computed, onMounted, ref } from 'vue'
import * as api from '../../api'
import { useAppStore } from '../../stores/app'
import { useConnectionsStore } from '../../stores/connections'
import { formatElapsed, useOpProgress } from '../../composables/useOpProgress'
import type { SearchHit, SearchResult } from '../../types'
import Modal from '../common/Modal.vue'
import Icon from '../common/Icon.vue'
import { tr } from '../../i18n'

const props = defineProps<{ connId: string; database?: string }>()
const emit = defineEmits<{
  (e: 'open-hit', hit: SearchHit): void
  (e: 'close'): void
}>()

const app = useAppStore()
const conns = useConnectionsStore()
const progress = useOpProgress()

const keyword = ref('')
const mode = ref<'object' | 'data'>('object')
const searchColumns = ref(true)
const caseSensitive = ref(false)
const maxRowsPerTable = ref(5)
const maxTables = ref(200)

const allDbs = ref<string[]>([])
const selected = ref<Set<string>>(new Set(props.database ? [props.database] : []))
const dbFilter = ref('')

const running = ref(false)
const result = ref<SearchResult | null>(null)

const connName = computed(() => conns.stateOf(props.connId)?.config.name ?? '')

const visibleDbs = computed(() => {
  const kw = dbFilter.value.trim().toLowerCase()
  if (!kw) return allDbs.value
  return allDbs.value.filter((n) => n.toLowerCase().includes(kw))
})

onMounted(async () => {
  try {
    const list = await api.ListDatabases(props.connId)
    allDbs.value = list.map((d) => d.name)
    if (!selected.value.size && allDbs.value.length === 1) selected.value.add(allDbs.value[0])
  } catch (e) {
    app.reportError(e, tr('读取数据库列表失败'))
  }
})

function toggleDb(name: string) {
  const next = new Set(selected.value)
  if (next.has(name)) next.delete(name)
  else next.add(name)
  selected.value = next
}

function selectVisible() {
  const next = new Set(selected.value)
  for (const n of visibleDbs.value) next.add(n)
  selected.value = next
}

function clearSelection() {
  selected.value = new Set()
}

async function run() {
  if (!keyword.value.trim()) {
    app.toast('warning', tr('请输入要搜索的内容'))
    return
  }
  if (!selected.value.size) {
    app.toast('warning', tr('请至少选择一个数据库'))
    return
  }
  running.value = true
  result.value = null
  progress.start()
  try {
    result.value = await api.Search({
      connId: props.connId,
      databases: [...selected.value],
      keyword: keyword.value,
      mode: mode.value,
      searchColumns: mode.value === 'object' && searchColumns.value,
      caseSensitive: caseSensitive.value,
      maxRowsPerTable: maxRowsPerTable.value,
      maxTables: maxTables.value,
      progressToken: progress.token,
    })
  } catch (e) {
    app.reportError(e, tr('搜索失败'))
  } finally {
    progress.stop()
    running.value = false
  }
}

async function stop() {
  try {
    await api.CancelOperation(progress.token)
  } catch {
    // 可能刚好自己结束了。
  }
}

const KIND_LABEL = computed<Record<string, string>>(() => ({
  table: tr('表'),
  view: tr('视图'),
  column: tr('列'),
  row: tr('数据'),
}))

const KIND_ICON: Record<string, string> = {
  table: 'grid',
  view: 'layers',
  column: 'table',
  row: 'search',
}

/** 数据模式下一张表可能命中多行，按表折叠成一条摘要更好读。 */
const grouped = computed(() => {
  const hits = result.value?.hits ?? []
  if (mode.value !== 'data') return hits.map((h) => ({ hit: h, extra: 0 }))
  const byTable = new Map<string, { hit: SearchHit; extra: number }>()
  for (const h of hits) {
    const key = `${h.database}.${h.table}`
    const cur = byTable.get(key)
    if (cur) cur.extra++
    else byTable.set(key, { hit: h, extra: 0 })
  }
  return [...byTable.values()]
})

const summary = computed(() => {
  const r = result.value
  if (!r) return ''
  const parts = [tr('{n} 条命中', { n: r.hits.length })]
  if (r.scannedTables) parts.push(tr('扫了 {n} 张表', { n: r.scannedTables }))
  parts.push(`${r.durationMs} ms`)
  if (r.stopped) parts.push(tr('已中断'))
  if (r.truncated) parts.push(tr('触达上限（最多 {n} 张表）', { n: maxTables.value }))
  return parts.join(' · ')
})
</script>

<template>
  <Modal :title="tr('在 {conn} 中搜索', { conn: connName })" :width="700" @close="emit('close')">
    <div class="row">
      <input
        v-model="keyword"
        class="input"
        :placeholder="tr('要找的内容')"
        autofocus
        @keydown.enter.prevent="run"
      />
      <button class="btn btn-primary" :disabled="running" @click="run">{{ tr('搜索') }}</button>
    </div>

    <div class="row modes">
      <label class="radio">
        <input v-model="mode" type="radio" value="object" />{{ tr('对象名') }}</label>
      <label class="radio">
        <input v-model="mode" type="radio" value="data" />{{ tr('表里的数据') }}</label>
      <span class="sep"></span>
      <label v-if="mode === 'object'" class="checkbox">
        <input v-model="searchColumns" type="checkbox" />{{ tr('连列名一起搜') }}</label>
      <label class="checkbox">
        <input v-model="caseSensitive" type="checkbox" />{{ tr('区分大小写') }}</label>
    </div>
    <div class="field-hint">
      <template v-if="mode === 'object'">{{ tr('搜表名与视图名，一个库一次查询。勾上「连列名一起搜」要逐表读结构，慢得多。') }}</template>
      <template v-else>{{ tr('每张表做一次全表扫描，只搜文本类列。范围选大了会很慢，随时可以停。') }}</template>
    </div>

    <div class="divider"></div>

    <div class="row scope-head">
      <span class="scope-title">
        {{ tr('范围：已选 {a} / {b} 个库', { a: selected.size, b: allDbs.length }) }}
      </span>
      <input v-model="dbFilter" class="input input-sm" :placeholder="tr('筛选库…')" />
      <button class="btn btn-ghost" @click="selectVisible">{{ tr('全选当前') }}</button>
      <button class="btn btn-ghost" @click="clearSelection">{{ tr('清空') }}</button>
    </div>
    <ul class="db-grid">
      <li v-for="n in visibleDbs.slice(0, 400)" :key="n">
        <button
          class="db-chip mono"
          :class="{ 'is-on': selected.has(n) }"
          @click="toggleDb(n)"
        >
          {{ n }}
        </button>
      </li>
    </ul>
    <div v-if="visibleDbs.length > 400" class="field-hint">{{ tr('只显示前 400 个，用筛选框缩小范围') }}</div>

    <template v-if="mode === 'data'">
      <div class="row limits">
        <label class="field-inline">{{ tr('每表最多') }}<input v-model.number="maxRowsPerTable" class="input input-sm" type="number" min="1" max="100" />{{ tr('行') }}</label>
        <label class="field-inline">{{ tr('最多扫') }}<input v-model.number="maxTables" class="input input-sm" type="number" min="1" max="5000" />{{ tr('张表') }}</label>
      </div>
    </template>

    <div class="divider"></div>

    <div v-if="running" class="searching">
      <div class="spinner"></div>
      <div class="progress-line">
        <span>{{ progress.stage.value || tr('准备中') }}</span>
        <span class="elapsed">{{ formatElapsed(progress.elapsedMs.value) }}</span>
      </div>
      <div class="progress-detail mono">{{ progress.detail.value }}</div>
      <button class="btn" @click="stop">{{ tr('停止') }}</button>
    </div>

    <template v-else-if="result">
      <div class="summary">{{ summary }}</div>
      <div v-if="result.note" class="field-hint">{{ result.note }}</div>
      <div v-if="!result.hits.length" class="muted">{{ tr('没有找到匹配的内容') }}</div>
      <ul v-else class="hits">
        <li v-for="(g, i) in grouped" :key="i">
          <button class="hit" @dblclick="emit('open-hit', g.hit)" @click="emit('open-hit', g.hit)">
            <Icon :name="KIND_ICON[g.hit.kind] ?? 'file'" :size="12" class="hit-icon" />
            <span class="hit-kind">{{ KIND_LABEL[g.hit.kind] ?? g.hit.kind }}</span>
            <span class="hit-path mono">
              {{ g.hit.database }}<span class="dim">.</span>{{ g.hit.table
              }}<template v-if="g.hit.column"><span class="dim">.</span>{{ g.hit.column }}</template>
            </span>
            <span v-if="g.hit.value" class="hit-value mono">{{ g.hit.value }}</span>
            <span v-else-if="g.hit.comment" class="hit-value">{{ g.hit.comment }}</span>
            <span v-if="g.extra" class="hit-extra">{{ tr('+{n} 行', { n: g.extra }) }}</span>
          </button>
        </li>
      </ul>
    </template>

    <template #footer>
      <span class="spacer"></span>
      <button class="btn" @click="emit('close')">{{ tr('关闭') }}</button>
    </template>
  </Modal>
</template>

<style scoped>
.row {
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  margin-bottom: var(--sp-2);
}
.row .input {
  flex: 1;
}
.input-sm {
  flex: none;
  width: 130px;
}

.modes {
  flex-wrap: wrap;
  gap: var(--sp-4);
}
.radio,
.checkbox {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  white-space: nowrap;
}
.sep {
  flex: 1;
}

.scope-head {
  gap: var(--sp-2);
}
.scope-title {
  flex: 1;
  color: var(--c-text-secondary);
  white-space: nowrap;
}

.db-grid {
  list-style: none;
  margin: 0 0 var(--sp-2);
  padding: 0;
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  max-height: 140px;
  overflow-y: auto;
}
.db-chip {
  padding: 2px var(--sp-2);
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--c-text-tertiary);
  font-size: var(--font-size-sm);
}
.db-chip.is-on {
  border-color: var(--c-accent);
  background: var(--c-accent-soft);
  color: var(--c-text);
}

.limits {
  gap: var(--sp-4);
}
.field-inline {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  color: var(--c-text-secondary);
  white-space: nowrap;
}
.field-inline .input-sm {
  width: 72px;
}

.searching {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--sp-2);
  padding: var(--sp-4) 0;
  color: var(--c-text-tertiary);
}
.progress-line {
  display: flex;
  gap: var(--sp-3);
  align-items: baseline;
}
.elapsed {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  color: var(--c-meta);
}
.progress-detail {
  font-size: var(--font-size-xs);
  max-width: 90%;
  text-align: center;
  word-break: break-all;
}

.summary {
  margin-bottom: var(--sp-2);
  color: var(--c-text-secondary);
}

.hits {
  list-style: none;
  margin: 0;
  padding: 0;
  max-height: 300px;
  overflow-y: auto;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
}
.hit {
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  width: 100%;
  padding: 4px var(--sp-2);
  border: none;
  border-bottom: 1px solid var(--c-border-soft);
  background: transparent;
  color: var(--c-text-secondary);
  text-align: left;
}
.hit:hover {
  background: var(--c-chrome-active);
  color: var(--c-text);
}
.hit-icon {
  flex: none;
  opacity: 0.75;
}
.hit-kind {
  flex: none;
  min-width: 28px;
  font-size: var(--font-size-sm);
  color: var(--c-text-tertiary);
}
.hit-path {
  flex: none;
  max-width: 45%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.dim {
  color: var(--c-text-tertiary);
}
.hit-value {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--c-text-tertiary);
  font-size: var(--font-size-sm);
}
.hit-extra {
  flex: none;
  color: var(--c-meta);
  font-size: var(--font-size-sm);
}
</style>
