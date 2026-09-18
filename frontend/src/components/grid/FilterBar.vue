<script setup lang="ts">
/**
 * 可视化筛选器。
 *
 * 条件只在界面上收集「字段 / 运算符 / 值」，翻译成 WHERE 由后端按方言完成——
 * 让界面去拼 SQL 字符串，等于把注入面放在最容易出错的地方，
 * 而且 LIKE 通配符、引号、标识符的转义规则各引擎并不一致。
 */
import { computed, onMounted, ref, watch } from 'vue'
import * as api from '../../api'
import type { Column, FilterCondition, FilterGroup, FilterOpInfo } from '../../types'
import Icon from '../common/Icon.vue'
import { tr } from '../../i18n'

const props = defineProps<{
  columns: Column[]
  modelValue: FilterGroup
  /** 手写条件，与可视化条件并存。 */
  where: string
  disabled?: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', v: FilterGroup): void
  (e: 'update:where', v: string): void
  (e: 'apply'): void
}>()

const expanded = ref(false)
const ops = ref<FilterOpInfo[]>([])
const showRaw = ref(false)

onMounted(async () => {
  try {
    ops.value = await api.ListFilterOps()
  } catch {
    // 拿不到运算符列表时退化为手写条件，不影响基本使用。
  }
})

const group = computed(() => props.modelValue)

function update(next: Partial<FilterGroup>) {
  emit('update:modelValue', { ...group.value, ...next })
}

function classOf(colName: string): string {
  const c = props.columns.find((x) => x.name === colName)
  if (!c) return 'unknown'
  const t = (c.type || '').toLowerCase()
  if (/int|decimal|numeric|float|double|real|serial|bit|year|money/.test(t)) return 'number'
  if (/date|time|timestamp|interval/.test(t)) return 'time'
  if (/blob|binary|bytea/.test(t)) return 'binary'
  if (/bool/.test(t)) return 'bool'
  if (/json/.test(t)) return 'json'
  return 'string'
}

/** 某列可用的运算符：字符串类的才给「包含 / 开头是」这些。 */
function opsFor(colName: string): FilterOpInfo[] {
  const cls = classOf(colName)
  return ops.value.filter((o) => !o.classes?.length || o.classes.includes(cls))
}

function arityOf(op: string): number {
  return ops.value.find((o) => o.op === op)?.arity ?? 1
}

function addCondition() {
  const first = props.columns[0]?.name ?? ''
  const next = [...group.value.conditions, {
    column: first,
    op: 'eq',
    values: [''],
    enabled: true,
  } as FilterCondition]
  update({ conditions: next })
  expanded.value = true
}

function removeCondition(i: number) {
  const next = group.value.conditions.filter((_, k) => k !== i)
  update({ conditions: next })
}

function patch(i: number, part: Partial<FilterCondition>) {
  const next = group.value.conditions.map((c, k) => (k === i ? { ...c, ...part } : c))
  update({ conditions: next })
}

function onColumnChange(i: number, column: string) {
  const available = opsFor(column)
  const cur = group.value.conditions[i].op
  // 换了列之后原运算符可能不再适用（比如数字列没有「包含」）。
  const op = available.some((o) => o.op === cur) ? cur : (available[0]?.op ?? 'eq')
  patch(i, { column, op })
}

function setValue(i: number, slot: number, v: string) {
  const values = [...(group.value.conditions[i].values ?? [])]
  while (values.length <= slot) values.push('')
  values[slot] = v
  patch(i, { values })
}

function clearAll() {
  update({ conditions: [], conjunction: 'and' })
  emit('update:where', '')
  emit('apply')
}

/** 收起态展示的条件 chip。 */
const chips = computed(() =>
  group.value.conditions
    .map((c, i) => ({ c, i }))
    .filter((x) => x.c.enabled && x.c.column)
    .map((x) => {
      const label = ops.value.find((o) => o.op === x.c.op)?.label ?? x.c.op
      const v = (x.c.values ?? []).filter(Boolean).join(', ')
      return { index: x.i, column: x.c.column, op: label, value: v }
    }),
)

const activeCount = computed(
  () => group.value.conditions.filter((c) => c.enabled && c.column).length + (props.where.trim() ? 1 : 0),
)

// 条件为空时不必展开占地方。
watch(
  () => group.value.conditions.length,
  (n) => {
    if (n === 0) expanded.value = false
  },
)

function onEnter() {
  emit('apply')
}
</script>

<template>
  <div class="filterbar" :class="{ 'is-expanded': expanded }">
    <div class="filter-head">
      <button
        class="filter-toggle"
        :class="{ 'is-active': activeCount > 0, 'is-open': expanded }"
        :disabled="disabled"
        @click="expanded = !expanded"
      >
        <Icon name="filter" :size="13" />
        <span>{{ tr('筛选') }}</span>
        <Icon :name="expanded ? 'chevronUp' : 'chevronDown'" :size="10" class="toggle-caret" />
      </button>

      <!-- 收起时把条件摊成 chip：一眼看清在筛什么，也能单独去掉某一条 -->
      <div v-if="!expanded" class="chips">
        <button
          v-for="chip in chips"
          :key="chip.index"
          class="chip"
          :title="tr('点击编辑该条件')"
          @click="expanded = true"
        >
          <span class="chip-col mono">{{ chip.column }}</span>
          <span class="chip-op">{{ chip.op }}</span>
          <span v-if="chip.value" class="chip-val mono">{{ chip.value }}</span>
          <span class="chip-x" :title="tr('移除')" @click.stop="removeCondition(chip.index)">
            <Icon name="close" :size="9" />
          </span>
        </button>
        <span v-if="where.trim()" class="chip is-raw" @click="expanded = true">
          <span class="chip-col">WHERE</span>
          <span class="chip-val mono">{{ where.trim() }}</span>
        </span>
        <span v-if="!chips.length && !where.trim()" class="chips-empty">{{ tr('未设置条件') }}</span>
      </div>

      <div v-else class="spacer"></div>

      <button
        v-if="activeCount"
        class="btn btn-ghost btn-mini"
        :disabled="disabled"
        :title="tr('清除全部条件')"
        @click="clearAll"
      >{{ tr('清除') }}</button>
      <button class="btn btn-primary btn-mini" :disabled="disabled" @click="emit('apply')">{{ tr('应用') }}</button>
    </div>

    <Transition name="filter-open">
      <div v-if="expanded" class="filter-body">
      <div v-for="(c, i) in group.conditions" :key="i" class="cond" :class="{ 'is-off': !c.enabled }">
        <label class="cond-on" :title="c.enabled ? tr('暂时停用该条件') : tr('启用该条件')">
          <input
            type="checkbox"
            :checked="c.enabled"
            @change="patch(i, { enabled: ($event.target as HTMLInputElement).checked })"
          />
        </label>

        <span v-if="i > 0" class="cond-conj" @click="update({ conjunction: group.conjunction === 'or' ? 'and' : 'or' })">
          {{ group.conjunction === 'or' ? tr('或') : tr('且') }}
        </span>
        <span v-else class="cond-conj is-first">where</span>

        <select
          class="bare-select cond-col mono"
          :value="c.column"
          @change="onColumnChange(i, ($event.target as HTMLSelectElement).value)"
        >
          <option v-for="col in columns" :key="col.name" :value="col.name">{{ col.name }}</option>
        </select>

        <select
          class="bare-select cond-op"
          :value="c.op"
          @change="patch(i, { op: ($event.target as HTMLSelectElement).value as FilterCondition['op'] })"
        >
          <option v-for="o in opsFor(c.column)" :key="o.op" :value="o.op">{{ o.label }}</option>
        </select>

        <template v-if="arityOf(c.op) === 1">
          <input
            class="bare-input mono"
            :value="c.values?.[0] ?? ''"
            :placeholder="tr('值')"
            @input="setValue(i, 0, ($event.target as HTMLInputElement).value)"
            @keydown.enter="onEnter"
          />
        </template>
        <template v-else-if="arityOf(c.op) === 2">
          <input
            class="bare-input bare-half mono"
            :value="c.values?.[0] ?? ''"
            :placeholder="tr('起')"
            @input="setValue(i, 0, ($event.target as HTMLInputElement).value)"
            @keydown.enter="onEnter"
          />
          <span class="cond-sep">~</span>
          <input
            class="bare-input bare-half mono"
            :value="c.values?.[1] ?? ''"
            :placeholder="tr('止')"
            @input="setValue(i, 1, ($event.target as HTMLInputElement).value)"
            @keydown.enter="onEnter"
          />
        </template>
        <template v-else-if="arityOf(c.op) === -1">
          <input
            class="bare-input mono"
            :value="c.values?.[0] ?? ''"
            :placeholder="tr('多个值用逗号分隔')"
            @input="setValue(i, 0, ($event.target as HTMLInputElement).value)"
            @keydown.enter="onEnter"
          />
        </template>
        <span v-else class="cond-noinput">{{ tr('无需输入') }}</span>

        <button class="cond-del" :title="tr('删除该条件')" @click="removeCondition(i)">
          <Icon name="close" :size="11" />
        </button>
      </div>

      <div class="filter-actions">
        <button class="btn btn-ghost btn-mini" @click="addCondition">
          <Icon name="plus" :size="11" />{{ tr('添加条件') }}</button>
        <button class="btn btn-ghost btn-mini" :class="{ 'is-on': showRaw }" @click="showRaw = !showRaw">
          <Icon name="code" :size="11" />{{ tr('自定义 WHERE') }}</button>
      </div>

        <div v-if="showRaw" class="raw-row">
          <span class="cond-conj is-first">where</span>
          <input
            class="bare-input mono"
            :value="where"
            :placeholder="tr('手写条件，与上面的条件用 AND 合并')"
            spellcheck="false"
            @input="emit('update:where', ($event.target as HTMLInputElement).value)"
            @keydown.enter="onEnter"
          />
        </div>
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.filterbar {
  flex: none;
  background: var(--c-sunken);
  border-bottom: 1px solid var(--c-border-soft);
}

.filter-head {
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  min-height: var(--size-bar);
  padding: 0 var(--sp-3);
}

.filter-toggle {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-1);
  height: 22px;
  padding: 0 var(--sp-2);
  border: 1px solid transparent;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--c-text-secondary);
  font-size: var(--font-size);
}
.filter-toggle:hover {
  background: var(--c-chrome-active);
  color: var(--c-text);
}
.filter-toggle.is-active {
  color: var(--c-accent);
}
.filter-toggle.is-open {
  background: var(--c-chrome-active);
}
.toggle-caret {
  opacity: 0.6;
}

/* 条件以标签形式呈现，而不是一排等宽控件 */
.chips {
  flex: 1;
  display: flex;
  align-items: center;
  gap: var(--sp-1);
  min-width: 0;
  overflow: hidden;
}

.chip {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-1);
  height: 20px;
  max-width: 320px;
  padding: 0 var(--sp-1) 0 var(--sp-2);
  border: 1px solid var(--c-accent-border);
  border-radius: 10px;
  background: var(--c-accent-soft);
  color: var(--c-accent);
  font-size: var(--font-size-sm);
  white-space: nowrap;
  overflow: hidden;
}
.chip:hover {
  border-color: var(--c-accent);
}
.chip.is-raw {
  border-color: var(--c-border);
  background: var(--c-chrome-active);
  color: var(--c-text-secondary);
}

.chip-col {
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
}
.chip-op {
  opacity: 0.75;
}
.chip-val {
  overflow: hidden;
  text-overflow: ellipsis;
}
.chip-x {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 14px;
  height: 14px;
  border-radius: 50%;
  opacity: 0.6;
}
.chip-x:hover {
  background: var(--c-accent);
  color: #fff;
  opacity: 1;
}

.chips-empty {
  font-size: var(--font-size-sm);
  color: var(--c-text-tertiary);
}

.spacer {
  flex: 1;
}

.btn-mini {
  height: 22px;
  padding: 0 var(--sp-3);
  font-size: var(--font-size-sm);
}
.btn-mini.is-on {
  background: var(--c-accent-soft);
  color: var(--c-accent);
}

.filter-body {
  padding: var(--sp-1) var(--sp-3) var(--sp-3);
  border-top: 1px solid var(--c-border-soft);
  overflow: hidden;
}

/* 条件区展开像抽屉拉开，而不是突然出现一块 */
.filter-open-enter-active,
.filter-open-leave-active {
  transition: max-height 190ms cubic-bezier(0.25, 0.8, 0.35, 1), opacity 140ms ease;
  max-height: 320px;
}
.filter-open-enter-from,
.filter-open-leave-to {
  max-height: 0;
  opacity: 0;
}

@media (prefers-reduced-motion: reduce) {
  .filter-open-enter-active,
  .filter-open-leave-active {
    transition: none;
  }
}

/* 条件行：去掉控件的框感，只在悬停/聚焦时浮出边界，
   整体读起来像一句话而不是一张表单 */
.cond {
  display: flex;
  align-items: center;
  gap: var(--sp-1);
  height: 26px;
}
.cond.is-off {
  opacity: 0.45;
}

.cond-on {
  display: flex;
  align-items: center;
}
.cond-on input {
  margin: 0;
  accent-color: var(--c-accent);
}

.cond-conj {
  min-width: 34px;
  text-align: center;
  font-size: var(--font-size-sm);
  color: var(--c-accent);
  border-radius: var(--radius-sm);
  padding: 1px 4px;
}
.cond-conj:hover:not(.is-first) {
  background: var(--c-accent-soft);
}
.cond-conj.is-first {
  color: var(--c-text-tertiary);
  font-family: var(--font-mono);
  font-size: var(--font-size-tag);
}

.bare-select,
.bare-input {
  height: 22px;
  padding: 0 var(--sp-2);
  border: 1px solid transparent;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--c-text);
  outline: none;
  font-size: var(--font-size-sm);
}
.bare-select:hover,
.bare-input:hover {
  background: var(--c-canvas);
  border-color: var(--c-border);
}
.bare-select:focus,
.bare-input:focus {
  background: var(--c-canvas);
  border-color: var(--c-accent);
  box-shadow: 0 0 0 2px var(--c-accent-soft);
}

.bare-select {
  appearance: none;
  padding-right: 18px;
  background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='8' height='8' viewBox='0 0 10 10'%3E%3Cpath d='M2 4l3 3 3-3' fill='none' stroke='%239096a1' stroke-width='1.6' stroke-linecap='round' stroke-linejoin='round'/%3E%3C/svg%3E");
  background-repeat: no-repeat;
  background-position: right 5px center;
}

.cond-col {
  max-width: 190px;
  font-weight: 600;
}
.cond-op {
  max-width: 130px;
  color: var(--c-text-secondary);
}

.bare-input {
  flex: 1;
  min-width: 90px;
  max-width: 320px;
}
.bare-half {
  flex: none;
  width: 110px;
}

.cond-sep {
  color: var(--c-text-tertiary);
}

.cond-noinput {
  flex: 1;
  font-size: var(--font-size-sm);
  color: var(--c-text-tertiary);
}

.cond-del {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  padding: 0;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--c-text-tertiary);
  opacity: 0;
}
.cond:hover .cond-del {
  opacity: 1;
}
.cond-del:hover {
  background: var(--c-danger-soft);
  color: var(--c-danger);
}

.filter-actions {
  display: flex;
  gap: var(--sp-1);
  margin-top: var(--sp-1);
  padding-left: 22px;
}

.raw-row {
  display: flex;
  align-items: center;
  gap: var(--sp-1);
  margin-top: var(--sp-1);
  padding-left: 22px;
}
.raw-row .bare-input {
  max-width: none;
}
</style>
