<script setup lang="ts">
/**
 * 底部状态栏。
 *
 * 右侧常驻内存与溢出指示：一个数据库客户端占了多少内存、
 * 有没有把数据压回磁盘，用户应该随时看得见，而不是等到系统卡了才发现。
 */
import { computed, onMounted, onBeforeUnmount } from 'vue'
import { useAppStore } from '../../stores/app'
import { useConnectionsStore } from '../../stores/connections'
import { useTabsStore } from '../../stores/tabs'
import Icon from '../common/Icon.vue'
import { formatBytes } from '../grid/cell'
import { tr } from '../../i18n'

const app = useAppStore()
const conns = useConnectionsStore()
const tabs = useTabsStore()

const emit = defineEmits<{ (e: 'action', action: string): void }>()

const props = defineProps<{ message?: string }>()

let timer: number | undefined
onMounted(() => {
  app.refreshStats()
  // 两秒一次足够反映趋势，又不会因为频繁读取 MemStats 影响性能。
  timer = window.setInterval(() => app.refreshStats(), 2000)
})
onBeforeUnmount(() => {
  if (timer) window.clearInterval(timer)
})

const activeConn = computed(() => {
  const t = tabs.active
  if (!t) return null
  return conns.stateOf(t.connId)
})

const stats = computed(() => app.stats)

const memoryText = computed(() => {
  const s = stats.value
  if (!s) return '—'
  return `${s.processHeapMb.toFixed(0)} MB`
})

/** 内存水位：结果集占用相对上限的比例。 */
const pressure = computed(() => {
  const s = stats.value?.resultSets
  if (!s || !s.memoryLimit) return 0
  return Math.min(1, s.memoryBytes / s.memoryLimit)
})

const pressureClass = computed(() => {
  const p = pressure.value
  if (p > 0.85) return 'is-high'
  if (p > 0.6) return 'is-mid'
  return ''
})

const themeIcon = computed(() => {
  if (app.settings.theme === 'system') return 'auto'
  return app.settings.theme === 'dark' ? 'moon' : 'sun'
})

const themeTitle = computed(() => {
  const cur = { system: tr('跟随系统'), light: tr('浅色'), dark: tr('深色') }[app.settings.theme]
  return tr('外观：{name}（点击切换）', { name: cur })
})

const spillText = computed(() => {
  const s = stats.value?.resultSets
  if (!s || !s.spillBytes) return ''
  return formatBytes(s.spillBytes)
})
</script>

<template>
  <div class="statusbar">
    <div class="status-left">
      <template v-if="activeConn">
        <span class="status-item">
          <Icon name="server" :size="11" />
          {{ activeConn.config.name }}
        </span>
        <span v-if="tabs.active?.database" class="status-item">
          <Icon name="database" :size="11" />
          {{ tabs.active.database }}
        </span>
        <span v-if="activeConn.serverInfo?.version" class="status-item muted">
          {{ activeConn.serverInfo.version.split(' ')[0] }}
        </span>
        <span v-if="activeConn.config.readOnly" class="badge badge-readonly">{{ tr('只读') }}</span>
      </template>
      <span v-else class="status-item muted">{{ tr('未选择连接') }}</span>
    </div>

    <div class="status-center">
      <span v-if="props.message" class="status-message">{{ props.message }}</span>
    </div>

    <div class="status-right">
      <button
        v-if="stats"
        class="status-btn"
        :class="pressureClass"
        :title="tr('点击查看资源监视器')"
        @click="emit('action', 'resource-monitor')"
      >
        <Icon name="memory" :size="11" />
        <span>{{ memoryText }}</span>
        <span class="gauge">
          <span class="gauge-fill" :style="{ width: pressure * 100 + '%' }"></span>
        </span>
      </button>
      <span
        v-if="spillText"
        class="status-item"
        :title="tr('结果集已溢出到磁盘 {size}，内存因此保持在预算内', { size: spillText })"
      >
        <Icon name="download" :size="11" />
        {{ tr('溢出 {size}', { size: spillText }) }}
      </span>
      <span v-if="stats?.resultSets.openResultSets" class="status-item muted">
        {{ tr('{n} 个结果集', { n: stats.resultSets.openResultSets }) }}
      </span>
      <button class="status-btn" :title="themeTitle" @click="app.cycleTheme()">
        <Icon :name="themeIcon" :size="12" />
      </button>
    </div>
  </div>
</template>

<style scoped>
.statusbar {
  flex: none;
  display: flex;
  align-items: center;
  height: var(--size-statusbar);
  padding: 0 var(--sp-3);
  gap: var(--sp-4);
  background: var(--c-surface);
  border-top: 1px solid var(--c-border-soft);
  font-size: var(--font-size-sm);
  color: var(--c-text-secondary);
}

.status-left,
.status-right {
  display: flex;
  align-items: center;
  gap: var(--sp-4);
}

.status-center {
  flex: 1;
  min-width: 0;
  text-align: center;
}

.status-message {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.status-item {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  white-space: nowrap;
}

.status-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 17px;
  padding: 0 6px;
  border: none;
  border-radius: 3px;
  background: transparent;
  color: inherit;
  font-size: inherit;
}
.status-btn:hover {
  background: var(--c-chrome-active);
}
.status-btn.is-mid {
  color: var(--c-warning);
}
.status-btn.is-high {
  color: var(--c-danger);
}

.gauge {
  display: inline-block;
  width: 36px;
  height: 4px;
  border-radius: 2px;
  background: var(--c-border);
  overflow: hidden;
}
.gauge-fill {
  display: block;
  height: 100%;
  background: currentColor;
  transition: width 0.3s ease;
}
</style>
