<script setup lang="ts">
/** 通用输入 / 确认对话框：重命名、新建库、危险操作确认都用它。 */
import { nextTick, onMounted, ref } from 'vue'
import Modal from '../common/Modal.vue'
import Icon from '../common/Icon.vue'
import { tr } from '../../i18n'

const props = withDefaults(
  defineProps<{
    title: string
    message?: string
    label?: string
    value?: string
    placeholder?: string
    confirmLabel?: string
    danger?: boolean
    /** 不传 label 时只作确认框，不显示输入框。 */
    input?: boolean
    /** 要求用户逐字输入这个词才能确认，用于不可逆操作。 */
    requireTyping?: string
  }>(),
  { confirmLabel: tr('确定'), danger: false, input: true },
)

const emit = defineEmits<{ (e: 'confirm', value: string): void; (e: 'close'): void }>()

const text = ref(props.value ?? '')
const typed = ref('')
const field = ref<HTMLInputElement | null>(null)

onMounted(() => nextTick(() => field.value?.select()))

const canConfirm = () => {
  if (props.requireTyping) return typed.value === props.requireTyping
  if (props.input) return text.value.trim().length > 0
  return true
}

function confirm() {
  if (!canConfirm()) return
  emit('confirm', text.value.trim())
}
</script>

<template>
  <Modal :title="title" :width="440" @close="emit('close')">
    <div v-if="message" class="message" :class="{ 'is-danger': danger }">
      <Icon v-if="danger" name="warning" :size="15" />
      <span>{{ message }}</span>
    </div>

    <div v-if="input" class="field">
      <label>{{ label ?? tr('名称') }}</label>
      <input
        ref="field"
        v-model="text"
        class="input"
        :placeholder="placeholder"
        @keydown.enter="confirm"
      />
    </div>

    <template v-if="requireTyping">
      <div class="field">
        <label>{{ tr('确认') }}</label>
        <input
          v-model="typed"
          class="input mono"
          :placeholder="tr('输入 {text} 以确认', { text: requireTyping })"
          @keydown.enter="confirm"
        />
      </div>
      <div class="field-hint">{{ tr('此操作不可撤销') }}</div>
    </template>

    <template #footer>
      <button class="btn" @click="emit('close')">{{ tr('取消') }}</button>
      <button
        class="btn"
        :class="danger ? 'btn-danger' : 'btn-primary'"
        :disabled="!canConfirm()"
        @click="confirm"
      >
        {{ confirmLabel }}
      </button>
    </template>
  </Modal>
</template>

<style scoped>
.message {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  margin-bottom: 12px;
  line-height: 1.6;
}
.message.is-danger {
  padding: 9px 11px;
  border-radius: var(--radius-sm);
  background: var(--c-danger-soft);
  color: var(--c-danger);
}
</style>
