<script setup lang="ts">
/** 模态对话框。Esc 关闭，点遮罩不关闭（避免误触丢失填了一半的表单）。 */
import { onMounted, onBeforeUnmount } from 'vue'
import Icon from './Icon.vue'
import { tr } from '../../i18n'

const props = withDefaults(
  defineProps<{ title: string; width?: number; height?: number; closable?: boolean }>(),
  { width: 560, closable: true },
)
const emit = defineEmits<{ (e: 'close'): void }>()

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape' && props.closable) {
    e.stopPropagation()
    emit('close')
  }
}

onMounted(() => window.addEventListener('keydown', onKey, true))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey, true))
</script>

<template>
  <div class="modal-backdrop">
    <div
      class="modal"
      :style="{ width: width + 'px', height: height ? height + 'px' : undefined }"
      role="dialog"
    >
      <header class="modal-head">
        <span class="modal-title">{{ title }}</span>
        <button v-if="closable" class="modal-close" :title="tr('关闭')" @click="emit('close')">
          <Icon name="close" :size="12" />
        </button>
      </header>
      <div class="modal-body">
        <slot />
      </div>
      <footer v-if="$slots.footer" class="modal-foot">
        <slot name="footer" />
      </footer>
    </div>
  </div>
</template>

<style scoped>
.modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 100;
  display: flex;
  /* 顶部锚定而不是垂直居中：对话框换页时高度会变，
     居中会让它上下跳，顶部锚定只会向下长，目光不用追。 */
  align-items: flex-start;
  justify-content: center;
  padding-top: 12vh;
  background: rgba(0, 0, 0, 0.28);
}

.modal {
  display: flex;
  flex-direction: column;
  max-width: calc(100vw - 48px);
  max-height: calc(100vh - 64px);
  background: var(--c-bg);
  border-radius: 14px;
  box-shadow: var(--shadow-modal);
  overflow: hidden;
}

.modal-head {
  flex: none;
  display: flex;
  align-items: center;
  justify-content: center;
  position: relative;
  height: 36px;
  padding: 0 12px;
  background: var(--c-chrome);
  border-bottom: 1px solid var(--c-border);
}

.modal-title {
  font-weight: 600;
}

.modal-close {
  position: absolute;
  left: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  padding: 0;
  border: none;
  border-radius: 50%;
  background: transparent;
  color: var(--c-text-secondary);
}
.modal-close:hover {
  background: var(--c-chrome-active);
  color: var(--c-text);
}

.modal-body {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 14px 16px;
}

.modal-foot {
  flex: none;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  padding: 10px 16px;
  background: var(--c-bg-sunken);
  border-top: 1px solid var(--c-border-soft);
}
</style>
