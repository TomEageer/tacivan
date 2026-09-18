<script setup lang="ts">
/** SQL 预览对话框：保存数据或改结构之前，先让用户看清将要执行什么。 */
import { useAppStore } from '../../stores/app'
import Modal from '../common/Modal.vue'
import Icon from '../common/Icon.vue'
import { tr } from '../../i18n'

const props = withDefaults(
  defineProps<{
    title: string
    sql: string
    warnings?: string[]
    confirmLabel?: string
    /** 只看不执行时传 false，footer 不出现确认按钮。 */
    confirmable?: boolean
  }>(),
  { confirmLabel: tr('执行'), confirmable: true },
)

const emit = defineEmits<{ (e: 'confirm'): void; (e: 'close'): void }>()
const app = useAppStore()

function copy() {
  navigator.clipboard?.writeText(props.sql)
  app.toast('success', tr('已复制 SQL'))
}
</script>

<template>
  <Modal :title="title" :width="740" :height="480" @close="emit('close')">
    <div class="preview">
      <div v-if="warnings?.length" class="warnings">
        <div v-for="(w, i) in warnings" :key="i" class="warning-item">
          <Icon name="warning" :size="13" />
          <span>{{ w }}</span>
        </div>
      </div>
      <pre v-if="sql" class="preview-sql mono selectable">{{ sql }}</pre>
      <div v-else class="empty-state">
        <div class="empty-state-title">{{ tr('没有需要执行的变更') }}</div>
      </div>
    </div>

    <template #footer>
      <button class="btn" :disabled="!sql" @click="copy">{{ tr('复制') }}</button>
      <button class="btn" @click="emit('close')">{{ tr('取消') }}</button>
      <button
        v-if="confirmable"
        class="btn btn-primary"
        :disabled="!sql"
        @click="emit('confirm')"
      >
        {{ confirmLabel }}
      </button>
    </template>
  </Modal>
</template>

<style scoped>
.preview {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 260px;
  gap: 8px;
}

.warnings {
  flex: none;
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 8px 10px;
  border: 1px solid var(--c-warning);
  border-radius: var(--radius-sm);
  background: var(--c-warning-soft);
  color: var(--c-warning);
}

.warning-item {
  display: flex;
  align-items: flex-start;
  gap: 6px;
}

.preview-sql {
  flex: 1;
  min-height: 0;
  margin: 0;
  padding: 10px 12px;
  overflow: auto;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  background: var(--c-bg-sunken);
  font-size: var(--font-size-mono);
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-word;
}
</style>
