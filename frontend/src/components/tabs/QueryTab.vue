<script setup lang="ts">
/** SQL 编辑器标签页：编辑 / 执行 / 多结果集 / 执行日志。 */
import { computed, onMounted, ref, shallowRef, watch, onBeforeUnmount } from 'vue'
import * as api from '../../api'
import { useAppStore } from '../../stores/app'
import { useConnectionsStore } from '../../stores/connections'
import { useTabsStore, type Tab } from '../../stores/tabs'
import { useGridData, type GridData } from '../../composables/useGridData'
import type { DatabaseInfo, StatementResult } from '../../types'
import SqlEditor from '../editor/SqlEditor.vue'
import DataGrid from '../grid/DataGrid.vue'
import Icon from '../common/Icon.vue'
import CellViewer from '../dialogs/CellViewer.vue'
import ExportDialog from '../dialogs/ExportDialog.vue'
import { tr } from '../../i18n'

const props = defineProps<{ tab: Tab }>()

const app = useAppStore()
const conns = useConnectionsStore()
const tabs = useTabsStore()

const editor = ref<InstanceType<typeof SqlEditor> | null>(null)
const sqlText = ref(props.tab.sql ?? '')
const database = ref(props.tab.database)
const databases = ref<DatabaseInfo[]>([])
const running = ref(false)
const executionId = ref('')
const inTransaction = ref(false)
const stopOnError = ref(true)

/** 每个查询结果一个网格数据源。 */
interface ResultPane {
  key: string
  label: string
  statement: StatementResult
  data: GridData
}
const panes = shallowRef<ResultPane[]>([])
const activePane = ref(0)
/** 'messages' 表示当前显示执行日志。 */
const bottomTab = ref<'result' | 'messages'>('messages')
const messages = ref<StatementResult[]>([])
const totalDuration = ref(0)

const editorHeight = ref(260)
const viewer = ref<{ paneIndex: number; row: number; column: string } | null>(null)
const showExport = ref(false)

/** 自动补全用的库表结构。 */
const schema = ref<Record<string, string[]>>({})

const engine = computed(() => props.tab.engine)
const connState = computed(() => conns.stateOf(props.tab.connId))

/** 连接没打开时，编辑器仍然要能开——恢复出来的草稿就属于这种情况。 */
const connOpen = computed(() => conns.isOpen(props.tab.connId))

onMounted(async () => {
  if (connOpen.value) {
    await loadDatabases()
    loadSchema()
  }
  editor.value?.focus()
})

// 连接随后被打开时再补上库列表和补全用的结构。
watch(connOpen, (open) => {
  if (!open) return
  void loadDatabases().then(loadSchema)
})

/**
 * 草稿自动落盘。
 *
 * 停止输入 1.2 秒后写一次。不是每次按键都写——那既是无谓的磁盘写入，
 * 也会让原子替换（写临时文件再改名）在快速输入时反复执行。
 * 1.2 秒是个折中：丢掉的最多是最后一句话，而绝大多数停顿都比它长。
 */
let draftTimer = 0
function scheduleDraft() {
  if (draftTimer) window.clearTimeout(draftTimer)
  draftTimer = window.setTimeout(saveDraft, 1200)
}

async function saveDraft() {
  draftTimer = 0
  try {
    await api.SaveDraft({
      id: props.tab.id,
      title: props.tab.title,
      connId: props.tab.connId,
      connName: props.tab.connName,
      database: props.tab.database,
      sql: sqlText.value,
    })
  } catch {
    // 草稿是尽力而为的，写失败不该打断用户输入。
  }
}

watch(sqlText, (v) => {
  props.tab.sql = v
  tabs.setDirty(props.tab.id, v.trim().length > 0 && v !== '')
  scheduleDraft()
})

// 标签页被销毁（切走或关闭）时，把还没到点的那次写入补上。
onBeforeUnmount(() => {
  if (draftTimer) {
    window.clearTimeout(draftTimer)
    void saveDraft()
  }
})

watch(database, (v) => {
  props.tab.database = v
  conns.setCurrentDatabase(props.tab.connId, v)
  loadSchema()
})

async function loadDatabases() {
  try {
    databases.value = await api.ListDatabases(props.tab.connId)
    if (!database.value && databases.value.length) {
      database.value = databases.value.find((d) => d.current)?.name ?? databases.value[0].name
    }
  } catch (e) {
    app.reportError(e, tr('读取数据库列表失败'))
  }
}

/** 拉当前库的表与列，喂给编辑器补全。 */
async function loadSchema() {
  if (!database.value) return
  try {
    const objs = await api.ListObjects(props.tab.connId, database.value, '', ['table', 'view'])
    const next: Record<string, string[]> = {}
    for (const o of objs) next[o.name] = []
    schema.value = next

    // 列信息按需补：表很多时一次性全查会拖慢打开速度，
    // 这里只为前若干张表预取，其余等用户实际用到再说。
    for (const o of objs.slice(0, 40)) {
      try {
        const def = await api.GetTableDefinition(props.tab.connId, {
          database: database.value,
          schema: o.schema ?? '',
          name: o.name,
          kind: o.kind,
        })
        next[o.name] = def.columns.map((c) => c.name)
      } catch {
        // 单张表读失败不影响整体补全。
      }
    }
    schema.value = { ...next }
  } catch {
    // 补全是锦上添花，失败就静默降级成关键字补全。
  }
}

function releasePanes() {
  for (const p of panes.value) {
    if (p.statement.resultId) api.CloseResult(p.statement.resultId).catch(() => {})
  }
  panes.value = []
}

async function run(scope: 'all' | 'current' | 'selection' = 'all') {
  if (running.value) return
  let script = sqlText.value
  if (scope === 'selection') {
    const sel = editor.value?.selectedText() ?? ''
    if (sel.trim()) script = sel
  } else if (scope === 'current') {
    const sel = editor.value?.selectedText() ?? ''
    if (sel.trim()) {
      script = sel
    } else {
      try {
        const st = await api.GetStatementAt(
          sqlText.value,
          editor.value?.cursorOffset() ?? 0,
          engine.value,
        )
        script = st.text
      } catch (e) {
        app.reportError(e, tr('定位当前语句失败'))
        return
      }
    }
  }
  if (!script.trim()) return

  running.value = true
  executionId.value = `exec-${Date.now()}`
  releasePanes()
  messages.value = []

  try {
    const res = await api.ExecuteSQL(props.tab.connId, database.value, script, {
      executionId: executionId.value,
      stopOnError: stopOnError.value,
      inTransaction: inTransaction.value,
      firstPageRows: app.settings.gridPageSize,
      timeoutSeconds: 0,
    })
    messages.value = res.statements
    totalDuration.value = res.totalDurationMs

    const next: ResultPane[] = []
    for (const st of res.statements) {
      if (!st.resultId || !st.page) continue
      const data = useGridData()
      data.reset(st.resultId, st.page)
      next.push({
        key: st.resultId,
        label: tr('结果 {n}', { n: next.length + 1 }),
        statement: st,
        data,
      })
      tabs.addResult(props.tab.id, st.resultId)
    }
    panes.value = next
    activePane.value = 0
    bottomTab.value = next.length ? 'result' : 'messages'

    const failed = res.statements.filter((s) => s.error)
    if (res.canceled) app.toast('warning', tr('执行已取消'))
    else if (failed.length)
    app.toast('error', tr('{n} 条语句执行失败', { n: failed.length }), failed[0].error)
    else if (!next.length) {
      const affected = res.statements.reduce((a, s) => a + s.rowsAffected, 0)
      app.toast(
      'success',
      tr('执行完成'),
      tr('影响 {rows} 行，耗时 {ms} ms', { rows: affected, ms: res.totalDurationMs }),
    )
    }
  } catch (e) {
    app.reportError(e, tr('执行失败'))
  } finally {
    running.value = false
    executionId.value = ''
  }
}

async function stop() {
  if (!executionId.value) return
  try {
    await api.CancelExecution(executionId.value)
    app.toast('warning', tr('已发送取消请求'))
  } catch (e) {
    app.reportError(e, tr('取消失败'))
  }
}

async function format() {
  try {
    sqlText.value = await api.FormatSQL(sqlText.value, engine.value)
    editor.value?.setValue(sqlText.value)
  } catch (e) {
    app.reportError(e, tr('格式化失败'))
  }
}

async function explain() {
  const sel = editor.value?.selectedText() ?? ''
  let stmt = sel
  if (!stmt.trim()) {
    try {
      const st = await api.GetStatementAt(
        sqlText.value,
        editor.value?.cursorOffset() ?? 0,
        engine.value,
      )
      stmt = st.text
    } catch {
      app.toast('warning', tr('请先把光标放在要解释的语句上'))
      return
    }
  }
  running.value = true
  try {
    releasePanes()
    const st = await api.ExplainSQL(props.tab.connId, database.value, stmt, false)
    const data = useGridData()
    data.reset(st.resultId!, st.page!)
    panes.value = [{ key: st.resultId!, label: tr('执行计划'), statement: st, data }]
    tabs.addResult(props.tab.id, st.resultId!)
    activePane.value = 0
    bottomTab.value = 'result'
  } catch (e) {
    app.reportError(e, tr('获取执行计划失败'))
  } finally {
    running.value = false
  }
}

async function openFile() {
  try {
    const p = await api.OpenSQLFileDialog()
    if (!p) return
    const text = await api.ReadTextFile(p)
    sqlText.value = text
    editor.value?.setValue(text)
    props.tab.filePath = p
    tabs.setTitle(props.tab.id, p.split('/').pop() ?? tr('查询'))
  } catch (e) {
    app.reportError(e, tr('打开文件失败'))
  }
}

async function saveFile(saveAs = false) {
  try {
    let path = props.tab.filePath
    if (!path || saveAs) {
      path = await api.SaveSQLFileDialog(`${props.tab.title}.sql`)
      if (!path) return
    }
    await api.WriteTextFile(path, sqlText.value)
    props.tab.filePath = path
    tabs.setTitle(props.tab.id, path.split('/').pop() ?? tr('查询'))
    tabs.setDirty(props.tab.id, false)
    app.toast('success', tr('已保存'), path)
  } catch (e) {
    app.reportError(e, tr('保存失败'))
  }
}

// --- 编辑器高度拖拽 ---
let dragStartY = 0
let dragStartH = 0
function onSplitterDown(e: MouseEvent) {
  dragStartY = e.clientY
  dragStartH = editorHeight.value
  window.addEventListener('mousemove', onSplitterMove)
  window.addEventListener('mouseup', onSplitterUp)
}
function onSplitterMove(e: MouseEvent) {
  editorHeight.value = Math.max(80, dragStartH + e.clientY - dragStartY)
}
function onSplitterUp() {
  window.removeEventListener('mousemove', onSplitterMove)
  window.removeEventListener('mouseup', onSplitterUp)
}

const currentPane = computed(() => panes.value[activePane.value])

function kindLabel(kind: string) {
  return (
    { query: tr('查询'), dml: tr('数据变更'), ddl: tr('结构变更'), tcl: tr('事务'), utility: tr('命令') }[kind] ?? kind
  )
}

defineExpose({
  run: () => run('all'),
  runCurrent: () => run('current'),
  stop,
  format,
  explain,
  openFile,
  save: () => saveFile(false),
  saveAs: () => saveFile(true),
  exportData: () => (showExport.value = true),
  insertText: (t: string) => editor.value?.insert(t),
})
</script>

<template>
  <div class="tabpane">
    <!-- 工具条 -->
    <div class="qbar">
      <button class="btn btn-primary" :disabled="running" title="⌘R" @click="run('all')">
        <Icon name="play" :size="11" />{{ tr('运行') }}</button>
      <button class="btn" :disabled="running" title="⇧⌘R" @click="run('current')">{{ tr('运行当前') }}</button>
      <button class="btn" :disabled="!running" title="⌘." @click="stop">
        <Icon name="stop" :size="10" />{{ tr('停止') }}</button>

      <div class="qbar-divider"></div>

      <select v-model="database" class="select db-select" :disabled="running">
        <option v-for="d in databases" :key="d.name" :value="d.name">{{ d.name }}</option>
      </select>

      <div class="qbar-divider"></div>

      <button class="btn btn-ghost" :title="tr('格式化 SQL（⇧⌘F）')" @click="format">
        <Icon name="code" :size="13" />{{ tr('格式化') }}</button>
      <button
        class="btn btn-ghost"
        :title="tr('解释执行计划（⌘E）')"
        :disabled="running || !connState?.capability.explainPlan"
        @click="explain"
      >
        <Icon name="chart" :size="13" />{{ tr('解释') }}</button>

      <div class="spacer"></div>

      <label class="checkbox" :title="tr('整批语句包在一个事务里执行')">
        <input v-model="inTransaction" type="checkbox" :disabled="!connState?.capability.transactions" />{{ tr('事务') }}</label>
      <label class="checkbox" :title="tr('遇到错误就停止后续语句')">
        <input v-model="stopOnError" type="checkbox" />{{ tr('出错即停') }}</label>
      <button class="btn btn-ghost" :title="tr('打开 SQL 文件')" @click="openFile">
        <Icon name="upload" :size="13" />
      </button>
      <button class="btn btn-ghost" :title="tr('保存到文件（⌘S）')" @click="saveFile(false)">
        <Icon name="save" :size="13" />
      </button>
    </div>

    <!-- 编辑器 -->
    <div class="editor-wrap" :style="{ height: editorHeight + 'px' }">
      <SqlEditor
        ref="editor"
        v-model="sqlText"
        :engine="engine"
        :schema="schema"
        @run="run('all')"
        @run-current="run('current')"
      />
    </div>

    <div class="splitter" @mousedown="onSplitterDown"></div>

    <!-- 结果区 -->
    <div class="results">
      <div class="result-tabs">
        <button
          v-for="(p, i) in panes"
          :key="p.key"
          class="rtab"
          :class="{ 'is-active': bottomTab === 'result' && activePane === i }"
          @click="
            () => {
              bottomTab = 'result'
              activePane = i
            }
          "
        >
          <Icon name="grid" :size="11" /> {{ p.label }}
        </button>
        <button
          class="rtab"
          :class="{ 'is-active': bottomTab === 'messages' }"
          @click="bottomTab = 'messages'"
        >
          <Icon name="info" :size="11" />{{ tr('信息') }}<span v-if="messages.some((m) => m.error)" class="dot-error"></span>
        </button>

        <div class="spacer"></div>
        <span v-if="totalDuration" class="muted">{{ tr('总耗时 {ms} ms', { ms: totalDuration }) }}</span>
        <button
          v-if="currentPane"
          class="btn btn-ghost btn-mini"
          :title="tr('导出当前结果集')"
          @click="showExport = true"
        >
          <Icon name="download" :size="12" />
        </button>
      </div>

      <template v-if="bottomTab === 'result' && currentPane">
        <DataGrid
          :key="currentPane.key"
          :data="currentPane.data"
          :editable="false"
          :key-columns="currentPane.statement.keyColumns ?? []"
          :table-columns="currentPane.statement.tableColumns ?? []"
          @view-cell="
            (row, column) => (viewer = { paneIndex: activePane, row, column })
          "
        />
        <div class="result-foot">
          <span>
            {{
              currentPane.data.total.value >= 0
                ? tr('共 {n} 行', { n: currentPane.data.total.value.toLocaleString() })
                : tr('已加载 {n} 行', { n: currentPane.data.loaded.value.toLocaleString() })
            }}
          </span>
          <button
            v-if="currentPane.data.total.value < 0 && !currentPane.data.complete.value"
            class="btn btn-ghost btn-mini"
            @click="currentPane.data.countExact()"
          >{{ tr('统计总行数') }}</button>
          <span v-if="currentPane.statement.editableReason" class="muted">
            {{ currentPane.statement.editableReason }}
          </span>
          <div class="spacer"></div>
          <span class="muted">{{ currentPane.statement.durationMs }} ms</span>
        </div>
      </template>

      <div v-else class="messages selectable">
        <div v-if="!messages.length" class="empty-state">
          <div class="empty-state-title">{{ tr('尚未执行任何语句') }}</div>
          <div>{{ tr('⌘R 执行全部，⇧⌘R 执行光标所在语句') }}</div>
        </div>
        <div
          v-for="m in messages"
          :key="m.index"
          class="msg"
          :class="{ 'is-error': m.error, 'is-skipped': m.skipped }"
        >
          <div class="msg-head">
            <span class="badge">{{ tr('第 {i} 条 · 行 {line}', { i: m.index + 1, line: m.line }) }}</span>
            <span class="badge">{{ kindLabel(m.kind) }}</span>
            <span v-if="m.skipped" class="badge">{{ tr('已跳过') }}</span>
            <span v-else-if="m.error" class="badge badge-error">{{ tr('失败') }}</span>
            <span v-else class="badge badge-ok">{{ tr('成功') }}</span>
            <span class="spacer"></span>
            <span v-if="!m.skipped" class="muted">{{ m.durationMs }} ms</span>
          </div>
          <pre class="msg-sql mono">{{ m.sql }}</pre>
          <div v-if="m.error" class="msg-error">{{ m.error }}</div>
          <div v-else-if="!m.resultId && !m.skipped" class="msg-ok">
            {{ tr('影响 {n} 行', { n: m.rowsAffected }) }}<span v-if="m.lastInsertId">
              {{ tr('· 自增 ID {id}', { id: m.lastInsertId }) }}</span
            >
          </div>
        </div>
      </div>
    </div>

    <CellViewer
      v-if="viewer && panes[viewer.paneIndex]"
      :result-id="panes[viewer.paneIndex].key"
      :row-index="viewer.row"
      :column="viewer.column"
      @close="viewer = null"
    />
    <ExportDialog
      v-if="showExport && currentPane"
      :result-id="currentPane.key"
      default-name="query_result"
      @close="showExport = false"
    />
  </div>
</template>

<style scoped>
.tabpane {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}

.qbar {
  flex: none;
  display: flex;
  align-items: center;
  gap: 5px;
  height: 32px;
  padding: 0 8px;
  background: var(--c-bg-sunken);
  border-bottom: 1px solid var(--c-border-soft);
}

.qbar-divider {
  width: 1px;
  height: 18px;
  margin: 0 3px;
  background: var(--c-border);
}

.db-select {
  width: auto;
  min-width: 130px;
}

.spacer {
  flex: 1;
}

.editor-wrap {
  flex: none;
  min-height: 80px;
  overflow: hidden;
  border-bottom: 1px solid var(--c-border-soft);
}

.splitter {
  flex: none;
  height: 5px;
  background: var(--c-bg-sunken);
  border-bottom: 1px solid var(--c-border);
  cursor: row-resize;
}
.splitter:hover {
  background: var(--c-accent-soft);
}

.results {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.result-tabs {
  flex: none;
  display: flex;
  align-items: center;
  gap: 2px;
  height: 26px;
  padding: 0 8px;
  background: var(--c-bg-sunken);
  border-bottom: 1px solid var(--c-border-soft);
  font-size: var(--font-size-sm);
}

.rtab {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 20px;
  padding: 0 9px;
  border: 1px solid transparent;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--c-text-secondary);
  font-size: var(--font-size-sm);
}
.rtab:hover {
  background: var(--c-chrome-active);
}
.rtab.is-active {
  background: var(--c-accent-soft);
  border-color: var(--c-accent-border);
  color: var(--c-accent);
}

.dot-error {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--c-danger);
}

.result-foot {
  flex: none;
  display: flex;
  align-items: center;
  gap: 10px;
  height: 22px;
  padding: 0 8px;
  background: var(--c-bg-sunken);
  border-top: 1px solid var(--c-border-soft);
  font-size: var(--font-size-sm);
  color: var(--c-text-secondary);
}

.messages {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 8px;
}

.msg {
  margin-bottom: 8px;
  padding: 7px 9px;
  border: 1px solid var(--c-border-soft);
  border-left: 3px solid var(--c-success);
  border-radius: var(--radius-sm);
  background: var(--c-bg-raised);
}
.msg.is-error {
  border-left-color: var(--c-danger);
  background: var(--c-danger-soft);
}
.msg.is-skipped {
  border-left-color: var(--c-border-strong);
  opacity: 0.65;
}

.msg-head {
  display: flex;
  align-items: center;
  gap: 5px;
  margin-bottom: 5px;
  font-size: var(--font-size-sm);
}

.msg-sql {
  margin: 0;
  font-size: var(--font-size-mono);
  color: var(--c-text-secondary);
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 96px;
  overflow: auto;
}

.msg-error {
  margin-top: 5px;
  color: var(--c-danger);
  word-break: break-word;
}

.msg-ok {
  margin-top: 5px;
  color: var(--c-text-secondary);
}

.btn-mini {
  height: 18px;
  padding: 0 7px;
  font-size: var(--font-size-sm);
}
</style>
