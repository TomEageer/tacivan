<script setup lang="ts">
/** 导出结果集。后端流式写盘，导出千万行与导出百行的内存占用相同。 */
import { computed, ref } from 'vue'
import * as api from '../../api'
import { useAppStore } from '../../stores/app'
import Modal from '../common/Modal.vue'
import { formatBytes } from '../grid/cell'
import type { ExportOptions, ExportFormat } from '../../types'
import { tr } from '../../i18n'

const props = defineProps<{ resultId: string; defaultName: string }>()
const emit = defineEmits<{ (e: 'close'): void }>()

const app = useAppStore()
const busy = ref(false)

/** 格式清单里带文案，语言切换时要跟着变，所以是 computed 不是常量。 */
const FORMATS = computed<{ value: ExportFormat; label: string; ext: string; hint: string }[]>(
  () => [
    { value: 'csv', label: 'CSV', ext: 'csv', hint: tr('通用表格格式，Excel 可直接打开') },
    { value: 'tsv', label: 'TSV', ext: 'tsv', hint: tr('制表符分隔') },
    { value: 'json', label: 'JSON', ext: 'json', hint: tr('对象数组') },
    { value: 'sql', label: 'SQL', ext: 'sql', hint: tr('INSERT 语句脚本') },
    { value: 'markdown', label: 'Markdown', ext: 'md', hint: tr('表格，便于贴进文档') },
  ],
)

const opts = ref<ExportOptions>({
  format: 'csv',
  path: '',
  includeHeader: true,
  delimiter: ',',
  encoding: 'utf8bom',
  nullText: '',
  maxRows: 0,
  tableName: props.defaultName,
  batchSize: 100,
})

const currentFormat = computed(() => FORMATS.value.find((f) => f.value === opts.value.format)!)

async function pickPath() {
  try {
    const name = `${props.defaultName}.${currentFormat.value.ext}`
    const p = await api.SaveFileDialogFor(opts.value.format, name)
    if (p) opts.value.path = p
  } catch (e) {
    app.reportError(e, tr('选择保存位置失败'))
  }
}

async function run() {
  if (!opts.value.path) {
    await pickPath()
    if (!opts.value.path) return
  }
  busy.value = true
  try {
    const res = await api.ExportResultSet(props.resultId, opts.value)
    app.toast(
      'success',
      tr('已导出 {n} 行', { n: res.rows.toLocaleString() }),
      `${res.path} · ${formatBytes(res.bytes)} · ${res.durationMs} ms${res.truncated ? tr('（已按上限截断）') : ''}`,
    )
    emit('close')
  } catch (e) {
    app.reportError(e, tr('导出失败'))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <Modal :title="tr('导出结果集')" :width="520" @close="emit('close')">
    <div class="field">
      <label>{{ tr('格式') }}</label>
      <div class="format-row">
        <button
          v-for="f in FORMATS"
          :key="f.value"
          class="format-btn"
          :class="{ 'is-active': opts.format === f.value }"
          @click="opts.format = f.value"
        >
          {{ f.label }}
        </button>
      </div>
    </div>
    <div class="field-hint">{{ currentFormat.hint }}</div>

    <div class="field">
      <label>{{ tr('保存到') }}</label>
      <div class="path-row">
        <input v-model="opts.path" class="input mono" :placeholder="tr('点击右侧选择位置')" readonly />
        <button class="btn" @click="pickPath">{{ tr('浏览…') }}</button>
      </div>
    </div>

    <div class="divider"></div>

    <template v-if="opts.format === 'csv' || opts.format === 'tsv'">
      <div class="field">
        <label>{{ tr('选项') }}</label>
        <label class="checkbox">
          <input v-model="opts.includeHeader" type="checkbox" />{{ tr('包含表头行') }}</label>
      </div>
      <div class="field">
        <label>{{ tr('编码') }}</label>
        <select v-model="opts.encoding" class="select">
          <option value="utf8bom">{{ tr('UTF-8 带 BOM（Excel 打开不乱码）') }}</option>
          <option value="utf8">UTF-8</option>
        </select>
      </div>
      <div class="field">
        <label>{{ tr('NULL 写作') }}</label>
        <input v-model="opts.nullText" class="input" :placeholder="tr('留空表示写成空字符串')" />
      </div>
    </template>

    <template v-else-if="opts.format === 'sql'">
      <div class="field">
        <label>{{ tr('目标表名') }}</label>
        <input v-model="opts.tableName" class="input mono" />
      </div>
      <div class="field">
        <label>{{ tr('每条语句') }}</label>
        <input v-model.number="opts.batchSize" class="input" type="number" min="1" max="1000" />
      </div>
      <div class="field-hint">{{ tr('一条 INSERT 里带多少行，值越大文件越小、导入越快') }}</div>
    </template>

    <div class="field">
      <label>{{ tr('行数上限') }}</label>
      <input
        v-model.number="opts.maxRows"
        class="input"
        type="number"
        min="0"
        :placeholder="tr('0 表示导出全部')"
      />
    </div>
    <div class="field-hint">{{ tr('导出全程流式写盘，行数再多也不会占用额外内存') }}</div>

    <template #footer>
      <button class="btn" @click="emit('close')">{{ tr('取消') }}</button>
      <button class="btn btn-primary" :disabled="busy" @click="run">
        {{ busy ? tr('导出中…') : tr('开始导出') }}
      </button>
    </template>
  </Modal>
</template>

<style scoped>
.format-row {
  display: flex;
  gap: 3px;
}

.format-btn {
  height: 24px;
  padding: 0 12px;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  background: var(--c-bg);
  color: var(--c-text);
  font-size: var(--font-size);
}
.format-btn:hover {
  background: var(--c-bg-sunken);
}
.format-btn.is-active {
  background: var(--c-accent);
  border-color: var(--c-accent);
  color: #fff;
}

.path-row {
  display: flex;
  gap: 6px;
}
.path-row .input {
  flex: 1;
}
</style>
