<script setup lang="ts">
/** 右键菜单。位置自动避让窗口边缘，点击别处或按 Esc 关闭。 */
import { computed, onMounted, onBeforeUnmount, ref, nextTick, watch } from 'vue'
import Icon from './Icon.vue'

export interface MenuItem {
  key: string
  label?: string
  icon?: string
  separator?: boolean
  disabled?: boolean
  danger?: boolean
  shortcut?: string
  children?: MenuItem[]
}

const props = defineProps<{ x: number; y: number; items: MenuItem[] }>()
const emit = defineEmits<{ (e: 'select', key: string): void; (e: 'close'): void }>()

const root = ref<HTMLElement | null>(null)
const pos = ref({ x: props.x, y: props.y })
const openSub = ref<string>('')

async function place() {
  await nextTick()
  const el = root.value
  if (!el) return
  const r = el.getBoundingClientRect()
  let x = props.x
  let y = props.y
  if (x + r.width > window.innerWidth - 8) x = Math.max(8, window.innerWidth - r.width - 8)
  if (y + r.height > window.innerHeight - 8) y = Math.max(8, window.innerHeight - r.height - 8)
  pos.value = { x, y }
}

watch(() => [props.x, props.y], place)

function onDocMouseDown(e: MouseEvent) {
  if (root.value && !root.value.contains(e.target as Node)) emit('close')
}
function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
}

onMounted(() => {
  place()
  // 用捕获阶段，避免被下层元素的 stopPropagation 挡住。
  document.addEventListener('mousedown', onDocMouseDown, true)
  document.addEventListener('keydown', onKey, true)
})
onBeforeUnmount(() => {
  document.removeEventListener('mousedown', onDocMouseDown, true)
  document.removeEventListener('keydown', onKey, true)
})

function pick(item: MenuItem) {
  if (item.disabled || item.separator || item.children) return
  emit('select', item.key)
  emit('close')
}

const visibleItems = computed(() => props.items.filter((i) => i.label || i.separator))
</script>

<template>
  <div ref="root" class="ctx" :style="{ left: pos.x + 'px', top: pos.y + 'px' }">
    <template v-for="item in visibleItems" :key="item.key">
      <div v-if="item.separator" class="ctx-sep"></div>
      <div
        v-else
        class="ctx-item"
        :class="{ 'is-disabled': item.disabled, 'is-danger': item.danger }"
        @mouseenter="openSub = item.children ? item.key : ''"
        @click="pick(item)"
      >
        <span class="ctx-icon">
          <Icon v-if="item.icon" :name="item.icon" :size="13" />
        </span>
        <span class="ctx-label">{{ item.label }}</span>
        <span v-if="item.shortcut" class="ctx-shortcut">{{ item.shortcut }}</span>
        <Icon v-if="item.children" name="chevronRight" :size="12" class="ctx-arrow" />

        <div v-if="item.children && openSub === item.key" class="ctx ctx-sub">
          <template v-for="sub in item.children" :key="sub.key">
            <div v-if="sub.separator" class="ctx-sep"></div>
            <div
              v-else
              class="ctx-item"
              :class="{ 'is-disabled': sub.disabled, 'is-danger': sub.danger }"
              @click.stop="pick(sub)"
            >
              <span class="ctx-icon">
                <Icon v-if="sub.icon" :name="sub.icon" :size="13" />
              </span>
              <span class="ctx-label">{{ sub.label }}</span>
              <span v-if="sub.shortcut" class="ctx-shortcut">{{ sub.shortcut }}</span>
            </div>
          </template>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.ctx {
  position: fixed;
  z-index: 200;
  min-width: 190px;
  padding: 4px;
  background: var(--c-bg);
  border: 1px solid var(--c-border);
  border-radius: var(--radius);
  box-shadow: var(--shadow-popup);
}

.ctx-sub {
  position: absolute;
  left: 100%;
  top: -5px;
  margin-left: 1px;
}

.ctx-item {
  position: relative;
  display: flex;
  align-items: center;
  gap: 7px;
  height: 24px;
  padding: 0 8px;
  border-radius: var(--radius-sm);
  white-space: nowrap;
}
.ctx-item:hover:not(.is-disabled) {
  background: var(--c-accent);
  color: #fff;
}
.ctx-item:hover:not(.is-disabled) .ctx-shortcut {
  color: rgba(255, 255, 255, 0.75);
}
.ctx-item.is-disabled {
  color: var(--c-text-tertiary);
}
.ctx-item.is-danger {
  color: var(--c-danger);
}
.ctx-item.is-danger:hover {
  background: var(--c-danger);
  color: #fff;
}

.ctx-icon {
  display: flex;
  width: 14px;
  justify-content: center;
  opacity: 0.85;
}

.ctx-label {
  flex: 1;
}

.ctx-shortcut {
  font-size: var(--font-size-sm);
  color: var(--c-text-tertiary);
}

.ctx-arrow {
  opacity: 0.6;
}

.ctx-sep {
  height: 1px;
  margin: 4px 6px;
  background: var(--c-border-soft);
}
</style>
