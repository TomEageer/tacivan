<script setup lang="ts">
/**
 * Redis 键浏览器。
 *
 * 键列表复用数据网格的虚拟滚动与分块取数，底层走 SCAN 游标，
 * 所以几百万键的实例也能平滑浏览，且全程不会执行 KEYS *。
 */
import { computed, onMounted, ref } from 'vue'
import * as api from '../../api'
import { useAppStore } from '../../stores/app'
import { useTabsStore, type Tab } from '../../stores/tabs'
import { useGridData } from '../../composables/useGridData'
import type { DatabaseInfo, KeyValue } from '../../types'
import DataGrid from '../grid/DataGrid.vue'
import Icon from '../common/Icon.vue'
import { tr } from '../../i18n'

const props = defineProps<{ tab: Tab }>()

const app = useAppStore()
const tabs = useTabsStore()
const data = useGridData()
const grid = ref<InstanceType<typeof DataGrid> | null>(null)

const databases = ref<DatabaseInfo[]>([])
const database = ref(props.tab.database || 'db0')
const pattern = ref(props.tab.pattern ?? '*')
const loading = ref(false)

const current = ref<KeyValue | null>(null)
const editValue = ref('')
const editTtl = ref(-1)
const dirty = ref(false)

const cmdLine = ref('')
const cmdOutput = ref<{ cmd: string; out: string; error: boolean }[]>([])
const showConsole = ref(false)

onMounted(async () => {
  try {
    databases.value = await api.ListDatabases(props.tab.connId)
    if (!databases.value.some((d) => d.name === database.value) && databases.value.length) {
      database.value = databases.value[0].name
    }
  } catch (e) {
    app.reportError(e, tr('读取 Redis 库列表失败'))
  }
  await scan()
})

async function scan() {
  loading.value = true
  current.value = null
  try {
    const res = await api.RedisScanKeys(
      props.tab.connId,
      database.value,
      pattern.value || '*',
      app.settings.gridPageSize,
    )
    tabs.setResult(props.tab.id, res.resultId)
    data.reset(res.resultId, res.page)
    props.tab.pattern = pattern.value
    props.tab.database = database.value
  } catch (e) {
    app.reportError(e, tr('扫描键失败'))
  } finally {
    loading.value = false
  }
}

/** 从网格行里取出键名。 */
function keyNameAt(rowIndex: number): string | null {
  const row = data.rowAt(rowIndex)
  if (!row || !row[0]) return null
  return row[0].v ?? ''
}

async function loadKey(rowIndex: number) {
  const key = keyNameAt(rowIndex)
  if (!key) return
  try {
    const kv = await api.RedisGetKey(props.tab.connId, database.value, key)
    current.value = kv
    editValue.value = kv.value ?? ''
    editTtl.value = kv.ttl
    dirty.value = false
  } catch (e) {
    app.reportError(e, tr('读取键失败'))
  }
}

async function saveKey() {
  if (!current.value) return
  try {
    await api.RedisSetKey(props.tab.connId, database.value, {
      ...current.value,
      value: editValue.value,
      ttl: editTtl.value,
    })
    app.toast('success', tr('已保存'), current.value.key)
    dirty.value = false
    await loadKey(grid.value?.selection.row ?? 0)
  } catch (e) {
    app.reportError(e, tr('保存失败'))
  }
}

async function deleteKey() {
  if (!current.value) return
  if (
    app.settings.confirmOnDelete &&
    !confirm(tr('确定删除键「{key}」？', { key: current.value.key }))
  )
    return
  try {
    const n = await api.RedisDeleteKeys(props.tab.connId, database.value, [current.value.key])
    app.toast('success', tr('已删除 {n} 个键', { n }))
    current.value = null
    await scan()
  } catch (e) {
    app.reportError(e, tr('删除失败'))
  }
}

async function applyTtl() {
  if (!current.value) return
  try {
    await api.RedisExpireKey(props.tab.connId, database.value, current.value.key, editTtl.value)
    app.toast(
      'success',
      editTtl.value > 0
        ? tr('已设置 {s} 秒后过期', { s: editTtl.value })
        : tr('已设为永不过期'),
    )
    await loadKey(grid.value?.selection.row ?? 0)
  } catch (e) {
    app.reportError(e, tr('设置过期时间失败'))
  }
}

async function runCommand() {
  const line = cmdLine.value.trim()
  if (!line) return
  try {
    const out = await api.RedisCommand(props.tab.connId, database.value, line)
    cmdOutput.value.unshift({ cmd: line, out, error: false })
  } catch (e) {
    cmdOutput.value.unshift({
      cmd: line,
      out: e instanceof Error ? e.message : String(e),
      error: true,
    })
  }
  cmdLine.value = ''
  // 只保留最近若干条，避免长时间使用后把内存耗在日志上。
  if (cmdOutput.value.length > 200) cmdOutput.value.length = 200
}

const ttlText = computed(() => {
  const t = current.value?.ttl ?? -1
  if (t < 0) return tr('永不过期')
  if (t < 60) return tr('{s} 秒', { s: t })
  if (t < 3600) return tr('{m} 分 {s} 秒', { m: Math.floor(t / 60), s: t % 60 })
  return tr('{h} 小时 {m} 分', { h: Math.floor(t / 3600), m: Math.floor((t % 3600) / 60) })
})

const isCollection = computed(
  () => !!current.value && current.value.type !== 'string' && current.value.type !== 'none',
)

defineExpose({ refresh: scan })
</script>

<template>
  <div class="tabpane">
    <div class="rbar">
      <select v-model="database" class="select db-select" @change="scan">
        <option v-for="d in databases" :key="d.name" :value="d.name">
          {{ d.name }}{{ d.collation ? ` (${d.collation})` : '' }}
        </option>
      </select>

      <div class="filter-input">
        <Icon name="search" :size="12" class="filter-icon" />
        <input
          v-model="pattern"
          class="filter-field mono"
          :placeholder="tr('键匹配模式，如 user:* （内部走 SCAN，不会阻塞实例）')"
          spellcheck="false"
          @keydown.enter="scan"
        />
      </div>
      <button class="btn" :disabled="loading" @click="scan">{{ tr('扫描') }}</button>

      <div class="spacer"></div>
      <button
        class="btn btn-ghost"
        :class="{ 'is-on': showConsole }"
        :title="tr('命令行')"
        @click="showConsole = !showConsole"
      >
        <Icon name="terminal" :size="13" />{{ tr('命令行') }}</button>
    </div>

    <div class="rbody">
      <div class="keylist">
        <DataGrid
          ref="grid"
          :data="data"
          :editable="false"
          :zebra="false"
          @row-activate="loadKey"
          @view-cell="(row) => loadKey(row)"
        />
        <div class="keylist-foot">
          <span>
            {{
              data.total.value >= 0
                ? tr('共 {n} 个键', { n: data.total.value.toLocaleString() })
                : tr('已扫描 {n} 个键', { n: data.loaded.value.toLocaleString() })
            }}
          </span>
          <span class="muted">{{ tr('双击或按回车查看值') }}</span>
        </div>
      </div>

      <div class="keydetail">
        <div v-if="!current" class="empty-state">
          <Icon name="layers" :size="26" />
          <div class="empty-state-title">{{ tr('未选择键') }}</div>
          <div>{{ tr('在左侧双击一个键查看它的值') }}</div>
        </div>

        <template v-else>
          <div class="detail-head">
            <span class="badge">{{ current.type }}</span>
            <span class="key-name mono selectable">{{ current.key }}</span>
            <div class="spacer"></div>
            <button class="btn btn-ghost" :title="tr('删除键')" @click="deleteKey">
              <Icon name="trash" :size="13" />
            </button>
          </div>

          <div class="detail-meta">
            <span>{{ tr('大小 {v}', { v: current.size }) }}</span>
            <span v-if="current.encoding">{{ tr('编码 {v}', { v: current.encoding }) }}</span>
            <span v-if="current.memoryUsage">{{ tr('占用 {n} B', { n: current.memoryUsage }) }}</span>
            <span>TTL {{ ttlText }}</span>
          </div>

          <div class="detail-ttl">
            <label>{{ tr('过期时间（秒）') }}</label>
            <input v-model.number="editTtl" class="input ttl-input" type="number" />
            <button class="btn" @click="applyTtl">{{ tr('应用') }}</button>
            <span class="muted">{{ tr('填 -1 或 0 表示永不过期') }}</span>
          </div>

          <div v-if="isCollection" class="entries">
            <table class="etable">
              <thead>
                <tr>
                  <th class="w-field">{{ current.type === 'zset' ? tr('成员') : tr('字段') }}</th>
                  <th v-if="current.type === 'zset'" class="w-score">{{ tr('分值') }}</th>
                  <th v-else>{{ tr('值') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(e, i) in current.entries" :key="i">
                  <td class="mono selectable">{{ e.field || e.value }}</td>
                  <td class="mono selectable">
                    {{ current.type === 'zset' ? e.score : e.value }}
                  </td>
                </tr>
              </tbody>
            </table>
            <div v-if="current.truncated" class="badge">{{ tr('仅显示前一页成员，完整内容请用命令行查询') }}</div>
          </div>

          <div v-else class="value-editor">
            <textarea
              v-model="editValue"
              class="value-area mono selectable"
              spellcheck="false"
              @input="dirty = true"
            ></textarea>
            <div class="value-foot">
              <span class="muted">{{ tr('{n} 字符', { n: editValue.length }) }}</span>
              <div class="spacer"></div>
              <button class="btn btn-primary" :disabled="!dirty" @click="saveKey">{{ tr('保存') }}</button>
            </div>
          </div>
        </template>
      </div>
    </div>

    <!-- 命令行 -->
    <div v-if="showConsole" class="console">
      <div class="console-out">
        <div v-for="(o, i) in cmdOutput" :key="i" class="console-entry">
          <div class="console-cmd mono">&gt; {{ o.cmd }}</div>
          <pre class="console-res mono selectable" :class="{ 'is-error': o.error }">{{ o.out }}</pre>
        </div>
      </div>
      <div class="console-in">
        <span class="console-prompt mono">{{ database }} &gt;</span>
        <input
          v-model="cmdLine"
          class="console-field mono"
          :placeholder="tr('输入 Redis 命令，回车执行（FLUSHALL / KEYS 等危险命令会被拦截）')"
          spellcheck="false"
          @keydown.enter="runCommand"
        />
      </div>
    </div>
  </div>
</template>

<style scoped>
.tabpane {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}

.rbar {
  flex: none;
  display: flex;
  align-items: center;
  gap: 6px;
  height: 32px;
  padding: 0 8px;
  background: var(--c-bg-sunken);
  border-bottom: 1px solid var(--c-border-soft);
}

.db-select {
  width: auto;
  min-width: 120px;
}

.filter-input {
  position: relative;
  flex: 1;
  max-width: 520px;
}
.filter-icon {
  position: absolute;
  left: 7px;
  top: 6px;
  color: var(--c-text-tertiary);
  pointer-events: none;
}
.filter-field {
  width: 100%;
  height: 22px;
  padding: 0 8px 0 24px;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  background: var(--c-bg);
  color: var(--c-text);
  outline: none;
  font-size: var(--font-size);
}
.filter-field:focus {
  border-color: var(--c-accent);
}

.spacer {
  flex: 1;
}

.btn.is-on {
  background: var(--c-accent-soft);
  color: var(--c-accent);
}

.rbody {
  flex: 1;
  display: flex;
  min-height: 0;
}

.keylist {
  display: flex;
  flex-direction: column;
  width: 46%;
  min-width: 260px;
  border-right: 1px solid var(--c-border);
}

.keylist-foot {
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

.keydetail {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
  padding: 10px;
  overflow: auto;
}

.detail-head {
  display: flex;
  align-items: center;
  gap: 7px;
  margin-bottom: 6px;
}

.key-name {
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.detail-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-bottom: 8px;
  font-size: var(--font-size-sm);
  color: var(--c-text-secondary);
}

.detail-ttl {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 10px;
  font-size: var(--font-size-sm);
  color: var(--c-text-secondary);
}
.ttl-input {
  width: 110px;
}

.entries {
  flex: 1;
  min-height: 0;
  overflow: auto;
}

.etable {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--font-size);
}
.etable th {
  position: sticky;
  top: 0;
  padding: 4px 6px;
  text-align: left;
  font-weight: 600;
  color: var(--c-text-secondary);
  background: var(--c-chrome);
  border-bottom: 1px solid var(--c-border);
}
.etable td {
  padding: 3px 6px;
  border-bottom: 1px solid var(--c-border-soft);
  word-break: break-all;
}
.w-field {
  width: 40%;
}
.w-score {
  width: 120px;
}

.value-editor {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 160px;
}

.value-area {
  flex: 1;
  min-height: 120px;
  padding: 8px;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  background: var(--c-bg);
  color: var(--c-text);
  outline: none;
  resize: none;
  font-size: var(--font-size-mono);
  line-height: 1.55;
}
.value-area:focus {
  border-color: var(--c-accent);
}

.value-foot {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 6px;
  font-size: var(--font-size-sm);
}

.console {
  flex: none;
  display: flex;
  flex-direction: column;
  height: 200px;
  border-top: 1px solid var(--c-border);
  background: var(--c-bg-sunken);
}

.console-out {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 6px 8px;
}

.console-entry {
  margin-bottom: 6px;
}

.console-cmd {
  color: var(--c-accent);
  font-size: var(--font-size-mono);
}

.console-res {
  margin: 2px 0 0;
  font-size: var(--font-size-mono);
  color: var(--c-text-secondary);
  white-space: pre-wrap;
  word-break: break-all;
}
.console-res.is-error {
  color: var(--c-danger);
}

.console-in {
  flex: none;
  display: flex;
  align-items: center;
  gap: 6px;
  height: 26px;
  padding: 0 8px;
  border-top: 1px solid var(--c-border-soft);
  background: var(--c-bg);
}

.console-prompt {
  color: var(--c-accent);
  font-size: var(--font-size-mono);
}

.console-field {
  flex: 1;
  border: none;
  background: transparent;
  color: var(--c-text);
  outline: none;
  font-size: var(--font-size-mono);
}
</style>
