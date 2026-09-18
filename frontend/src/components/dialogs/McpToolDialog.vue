<script setup lang="ts">
/**
 * 调用一个 MCP 工具 / 读取一个 MCP 资源。
 *
 * 表单按工具的 JSON Schema 生成：字符串、数字、布尔、枚举直接给控件，
 * 对象和数组给一个 JSON 文本框。结果长得像表的进网格（筛选、导出照常），其余当文本。
 */
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import * as api from '../../api'
import { useAppStore } from '../../stores/app'
import type { TreeNode } from '../../stores/connections'
import { useGridData } from '../../composables/useGridData'
import type { McpCallResult, McpResource, McpTool } from '../../types'
import Modal from '../common/Modal.vue'
import DataGrid from '../grid/DataGrid.vue'
import { tr } from '../../i18n'

const props = defineProps<{ node: TreeNode }>()
const emit = defineEmits<{ (e: 'close'): void }>()
const app = useAppStore()

const isTool = computed(() => props.node.objectKind === 'tool')
const tool = computed(() => (isTool.value ? (props.node.mcp as McpTool) : null))
const resource = computed(() => (!isTool.value ? (props.node.mcp as McpResource) : null))

interface Field {
  key: string
  label: string
  type: 'string' | 'number' | 'boolean' | 'enum' | 'json'
  required: boolean
  description?: string
  options?: string[]
  multiline?: boolean
}

/** 把 JSON Schema 的 properties 压成表单字段；认不出的类型一律 JSON 文本框。 */
const fields = computed<Field[]>(() => {
  const raw = tool.value?.inputSchema
  const schema = (typeof raw === 'string' ? safeParse(raw) : raw) as
    | { properties?: Record<string, Record<string, unknown>>; required?: string[] }
    | undefined
  if (!schema?.properties) return []
  const required = new Set(schema.required ?? [])
  return Object.entries(schema.properties).map(([key, p]) => {
    const t = (Array.isArray(p.type) ? p.type[0] : p.type) as string | undefined
    const f: Field = { key, label: (p.title as string) || key, type: 'json', required: required.has(key), description: p.description as string }
    if (Array.isArray(p.enum)) {
      f.type = 'enum'
      f.options = (p.enum as unknown[]).map(String)
    } else if (t === 'string') {
      f.type = 'string'
      f.multiline = (p.format as string) === 'multiline' || key.toLowerCase().includes('sql')
    } else if (t === 'number' || t === 'integer') f.type = 'number'
    else if (t === 'boolean') f.type = 'boolean'
    return f
  })
})
function safeParse(s: string) {
  try {
    return JSON.parse(s)
  } catch {
    return undefined
  }
}

const values = ref<Record<string, string>>({})
const busy = ref(false)
const result = ref<McpCallResult | null>(null)
const text = ref('')
const data = useGridData()

async function run() {
  if (!tool.value) return
  const args: Record<string, unknown> = {}
  for (const f of fields.value) {
    const v = values.value[f.key] ?? ''
    if (v === '' || v === undefined) {
      if (f.required) {
        app.toast('warning', tr('参数') + '「' + f.label + '」' + tr('必填'))
        return
      }
      continue
    }
    if (f.type === 'number') args[f.key] = Number(v)
    else if (f.type === 'boolean') args[f.key] = v === 'true'
    else if (f.type === 'json') {
      try {
        args[f.key] = JSON.parse(v)
      } catch {
        app.toast('error', tr('JSON 格式不对：{field}', { field: f.label }))
        return
      }
    } else args[f.key] = v
  }
  busy.value = true
  releaseResult()
  try {
    const res = await api.McpCallTool(props.node.connId, tool.value.name, args)
    result.value = res
    text.value = res.text
    if (res.resultId) {
      const page = await api.FetchRows(res.resultId, 0, app.settings.gridPageSize || 200)
      data.reset(res.resultId, page)
    }
    if (res.isError) app.toast('error', tr('调用失败'), res.text.slice(0, 200))
  } catch (e) {
    app.reportError(e, tr('调用失败'))
  } finally {
    busy.value = false
  }
}

async function readResource() {
  if (!resource.value) return
  busy.value = true
  try {
    text.value = await api.McpReadResource(props.node.connId, resource.value.uri)
  } catch (e) {
    app.reportError(e, tr('读取失败'))
  } finally {
    busy.value = false
  }
}

function releaseResult() {
  if (result.value?.resultId) api.CloseResult(result.value.resultId).catch(() => {})
  result.value = null
  text.value = ''
}

onMounted(() => {
  if (!isTool.value) void readResource()
})
onBeforeUnmount(releaseResult)
</script>

<template>
  <Modal :title="isTool ? tool!.name : resource!.name || resource!.uri" :width="760" @close="emit('close')">
    <div v-if="(isTool ? tool?.description : resource?.description)" class="muted desc">
      {{ isTool ? tool?.description : resource?.description }}
    </div>

    <template v-if="isTool">
      <div v-if="!fields.length" class="muted">{{ tr('这个工具没有参数') }}</div>
      <div v-for="f in fields" :key="f.key" class="field">
        <label :title="f.key">{{ f.label }}<span v-if="f.required" class="req">*</span></label>
        <select v-if="f.type === 'enum'" v-model="values[f.key]" class="select">
          <option value=""></option>
          <option v-for="o in f.options" :key="o" :value="o">{{ o }}</option>
        </select>
        <label v-else-if="f.type === 'boolean'" class="checkbox">
          <input type="checkbox" :checked="values[f.key] === 'true'" @change="values[f.key] = ($event.target as HTMLInputElement).checked ? 'true' : ''" />
        </label>
        <textarea v-else-if="f.type === 'json' || f.multiline" v-model="values[f.key]" class="input mono args-textarea" :placeholder="f.type === 'json' ? '{ } / [ ]' : ''"></textarea>
        <input v-else v-model="values[f.key]" class="input" :type="f.type === 'number' ? 'number' : 'text'" @keydown.enter.prevent="run" />
        <div v-if="f.description" class="field-hint">{{ f.description }}</div>
      </div>
    </template>

    <div v-if="result?.resultId" class="result-grid">
      <div class="muted result-meta">{{ tr('{n} 行 · {ms} ms · 结果已进网格，可筛选、导出', { n: result.rows, ms: result.durationMs }) }}</div>
      <div class="grid-wrap"><DataGrid :data="data" :zebra="true" /></div>
    </div>
    <pre v-else-if="text" class="result-text selectable" :class="{ 'is-error': result?.isError }">{{ text }}</pre>

    <template #footer>
      <span class="spacer"></span>
      <button class="btn" @click="emit('close')">{{ tr('关闭') }}</button>
      <button v-if="isTool" class="btn btn-primary" :disabled="busy" @click="run">{{ busy ? tr('读取中…') : tr('调用') }}</button>
    </template>
  </Modal>
</template>

<style scoped>
.desc { margin-bottom: var(--sp-3); }
.req { color: var(--c-danger); margin-left: 2px; }
.args-textarea { min-height: 64px; resize: vertical; }
.result-meta { margin: var(--sp-3) 0 var(--sp-2); }
.grid-wrap { height: 320px; border: 1px solid var(--c-border); border-radius: var(--radius-sm); overflow: hidden; }
.result-text {
  margin: var(--sp-3) 0 0;
  max-height: 360px;
  overflow: auto;
  padding: var(--sp-3);
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  background: var(--c-sunken);
  font-family: var(--font-mono);
  font-size: var(--font-size-sm);
  white-space: pre-wrap;
  word-break: break-all;
}
.result-text.is-error { border-color: var(--c-danger); }
</style>
