<script setup lang="ts">
/** 查询历史。双击一条即把 SQL 填回当前编辑器。 */
import { computed, onMounted, ref } from 'vue'
import * as api from '../../api'
import { useAppStore } from '../../stores/app'
import Modal from '../common/Modal.vue'
import Icon from '../common/Icon.vue'
import type { HistoryEntry } from '../../types'
import { tr } from '../../i18n'

const emit = defineEmits<{ (e: 'use', sql: string): void; (e: 'close'): void }>()
const app = useAppStore()

const entries = ref<HistoryEntry[]>([])
const keyword = ref('')
const selected = ref<HistoryEntry | null>(null)
const loading = ref(false)

onMounted(load)

async function load() {
  loading.value = true
  try {
    entries.value = await api.GetHistory(0)
  } catch (e) {
    app.reportError(e, tr('读取历史失败'))
  } finally {
    loading.value = false
  }
}

const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  if (!kw) return entries.value
  return entries.value.filter(
    (e) => e.sql.toLowerCase().includes(kw) || e.connection.toLowerCase().includes(kw),
  )
})

async function clear() {
  if (!confirm(tr('确定清空全部查询历史？'))) return
  try {
    await api.ClearHistory()
    entries.value = []
    selected.value = null
    app.toast('success', tr('历史已清空'))
  } catch (e) {
    app.reportError(e, tr('清空失败'))
  }
}

function formatTime(iso: string) {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(
    d.getMinutes(),
  )}:${pad(d.getSeconds())}`
}

function oneLine(sql: string) {
  return sql.replace(/\s+/g, ' ').trim()
}

function use(e: HistoryEntry) {
  emit('use', e.sql)
  emit('close')
}
</script>

<template>
  <Modal :title="tr('查询历史')" :width="820" :height="560" @close="emit('close')">
    <div class="history">
      <div class="hbar">
        <div class="search">
          <Icon name="search" :size="12" class="search-icon" />
          <input v-model="keyword" class="search-field" :placeholder="tr('搜索 SQL 或连接名')" />
        </div>
        <span class="muted">{{ tr('{a} / {b} 条', { a: filtered.length, b: entries.length }) }}</span>
        <div class="spacer"></div>
        <button class="btn btn-ghost" :disabled="loading" @click="load">
          <Icon name="refresh" :size="13" />
        </button>
        <button class="btn btn-ghost" @click="clear">
          <Icon name="trash" :size="13" />{{ tr('清空') }}</button>
      </div>

      <div class="hbody">
        <div class="hlist">
          <div
            v-for="e in filtered"
            :key="e.id"
            class="hitem"
            :class="{ 'is-selected': selected?.id === e.id, 'is-failed': !e.success }"
            @click="selected = e"
            @dblclick="use(e)"
          >
            <div class="hitem-main mono">{{ oneLine(e.sql) }}</div>
            <div class="hitem-meta">
              <span>{{ formatTime(e.at) }}</span>
              <span>{{ e.connection }}{{ e.database ? ` / ${e.database}` : '' }}</span>
              <span>{{ e.durationMs }} ms</span>
              <span v-if="e.success && e.rowsAffected">{{ tr('{n} 行', { n: e.rowsAffected }) }}</span>
              <span v-if="!e.success" class="badge badge-error">{{ tr('失败') }}</span>
            </div>
          </div>
          <div v-if="!filtered.length" class="empty-state">
            <div class="empty-state-title">{{ tr('没有匹配的历史记录') }}</div>
          </div>
        </div>

        <div class="hdetail">
          <template v-if="selected">
            <pre class="hsql mono selectable">{{ selected.sql }}</pre>
            <div v-if="selected.error" class="herror selectable">{{ selected.error }}</div>
          </template>
          <div v-else class="empty-state">
            <div class="empty-state-title">{{ tr('选择一条记录查看完整 SQL') }}</div>
          </div>
        </div>
      </div>
    </div>

    <template #footer>
      <button class="btn" @click="emit('close')">{{ tr('关闭') }}</button>
      <button class="btn btn-primary" :disabled="!selected" @click="selected && use(selected)">{{ tr('填入编辑器') }}</button>
    </template>
  </Modal>
</template>

<style scoped>
.history {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 340px;
}

.hbar {
  flex: none;
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

.search {
  position: relative;
  width: 260px;
}
.search-icon {
  position: absolute;
  left: 7px;
  top: 6px;
  color: var(--c-text-tertiary);
  pointer-events: none;
}
.search-field {
  width: 100%;
  height: 22px;
  padding: 0 8px 0 24px;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  background: var(--c-bg);
  color: var(--c-text);
  outline: none;
}
.search-field:focus {
  border-color: var(--c-accent);
}

.spacer {
  flex: 1;
}

.hbody {
  flex: 1;
  display: flex;
  min-height: 0;
  gap: 8px;
}

.hlist {
  flex: 1;
  min-width: 0;
  overflow: auto;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
}

.hitem {
  padding: 5px 8px;
  border-bottom: 1px solid var(--c-border-soft);
}
.hitem:hover {
  background: var(--c-row-hover);
}
.hitem.is-selected {
  background: var(--c-accent-soft);
}
.hitem.is-failed {
  border-left: 3px solid var(--c-danger);
}

.hitem-main {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--font-size-mono);
}

.hitem-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 9px;
  margin-top: 2px;
  font-size: var(--font-size-sm);
  color: var(--c-text-tertiary);
}

.hdetail {
  width: 42%;
  min-width: 240px;
  display: flex;
  flex-direction: column;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  background: var(--c-bg-sunken);
  overflow: hidden;
}

.hsql {
  flex: 1;
  margin: 0;
  padding: 9px;
  overflow: auto;
  font-size: var(--font-size-mono);
  line-height: 1.55;
  white-space: pre-wrap;
  word-break: break-word;
}

.herror {
  flex: none;
  max-height: 40%;
  overflow: auto;
  padding: 8px 9px;
  border-top: 1px solid var(--c-border);
  background: var(--c-danger-soft);
  color: var(--c-danger);
  word-break: break-word;
}
</style>
