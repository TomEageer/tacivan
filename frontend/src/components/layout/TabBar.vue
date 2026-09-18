<script setup lang="ts">
/** 标签页栏。中键点击关闭，右键出菜单，可横向滚动。 */
import { computed, ref } from 'vue'
import { useTabsStore, type Tab } from '../../stores/tabs'
import Icon from '../common/Icon.vue'
import ContextMenu, { type MenuItem } from '../common/ContextMenu.vue'
import { tr } from '../../i18n'

const tabs = useTabsStore()

const ICONS: Record<string, string> = {
  table: 'grid',
  query: 'sql',
  designer: 'table',
  redis: 'layers',
  ddl: 'code',
  monitor: 'chart',
}

const menu = ref<{ x: number; y: number; tab: Tab } | null>(null)

function onContextMenu(tab: Tab, e: MouseEvent) {
  e.preventDefault()
  menu.value = { x: e.clientX, y: e.clientY, tab }
}

const menuItems = computed<MenuItem[]>(() => [
  { key: 'close', label: tr('关闭标签页'), icon: 'close' },
  { key: 'close-others', label: tr('关闭其他标签页') },
  { key: 'close-all', label: tr('关闭全部标签页') },
])

function onMenuSelect(key: string) {
  const t = menu.value?.tab
  if (!t) return
  if (key === 'close') tabs.close(t.id)
  else if (key === 'close-others') tabs.closeOthers(t.id)
  else if (key === 'close-all') tabs.closeAll()
  menu.value = null
}

function onMouseDown(tab: Tab, e: MouseEvent) {
  // 中键关闭，与浏览器一致。
  if (e.button === 1) {
    e.preventDefault()
    tabs.close(tab.id)
  }
}
</script>

<template>
  <div v-if="tabs.tabs.length" class="tabbar">
    <div
      v-for="tab in tabs.tabs"
      :key="tab.id"
      class="tab"
      :class="{ 'is-active': tab.id === tabs.activeId }"
      :title="`${tab.connName}${tab.database ? ' / ' + tab.database : ''}`"
      @click="tabs.activate(tab.id)"
      @mousedown="onMouseDown(tab, $event)"
      @contextmenu="onContextMenu(tab, $event)"
    >
      <Icon :name="ICONS[tab.type] ?? 'file'" :size="12" class="tab-icon" />
      <span class="tab-title">{{ tab.title }}</span>
      <span v-if="tab.dirty" class="tab-dirty" :title="tr('有未保存的更改')"></span>
      <button class="tab-close" :title="tr('关闭')" @click.stop="tabs.close(tab.id)">
        <Icon name="close" :size="9" :stroke-width="1.8" />
      </button>
    </div>

    <ContextMenu
      v-if="menu"
      :x="menu.x"
      :y="menu.y"
      :items="menuItems"
      @select="onMenuSelect"
      @close="menu = null"
    />
  </div>
</template>

<style scoped>
.tabbar {
  flex: none;
  display: flex;
  align-items: stretch;
  height: var(--size-tabbar);
  background: var(--c-surface);
  border-bottom: 1px solid var(--c-border-soft);
  overflow-x: auto;
  overflow-y: hidden;
}
.tabbar::-webkit-scrollbar {
  height: 0;
}

.tab {
  position: relative;
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  min-width: 112px;
  max-width: 230px;
  padding: 0 var(--sp-2) 0 var(--sp-4);
  border-right: 1px solid var(--c-border-soft);
  color: var(--c-text-secondary);
  white-space: nowrap;
  transition: background 0.12s ease, color 0.12s ease;
}
.tab:hover {
  background: var(--c-chrome-active);
}
.tab.is-active {
  background: var(--c-canvas);
  color: var(--c-text);
  font-weight: 500;
  /* 顶部一条强调线标出当前页，与底部内容区连成一体。 */
  box-shadow: inset 0 2px 0 var(--c-accent);
}

.tab-icon {
  flex: none;
  opacity: 0.8;
}

.tab-title {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
}

.tab-dirty {
  flex: none;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--c-accent);
}

.tab-close {
  display: flex;
  align-items: center;
  justify-content: center;
  flex: none;
  width: 16px;
  height: 16px;
  padding: 0;
  border: none;
  border-radius: 3px;
  background: transparent;
  color: var(--c-text-tertiary);
  opacity: 0;
}
.tab:hover .tab-close,
.tab.is-active .tab-close {
  opacity: 1;
}
.tab-close:hover {
  background: var(--c-border);
  color: var(--c-text);
}
</style>
