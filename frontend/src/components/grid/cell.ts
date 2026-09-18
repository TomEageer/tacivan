import type { Cell, ColumnMeta } from '../../types'
import { tr } from '../../i18n'

/** 单元格是否为 NULL。 */
export function isNullCell(c: Cell | undefined): boolean {
  return !c || c.n === true
}

/** 人类可读的字节数。 */
export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let v = n / 1024
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(v >= 10 ? 0 : 1)} ${units[i]}`
}

/**
 * 单元格的展示文本。
 *
 * 二进制不展示十六进制乱码，而是像 Navicat 那样显示类型与大小；
 * 想看内容要显式打开值查看器。换行与制表符替换成可见符号，
 * 否则一行数据会把网格行高撑乱。
 */
export function formatCell(c: Cell | undefined): string {
  if (isNullCell(c)) return '(NULL)'
  const cell = c as Cell
  if (cell.x) return `(BLOB) ${formatBytes(cell.s ?? 0)}`
  const v = cell.v ?? ''
  if (v === '') return ''
  if (v.includes('\n') || v.includes('\r') || v.includes('\t')) {
    return v.replace(/\r\n|\r|\n/g, '↵').replace(/\t/g, '→')
  }
  return v
}

/** 鼠标悬停时的完整提示。 */
export function cellTitle(c: Cell | undefined): string {
  if (isNullCell(c)) return 'NULL'
  const cell = c as Cell
  if (cell.x) return tr('二进制数据，共 {size}（双击查看）', { size: formatBytes(cell.s ?? 0) })
  if (cell.t)
    return (
      (cell.v ?? '') +
      '\n\n' +
      tr('…内容已截断，完整值共 {size}（双击查看）', { size: formatBytes(cell.s ?? 0) })
    )
  return cell.v ?? ''
}

/** 编辑时用的原始文本；二进制与被截断的值不能直接编辑。 */
export function editableText(c: Cell | undefined): string {
  if (isNullCell(c)) return ''
  return (c as Cell).v ?? ''
}

/** 该单元格能否直接在网格里编辑。 */
export function canEditInline(c: Cell | undefined): boolean {
  if (!c) return true
  return !c.x && !c.t
}

/** 列的水平对齐：数字右对齐，其余左对齐，与主流客户端一致。 */
export function alignOf(col: ColumnMeta | undefined): 'left' | 'right' {
  return col?.class === 'number' ? 'right' : 'left'
}

/**
 * 估算列宽。
 *
 * 用列名与首屏若干行的内容长度取一个折中值，再夹在上下限之间——
 * 既不会因为某一行超长文本把列撑到屏幕外，也不会窄到看不见内容。
 */
export function estimateWidth(col: ColumnMeta, sample: (Cell | undefined)[]): number {
  const CHAR = 7
  const MIN = 64
  const MAX = 320
  let max = col.name.length + 3
  let count = 0
  for (const c of sample) {
    if (count++ > 60) break
    if (!c) continue
    const len = isNullCell(c) ? 6 : Math.min((c.v ?? '').length, 60)
    if (len > max) max = len
  }
  return Math.min(MAX, Math.max(MIN, max * CHAR + 16))
}
