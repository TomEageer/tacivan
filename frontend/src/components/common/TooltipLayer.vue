<script setup lang="ts">
/** 全局提示层，挂在应用根部，由 useTooltip 驱动。 */
import { computed, nextTick, ref, watch } from 'vue'
import { tipState } from '../../composables/useTooltip'

const el = ref<HTMLElement | null>(null)
const pos = ref({ x: 0, y: 0 })

// 出现后按实际尺寸避让窗口边缘：宽度取决于内容，事先算不准。
watch(
  () => [tipState.visible, tipState.content] as const,
  async () => {
    if (!tipState.visible) return
    await nextTick()
    const node = el.value
    if (!node) return
    const r = node.getBoundingClientRect()
    let x = tipState.x
    let y = tipState.y
    if (x + r.width > window.innerWidth - 8) x = Math.max(8, window.innerWidth - r.width - 8)
    // 下方放不下就翻到上方
    if (y + r.height > window.innerHeight - 8) y = Math.max(8, tipState.anchor.top - r.height - 6)
    pos.value = { x, y }
  },
  { deep: true },
)

const c = computed(() => tipState.content)
</script>

<template>
  <div
    v-if="tipState.visible"
    ref="el"
    class="tip"
    :style="{ left: pos.x + 'px', top: pos.y + 'px' }"
  >
    <div v-if="c.title || c.tags?.length" class="tip-head">
      <span v-if="c.title" class="tip-title mono">{{ c.title }}</span>
      <span v-for="(t, i) in c.tags" :key="i" class="tip-tag" :class="t.tone ? `is-${t.tone}` : ''">
        {{ t.text }}
      </span>
    </div>

    <div v-if="c.rows?.length" class="tip-rows">
      <div v-for="(r, i) in c.rows" :key="i" class="tip-row">
        <span class="tip-label">{{ r.label }}</span>
        <span class="tip-value" :class="[r.mono ? 'mono' : '', r.tone ? `is-${r.tone}` : '']">
          {{ r.value }}
        </span>
      </div>
    </div>

    <div v-if="c.note" class="tip-note">{{ c.note }}</div>
  </div>
</template>

<style scoped>
.tip {
  position: fixed;
  z-index: 500;
  max-width: 380px;
  padding: var(--sp-2) var(--sp-3);
  border: 1px solid var(--c-border);
  border-radius: var(--radius);
  background: var(--c-raised);
  box-shadow: var(--shadow-popup);
  font-size: var(--font-size-sm);
  line-height: 1.5;
  pointer-events: none;
  animation: tip-in 0.1s ease-out;
}

@keyframes tip-in {
  from {
    opacity: 0;
    transform: translateY(-2px);
  }
}

.tip-head {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--sp-1);
  margin-bottom: var(--sp-1);
}

.tip-title {
  font-size: var(--font-size);
  font-weight: 600;
  color: var(--c-text);
}

.tip-tag {
  padding: 1px 5px;
  border-radius: 3px;
  background: var(--c-chrome-active);
  color: var(--c-text-secondary);
  font-size: var(--font-size-tag);
  line-height: 1.4;
}
.tip-tag.is-meta {
  background: var(--c-meta-soft);
  color: var(--c-meta);
}
.tip-tag.is-warning {
  background: var(--c-warning-soft);
  color: var(--c-warning);
}
.tip-tag.is-danger {
  background: var(--c-danger-soft);
  color: var(--c-danger);
}
.tip-tag.is-success {
  background: var(--c-success-soft);
  color: var(--c-success);
}

.tip-rows {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 1px var(--sp-3);
}

.tip-row {
  display: contents;
}

.tip-label {
  color: var(--c-text-tertiary);
  white-space: nowrap;
}

.tip-value {
  color: var(--c-text);
  word-break: break-word;
}
.tip-value.is-meta {
  color: var(--c-meta);
}
.tip-value.is-warning {
  color: var(--c-warning);
}
.tip-value.is-danger {
  color: var(--c-danger);
}
.tip-value.is-success {
  color: var(--c-success);
}

.tip-note {
  margin-top: var(--sp-2);
  padding-top: var(--sp-2);
  border-top: 1px solid var(--c-border-soft);
  color: var(--c-text-secondary);
  white-space: pre-wrap;
  word-break: break-word;
}
</style>
