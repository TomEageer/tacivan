<script setup lang="ts">
/**
 * 数据网格。
 *
 * 行与列都做虚拟化：DOM 里始终只有视口内的那几十行、十几列，
 * 所以打开一百万行的表和打开一百行的表，渲染开销是一样的。
 * 数据由 useGridData 按块供给，前端行缓存同样有上限。
 */
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { Cell, Column, FieldValue, OrderTerm, Row, RowChange } from '../../types'
import type { GridData } from '../../composables/useGridData'
import {
  alignOf,
  canEditInline,
  cellTitle,
  editableText,
  estimateWidth,
  formatCell,
  isNullCell,
} from './cell'
import ContextMenu, { type MenuItem } from '../common/ContextMenu.vue'
import Icon from '../common/Icon.vue'
import { hideTip, hideTipNow, showTip, type TipContent } from '../../composables/useTooltip'
import { tr } from '../../i18n'

const props = withDefaults(
  defineProps<{
    data: GridData
    editable?: boolean
    keyColumns?: string[]
    tableColumns?: Column[]
    orderBy?: OrderTerm[]
    zebra?: boolean
  }>(),
  { editable: false, keyColumns: () => [], tableColumns: () => [], orderBy: () => [], zebra: true },
)

const emit = defineEmits<{
  (e: 'sort', column: string, desc: boolean): void
  (e: 'changes', count: number): void
  (e: 'view-cell', rowIndex: number, column: string): void
  (e: 'row-activate', rowIndex: number): void
  (e: 'copy-sql', kind: string, rowIndexes: number[]): void
  (e: 'select', rowIndex: number, colIndex: number): void
  (e: 'filter-by', column: string, value: string | null): void
}>()

const ROW_H = 24
const HEADER_H = 30
const ROWNUM_W = 46
/** 视口上下各多渲染几行，滚动时不会看到空白。 */
const OVERSCAN = 6

const scroller = ref<HTMLElement | null>(null)
const scrollTop = ref(0)
const scrollLeft = ref(0)
const viewportH = ref(400)
const viewportW = ref(800)
const focused = ref(false)

/** 已提交到服务端之前的本地编辑：行号 -> 列名 -> 新值（null 表示 NULL）。 */
const edits = ref(new Map<number, Map<string, string | null>>())
/** 标记为删除的已有行。 */
const deleted = ref(new Set<number>())
/** 新增的行，存在末尾。 */
const inserted = ref<Map<string, string | null>[]>([])

const selection = ref({ row: 0, col: 0 })
/** 多选的行范围，Shift 点击行号时产生。 */
const rowRange = ref<{ from: number; to: number } | null>(null)
const editing = ref<{ row: number; col: number } | null>(null)
const editValue = ref('')
const editInput = ref<HTMLInputElement | null>(null)

const columns = computed(() => props.data.columns.value)
const widths = ref<number[]>([])

/** 总显示行数 = 服务端行数 + 本地新增行。 */
const totalRows = computed(() => props.data.scrollRows.value + inserted.value.length)

const totalWidth = computed(
  () => ROWNUM_W + widths.value.reduce((a, b) => a + b, 0),
)

// --- 列宽初始化 ---
watch(
  () => [columns.value, props.data.version.value] as const,
  () => {
    if (columns.value.length === 0) {
      widths.value = []
      return
    }
    if (widths.value.length === columns.value.length) return
    const sample: Row[] = []
    for (let i = 0; i < 40; i++) {
      const r = props.data.rowAt(i)
      if (r) sample.push(r)
    }
    widths.value = columns.value.map((c, i) =>
      estimateWidth(c, sample.map((r) => r[i])),
    )
  },
  { immediate: true },
)

// --- 行虚拟化 ---
const visibleRange = computed(() => {
  const start = Math.max(0, Math.floor(scrollTop.value / ROW_H) - OVERSCAN)
  const count = Math.ceil(viewportH.value / ROW_H) + OVERSCAN * 2
  return { start, end: Math.min(totalRows.value, start + count) }
})

const visibleRows = computed(() => {
  void props.data.version.value
  const { start, end } = visibleRange.value
  const out: { index: number; row: Row | undefined; isNew: boolean }[] = []
  const serverRows = props.data.scrollRows.value
  for (let i = start; i < end; i++) {
    if (i >= serverRows) {
      out.push({ index: i, row: undefined, isNew: true })
    } else {
      out.push({ index: i, row: props.data.rowAt(i), isNew: false })
    }
  }
  return out
})

// --- 列虚拟化 ---
const columnOffsets = computed(() => {
  const offs: number[] = []
  let acc = 0
  for (const w of widths.value) {
    offs.push(acc)
    acc += w
  }
  return offs
})

const visibleCols = computed(() => {
  const offs = columnOffsets.value
  if (offs.length === 0) return { start: 0, end: 0, leftPad: 0, rightPad: 0 }
  const viewLeft = Math.max(0, scrollLeft.value - ROWNUM_W)
  const viewRight = viewLeft + viewportW.value
  let start = 0
  while (start < offs.length - 1 && offs[start + 1] <= viewLeft) start++
  let end = start
  while (end < offs.length && offs[end] < viewRight) end++
  // 两侧各多渲染一列，避免横向滚动时边缘闪空。
  start = Math.max(0, start - 1)
  end = Math.min(offs.length, end + 1)
  const leftPad = offs[start]
  const rightPad = offs[offs.length - 1] + widths.value[offs.length - 1] - (offs[end - 1] + widths.value[end - 1])
  return { start, end, leftPad, rightPad: Math.max(0, rightPad) }
})

const renderedCols = computed(() => {
  const { start, end } = visibleCols.value
  const out: { index: number; meta: (typeof columns.value)[number]; width: number }[] = []
  for (let i = start; i < end; i++) {
    out.push({ index: i, meta: columns.value[i], width: widths.value[i] })
  }
  return out
})

// --- 滚动 ---
let prefetchTimer: number | undefined
function onScroll() {
  const el = scroller.value
  if (!el) return
  hideTipNow()
  scrollTop.value = el.scrollTop
  scrollLeft.value = el.scrollLeft
  // 节流预取：滚动过程中不必每帧都发请求。
  if (prefetchTimer) window.clearTimeout(prefetchTimer)
  prefetchTimer = window.setTimeout(() => {
    const { start, end } = visibleRange.value
    props.data.ensureRange(start, end)
  }, 60)
}

let ro: ResizeObserver | undefined
onMounted(() => {
  const el = scroller.value
  if (!el) return
  viewportH.value = el.clientHeight
  viewportW.value = el.clientWidth
  ro = new ResizeObserver(() => {
    viewportH.value = el.clientHeight
    viewportW.value = el.clientWidth
  })
  ro.observe(el)
})
onBeforeUnmount(() => {
  ro?.disconnect()
  if (prefetchTimer) window.clearTimeout(prefetchTimer)
  window.removeEventListener('mousemove', onResizeMove)
  window.removeEventListener('mouseup', onResizeEnd)
})

// --- 取值 ---
function cellOf(rowIndex: number, colIndex: number, row: Row | undefined): Cell | undefined {
  const name = columns.value[colIndex]?.name
  if (!name) return undefined
  const serverRows = props.data.scrollRows.value
  if (rowIndex >= serverRows) {
    const rec = inserted.value[rowIndex - serverRows]
    const v = rec?.get(name)
    if (v === undefined) return { n: true }
    return v === null ? { n: true } : { v }
  }
  const edit = edits.value.get(rowIndex)
  if (edit && edit.has(name)) {
    const v = edit.get(name)!
    return v === null ? { n: true } : { v }
  }
  return row?.[colIndex]
}

function isEdited(rowIndex: number, colIndex: number) {
  const name = columns.value[colIndex]?.name
  return !!name && edits.value.get(rowIndex)?.has(name)
}

function rowClass(index: number, isNew: boolean) {
  return {
    'is-new': isNew,
    'is-deleted': deleted.value.has(index),
    'is-modified': !isNew && edits.value.has(index),
    'is-selected': isRowSelected(index),
    'is-zebra': props.zebra && index % 2 === 1,
  }
}

function isRowSelected(index: number) {
  const r = rowRange.value
  if (r) return index >= Math.min(r.from, r.to) && index <= Math.max(r.from, r.to)
  return selection.value.row === index
}

// --- 选择与键盘 ---
function selectCell(row: number, col: number, extend = false) {
  selection.value = { row, col }
  if (!extend) rowRange.value = null
  emit('select', row, col)
  focus()
}

function focus() {
  focused.value = true
  scroller.value?.focus()
}

function selectRowNumber(index: number, e: MouseEvent) {
  if (e.shiftKey && rowRange.value) {
    rowRange.value = { from: rowRange.value.from, to: index }
  } else {
    rowRange.value = { from: index, to: index }
  }
  selection.value = { row: index, col: selection.value.col }
  emit('select', index, selection.value.col)
  focus()
}

function scrollCellIntoView(row: number, col: number) {
  const el = scroller.value
  if (!el) return
  const top = row * ROW_H
  if (top < el.scrollTop) el.scrollTop = top
  else if (top + ROW_H > el.scrollTop + el.clientHeight - HEADER_H) {
    el.scrollTop = top + ROW_H - el.clientHeight + HEADER_H
  }
  const left = columnOffsets.value[col] ?? 0
  const w = widths.value[col] ?? 0
  if (left < el.scrollLeft) el.scrollLeft = left
  else if (left + w > el.scrollLeft + el.clientWidth - ROWNUM_W) {
    el.scrollLeft = left + w - el.clientWidth + ROWNUM_W
  }
}

function move(dRow: number, dCol: number) {
  let { row, col } = selection.value
  row = Math.max(0, Math.min(totalRows.value - 1, row + dRow))
  col = Math.max(0, Math.min(columns.value.length - 1, col + dCol))
  selection.value = { row, col }
  rowRange.value = null
  emit('select', row, col)
  scrollCellIntoView(row, col)
  props.data.ensureRange(row - 20, row + 20)
}

function onKeydown(e: KeyboardEvent) {
  if (editing.value) return
  const meta = e.metaKey || e.ctrlKey
  switch (e.key) {
    case 'ArrowDown':
      e.preventDefault()
      move(1, 0)
      break
    case 'ArrowUp':
      e.preventDefault()
      move(-1, 0)
      break
    case 'ArrowLeft':
      e.preventDefault()
      move(0, -1)
      break
    case 'ArrowRight':
    case 'Tab':
      e.preventDefault()
      move(0, e.shiftKey ? -1 : 1)
      break
    case 'PageDown':
      e.preventDefault()
      move(Math.floor(viewportH.value / ROW_H), 0)
      break
    case 'PageUp':
      e.preventDefault()
      move(-Math.floor(viewportH.value / ROW_H), 0)
      break
    case 'Home':
      e.preventDefault()
      if (meta) move(-totalRows.value, 0)
      else selectCell(selection.value.row, 0)
      break
    case 'End':
      e.preventDefault()
      if (meta) move(totalRows.value, 0)
      else selectCell(selection.value.row, columns.value.length - 1)
      break
    case 'Enter':
      e.preventDefault()
      if (props.editable) beginEdit()
      else emit('row-activate', selection.value.row)
      break
    case 'F2':
      e.preventDefault()
      if (props.editable) beginEdit()
      break
    case 'Delete':
    case 'Backspace':
      if (props.editable) {
        e.preventDefault()
        setCellNull()
      }
      break
    case 'c':
      if (meta) {
        e.preventDefault()
        copySelection()
      }
      break
    default:
      // 直接敲字符即进入编辑，和电子表格的习惯一致。
      if (props.editable && e.key.length === 1 && !meta && !e.altKey) {
        beginEdit(e.key)
      }
  }
}

// --- 编辑 ---
function beginEdit(initial?: string) {
  if (!props.editable) return
  const { row, col } = selection.value
  const name = columns.value[col]?.name
  if (!name) return
  if (deleted.value.has(row)) return

  const serverRows = props.data.scrollRows.value
  const raw = cellOf(row, col, row < serverRows ? props.data.rowAt(row) : undefined)
  if (!canEditInline(raw)) {
    // 截断值与二进制不能在格子里改——用户看到的只是预览，直接保存会把数据截断。
    emit('view-cell', row, name)
    return
  }
  editing.value = { row, col }
  editValue.value = initial ?? editableText(raw)
  nextTick(() => {
    editInput.value?.focus()
    if (initial === undefined) editInput.value?.select()
  })
}

function commitEdit(moveAfter: 'down' | 'right' | 'none' = 'none') {
  const cur = editing.value
  if (!cur) return
  const name = columns.value[cur.col]?.name
  if (name) applyEdit(cur.row, name, editValue.value)
  editing.value = null
  focus()
  if (moveAfter === 'down') move(1, 0)
  else if (moveAfter === 'right') move(0, 1)
}

function cancelEdit() {
  editing.value = null
  focus()
}

function applyEdit(rowIndex: number, column: string, value: string | null) {
  const serverRows = props.data.scrollRows.value
  if (rowIndex >= serverRows) {
    const rec = inserted.value[rowIndex - serverRows]
    if (rec) rec.set(column, value)
  } else {
    let m = edits.value.get(rowIndex)
    if (!m) {
      m = new Map()
      edits.value.set(rowIndex, m)
    }
    m.set(column, value)
  }
  // Map 的原地修改不触发依赖，手动换一个引用。
  edits.value = new Map(edits.value)
  inserted.value = [...inserted.value]
  emitChanges()
}

function setCellNull() {
  const { row, col } = selection.value
  const name = columns.value[col]?.name
  if (name) applyEdit(row, name, null)
}

function onEditKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter') {
    e.preventDefault()
    commitEdit('down')
  } else if (e.key === 'Escape') {
    e.preventDefault()
    cancelEdit()
  } else if (e.key === 'Tab') {
    e.preventDefault()
    commitEdit('right')
  }
}

// --- 行操作 ---
function addRow() {
  const rec = new Map<string, string | null>()
  for (const c of columns.value) rec.set(c.name, null)
  inserted.value = [...inserted.value, rec]
  const idx = props.data.scrollRows.value + inserted.value.length - 1
  nextTick(() => {
    selectCell(idx, 0)
    scrollCellIntoView(idx, 0)
    emitChanges()
  })
}

function deleteSelectedRows() {
  const serverRows = props.data.scrollRows.value
  const r = rowRange.value
  const from = r ? Math.min(r.from, r.to) : selection.value.row
  const to = r ? Math.max(r.from, r.to) : selection.value.row
  for (let i = from; i <= to; i++) {
    if (i >= serverRows) {
      // 还没提交的新增行直接抹掉，不必产生 DELETE 语句。
      const k = i - serverRows
      inserted.value = inserted.value.filter((_, idx) => idx !== k)
    } else {
      if (deleted.value.has(i)) deleted.value.delete(i)
      else deleted.value.add(i)
    }
  }
  deleted.value = new Set(deleted.value)
  emitChanges()
}

function revertChanges() {
  edits.value = new Map()
  deleted.value = new Set()
  inserted.value = []
  emitChanges()
}

const changeCount = computed(
  () => edits.value.size + deleted.value.size + inserted.value.length,
)

function emitChanges() {
  emit('changes', changeCount.value)
}

/** 把本地变更翻译成后端要的 RowChange 列表。 */
function collectChanges(): RowChange[] {
  const out: RowChange[] = []
  const keys = props.keyColumns ?? []
  const colIndex = new Map(columns.value.map((c, i) => [c.name, i]))

  const fv = (v: string | null): FieldValue => (v === null ? { null: true } : { value: v })

  const keysOf = (rowIndex: number): Record<string, FieldValue> | null => {
    const row = props.data.rowAt(rowIndex)
    if (!row) return null
    const k: Record<string, FieldValue> = {}
    for (const name of keys) {
      const i = colIndex.get(name)
      if (i === undefined) return null
      const c = row[i]
      k[name] = isNullCell(c) ? { null: true } : { value: c.v ?? '' }
    }
    return k
  }

  for (const [rowIndex, m] of edits.value) {
    if (deleted.value.has(rowIndex)) continue
    const k = keysOf(rowIndex)
    if (!k) continue
    const values: Record<string, FieldValue> = {}
    for (const [name, v] of m) values[name] = fv(v)
    out.push({ op: 'update', values, keys: k })
  }

  for (const rowIndex of deleted.value) {
    const k = keysOf(rowIndex)
    if (!k) continue
    out.push({ op: 'delete', values: {}, keys: k })
  }

  for (const rec of inserted.value) {
    const values: Record<string, FieldValue> = {}
    let any = false
    for (const [name, v] of rec) {
      // 全是 NULL 的列不写进 INSERT，让数据库套用默认值/自增。
      if (v !== null) {
        values[name] = fv(v)
        any = true
      }
    }
    if (!any) continue
    out.push({ op: 'insert', values, keys: {} })
  }
  return out
}

// --- 复制 ---
function copySelection() {
  const r = rowRange.value
  let text = ''
  if (r) {
    const from = Math.min(r.from, r.to)
    const to = Math.max(r.from, r.to)
    const lines: string[] = []
    for (let i = from; i <= to; i++) {
      const row = props.data.rowAt(i)
      lines.push(
        columns.value
          .map((_, ci) => {
            const c = cellOf(i, ci, row)
            return isNullCell(c) ? '' : (c!.v ?? '')
          })
          .join('\t'),
      )
    }
    text = lines.join('\n')
  } else {
    const { row, col } = selection.value
    const c = cellOf(row, col, props.data.rowAt(row))
    text = isNullCell(c) ? '' : (c!.v ?? '')
  }
  navigator.clipboard?.writeText(text)
}

// --- 列宽拖拽 ---
let resizing: { index: number; startX: number; startW: number } | null = null

function onResizeStart(index: number, e: MouseEvent) {
  e.preventDefault()
  e.stopPropagation()
  resizing = { index, startX: e.clientX, startW: widths.value[index] }
  window.addEventListener('mousemove', onResizeMove)
  window.addEventListener('mouseup', onResizeEnd)
}

function onResizeMove(e: MouseEvent) {
  if (!resizing) return
  const next = [...widths.value]
  next[resizing.index] = Math.max(40, resizing.startW + e.clientX - resizing.startX)
  widths.value = next
}

function onResizeEnd() {
  resizing = null
  window.removeEventListener('mousemove', onResizeMove)
  window.removeEventListener('mouseup', onResizeEnd)
}

/** 双击列头分隔线：按当前可见行内容自动调整列宽。 */
function autoFit(index: number) {
  const { start, end } = visibleRange.value
  const sample: (Cell | undefined)[] = []
  for (let i = start; i < end; i++) {
    const row = props.data.rowAt(i)
    if (row) sample.push(row[index])
  }
  const next = [...widths.value]
  next[index] = estimateWidth(columns.value[index], sample)
  widths.value = next
}

// --- 列头信息 ---

/** 表定义里的列，SQL 查询结果可能没有。 */
function columnDef(name: string): Column | undefined {
  return props.tableColumns?.find((c) => c.name === name)
}

/** 列头上常驻的类型标签：优先用表定义里的完整类型（含长度）。 */
function typeLabel(i: number): string {
  const meta = columns.value[i]
  if (!meta) return ''
  const def = columnDef(meta.name)
  const full = def?.fullType?.trim()
  if (full) return full
  return (meta.type || '').toLowerCase()
}

/** 该列是否参与行定位（主键或唯一键）。 */
function isKeyColumn(name: string) {
  return props.keyColumns?.includes(name)
}

/** 悬停列头时给出字段的完整信息，省得为了看一眼注释去开设计表。 */
function columnTip(i: number): TipContent {
  const meta = columns.value[i]
  const def = columnDef(meta.name)
  const tags: TipContent['tags'] = []
  if (def?.primaryKey || isKeyColumn(meta.name)) tags.push({ text: tr('主键'), tone: 'meta' })
  if (def?.autoIncrement) tags.push({ text: tr('自增'), tone: 'meta' })
  if (def ? !def.nullable : meta.nullable === 0) tags.push({ text: 'NOT NULL' })

  const rows: TipContent['rows'] = [
    { label: tr('类型'), value: typeLabel(i) || tr('未知'), mono: true },
  ]
  if (def?.length) {
    rows.push({
      label: def.scale ? tr('精度') : tr('长度'),
      value: def.scale ? `${def.length}, ${def.scale}` : String(def.length),
      mono: true,
    })
  }
  if (def?.hasDefault) {
    rows.push({ label: tr('默认值'), value: def.default || "''", mono: true })
  }
  if (def?.collation) rows.push({ label: tr('排序规则'), value: def.collation, mono: true })
  if (def?.enumValues?.length) {
    rows.push({ label: tr('可选值'), value: def.enumValues.join(' / '), mono: true })
  }
  if (def?.generated) rows.push({ label: tr('生成列'), value: def.generated, mono: true })

  return {
    title: meta.name,
    tags,
    rows,
    note: def?.comment || undefined,
  }
}

function onHeaderEnter(e: MouseEvent, i: number) {
  showTip(e.currentTarget as HTMLElement, columnTip(i))
}

// --- 排序 ---
function sortDir(name: string): 'asc' | 'desc' | null {
  const t = props.orderBy?.find((o) => o.column === name)
  if (!t) return null
  return t.desc ? 'desc' : 'asc'
}

function onHeaderClick(name: string) {
  const cur = sortDir(name)
  emit('sort', name, cur === 'asc')
}

function onCellDblClick(rowIndex: number, colIndex: number) {
  const name = columns.value[colIndex]?.name
  if (!name) return
  const raw = cellOf(rowIndex, colIndex, props.data.rowAt(rowIndex))
  if (!canEditInline(raw)) {
    emit('view-cell', rowIndex, name)
    return
  }
  if (props.editable) beginEdit()
  else emit('view-cell', rowIndex, name)
}

/** 某列在表定义里的枚举候选值，有则在编辑时给下拉。 */
function enumValuesOf(colIndex: number): string[] | undefined {
  return columnDef(columns.value[colIndex]?.name ?? '')?.enumValues?.length
    ? columnDef(columns.value[colIndex]!.name)!.enumValues
    : undefined
}

// --- 右键菜单 ---
const menu = ref<{ x: number; y: number; row: number; col: number } | null>(null)

function onContextMenu(rowIndex: number, colIndex: number, e: MouseEvent) {
  e.preventDefault()
  // 右键点在选区之外时，先把选中移过去，符合「操作的是我点的那行」的直觉。
  if (!isRowSelected(rowIndex)) {
    selection.value = { row: rowIndex, col: colIndex }
    rowRange.value = null
  } else {
    selection.value = { row: rowIndex, col: colIndex }
  }
  menu.value = { x: e.clientX, y: e.clientY, row: rowIndex, col: colIndex }
}

/** 当前操作涉及的行号：有多选就用多选，否则用光标所在行。 */
function targetRows(): number[] {
  const r = rowRange.value
  if (!r) return [selection.value.row]
  const from = Math.min(r.from, r.to)
  const to = Math.max(r.from, r.to)
  const out: number[] = []
  for (let i = from; i <= to; i++) out.push(i)
  return out
}

const menuItems = computed<MenuItem[]>(() => {
  const n = targetRows().length
  const suffix = n > 1 ? tr(' ({n} 行)', { n }) : ''
  const colName = columns.value[menu.value?.col ?? 0]?.name ?? ''
  const cell = menu.value
    ? cellOf(menu.value.row, menu.value.col, props.data.rowAt(menu.value.row))
    : undefined
  const preview = cell ? formatCell(cell) : ''
  const shortPreview = preview.length > 18 ? preview.slice(0, 18) + '…' : preview

  return [
    { key: 'copy-cell', label: tr('复制单元格'), icon: 'copy', shortcut: '⌘C' },
    { key: 'copy-row', label: tr('复制行') + suffix, icon: 'copy' },
    { key: 'copy-with-header', label: tr('复制行（含列名）') + suffix },
    { key: 'sep1', separator: true },
    {
      key: 'copy-sql',
      label: tr('复制为 SQL') + suffix,
      icon: 'sql',
      children: [
        { key: 'sql-insert', label: tr('INSERT 语句') },
        { key: 'sql-update', label: tr('UPDATE 语句（主键作条件）') },
        { key: 'sql-delete', label: tr('DELETE 语句（主键作条件）') },
        { key: 'sql-select', label: tr('SELECT 语句（主键作条件）') },
      ],
    },
    { key: 'sep2', separator: true },
    {
      key: 'filter-eq',
      label: colName
        ? tr('筛选：{col} = {value}', { col: colName, value: shortPreview })
        : tr('按此值筛选'),
      icon: 'filter',
      disabled: !colName,
    },
    { key: 'sep3', separator: true },
    { key: 'view-cell', label: tr('查看完整值…'), icon: 'view' },
    {
      key: 'set-null',
      label: tr('置为 NULL'),
      icon: 'minus',
      disabled: !props.editable,
    },
    {
      key: 'delete-rows',
      label: tr('标记删除') + suffix,
      icon: 'trash',
      danger: true,
      disabled: !props.editable,
    },
  ]
})

function onMenuSelect(key: string) {
  const m = menu.value
  menu.value = null
  if (!m) return
  const colName = columns.value[m.col]?.name ?? ''

  switch (key) {
    case 'copy-cell':
      copySelection()
      return
    case 'copy-row':
      copyRows(false)
      return
    case 'copy-with-header':
      copyRows(true)
      return
    case 'sql-insert':
    case 'sql-update':
    case 'sql-delete':
    case 'sql-select':
      emit('copy-sql', key.replace('sql-', ''), targetRows())
      return
    case 'filter-eq': {
      const cell = cellOf(m.row, m.col, props.data.rowAt(m.row))
      emit('filter-by', colName, isNullCell(cell) ? null : (cell?.v ?? ''))
      return
    }
    case 'view-cell':
      if (colName) emit('view-cell', m.row, colName)
      return
    case 'set-null':
      setCellNull()
      return
    case 'delete-rows':
      deleteSelectedRows()
      return
  }
}

/** 复制整行，制表符分隔，可带列名表头——直接粘进表格软件。 */
function copyRows(withHeader: boolean) {
  const rows = targetRows()
  const lines: string[] = []
  if (withHeader) lines.push(columns.value.map((c) => c.name).join('\t'))
  for (const i of rows) {
    const row = props.data.rowAt(i)
    lines.push(
      columns.value
        .map((_, ci) => {
          const c = cellOf(i, ci, row)
          return isNullCell(c) ? '' : (c!.v ?? '')
        })
        .join('\t'),
    )
  }
  navigator.clipboard?.writeText(lines.join('\n'))
}

/** 按列名写入一个值，供表单视图复用网格的改动收集机制。 */
function applyEditByName(rowIndex: number, column: string, value: string | null) {
  applyEdit(rowIndex, column, value)
}

/** 某一行尚未提交的改动，键为列名。 */
function pendingEditsFor(rowIndex: number): Record<string, string | null> {
  const out: Record<string, string | null> = {}
  const serverRows = props.data.scrollRows.value
  if (rowIndex >= serverRows) {
    const rec = inserted.value[rowIndex - serverRows]
    if (rec) for (const [k, v] of rec) out[k] = v
    return out
  }
  const m = edits.value.get(rowIndex)
  if (m) for (const [k, v] of m) out[k] = v
  return out
}

/** 跳到指定行并保持可见，表单视图的上一行/下一行用它。 */
function gotoRow(rowIndex: number) {
  const row = Math.max(0, Math.min(totalRows.value - 1, rowIndex))
  selection.value = { row, col: selection.value.col }
  rowRange.value = null
  emit('select', row, selection.value.col)
  scrollCellIntoView(row, selection.value.col)
  props.data.ensureRange(row - 20, row + 20)
}

defineExpose({
  collectChanges,
  applyEditByName,
  pendingEditsFor,
  gotoRow,
  revertChanges,
  addRow,
  deleteSelectedRows,
  changeCount,
  selection,
  focus,
  copySelection,
})
</script>

<template>
  <div
    ref="scroller"
    class="grid"
    tabindex="0"
    @scroll="onScroll"
    @keydown="onKeydown"
    @focus="focused = true"
    @blur="focused = false"
  >
    <div
      class="grid-inner"
      :style="{ width: totalWidth + 'px', height: HEADER_H + totalRows * ROW_H + 'px' }"
    >
      <!-- 列头 -->
      <div class="grid-header" :style="{ height: HEADER_H + 'px' }">
        <div class="grid-corner" :style="{ width: ROWNUM_W + 'px' }"></div>
        <div :style="{ width: visibleCols.leftPad + 'px' }"></div>
        <div
          v-for="c in renderedCols"
          :key="c.index"
          class="grid-th"
          :class="{ 'is-key': isKeyColumn(c.meta.name), 'is-sorted': !!sortDir(c.meta.name) }"
          :style="{ width: c.width + 'px' }"
          @click="onHeaderClick(c.meta.name)"
          @mouseenter="onHeaderEnter($event, c.index)"
          @mouseleave="hideTip()"
        >
          <span class="th-main">
            <Icon v-if="isKeyColumn(c.meta.name)" name="key" :size="10" class="th-key" />
            <span class="th-name">{{ c.meta.name }}</span>
            <Icon
              v-if="sortDir(c.meta.name)"
              :name="sortDir(c.meta.name) === 'asc' ? 'chevronUp' : 'chevronDown'"
              :size="10"
              class="th-sort"
            />
          </span>
          <span class="th-type">{{ typeLabel(c.index) }}</span>
          <span
            class="grid-resizer"
            @mousedown="onResizeStart(c.index, $event)"
            @dblclick.stop="autoFit(c.index)"
          ></span>
        </div>
        <div :style="{ width: visibleCols.rightPad + 'px' }"></div>
      </div>

      <!-- 行 -->
      <div class="grid-rows" :style="{ top: HEADER_H + 'px' }">
        <div
          v-for="r in visibleRows"
          :key="r.index"
          class="grid-row"
          :class="rowClass(r.index, r.isNew)"
          :style="{ transform: `translateY(${r.index * ROW_H}px)`, height: ROW_H + 'px' }"
        >
          <div
            class="grid-rownum"
            :style="{ width: ROWNUM_W + 'px' }"
            @mousedown="selectRowNumber(r.index, $event)"
            @contextmenu="onContextMenu(r.index, selection.col, $event)"
          >
            <span v-if="r.isNew" class="rownum-mark" :title="tr('新增行')">+</span>
            <span v-else-if="deleted.has(r.index)" class="rownum-mark del" :title="tr('待删除')">−</span>
            <span v-else-if="edits.has(r.index)" class="rownum-mark mod" :title="tr('已修改')">•</span>
            <span class="rownum-text">{{ r.index + 1 }}</span>
          </div>
          <div :style="{ width: visibleCols.leftPad + 'px' }"></div>
          <div
            v-for="c in renderedCols"
            :key="c.index"
            class="grid-td"
            :class="{
              'is-null': isNullCell(cellOf(r.index, c.index, r.row)),
              'is-edited': isEdited(r.index, c.index),
              'is-active':
                selection.row === r.index && selection.col === c.index && !editing,
              'align-right': alignOf(c.meta) === 'right',
              'is-loading': !r.row && !r.isNew,
            }"
            :style="{ width: c.width + 'px' }"
            :title="r.row || r.isNew ? cellTitle(cellOf(r.index, c.index, r.row)) : ''"
            @mousedown="selectCell(r.index, c.index)"
            @dblclick="onCellDblClick(r.index, c.index)"
            @contextmenu="onContextMenu(r.index, c.index, $event)"
          >
            <template v-if="editing && editing.row === r.index && editing.col === c.index">
              <select
                v-if="enumValuesOf(c.index)"
                ref="editInput"
                v-model="editValue"
                class="grid-editor"
                @change="commitEdit('none')"
                @keydown="onEditKeydown"
                @blur="commitEdit('none')"
              >
                <option v-for="v in enumValuesOf(c.index)" :key="v" :value="v">{{ v }}</option>
              </select>
              <input
                v-else
                ref="editInput"
                v-model="editValue"
                class="grid-editor"
                spellcheck="false"
                @keydown="onEditKeydown"
                @blur="commitEdit('none')"
              />
            </template>
            <template v-else-if="r.row || r.isNew">
              {{ formatCell(cellOf(r.index, c.index, r.row)) }}
            </template>
            <span v-else class="cell-skeleton"></span>
          </div>
          <div :style="{ width: visibleCols.rightPad + 'px' }"></div>
        </div>
      </div>
    </div>

    <ContextMenu
      v-if="menu"
      :x="menu.x"
      :y="menu.y"
      :items="menuItems"
      @select="onMenuSelect"
      @close="menu = null"
    />
  </div>
</template>

<style scoped>
.grid {
  position: relative;
  flex: 1;
  overflow: auto;
  outline: none;
  background: var(--c-bg);
  -webkit-user-select: none;
  user-select: none;
}

/* 就地编辑的输入框要能正常选中与输入。 */
.grid :deep(input),
.grid :deep(select),
.grid-editor {
  -webkit-user-select: text;
  user-select: text;
}

.grid-inner {
  position: relative;
  min-width: 100%;
}

.grid-header {
  position: sticky;
  top: 0;
  z-index: 3;
  display: flex;
  background: var(--c-sunken);
  border-bottom: 1px solid var(--c-border);
}

.grid-corner {
  position: sticky;
  left: 0;
  z-index: 4;
  flex: none;
  background: var(--c-sunken);
  border-right: 1px solid var(--c-border);
}

.grid-th {
  position: relative;
  flex: none;
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 1px;
  padding: 0 var(--sp-3);
  border-right: 1px solid var(--c-border-soft);
  white-space: nowrap;
  overflow: hidden;
  transition: background 0.1s ease;
}
.grid-th:hover {
  background: var(--c-chrome-active);
}
.grid-th.is-sorted {
  background: var(--c-accent-soft);
}

.th-main {
  display: flex;
  align-items: center;
  gap: var(--sp-1);
  overflow: hidden;
  font-size: var(--font-size-grid);
  font-weight: 600;
  line-height: 1.15;
  color: var(--c-text);
}

.th-name {
  overflow: hidden;
  text-overflow: ellipsis;
}

.th-key {
  flex: none;
  color: var(--c-meta);
}

.th-sort {
  flex: none;
  color: var(--c-accent);
}

/* 类型常驻显示：查数时最想知道的就是这个，
   不该逼着人去开设计表。用青色与操作蓝区分开。 */
.th-type {
  font-size: var(--font-size-tag);
  font-family: var(--font-mono);
  line-height: 1.1;
  color: var(--c-meta);
  opacity: 0.85;
  overflow: hidden;
  text-overflow: ellipsis;
  letter-spacing: 0;
}

.grid-resizer {
  position: absolute;
  top: 0;
  right: -3px;
  width: 7px;
  height: 100%;
  cursor: col-resize;
  z-index: 1;
}
.grid-resizer:hover {
  background: var(--c-accent);
  opacity: 0.35;
}

.grid-rows {
  position: absolute;
  left: 0;
  right: 0;
}

.grid-row {
  position: absolute;
  left: 0;
  display: flex;
  width: 100%;
  border-bottom: 1px solid var(--c-border-soft);
}
.grid-row.is-zebra {
  background: var(--c-row-zebra);
}
.grid-row:hover {
  background: var(--c-row-hover);
}
.grid-row.is-selected {
  background: var(--c-row-selected);
  box-shadow: inset 2px 0 0 var(--c-accent);
}
.grid-row.is-new {
  background: var(--c-row-inserted);
}
.grid-row.is-modified {
  background: var(--c-row-modified);
}
.grid-row.is-deleted {
  background: var(--c-row-deleted);
  text-decoration: line-through;
  opacity: 0.7;
}

.grid-rownum {
  position: sticky;
  left: 0;
  z-index: 2;
  flex: none;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: var(--sp-1);
  padding: 0 var(--sp-2);
  font-size: var(--font-size-sm);
  color: var(--c-text-tertiary);
  background: var(--c-sunken);
  border-right: 1px solid var(--c-border);
  cursor: default;
}
.grid-row.is-selected .grid-rownum {
  background: var(--c-accent-soft);
  color: var(--c-accent);
}
.rownum-mark {
  font-weight: 700;
  color: var(--c-success);
}
.rownum-mark.del {
  color: var(--c-danger);
}
.rownum-mark.mod {
  color: var(--c-warning);
}
.rownum-text {
  font-variant-numeric: tabular-nums;
}

.grid-td {
  position: relative;
  flex: none;
  display: flex;
  align-items: center;
  padding: 0 var(--sp-3);
  font-size: var(--font-size-grid);
  border-right: 1px solid var(--c-border-soft);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  font-variant-numeric: tabular-nums;
}
.grid-td.align-right {
  justify-content: flex-end;
}
.grid-td.is-null {
  color: var(--c-cell-null);
  font-size: var(--font-size-sm);
}
.grid-td.is-edited {
  background: var(--c-warning-soft);
  font-weight: 600;
}
.grid-td.is-active {
  outline: 2px solid var(--c-accent);
  outline-offset: -2px;
  z-index: 1;
}

.cell-skeleton {
  display: block;
  width: 60%;
  height: 8px;
  border-radius: 2px;
  background: var(--c-border-soft);
  animation: pulse 1.1s ease-in-out infinite;
}
@keyframes pulse {
  0%,
  100% {
    opacity: 0.45;
  }
  50% {
    opacity: 0.9;
  }
}

.grid-editor {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  padding: 0 6px;
  border: 2px solid var(--c-accent);
  background: var(--c-bg);
  color: var(--c-text);
  outline: none;
  font: inherit;
  z-index: 5;
}
</style>
