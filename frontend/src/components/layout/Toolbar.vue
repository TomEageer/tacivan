<script setup lang="ts">
/**
 * 顶栏：标题行 + 工具行，共用一块背景。
 *
 * 系统标题栏已经隐藏（TitleBarHiddenInset），窗口最顶端就是这里，
 * 所以标题行要自己承担两件原本归系统管的事：给红绿灯让位、显示软件名。
 */
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import {
  WindowGetPosition,
  WindowGetSize,
  WindowIsFullscreen,
  WindowMaximise,
  WindowMinimise,
  WindowSetPosition,
  WindowSetSize,
} from '../../../wailsjs/runtime/runtime'
import * as api from '../../api'
import { useConnectionsStore } from '../../stores/connections'
import { useTabsStore } from '../../stores/tabs'
import Icon from '../common/Icon.vue'
import { tr } from '../../i18n'

const conns = useConnectionsStore()
const tabs = useTabsStore()

const emit = defineEmits<{ (e: 'action', action: string): void }>()

/**
 * 全屏时 macOS 会把红绿灯收起来，左侧那段预留就成了一块空白，
 * 所以要跟着全屏状态把它收回去。
 */
const fullscreen = ref(false)
async function syncFullscreen() {
  try {
    fullscreen.value = await WindowIsFullscreen()
  } catch {
    // 非 Wails 环境（浏览器里调试）拿不到，按非全屏处理。
  }
}
onMounted(() => {
  syncFullscreen()
  window.addEventListener('resize', syncFullscreen)
})
onBeforeUnmount(() => window.removeEventListener('resize', syncFullscreen))

/**
 * 双击标题行 = 系统里设定的动作（默认缩放/填满屏幕）。
 *
 * Wails 的拖拽是在 mousemove 才真正开始的，原地双击不会触发拖拽，
 * 所以这里能收到 dblclick。偏好只读一次，它极少变。
 */
let dblAction = 'Maximize'
api.TitleBarDoubleClickAction().then((v) => (dblAction = v || 'Maximize')).catch(() => {})

/**
 * 自己做「缩放」的往返：Wails 的 ToggleMaximise 在 macOS 上认不出自己放大后的状态，
 * 第二次双击不会缩回去。这里记住放大前的位置和尺寸，再双击就还原。
 */
let savedFrame: { x: number; y: number; w: number; h: number } | null = null
async function zoomToggle() {
  if (savedFrame) {
    const f = savedFrame
    savedFrame = null
    WindowSetSize(f.w, f.h)
    WindowSetPosition(f.x, f.y)
    return
  }
  // 先记再放大。放大是异步落到原生层的，放大后立刻读尺寸拿到的还是旧值，
  // 所以不能靠"放大前后尺寸有没有变"来决定要不要留还原点。
  const [pos, size] = await Promise.all([WindowGetPosition(), WindowGetSize()])
  savedFrame = { x: pos.x, y: pos.y, w: size.w, h: size.h }
  WindowMaximise()
}
function onTitleDblClick(e: MouseEvent) {
  if ((e.target as HTMLElement).closest('button')) return
  switch (dblAction) {
    case 'Minimize':
      WindowMinimise()
      break
    case 'None':
      break
    default:
      void zoomToggle()
  }
}

const node = computed(() => conns.selectedNode)
const connOpen = computed(() => !!node.value && conns.isOpen(node.value.connId))
/** 选中的是表/视图时才允许「打开表」「设计表」。 */
const tableSelected = computed(
  () => node.value?.kind === 'object' && node.value.objectKind === 'table',
)
const tableLikeSelected = computed(
  () =>
    node.value?.kind === 'object' &&
    (node.value.objectKind === 'table' || node.value.objectKind === 'view'),
)
/** 标题行右侧跟一句当前上下文，和 Navicat 的标题栏同义。 */
const context = computed(() => {
  const t = tabs.active
  if (!t) return ''
  return [t.connName, t.database, t.title].filter(Boolean).join(' — ')
})

const canQuery = computed(() => {
  if (!node.value || !connOpen.value) return false
  return node.value.engine !== 'redis'
})

const items = computed(() => [
  { key: 'new-connection', label: tr('连接'), icon: 'link', enabled: true },
  { key: 'new-query', label: tr('新建查询'), icon: 'sql', enabled: canQuery.value },
  { key: 'divider1', divider: true },
  { key: 'open-table', label: tr('表数据'), icon: 'grid', enabled: tableLikeSelected.value },
  { key: 'design-table', label: tr('设计表'), icon: 'table', enabled: tableSelected.value },
  { key: 'new-table', label: tr('新建表'), icon: 'plus', enabled: connOpen.value && canQuery.value },
  { key: 'divider2', divider: true },
  { key: 'export', label: tr('导出'), icon: 'download', enabled: !!tabs.active },
  { key: 'refresh', label: tr('刷新'), icon: 'refresh', enabled: connOpen.value },
])
</script>

<template>
  <div class="topbar" :class="{ 'is-fullscreen': fullscreen }">
    <!-- 标题行：左侧留给红绿灯，整行可拖拽 -->
    <div class="titlebar" @dblclick="onTitleDblClick">
      <span class="app-name">Tacivan</span>
      <span v-if="context" class="app-context">{{ context }}</span>
    </div>

    <div class="toolbar" @dblclick="onTitleDblClick">
      <div class="toolbar-items">
      <template v-for="item in items" :key="item.key">
        <div v-if="item.divider" class="toolbar-divider"></div>
        <button
          v-else
          class="toolbar-btn"
          :disabled="!item.enabled"
          :title="item.label"
          @click="emit('action', item.key)"
        >
          <Icon :name="item.icon!" :size="18" :stroke-width="1.3" />
          <span>{{ item.label }}</span>
        </button>
      </template>
      </div>
      <div class="toolbar-right">
      <button
        class="toolbar-btn is-planned"
        :title="tr('AI 助手（规划中）')"
        @click="emit('action', 'ai')"
      >
        <Icon name="sparkle" :size="18" :stroke-width="1.3" />
        <span>AI</span>
      </button>
      <div class="toolbar-divider"></div>
      <button class="toolbar-btn" :title="tr('资源监视器')" @click="emit('action', 'resource-monitor')">
        <Icon name="memory" :size="18" :stroke-width="1.3" />
        <span>{{ tr('监视器') }}</span>
      </button>
      <button class="toolbar-btn" :title="tr('查询历史')" @click="emit('action', 'history')">
        <Icon name="history" :size="18" :stroke-width="1.3" />
        <span>{{ tr('历史') }}</span>
      </button>
        <button class="toolbar-btn" :title="tr('设置')" @click="emit('action', 'settings')">
          <Icon name="settings" :size="18" :stroke-width="1.3" />
          <span>{{ tr('设置') }}</span>
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
/*
 * 两行共用一块背景，中间不画任何分隔——顶端略亮、底端正好落在 surface 上，
 * 于是它和紧挨着的侧栏、标签栏是同一个颜色，接缝看不出来。
 */
.topbar {
  position: relative;
  flex: none;
  background: linear-gradient(var(--c-chrome-top), var(--c-surface));
  border-bottom: 1px solid var(--c-border-soft);
  /* 顶栏整体兼任窗口拖拽区，按钮区再单独排除 */
  --wails-draggable: drag;
}

.titlebar {
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  height: 28px;
  /* 左侧这一段是红绿灯的位置（三颗到 x=72 为止），全屏时它们收起，预留也跟着撤掉 */
  padding: 0 var(--sp-3) 0 80px;
  min-width: 0;
}
.topbar.is-fullscreen .titlebar {
  padding-left: var(--sp-3);
}

.app-name {
  flex: none;
  font-size: var(--font-size);
  font-weight: 600;
  color: var(--c-text);
  letter-spacing: 0.01em;
}

.app-context {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--font-size-sm);
  color: var(--c-text-tertiary);
}
.app-context::before {
  content: "";
  display: inline-block;
  width: 1px;
  height: 10px;
  margin-right: var(--sp-2);
  vertical-align: -1px;
  background: var(--c-border);
}

.toolbar {
  display: flex;
  align-items: flex-end;
  height: var(--size-toolbar);
  padding: 0 var(--sp-3) var(--sp-2);
}

.toolbar-items,
.toolbar-right {
  display: flex;
  align-items: flex-end;
  gap: 1px;
  /* 按钮区不参与拖拽，否则点不动 */
  --wails-draggable: no-drag;
}

.toolbar-right {
  margin-left: auto;
}

.toolbar-btn {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 3px;
  min-width: 54px;
  padding: var(--sp-1) var(--sp-2) 3px;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--c-text-secondary);
  font-size: var(--font-size-sm);
  letter-spacing: 0;
  white-space: nowrap;
  transition: background 0.12s ease, color 0.12s ease;
}
.toolbar-btn:hover:not(:disabled) {
  color: var(--c-text);
}
.toolbar-btn:hover:not(:disabled) {
  background: var(--c-chrome-active);
}
.toolbar-btn:active:not(:disabled) {
  background: var(--c-border);
}
.toolbar-btn:disabled {
  color: var(--c-text-tertiary);
  opacity: 0.55;
}

/* 规划中的入口：正常可点（点开说明），但视觉上弱一档，
   不跟已经能用的功能抢注意力。 */
.toolbar-btn.is-planned {
  color: var(--c-text-secondary);
}
.toolbar-btn.is-planned:hover {
  color: var(--c-accent);
}

.toolbar-divider {
  width: 1px;
  height: 24px;
  margin: 0 var(--sp-2) var(--sp-2);
  background: var(--c-border);
}
</style>
