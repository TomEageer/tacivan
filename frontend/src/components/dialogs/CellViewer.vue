<script setup lang="ts">
/**
 * 单元格完整值查看器。
 *
 * 网格里显示的是截断预览，这里按主键回查完整值——
 * 因此几百 MB 的大字段既不进内存缓存也不进溢出文件，只有打开时才取一次。
 */
import { computed, onMounted, ref } from 'vue'
import * as api from '../../api'
import { useAppStore } from '../../stores/app'
import Modal from '../common/Modal.vue'
import { formatBytes } from '../grid/cell'
import { tr } from '../../i18n'

const props = defineProps<{ resultId: string; rowIndex: number; column: string }>()
const emit = defineEmits<{ (e: 'close'): void }>()

const app = useAppStore()
const loading = ref(true)
const value = ref('')
const error = ref('')
const mode = ref<'text' | 'json' | 'hex'>('text')

onMounted(async () => {
  try {
    value.value = await api.GetCellValue(props.resultId, props.rowIndex, props.column)
    if (looksLikeJson(value.value)) mode.value = 'json'
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
})

function looksLikeJson(s: string) {
  const t = s.trim()
  if (!(t.startsWith('{') || t.startsWith('['))) return false
  try {
    JSON.parse(t)
    return true
  } catch {
    return false
  }
}

const byteLength = computed(() => new TextEncoder().encode(value.value).length)

const displayed = computed(() => {
  if (mode.value === 'json') {
    try {
      return JSON.stringify(JSON.parse(value.value), null, 2)
    } catch {
      return value.value
    }
  }
  if (mode.value === 'hex') return toHexDump(value.value)
  return value.value
})

/** 十六进制视图：偏移 + 16 字节一行 + ASCII 侧栏。 */
function toHexDump(s: string): string {
  const bytes = new TextEncoder().encode(s)
  // 超大值全量转 hex 会把界面卡住，只展示前一段。
  const limit = Math.min(bytes.length, 64 * 1024)
  const lines: string[] = []
  for (let i = 0; i < limit; i += 16) {
    const chunk = bytes.slice(i, i + 16)
    const hex = [...chunk].map((b) => b.toString(16).padStart(2, '0')).join(' ')
    const ascii = [...chunk].map((b) => (b >= 32 && b < 127 ? String.fromCharCode(b) : '.')).join('')
    lines.push(`${i.toString(16).padStart(8, '0')}  ${hex.padEnd(47)}  |${ascii}|`)
  }
  if (bytes.length > limit)
    lines.push(tr('… 其余 {size} 未显示', { size: formatBytes(bytes.length - limit) }))
  return lines.join('\n')
}

function copy() {
  navigator.clipboard?.writeText(value.value)
  app.toast('success', tr('已复制到剪贴板'))
}
</script>

<template>
  <Modal :title="tr('值查看器 — {column}', { column })" :width="720" :height="520" @close="emit('close')">
    <div class="viewer">
      <div class="viewer-tabs">
        <button
          v-for="m in (['text', 'json', 'hex'] as const)"
          :key="m"
          class="vtab"
          :class="{ 'is-active': mode === m }"
          @click="mode = m"
        >
          {{ m === 'text' ? tr('文本') : m === 'json' ? 'JSON' : tr('十六进制') }}
        </button>
        <span class="spacer"></span>
        <span class="muted">{{ formatBytes(byteLength) }}</span>
      </div>

      <div v-if="loading" class="viewer-body muted">{{ tr('正在读取完整值…') }}</div>
      <div v-else-if="error" class="viewer-body error">{{ error }}</div>
      <pre v-else class="viewer-body mono selectable">{{ displayed }}</pre>
    </div>

    <template #footer>
      <button class="btn" @click="copy">{{ tr('复制') }}</button>
      <button class="btn btn-primary" @click="emit('close')">{{ tr('关闭') }}</button>
    </template>
  </Modal>
</template>

<style scoped>
.viewer {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 300px;
}

.viewer-tabs {
  flex: none;
  display: flex;
  align-items: center;
  gap: 2px;
  margin-bottom: 8px;
}

.vtab {
  height: 22px;
  padding: 0 11px;
  border: 1px solid transparent;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--c-text-secondary);
  font-size: var(--font-size);
}
.vtab:hover {
  background: var(--c-bg-sunken);
}
.vtab.is-active {
  background: var(--c-accent-soft);
  border-color: var(--c-accent-border);
  color: var(--c-accent);
}

.spacer {
  flex: 1;
}

.viewer-body {
  flex: 1;
  min-height: 0;
  margin: 0;
  padding: 10px;
  overflow: auto;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  background: var(--c-bg-sunken);
  font-size: var(--font-size-mono);
  line-height: 1.55;
  white-space: pre-wrap;
  word-break: break-word;
}
.viewer-body.error {
  color: var(--c-danger);
}
</style>
