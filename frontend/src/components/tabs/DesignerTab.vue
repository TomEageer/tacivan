<script setup lang="ts">
/**
 * 表设计器。
 *
 * 改名靠 Column.origName 追踪：设计器里把 a 改成 b 时原名会带着走，
 * 后端因此能生成 CHANGE/RENAME 而不是「删 a 建 b」——后者会丢掉整列数据。
 */
import { computed, onMounted, ref } from 'vue'
import * as api from '../../api'
import { useAppStore } from '../../stores/app'
import { useConnectionsStore } from '../../stores/connections'
import { useTabsStore, type Tab } from '../../stores/tabs'
import type { Column, ForeignKey, TableDefinition, TableIndex } from '../../types'
import Icon from '../common/Icon.vue'
import SqlPreviewDialog from '../dialogs/SqlPreviewDialog.vue'
import { tr } from '../../i18n'

const props = defineProps<{ tab: Tab; isNew?: boolean }>()

const app = useAppStore()
const conns = useConnectionsStore()
const tabs = useTabsStore()

const loading = ref(false)
const saving = ref(false)
const failed = ref('')
const original = ref<TableDefinition | null>(null)
const def = ref<TableDefinition | null>(null)
const section = ref<SectionKey>('columns')
const selectedColumn = ref(0)
const preview = ref<{ sql: string } | null>(null)

const engine = computed(() => props.tab.engine)

/** 索引方法按引擎给：写错一个字母服务端才报错，这种东西本来就该是选项。 */
const indexMethods = computed(() => {
  switch (engine.value) {
    case 'postgres':
      return ['btree', 'hash', 'gin', 'gist', 'spgist', 'brin']
    case 'sqlite':
      return ['']
    default:
      return ['BTREE', 'HASH', 'FULLTEXT', 'SPATIAL']
  }
})
const CHARSETS = ['utf8mb4', 'utf8mb3', 'utf8', 'latin1', 'gbk', 'gb18030', 'ascii', 'binary']
const COLLATIONS: Record<string, string[]> = {
  utf8mb4: ['utf8mb4_0900_ai_ci', 'utf8mb4_general_ci', 'utf8mb4_unicode_ci', 'utf8mb4_bin', 'utf8mb4_0900_as_cs'],
  utf8mb3: ['utf8mb3_general_ci', 'utf8mb3_unicode_ci', 'utf8mb3_bin'],
  utf8: ['utf8_general_ci', 'utf8_unicode_ci', 'utf8_bin'],
  latin1: ['latin1_swedish_ci', 'latin1_general_ci', 'latin1_bin'],
  gbk: ['gbk_chinese_ci', 'gbk_bin'],
  gb18030: ['gb18030_chinese_ci', 'gb18030_bin'],
  ascii: ['ascii_general_ci', 'ascii_bin'],
  binary: ['binary'],
}
function collationsFor(charset: string): string[] {
  return COLLATIONS[charset] ?? Object.values(COLLATIONS).flat()
}
/** 同一个库里的表，给外键的引用表当候选。 */
const siblingTables = ref<string[]>([])
async function loadSiblingTables() {
  try {
    const objs = await api.ListObjects(props.tab.connId, props.tab.database, props.tab.ref?.schema ?? '', ['table'])
    siblingTables.value = objs.map((o) => o.name)
  } catch {
    siblingTables.value = []
  }
}
onMounted(loadSiblingTables)
const isMySQL = computed(() => engine.value === 'mysql' || engine.value === 'mariadb')

/** 各引擎的常用类型，放进下拉里；用户也可以直接输入任意类型。 */
const TYPES: Record<string, string[]> = {
  mysql: [
    'int', 'bigint', 'tinyint', 'smallint', 'mediumint', 'decimal', 'float', 'double',
    'varchar', 'char', 'text', 'mediumtext', 'longtext', 'json',
    'date', 'datetime', 'timestamp', 'time', 'year',
    'blob', 'longblob', 'binary', 'varbinary', 'enum', 'set', 'bit',
  ],
  postgres: [
    'integer', 'bigint', 'smallint', 'numeric', 'real', 'double precision',
    'varchar', 'char', 'text', 'json', 'jsonb', 'uuid',
    'date', 'timestamp', 'timestamptz', 'time', 'interval',
    'boolean', 'bytea', 'inet', 'serial', 'bigserial',
  ],
  sqlite: ['INTEGER', 'TEXT', 'REAL', 'BLOB', 'NUMERIC'],
}

const typeOptions = computed(() => TYPES[isMySQL.value ? 'mysql' : engine.value] ?? TYPES.mysql)

function emptyColumn(): Column {
  return {
    name: '',
    origName: '',
    position: 0,
    type: typeOptions.value[0],
    fullType: typeOptions.value[0],
    length: 0,
    scale: 0,
    nullable: true,
    default: '',
    hasDefault: false,
    defaultIsExpr: false,
    autoIncrement: false,
    primaryKey: false,
    unsigned: false,
    charset: '',
    collation: '',
    comment: '',
    generated: '',
  }
}

function blankDefinition(): TableDefinition {
  return {
    ref: {
      database: props.tab.database,
      schema: props.tab.ref?.schema ?? '',
      name: '',
      kind: 'table',
    },
    columns: [{ ...emptyColumn(), name: 'id', type: 'int', fullType: 'int', primaryKey: true, autoIncrement: true, nullable: false }],
    indexes: [],
    foreignKeys: [],
    comment: '',
    engine: isMySQL.value ? 'InnoDB' : '',
    charset: isMySQL.value ? 'utf8mb4' : '',
    collation: '',
    autoIncrement: 0,
    ddl: '',
  }
}

onMounted(load)

async function load() {
  if (props.isNew) {
    def.value = blankDefinition()
    original.value = null
    return
  }
  loading.value = true
  failed.value = ''
  try {
    const d = await api.GetTableDefinition(props.tab.connId, props.tab.ref!)
    original.value = JSON.parse(JSON.stringify(d))
    def.value = d
  } catch (e) {
    failed.value = e instanceof Error ? e.message : String(e)
    app.reportError(e, tr('读取表结构失败'))
  } finally {
    loading.value = false
  }
}

function markDirty() {
  tabs.setDirty(props.tab.id, true)
}

// --- 列操作 ---
function addColumn() {
  if (!def.value) return
  def.value.columns.push(emptyColumn())
  selectedColumn.value = def.value.columns.length - 1
  markDirty()
}

function removeColumn(i: number) {
  if (!def.value) return
  def.value.columns.splice(i, 1)
  selectedColumn.value = Math.max(0, Math.min(selectedColumn.value, def.value.columns.length - 1))
  markDirty()
}

function moveColumn(i: number, delta: number) {
  if (!def.value) return
  const j = i + delta
  if (j < 0 || j >= def.value.columns.length) return
  const cols = def.value.columns
  ;[cols[i], cols[j]] = [cols[j], cols[i]]
  selectedColumn.value = j
  markDirty()
}

/** 类型或长度改变时同步 fullType——后端比对的是它。 */
function syncFullType(c: Column) {
  const t = c.type.trim()
  if (!t) return
  if (c.length > 0 && c.scale > 0) c.fullType = `${t}(${c.length},${c.scale})`
  else if (c.length > 0) c.fullType = `${t}(${c.length})`
  else c.fullType = t
  markDirty()
}

function onPrimaryKeyToggle(c: Column) {
  if (c.primaryKey) c.nullable = false
  markDirty()
}

// --- 索引 ---
function addIndex() {
  if (!def.value) return
  def.value.indexes.push({
    name: `idx_${def.value.ref.name || 'table'}_${def.value.indexes.length + 1}`,
    columns: [],
    unique: false,
    primary: false,
    type: 'BTREE',
    comment: '',
  })
  markDirty()
}

function removeIndex(i: number) {
  def.value?.indexes.splice(i, 1)
  markDirty()
}

function indexColumnText(idx: TableIndex) {
  return idx.columns.map((c) => c.name).join(', ')
}

function setIndexColumns(idx: TableIndex, text: string) {
  idx.columns = text
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
    .map((name) => ({ name, order: 'ASC', length: 0 }))
  markDirty()
}

// --- 外键 ---
function addForeignKey() {
  if (!def.value) return
  def.value.foreignKeys.push({
    name: `fk_${def.value.ref.name || 'table'}_${def.value.foreignKeys.length + 1}`,
    columns: [],
    referencedSchema: '',
    referencedTable: '',
    referencedColumns: [],
    onUpdate: 'RESTRICT',
    onDelete: 'RESTRICT',
  })
  markDirty()
}

function removeForeignKey(i: number) {
  def.value?.foreignKeys.splice(i, 1)
  markDirty()
}

function listText(v: string[]) {
  return v.join(', ')
}

function setList(fk: ForeignKey, field: 'columns' | 'referencedColumns', text: string) {
  fk[field] = text
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
  markDirty()
}

// --- 保存 ---
async function buildPreview() {
  if (!def.value) return null
  try {
    if (props.isNew || !original.value) {
      return await api.PreviewCreateTable(props.tab.connId, def.value)
    }
    return await api.PreviewAlterTable(props.tab.connId, original.value, def.value)
  } catch (e) {
    app.reportError(e, tr('生成变更语句失败'))
    return null
  }
}

async function showPreview() {
  const p = await buildPreview()
  if (!p) return
  if (!p.sql) {
    app.toast('info', tr('没有需要执行的变更'))
    return
  }
  preview.value = { sql: p.sql }
}

async function save() {
  if (!def.value) return
  if (!def.value.ref.name.trim()) {
    app.toast('warning', tr('请先填写表名'))
    return
  }
  if (def.value.columns.length === 0) {
    app.toast('warning', tr('至少需要一个字段'))
    return
  }
  saving.value = true
  try {
    if (props.isNew || !original.value) {
      await api.CreateTable(props.tab.connId, def.value)
      app.toast('success', tr('表 {name} 已创建', { name: def.value.ref.name }))
    } else {
      await api.AlterTable(props.tab.connId, original.value, def.value)
      app.toast('success', tr('表 {name} 已更新', { name: def.value.ref.name }))
    }
    tabs.setDirty(props.tab.id, false)
    tabs.setTitle(props.tab.id, tr('设计: {name}', { name: def.value.ref.name }))
    await conns.refreshObjectFolder(
      props.tab.connId,
      def.value.ref.database,
      def.value.ref.schema,
      'table',
    )
    // 重新读一次，把服务端补齐的默认值（如 collation）同步回来。
    props.tab.ref = { ...def.value.ref }
    original.value = null
    await load()
  } catch (e) {
    app.reportError(e, tr('保存表结构失败'))
  } finally {
    saving.value = false
  }
}

type SectionKey = 'columns' | 'indexes' | 'foreignKeys' | 'options' | 'ddl'

const sections = computed<{ key: SectionKey; label: string; count: number }[]>(() => [
  { key: 'columns', label: tr('字段'), count: def.value?.columns.length ?? 0 },
  { key: 'indexes', label: tr('索引'), count: def.value?.indexes.length ?? 0 },
  { key: 'foreignKeys', label: tr('外键'), count: def.value?.foreignKeys.length ?? 0 },
  { key: 'options', label: tr('选项'), count: 0 },
  { key: 'ddl', label: 'DDL', count: 0 },
])
</script>

<template>
  <div class="tabpane">
    <div class="dbar">
      <div class="field-inline">
        <label>{{ tr('表名') }}</label>
        <input
          v-if="def"
          v-model="def.ref.name"
          class="input name-input mono"
          :placeholder="tr('表名')"
          @input="markDirty"
        />
      </div>
      <div class="spacer"></div>
      <button class="btn" :disabled="loading" @click="showPreview">
        <Icon name="code" :size="13" />{{ tr('预览 SQL') }}</button>
      <button class="btn" :disabled="loading || isNew" @click="load">
        <Icon name="refresh" :size="13" :class="{ spin: loading }" />{{ tr('重新载入') }}</button>
      <button class="btn btn-primary" :disabled="saving || loading" @click="save">
        <Icon name="save" :size="13" />{{ tr('保存') }}</button>
    </div>

    <nav class="dsections">
      <button
        v-for="s in sections"
        :key="s.key"
        class="dsection"
        :class="{ 'is-active': section === s.key }"
        @click="section = s.key"
      >
        {{ s.label }}
        <span v-if="s.count" class="badge">{{ s.count }}</span>
      </button>
    </nav>

    <div v-if="loading && !def" class="loading-state">
      <div class="spinner"></div>
      <div class="empty-state-title">{{ tr('正在读取 {name} 的结构', { name: tab.ref?.name ?? '' }) }}</div>
      <div class="muted">{{ tr('列 / 索引 / 外键三条语句并发读取中…') }}</div>
    </div>
    <div v-else-if="failed" class="empty-state">
      <div class="empty-state-title">{{ tr('读取表结构失败') }}</div>
      <div class="muted selectable">{{ failed }}</div>
      <button class="btn" @click="load">{{ tr('重试') }}</button>
    </div>
    <div v-else-if="!def" class="empty-state">
      <div class="empty-state-title">{{ tr('没有可显示的结构') }}</div>
    </div>

    <!-- 字段 -->
    <div v-else-if="section === 'columns'" class="dbody">
      <table class="dtable">
        <thead>
          <tr>
            <th class="w-num"></th>
            <th class="w-name">{{ tr('名称') }}</th>
            <th class="w-type">{{ tr('类型') }}</th>
            <th class="w-len">{{ tr('长度') }}</th>
            <th class="w-len">{{ tr('小数位') }}</th>
            <th class="w-flag">{{ tr('非空') }}</th>
            <th class="w-flag">{{ tr('主键') }}</th>
            <th v-if="isMySQL" class="w-flag">{{ tr('自增') }}</th>
            <th v-if="isMySQL" class="w-flag">{{ tr('无符号') }}</th>
            <th class="w-default">{{ tr('默认值') }}</th>
            <th>{{ tr('注释') }}</th>
            <th class="w-ops"></th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="(c, i) in def.columns"
            :key="i"
            :class="{ 'is-selected': selectedColumn === i, 'is-new': !c.origName }"
            @click="selectedColumn = i"
          >
            <td class="w-num">{{ i + 1 }}</td>
            <td><input v-model="c.name" class="cell-input mono" @input="markDirty" /></td>
            <td>
              <input
                v-model="c.type"
                class="cell-input mono"
                list="type-options"
                @change="syncFullType(c)"
              />
            </td>
            <td>
              <input
                v-model.number="c.length"
                class="cell-input"
                type="number"
                min="0"
                @change="syncFullType(c)"
              />
            </td>
            <td>
              <input
                v-model.number="c.scale"
                class="cell-input"
                type="number"
                min="0"
                @change="syncFullType(c)"
              />
            </td>
            <td class="w-flag">
              <input
                type="checkbox"
                :checked="!c.nullable"
                @change="
                  (e) => {
                    c.nullable = !(e.target as HTMLInputElement).checked
                    markDirty()
                  }
                "
              />
            </td>
            <td class="w-flag">
              <input v-model="c.primaryKey" type="checkbox" @change="onPrimaryKeyToggle(c)" />
            </td>
            <td v-if="isMySQL" class="w-flag">
              <input v-model="c.autoIncrement" type="checkbox" @change="markDirty" />
            </td>
            <td v-if="isMySQL" class="w-flag">
              <input v-model="c.unsigned" type="checkbox" @change="markDirty" />
            </td>
            <td>
              <input
                v-model="c.default"
                class="cell-input mono"
                :placeholder="c.hasDefault ? '' : tr('无')"
                @input="
                  () => {
                    c.hasDefault = c.default !== ''
                    markDirty()
                  }
                "
              />
            </td>
            <td><input v-model="c.comment" class="cell-input" @input="markDirty" /></td>
            <td class="w-ops">
              <button class="icon-btn" :title="tr('上移')" @click.stop="moveColumn(i, -1)">
                <Icon name="chevronUp" :size="11" />
              </button>
              <button class="icon-btn" :title="tr('下移')" @click.stop="moveColumn(i, 1)">
                <Icon name="chevronDown" :size="11" />
              </button>
              <button class="icon-btn danger" :title="tr('删除字段')" @click.stop="removeColumn(i)">
                <Icon name="trash" :size="11" />
              </button>
            </td>
          </tr>
        </tbody>
      </table>
      <datalist id="type-options">
        <option v-for="t in typeOptions" :key="t" :value="t" />
      </datalist>
      <button class="btn add-row" @click="addColumn">
        <Icon name="plus" :size="12" />{{ tr('添加字段') }}</button>
    </div>

    <!-- 索引 -->
    <div v-else-if="section === 'indexes'" class="dbody">
      <table class="dtable">
        <thead>
          <tr>
            <th class="w-num"></th>
            <th class="w-name">{{ tr('索引名') }}</th>
            <th>{{ tr('字段（逗号分隔）') }}</th>
            <th class="w-flag">{{ tr('唯一') }}</th>
            <th class="w-flag">{{ tr('主键') }}</th>
            <th class="w-type">{{ tr('方法') }}</th>
            <th class="w-ops"></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(idx, i) in def.indexes" :key="i">
            <td class="w-num">{{ i + 1 }}</td>
            <td>
              <input
                v-model="idx.name"
                class="cell-input mono"
                :disabled="idx.primary"
                @input="markDirty"
              />
            </td>
            <td>
              <input
                class="cell-input mono"
                :value="indexColumnText(idx)"
                placeholder="col_a, col_b"
                @change="setIndexColumns(idx, ($event.target as HTMLInputElement).value)"
              />
            </td>
            <td class="w-flag">
              <input v-model="idx.unique" type="checkbox" :disabled="idx.primary" @change="markDirty" />
            </td>
            <td class="w-flag">
              <input v-model="idx.primary" type="checkbox" disabled />
            </td>
            <td>
              <select v-model="idx.type" class="cell-input mono" @change="markDirty">
                <option v-if="idx.type && !indexMethods.includes(idx.type)" :value="idx.type">{{ idx.type }}</option>
                <option v-for="m in indexMethods" :key="m" :value="m">{{ m }}</option>
              </select>
            </td>
            <td class="w-ops">
              <button
                class="icon-btn danger"
                :title="tr('删除索引')"
                :disabled="idx.primary"
                @click="removeIndex(i)"
              >
                <Icon name="trash" :size="11" />
              </button>
            </td>
          </tr>
        </tbody>
      </table>
      <button class="btn add-row" @click="addIndex"><Icon name="plus" :size="12" />{{ tr('添加索引') }}</button>
    </div>

    <!-- 外键 -->
    <div v-else-if="section === 'foreignKeys'" class="dbody">
      <table class="dtable">
        <thead>
          <tr>
            <th class="w-num"></th>
            <th class="w-name">{{ tr('约束名') }}</th>
            <th>{{ tr('本表字段') }}</th>
            <th class="w-name">{{ tr('引用表') }}</th>
            <th>{{ tr('引用字段') }}</th>
            <th class="w-type">{{ tr('更新时') }}</th>
            <th class="w-type">{{ tr('删除时') }}</th>
            <th class="w-ops"></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(fk, i) in def.foreignKeys" :key="i">
            <td class="w-num">{{ i + 1 }}</td>
            <td><input v-model="fk.name" class="cell-input mono" @input="markDirty" /></td>
            <td>
              <input
                class="cell-input mono"
                :value="listText(fk.columns)"
                @change="setList(fk, 'columns', ($event.target as HTMLInputElement).value)"
              />
            </td>
            <td>
              <input v-model="fk.referencedTable" class="cell-input mono" list="ref-table-options" @input="markDirty" />
              <datalist id="ref-table-options"><option v-for="t in siblingTables" :key="t" :value="t" /></datalist>
            </td>
            <td>
              <input
                class="cell-input mono"
                :value="listText(fk.referencedColumns)"
                @change="setList(fk, 'referencedColumns', ($event.target as HTMLInputElement).value)"
              />
            </td>
            <td>
              <select v-model="fk.onUpdate" class="cell-input" @change="markDirty">
                <option>RESTRICT</option>
                <option>CASCADE</option>
                <option>SET NULL</option>
                <option>NO ACTION</option>
              </select>
            </td>
            <td>
              <select v-model="fk.onDelete" class="cell-input" @change="markDirty">
                <option>RESTRICT</option>
                <option>CASCADE</option>
                <option>SET NULL</option>
                <option>NO ACTION</option>
              </select>
            </td>
            <td class="w-ops">
              <button class="icon-btn danger" :title="tr('删除外键')" @click="removeForeignKey(i)">
                <Icon name="trash" :size="11" />
              </button>
            </td>
          </tr>
        </tbody>
      </table>
      <button class="btn add-row" @click="addForeignKey">
        <Icon name="plus" :size="12" />{{ tr('添加外键') }}</button>
    </div>

    <!-- 选项 -->
    <div v-else-if="section === 'options'" class="dbody options">
      <div class="field">
        <label>{{ tr('表注释') }}</label>
        <input v-model="def.comment" class="input" @input="markDirty" />
      </div>
      <template v-if="isMySQL">
        <div class="field">
          <label>{{ tr('存储引擎') }}</label>
          <select v-model="def.engine" class="select" @change="markDirty">
            <option>InnoDB</option>
            <option>MyISAM</option>
            <option>MEMORY</option>
            <option>ARCHIVE</option>
          </select>
        </div>
        <div class="field">
          <label>{{ tr('字符集') }}</label>
          <input v-model="def.charset" class="input mono" list="charset-options" @input="markDirty" />
          <datalist id="charset-options"><option v-for="c in CHARSETS" :key="c" :value="c" /></datalist>
        </div>
        <div class="field">
          <label>{{ tr('排序规则') }}</label>
          <input v-model="def.collation" class="input mono" list="collation-options" @input="markDirty" />
          <datalist id="collation-options"><option v-for="c in collationsFor(def.charset)" :key="c" :value="c" /></datalist>
        </div>
        <div class="field">
          <label>{{ tr('自增起始') }}</label>
          <input v-model.number="def.autoIncrement" class="input" type="number" @input="markDirty" />
        </div>
      </template>
    </div>

    <!-- DDL -->
    <div v-else class="dbody">
      <pre class="ddl mono selectable">{{ def.ddl || tr('（新建的表尚未生成 DDL，保存后可见）') }}</pre>
    </div>

    <SqlPreviewDialog
      v-if="preview"
      :title="tr('将要执行的结构变更')"
      :sql="preview.sql"
      confirm-:label="tr('执行')"
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
.tabpane {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}

.spin {
  animation: spin 0.9s linear infinite;
}

.dbar {
  flex: none;
  display: flex;
  align-items: center;
  gap: 6px;
  height: 32px;
  padding: 0 8px;
  background: var(--c-bg-sunken);
  border-bottom: 1px solid var(--c-border-soft);
}

.field-inline {
  display: flex;
  align-items: center;
  gap: 6px;
}
.field-inline label {
  color: var(--c-text-secondary);
}

.name-input {
  width: 220px;
}

.spacer {
  flex: 1;
}

.dsections {
  flex: none;
  display: flex;
  gap: 2px;
  padding: 0 8px;
  background: var(--c-bg-sunken);
  border-bottom: 1px solid var(--c-border);
}

.dsection {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 25px;
  padding: 0 12px;
  border: none;
  border-bottom: 2px solid transparent;
  background: transparent;
  color: var(--c-text-secondary);
  font-size: var(--font-size);
}
.dsection:hover {
  color: var(--c-text);
}
.dsection.is-active {
  border-bottom-color: var(--c-accent);
  color: var(--c-accent);
  font-weight: 600;
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

.spinner {
  width: 22px;
  height: 22px;
  border: 2px solid var(--c-border);
  border-top-color: var(--c-accent);
  border-radius: 50%;
  animation: spin 0.7s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

.dbody {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 8px;
}

.dbody.options {
  max-width: 460px;
}

.dtable {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--font-size);
}

.dtable th {
  position: sticky;
  top: -8px;
  z-index: 1;
  padding: 4px 6px;
  text-align: left;
  font-weight: 600;
  color: var(--c-text-secondary);
  background: var(--c-chrome);
  border-bottom: 1px solid var(--c-border);
  white-space: nowrap;
}

.dtable td {
  padding: 1px 3px;
  border-bottom: 1px solid var(--c-border-soft);
}

.dtable tbody tr:hover {
  background: var(--c-row-hover);
}
.dtable tbody tr.is-selected {
  background: var(--c-accent-soft);
}
.dtable tbody tr.is-new {
  background: var(--c-row-inserted);
}

.w-num {
  width: 30px;
  color: var(--c-text-tertiary);
  text-align: right;
  padding-right: 6px !important;
}
.w-name {
  width: 170px;
}
.w-type {
  width: 120px;
}
.w-len {
  width: 70px;
}
.w-flag {
  width: 48px;
  text-align: center;
}
.w-default {
  width: 140px;
}
.w-ops {
  width: 74px;
  white-space: nowrap;
}

.cell-input {
  width: 100%;
  height: 21px;
  padding: 0 5px;
  border: 1px solid transparent;
  border-radius: 2px;
  background: transparent;
  color: var(--c-text);
  outline: none;
  font-size: var(--font-size);
}
.cell-input:hover:not(:disabled) {
  border-color: var(--c-border);
  background: var(--c-bg);
}
.cell-input:focus {
  border-color: var(--c-accent);
  background: var(--c-bg);
}
.cell-input:disabled {
  color: var(--c-text-tertiary);
}

.icon-btn {
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
.icon-btn.danger:hover:not(:disabled) {
  background: var(--c-danger);
  color: #fff;
}
.icon-btn:disabled {
  opacity: 0.35;
}

.add-row {
  margin-top: 8px;
}

.ddl {
  margin: 0;
  padding: 10px;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  background: var(--c-bg-sunken);
  font-size: var(--font-size-mono);
  line-height: 1.6;
  white-space: pre-wrap;
}
</style>
