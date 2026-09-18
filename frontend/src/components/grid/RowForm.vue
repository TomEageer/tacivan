<script setup lang="ts">
/**
 * 单行表单视图。
 *
 * 宽表在网格里横向铺开几十上百列，看一条记录要一直横向滚。
 * 这里把当前行竖过来，字段名与值一一对应，长文本给多行输入框，
 * 和主流客户端的表单视图是同一个用途。
 */
import { computed, ref, watch } from 'vue'
import type { Cell, Column, ColumnMeta } from '../../types'
import type { GridData } from '../../composables/useGridData'
import { formatBytes, isNullCell } from './cell'
import Icon from '../common/Icon.vue'
import { tr } from '../../i18n'

const props = defineProps<{
  data: GridData
  rowIndex: number
  tableColumns: Column[]
  keyColumns: string[]
  editable: boolean
  /** 网格里本地未提交的改动，键为列名。 */
  pendingEdits?: Record<string, string | null>
}>()

const emit = defineEmits<{
  (e: 'edit', column: string, value: string | null): void
  (e: 'goto', rowIndex: number): void
  (e: 'view-cell', rowIndex: number, column: string): void
  (e: 'close'): void
}>()

const filter = ref('')

const columns = computed<ColumnMeta[]>(() => props.data.columns.value)

const row = computed(() => {
  void props.data.version.value
  return props.data.rowAt(props.rowIndex)
})

/** 取某列的当前值，优先用未提交的本地改动。 */
function cellOf(name: string, i: number): Cell | undefined {
  const pending = props.pendingEdits?.[name]
  if (pending !== undefined) {
    return pending === null ? { n: true } : { v: pending }
  }
  return row.value?.[i]
}

function defOf(name: string): Column | undefined {
  return props.tableColumns.find((c) => c.name === name)
}

function isKey(name: string) {
  return props.keyColumns.includes(name)
}

/** 值长或含换行时用多行输入框。 */
function isMultiline(c: Cell | undefined): boolean {
  if (!c || c.n) return false
  const v = c.v ?? ''
  return v.length > 60 || v.includes('\n')
}

const visibleColumns = computed(() => {
  const kw = filter.value.trim().toLowerCase()
  return columns.value
    .map((c, i) => ({ meta: c, index: i }))
    .filter((x) => !kw || x.meta.name.toLowerCase().includes(kw))
})

const total = computed(() => {
  const t = props.data.total.value
  return t >= 0 ? t : props.data.loaded.value
})

function onInput(name: string, v: string) {
  emit('edit', name, v)
}

function setNull(name: string) {
  emit('edit', name, null)
}

function prev() {
  if (props.rowIndex > 0) emit('goto', props.rowIndex - 1)
}
function next() {
  emit('goto', props.rowIndex + 1)
}

// 换行时把筛选保留，方便逐行核对同一批字段。
watch(
  () => props.rowIndex,
  () => {
    /* 保留 filter */
  },
)
</script>

<template>
  <div class="rowform">
    <header class="rf-head">
      <div class="rf-nav">
        <button class="icon-btn" :disabled="rowIndex <= 0" :title="tr('上一行')" @click="prev">
          <Icon name="chevronUp" :size="12" />
        </button>
        <span class="rf-pos">{{ rowIndex + 1 }} / {{ total.toLocaleString() }}</span>
        <button class="icon-btn" :title="tr('下一行')" @click="next">
          <Icon name="chevronDown" :size="12" />
        </button>
      </div>
      <div class="spacer"></div>
      <button class="icon-btn" :title="tr('关闭表单视图')" @click="emit('close')">
        <Icon name="close" :size="11" />
      </button>
    </header>

    <div class="rf-search">
      <Icon name="search" :size="11" class="rf-search-icon" />
      <input v-model="filter" class="rf-search-input" :placeholder="tr('筛选字段')" spellcheck="false" />
    </div>

    <div v-if="!row" class="rf-empty muted">{{ tr('该行尚未加载') }}</div>

    <div v-else class="rf-fields">
      <div v-for="x in visibleColumns" :key="x.meta.name" class="rf-field">
        <div class="rf-label">
          <Icon v-if="isKey(x.meta.name)" name="key" :size="11" class="rf-key" />
          <span class="rf-name" :title="x.meta.name">{{ x.meta.name }}</span>
          <span class="rf-type">{{ defOf(x.meta.name)?.fullType || x.meta.type }}</span>
        </div>

        <div class="rf-value">
          <template v-if="cellOf(x.meta.name, x.index)?.x">
            <button class="rf-blob" @click="emit('view-cell', rowIndex, x.meta.name)">
              {{ tr('二进制数据 {size} · 点击查看', { size: formatBytes(cellOf(x.meta.name, x.index)?.s ?? 0) }) }}
            </button>
          </template>

          <template v-else-if="cellOf(x.meta.name, x.index)?.t">
            <button class="rf-blob" @click="emit('view-cell', rowIndex, x.meta.name)">
              {{ tr('内容较长（{size}）· 点击查看完整值', { size: formatBytes(cellOf(x.meta.name, x.index)?.s ?? 0) }) }}
            </button>
          </template>

          <template v-else>
            <select
              v-if="editable && defOf(x.meta.name)?.enumValues?.length"
              class="select"
              :value="cellOf(x.meta.name, x.index)?.v ?? ''"
              @change="onInput(x.meta.name, ($event.target as HTMLSelectElement).value)"
            >
              <option v-for="v in defOf(x.meta.name)!.enumValues" :key="v" :value="v">{{ v }}</option>
            </select>

            <textarea
              v-else-if="isMultiline(cellOf(x.meta.name, x.index))"
              class="rf-input rf-textarea mono"
              :readonly="!editable"
              :value="cellOf(x.meta.name, x.index)?.v ?? ''"
              spellcheck="false"
              @input="onInput(x.meta.name, ($event.target as HTMLTextAreaElement).value)"
            ></textarea>

            <input
              v-else
              class="rf-input mono"
              :class="{ 'is-null': isNullCell(cellOf(x.meta.name, x.index)) }"
              :readonly="!editable"
              :value="isNullCell(cellOf(x.meta.name, x.index)) ? '' : (cellOf(x.meta.name, x.index)?.v ?? '')"
              :placeholder="isNullCell(cellOf(x.meta.name, x.index)) ? '(NULL)' : ''"
              spellcheck="false"
              @input="onInput(x.meta.name, ($event.target as HTMLInputElement).value)"
            />
          </template>

          <button
            v-if="editable"
            class="rf-null"
            :title="tr('置为 NULL')"
            :disabled="isNullCell(cellOf(x.meta.name, x.index))"
            @click="setNull(x.meta.name)"
          >
            NULL
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.rowform {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  background: var(--c-bg);
  border-left: 1px solid var(--c-border);
}

.rf-head {
  flex: none;
  display: flex;
  align-items: center;
  gap: 6px;
  height: 28px;
  padding: 0 6px;
  background: var(--c-bg-sunken);
  border-bottom: 1px solid var(--c-border-soft);
}

.rf-nav {
  display: flex;
  align-items: center;
  gap: 2px;
}

.rf-pos {
  min-width: 78px;
  text-align: center;
  font-size: var(--font-size-sm);
  color: var(--c-text-secondary);
  font-variant-numeric: tabular-nums;
}

.spacer {
  flex: 1;
}

.icon-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  padding: 0;
  border: none;
  border-radius: 3px;
  background: transparent;
  color: var(--c-text-secondary);
}
.icon-btn:hover:not(:disabled) {
  background: var(--c-chrome-active);
  color: var(--c-text);
}
.icon-btn:disabled {
  opacity: 0.35;
}

.rf-search {
  flex: none;
  position: relative;
  padding: 5px 6px;
  border-bottom: 1px solid var(--c-border-soft);
}
.rf-search-icon {
  position: absolute;
  left: 13px;
  top: 10px;
  color: var(--c-text-tertiary);
  pointer-events: none;
}
.rf-search-input {
  width: 100%;
  height: 21px;
  padding: 0 7px 0 22px;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  background: var(--c-bg);
  color: var(--c-text);
  outline: none;
  font-size: var(--font-size-sm);
}
.rf-search-input:focus {
  border-color: var(--c-accent);
}

.rf-empty {
  padding: 20px;
  text-align: center;
}

.rf-fields {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 6px;
}

.rf-field {
  margin-bottom: 8px;
}

.rf-label {
  display: flex;
  align-items: center;
  gap: 4px;
  margin-bottom: 2px;
  font-size: var(--font-size-sm);
}

.rf-key {
  color: var(--c-accent);
}

.rf-name {
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.rf-type {
  margin-left: auto;
  padding-left: 8px;
  color: var(--c-text-tertiary);
  font-size: 10px;
  white-space: nowrap;
}

.rf-value {
  display: flex;
  align-items: flex-start;
  gap: 4px;
}

.rf-input {
  flex: 1;
  min-width: 0;
  padding: 3px 6px;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  background: var(--c-bg-sunken);
  color: var(--c-text);
  outline: none;
  font-size: var(--font-size-mono);
}
.rf-input:focus {
  border-color: var(--c-accent);
  background: var(--c-bg);
}
.rf-input:read-only {
  color: var(--c-text-secondary);
}
.rf-input.is-null::placeholder {
  color: var(--c-cell-null);
  font-style: italic;
}

.rf-textarea {
  min-height: 62px;
  max-height: 200px;
  resize: vertical;
  line-height: 1.5;
}

.rf-blob {
  flex: 1;
  padding: 4px 8px;
  border: 1px dashed var(--c-border-strong);
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--c-text-secondary);
  font-size: var(--font-size-sm);
  text-align: left;
}
.rf-blob:hover {
  border-color: var(--c-accent);
  color: var(--c-accent);
}

.rf-null {
  flex: none;
  height: 22px;
  padding: 0 6px;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--c-text-tertiary);
  font-size: 10px;
}
.rf-null:hover:not(:disabled) {
  background: var(--c-chrome-active);
  color: var(--c-text);
}
.rf-null:disabled {
  opacity: 0.3;
}

.select {
  flex: 1;
  min-width: 0;
  height: 24px;
}
</style>
