<script setup lang="ts">
/** 表数据标签页：筛选 / 排序 / 就地编辑 / 导出。 */
import { computed, onMounted, ref, watch } from 'vue'
import * as api from '../../api'
import { useAppStore } from '../../stores/app'
import { useTabsStore, type Tab } from '../../stores/tabs'
import { useGridData } from '../../composables/useGridData'
import { formatElapsed, useOpProgress } from '../../composables/useOpProgress'
import type { Column, FilterGroup, OrderTerm, TableDataResult } from '../../types'
import DataGrid from '../grid/DataGrid.vue'
import FilterBar from '../grid/FilterBar.vue'
import RowForm from '../grid/RowForm.vue'
import Icon from '../common/Icon.vue'
import CellViewer from '../dialogs/CellViewer.vue'
import ExportDialog from '../dialogs/ExportDialog.vue'
import SqlPreviewDialog from '../dialogs/SqlPreviewDialog.vue'
import { tr } from '../../i18n'

const props = defineProps<{ tab: Tab }>()

const app = useAppStore()
const tabs = useTabsStore()
const data = useGridData()
const grid = ref<InstanceType<typeof DataGrid> | null>(null)

const loading = ref(false)
const progress = useOpProgress()
const stopping = ref(false)

/**
 * 中断打开过程。已经读到的行会保留下来，不是整个作废——
 * 慢链路上卡住时用户要的是「停在这儿看看」，不是从头再来。
 */
async function stop() {
  stopping.value = true
  try {
    await api.CancelOperation(progress.token)
  } catch {
    // 操作可能刚好自己结束了，没什么可做的。
  }
}
const info = ref<TableDataResult | null>(null)
const tableColumns = ref<Column[]>([])
const where = ref(props.tab.where ?? '')
const filter = ref<FilterGroup>(props.tab.filter ?? { conditions: [], conjunction: 'and' })
const rowLimit = ref<number>(props.tab.rowLimit ?? 0)
/** 插件连接：一次一页、行数由插件按库/表限定，界面上不提供取数上限和总行数统计。 */
const isPlugin = computed(() => String(props.tab.engine).startsWith('plugin:'))

/** 取数上限的可选档位，最后一档是「不限」。 */
const LIMIT_OPTIONS = computed(() => [
  { value: 200, label: tr('200 行') },
  { value: 1000, label: tr('1000 行') },
  { value: 5000, label: tr('5000 行') },
  { value: 20000, label: tr('2 万行') },
  { value: -1, label: tr('不限') },
])
const orderBy = ref<OrderTerm[]>(props.tab.orderBy ?? [])
const changeCount = ref(0)
const durationMs = ref(0)
const failed = ref('')
const estimatedRows = ref(0)

const viewer = ref<{ row: number; column: string } | null>(null)

/** 表单视图：把当前行竖过来看，宽表时比横向滚动实用得多。 */
const showForm = ref(localStorage.getItem('tacivan.rowForm') === '1')
const formWidth = ref(Number(localStorage.getItem('tacivan.rowFormWidth')) || 320)
const currentRow = ref(0)
const formEdits = ref<Record<string, string | null>>({})

watch(showForm, (v) => localStorage.setItem('tacivan.rowForm', v ? '1' : '0'))
watch(formWidth, (v) => localStorage.setItem('tacivan.rowFormWidth', String(Math.round(v))))

function onGridSelect(rowIndex: number) {
  currentRow.value = rowIndex
  formEdits.value = grid.value?.pendingEditsFor(rowIndex) ?? {}
}

function onFormEdit(column: string, value: string | null) {
  grid.value?.applyEditByName(currentRow.value, column, value)
  formEdits.value = { ...formEdits.value, [column]: value }
}

function onFormGoto(rowIndex: number) {
  grid.value?.gotoRow(rowIndex)
}

// --- 表单视图宽度拖拽 ---
let formDragX = 0
let formDragW = 0
function onFormSplitDown(e: MouseEvent) {
  formDragX = e.clientX
  formDragW = formWidth.value
  window.addEventListener('mousemove', onFormSplitMove)
  window.addEventListener('mouseup', onFormSplitUp)
}
function onFormSplitMove(e: MouseEvent) {
  formWidth.value = Math.max(220, Math.min(620, formDragW - (e.clientX - formDragX)))
}
function onFormSplitUp() {
  window.removeEventListener('mousemove', onFormSplitMove)
  window.removeEventListener('mouseup', onFormSplitUp)
}
const showExport = ref(false)
const preview = ref<{ title: string; sql: string; warnings?: string[] } | null>(null)

const editable = computed(() => info.value?.editable === true)
const keyColumns = computed(() => info.value?.keyColumns ?? [])

async function load(notify = false) {
  loading.value = true
  failed.value = ''
  progress.start()
  try {
    const res = await api.OpenTableData(props.tab.connId, props.tab.ref!, {
      where: where.value,
      filter: filter.value,
      orderBy: orderBy.value,
      columns: [],
      firstPageRows: app.settings.gridPageSize,
      estimatedRows: props.tab.estimatedRows ?? 0,
      limit: rowLimit.value,
      progressToken: progress.token,
    })
    info.value = res
    tableColumns.value = res.columns
    durationMs.value = res.durationMs
    estimatedRows.value = res.estimatedRows
    tabs.setResult(props.tab.id, res.resultId)
    data.reset(res.resultId, res.page)
    changeCount.value = 0
    grid.value?.revertChanges()
    formEdits.value = {}
    if (notify) {
      app.toast(
        'success',
        tr('已刷新'),
        tr('{n} 行 · {ms} ms', { n: res.page.rows.length, ms: res.durationMs }),
      )
    }
  } catch (e) {
    failed.value = e instanceof Error ? e.message : String(e)
    app.reportError(e, tr('打开表数据失败'))
  } finally {
    progress.stop()
    stopping.value = false
    loading.value = false
  }
}

onMounted(load)

// 筛选与排序变化时保存回标签页状态，切走再切回来条件还在。
watch([where, orderBy, filter, rowLimit], () => {
  props.tab.where = where.value
  props.tab.filter = filter.value
  props.tab.orderBy = orderBy.value
  props.tab.rowLimit = rowLimit.value
})

function onLimitChange(v: number) {
  rowLimit.value = v
  load()
}

function onSort(column: string, desc: boolean) {
  const cur = orderBy.value.find((o) => o.column === column)
  if (cur && cur.desc === desc) {
    // 第三次点击取消该列排序。
    orderBy.value = orderBy.value.filter((o) => o.column !== column)
  } else {
    orderBy.value = [{ column, desc }]
  }
  load()
}

function applyFilter() {
  load()
}

function clearFilter() {
  where.value = ''
  filter.value = { conditions: [], conjunction: 'and' }
  orderBy.value = []
  load()
}

async function countExact() {
  loading.value = true
  try {
    await data.countExact()
  } catch (e) {
    app.reportError(e, tr('统计行数失败'))
  } finally {
    loading.value = false
  }
}

async function save() {
  const changes = grid.value?.collectChanges() ?? []
  if (changes.length === 0) return
  loading.value = true
  try {
    const res = await api.ApplyChanges({ resultId: data.resultId.value, changes })
    app.toast(
      'success',
      tr('已保存 {n} 处变更', { n: res.applied }),
      tr('影响 {rows} 行，耗时 {ms} ms', { rows: res.rowsAffected, ms: res.durationMs }),
    )
    await load()
  } catch (e) {
    app.reportError(e, tr('保存失败'))
  } finally {
    loading.value = false
  }
}

async function previewChanges() {
  const changes = grid.value?.collectChanges() ?? []
  if (changes.length === 0) {
    app.toast('info', tr('没有待保存的更改'))
    return
  }
  try {
    const p = await api.PreviewChanges({ resultId: data.resultId.value, changes })
    preview.value = { title: tr('待执行的 SQL'), sql: p.sql, warnings: p.warnings }
  } catch (e) {
    app.reportError(e, tr('生成 SQL 预览失败'))
  }
}

function revert() {
  grid.value?.revertChanges()
  changeCount.value = 0
}

function onChanges(n: number) {
  changeCount.value = n
  tabs.setDirty(props.tab.id, n > 0)
}

function onViewCell(row: number, column: string) {
  viewer.value = { row, column }
}

/** 把选中的行复制成可执行语句。 */
async function onCopySql(kind: string, rowIndexes: number[]) {
  try {
    const sql = await api.GenerateRowSQL(data.resultId.value, {
      kind,
      rowIndexes,
      columns: [],
    })
    await navigator.clipboard?.writeText(sql)
    app.toast(
      'success',
      tr('已复制 {n} 条 {kind} 语句', { n: rowIndexes.length, kind: kind.toUpperCase() }),
      sql.length > 160 ? sql.slice(0, 160) + '…' : sql,
    )
  } catch (e) {
    app.reportError(e, tr('生成语句失败'))
  }
}

/** 右键「按此值筛选」：往筛选器里加一条等值条件并立即应用。 */
function onFilterByCell(column: string, value: string | null) {
  if (!column) return
  const cond = {
    column,
    op: (value === null ? 'isNull' : 'eq') as 'isNull' | 'eq',
    values: value === null ? [] : [value],
    enabled: true,
  }
  const rest = filter.value.conditions.filter((c) => c.column !== column)
  filter.value = { ...filter.value, conditions: [...rest, cond] }
  load()
}

/** 结果正好停在上限处，说明后面还有数据没取。 */
const atLimit = computed(() => {
  const lim = info.value?.limit ?? 0
  return lim > 0 && data.complete.value && data.loaded.value >= lim
})

const oneLineSql = computed(() => (info.value?.sql ?? '').replace(/\s+/g, ' ').trim())

function copySql() {
  if (!oneLineSql.value) return
  navigator.clipboard?.writeText(info.value?.sql ?? '')
  app.toast('success', tr('已复制 SQL'))
}

const rowsText = computed(() => {
  const loaded = data.loaded.value
  const total = data.total.value
  // 被中断过就必须说出来，否则「已加载 112 行」看起来像是表里只有这么多。
  if (data.stopped.value) return tr('已停止，已加载 {n} 行', { n: loaded.toLocaleString() })
  if (total >= 0) return tr('共 {n} 行', { n: total.toLocaleString() })
  if (data.complete.value) return tr('共 {n} 行', { n: loaded.toLocaleString() })
  const est = estimatedRows.value
  const estText = est > 0 ? tr('，估算约 {n} 行', { n: est.toLocaleString() }) : ''
  return tr('已加载 {n} 行', { n: loaded.toLocaleString() }) + estText
})

defineExpose({ refresh: () => load(true), save, exportData: () => (showExport.value = true) })
</script>

<template>
  <div class="tabpane">
    <FilterBar
      v-model="filter"
      :where="where"
      :columns="tableColumns"
      :disabled="loading"
      @update:where="(v) => (where = v)"
      @apply="load()"
    />

    <div class="tablebar">
      <template v-if="editable">
        <button class="btn btn-ghost" :title="tr('添加一行')" @click="grid?.addRow()">
          <Icon name="plus" :size="13" />{{ tr('添加') }}</button>
        <button class="btn btn-ghost" :title="tr('标记删除所选行')" @click="grid?.deleteSelectedRows()">
          <Icon name="minus" :size="13" />{{ tr('删除') }}</button>
        <button class="btn btn-ghost" :disabled="!changeCount" @click="previewChanges">
          <Icon name="code" :size="13" />{{ tr('预览 SQL') }}</button>
        <button class="btn btn-ghost" :disabled="!changeCount" @click="revert">{{ tr('撤销更改') }}</button>
        <button class="btn btn-primary" :disabled="!changeCount || loading" @click="save">
          <Icon name="save" :size="13" /> {{ tr('保存') }}{{ changeCount ? ` (${changeCount})` : '' }}
        </button>
      </template>
      <span v-else-if="info?.editableReason" class="badge" :title="info.editableReason">
        {{ tr('只读') }} · {{ info.editableReason }}
      </span>

      <div class="spacer"></div>

      <button
        class="btn btn-ghost"
        :class="{ 'is-on': showForm }"
        :title="tr('表单视图：把当前行竖过来看（宽表推荐）')"
        @click="showForm = !showForm"
      >
        <Icon name="column" :size="13" />{{ tr('单行') }}</button>

      <span v-if="isPlugin" class="muted limit-note" :title="tr('行数由插件按库/表限定，不分页')">{{ tr('行数由插件限定') }}</span>
      <label v-else class="limit-label" :title="tr('给查询加的 LIMIT，避免在大表上拉取过多数据')">{{ tr('限制') }}<select
          class="select limit-select"
          :value="rowLimit"
          @change="onLimitChange(Number(($event.target as HTMLSelectElement).value))"
        >
          <option :value="0">
            {{ tr('默认 ({n})', { n: app.settings.rowLimit || tr('不限') }) }}
          </option>
          <option v-for="o in LIMIT_OPTIONS" :key="o.value" :value="o.value">{{ o.label }}</option>
        </select>
      </label>

      <button class="btn btn-ghost" :title="tr('刷新（⌘R）')" :disabled="loading" @click="load(true)">
        <Icon name="refresh" :size="13" :class="{ spin: loading }" />
      </button>
      <button class="btn btn-ghost" :title="tr('导出数据')" @click="showExport = true">
        <Icon name="download" :size="13" />
      </button>
    </div>

    <!--
      首次打开要读表结构，慢实例上能等几十秒。
      光转圈说明不了任何问题，这里同时给出当前步骤、该步骤的细节和已用时间。
    -->
    <div v-if="loading && !info" class="loading-state">
      <div class="spinner"></div>
      <div class="empty-state-title">{{ tr('正在打开 {name}', { name: tab.ref?.name ?? '' }) }}</div>
      <div class="progress-line">
        <span class="progress-stage">{{ progress.stage.value || tr('准备中') }}</span>
        <span class="progress-elapsed">{{ formatElapsed(progress.elapsedMs.value) }}</span>
      </div>
      <div v-if="progress.detail.value" class="progress-detail selectable">
        {{ progress.detail.value }}
      </div>
      <button class="btn" :disabled="stopping" @click="stop">
        {{ stopping ? tr('正在停止…') : tr('停止') }}
      </button>
    </div>
    <div v-else-if="failed" class="empty-state">
      <div class="empty-state-title">{{ tr('打开失败') }}</div>
      <div class="muted selectable">{{ failed }}</div>
      <button class="btn" @click="load()">{{ tr('重试') }}</button>
    </div>
    <div v-else class="grid-area">
      <DataGrid
        ref="grid"
        :data="data"
        :editable="editable"
        :key-columns="keyColumns"
        :table-columns="tableColumns"
        :order-by="orderBy"
        @sort="onSort"
        @changes="onChanges"
        @view-cell="onViewCell"
        @copy-sql="onCopySql"
        @filter-by="onFilterByCell"
        @select="onGridSelect"
      />

      <template v-if="showForm">
        <div class="form-split" :title="tr('拖动调整宽度')" @mousedown="onFormSplitDown"></div>
        <div class="form-pane" :style="{ width: formWidth + 'px' }">
          <RowForm
            :data="data"
            :row-index="currentRow"
            :table-columns="tableColumns"
            :key-columns="keyColumns"
            :editable="editable"
            :pending-edits="formEdits"
            @edit="onFormEdit"
            @goto="onFormGoto"
            @view-cell="onViewCell"
            @close="showForm = false"
          />
        </div>
      </template>
    </div>

    <!-- 底部状态条：直接显示本次真正执行的语句 -->
    <div class="gridfooter">
      <span class="foot-item">{{ rowsText }}</span>
      <button
        v-if="!isPlugin && data.total.value < 0 && !data.complete.value"
        class="btn btn-ghost btn-mini"
        :title="tr('把结果读到底以取得精确行数')"
        @click="countExact"
      >{{ tr('统计总行数') }}</button>
      <span v-if="atLimit" class="badge badge-readonly" :title="tr('当前上限 {n} 行', { n: info?.limit ?? 0 })">{{ tr('已达取数上限') }}</span>
      <span v-if="data.error.value" class="badge badge-error">{{ data.error.value }}</span>

      <div class="spacer"></div>
      <span v-if="durationMs" class="foot-item muted">{{ durationMs }} ms</span>
      <code
        class="foot-sql mono"
        :title="info?.sql"
        @click="copySql"
      >{{ oneLineSql }}</code>
    </div>

    <CellViewer
      v-if="viewer"
      :result-id="data.resultId.value"
      :row-index="viewer.row"
      :column="viewer.column"
      @close="viewer = null"
    />
    <ExportDialog
      v-if="showExport"
      :result-id="data.resultId.value"
      :default-name="props.tab.ref?.name ?? 'export'"
      @close="showExport = false"
    />
    <SqlPreviewDialog
      v-if="preview"
      :title="preview.title"
      :sql="preview.sql"
      :warnings="preview.warnings"
      confirm-:label="tr('执行并保存')"
      @confirm="
        () => {
          preview = null
          save()
        }
      "
      @close="preview = null"
    />
  </div>
</template>

<style scoped>
.limit-note { font-size: var(--font-size-sm); }

.tabpane {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}

.grid-area {
  flex: 1;
  display: flex;
  min-height: 0;
}

.form-split {
  flex: none;
  width: 4px;
  cursor: col-resize;
  background: var(--c-border-soft);
}
.form-split:hover {
  background: var(--c-accent);
}

.form-pane {
  flex: none;
  min-width: 220px;
  animation: form-in 190ms cubic-bezier(0.25, 0.8, 0.35, 1);
  overflow: hidden;
}

@keyframes form-in {
  from {
    opacity: 0;
    transform: translateX(16px);
  }
}

@media (prefers-reduced-motion: reduce) {
  .form-pane {
    animation: none;
  }
}

.btn.is-on {
  background: var(--c-accent-soft);
  color: var(--c-accent);
}

.tablebar {
  flex: none;
  display: flex;
  align-items: center;
  gap: var(--sp-1);
  height: var(--size-bar);
  padding: 0 var(--sp-3);
  background: var(--c-sunken);
  border-bottom: 1px solid var(--c-border-soft);
}

.limit-label {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: var(--font-size-sm);
  color: var(--c-text-secondary);
}

.limit-select {
  width: auto;
  height: 20px;
  font-size: var(--font-size-sm);
}

.foot-sql {
  flex: 1;
  min-width: 0;
  max-width: 55%;
  padding: 1px 6px;
  border-radius: 3px;
  background: var(--c-bg);
  color: var(--c-text-secondary);
  font-size: var(--font-size-sm);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  cursor: default;
}
.foot-sql:hover {
  background: var(--c-chrome-active);
  color: var(--c-text);
}

.spacer {
  flex: 1;
}

.gridfooter {
  flex: none;
  display: flex;
  align-items: center;
  gap: var(--sp-4);
  height: 26px;
  padding: 0 var(--sp-3);
  background: var(--c-sunken);
  border-top: 1px solid var(--c-border-soft);
  font-size: var(--font-size-sm);
  color: var(--c-text-secondary);
  overflow: hidden;
}

.foot-item {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.btn-mini {
  height: 18px;
  padding: 0 7px;
  font-size: var(--font-size-sm);
}

.loading-state {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: var(--c-text-tertiary);
}

.progress-line {
  display: flex;
  align-items: baseline;
  gap: var(--sp-3);
  font-size: var(--font-size);
}
.progress-stage {
  color: var(--c-text-secondary);
}
.progress-elapsed {
  /* 等宽，否则秒数跳动时整行会左右抖 */
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  color: var(--c-meta);
}

.progress-detail {
  max-width: min(720px, 80%);
  font-family: var(--font-mono);
  font-size: var(--font-size-xs);
  color: var(--c-text-tertiary);
  text-align: center;
  word-break: break-all;
}

.spinner {
  width: 22px;
  height: 22px;
  border: 2px solid var(--c-border);
  border-top-color: var(--c-accent);
  border-radius: 50%;
  animation: spin 0.7s linear infinite;
}

.spin {
  animation: spin 0.9s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
