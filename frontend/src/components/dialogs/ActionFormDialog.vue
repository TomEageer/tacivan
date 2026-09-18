<script setup lang="ts">
/** 插件动作的参数表单：字段由插件声明，这里只负责渲染和收集。 */
import { ref } from 'vue'
import Modal from '../common/Modal.vue'
import { tr } from '../../i18n'
import type { PluginField } from '../../types'

const props = defineProps<{ title: string; fields: PluginField[]; danger?: boolean }>()
const emit = defineEmits<{ (e: 'submit', params: Record<string, string>): void; (e: 'close'): void }>()

const values = ref<Record<string, string>>(
  Object.fromEntries(props.fields.map((f) => [f.key, f.default ?? ''])),
)
function submit() {
  // type="number" 的输入框会被 v-model 自动转成 Number；插件那头只认字符串，
  // 校验和提交都先统一成字符串，否则 .trim() 在数字上直接抛错，按钮看起来就是死的。
  const out: Record<string, string> = {}
  for (const f of props.fields) {
    const v = String(values.value[f.key] ?? '').trim()
    if (f.required && !v) return
    out[f.key] = v
  }
  emit('submit', out)
}
</script>

<template>
  <Modal :title="title" :width="480" @close="emit('close')">
    <div v-for="f in fields" :key="f.key" class="field">
      <label>{{ f.label }}</label>
      <input
        v-if="f.type === 'text' || f.type === 'password' || f.type === 'number'"
        v-model="values[f.key]"
        class="input"
        :type="f.type"
        :placeholder="f.placeholder"
        autocomplete="off"
        @keydown.enter.prevent="submit"
      />
      <textarea
        v-else-if="f.type === 'textarea'"
        v-model="values[f.key]"
        class="input"
        :placeholder="f.placeholder"
      ></textarea>
      <select v-else-if="f.type === 'select'" v-model="values[f.key]" class="select">
        <option v-for="o in f.options ?? []" :key="o" :value="o">{{ o }}</option>
      </select>
      <label v-else-if="f.type === 'bool'" class="checkbox">
        <input
          type="checkbox"
          :checked="values[f.key] === 'true'"
          @change="values[f.key] = ($event.target as HTMLInputElement).checked ? 'true' : ''"
        />
      </label>
      <div v-if="f.hint" class="field-hint">{{ f.hint }}</div>
    </div>
    <template #footer>
      <span class="spacer"></span>
      <button class="btn" @click="emit('close')">{{ tr('取消') }}</button>
      <button class="btn" :class="danger ? 'danger' : 'btn-primary'" @click="submit">{{ tr('确定') }}</button>
    </template>
  </Modal>
</template>
