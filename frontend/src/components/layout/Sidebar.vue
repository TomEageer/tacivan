<script setup lang="ts">
/**
 * 左侧对象树。
 *
 * 节点扁平化后做虚拟滚动：一个库里有几千张表时，展开也是瞬时的。
 */
import { computed, onBeforeUnmount, ref, shallowRef } from 'vue'
import * as api from '../../api'
import { useAppStore } from '../../stores/app'
import { useConnectionsStore, type TreeNode } from '../../stores/connections'
import Icon from '../common/Icon.vue'
import ActionFormDialog from '../dialogs/ActionFormDialog.vue'
import McpToolDialog from '../dialogs/McpToolDialog.vue'
import type { PluginAction, PluginNode } from '../../types'
import { logoFor } from '../common/engineLogos'
import ContextMenu, { type MenuItem } from '../common/ContextMenu.vue'
import { tr } from '../../i18n'

const conns = useConnectionsStore()
const app = useAppStore()

const emit = defineEmits<{
  (e: 'action', action: string, node: TreeNode | null): void
}>()

const ROW_H = 24
const OVERSCAN = 8

const scroller = ref<HTMLElement | null>(null)
const scrollTop = ref(0)
const viewportH = ref(600)

const nodes = computed(() => conns.visibleNodes)

const range = computed(() => {
  const start = Math.max(0, Math.floor(scrollTop.value / ROW_H) - OVERSCAN)
  const count = Math.ceil(viewportH.value / ROW_H) + OVERSCAN * 2
  return { start, end: Math.min(nodes.value.length, start + count) }
})

const rendered = computed(() => {
  const { start, end } = range.value
  const skip = drawerIds.value
  const out: { node: TreeNode; index: number }[] = []
  for (let i = start; i < end; i++) {
    const node = nodes.value[i]
    if (!node || skip.has(node.id)) continue
    out.push({ node, index: i })
  }
  return out
})

function onScroll(e: Event) {
  const el = e.target as HTMLElement
  scrollTop.value = el.scrollTop
  viewportH.value = el.clientHeight
}

function iconFor(node: TreeNode): string {
  switch (node.kind) {
    case 'group':
      return conns.expanded.has(node.id) ? 'folderOpen' : 'folder'
    case 'connection': {
      const logo = logoFor(node.engine)
      return logo ? 'logo:' + logo : 'server'
    }
    case 'database':
      return 'database'
    case 'schema':
      return 'layers'
    case 'folder':
      return conns.expanded.has(node.id) ? 'folderOpen' : 'folder'
    case 'object':
      if (node.objectKind === 'tool') return 'sparkle'
      if (node.objectKind === 'resource') return 'file'
      return node.objectKind ?? 'table'
  }
  return 'table'
}

function engineColor(node: TreeNode) {
  return `var(--c-engine-${node.engine === 'mariadb' ? 'mariadb' : node.engine})`
}

function onRowClick(node: TreeNode) {
  conns.select(node.id)
}

/** 连上离线节点所属的连接。 */
async function connectNode(node: TreeNode) {
  await conns.openConnection(node.connId)
}

/**
 * 展开 / 收起的抽屉动画。
 *
 * 两个要点：
 *
 * 1. 视觉上是真的「卷进去」——子树被放进一个 overflow:hidden 的容器，
 *    容器高度收到 0，节点是被裁掉的，不是淡出消失的。
 *
 * 2. 可打断。之前用 setTimeout 播一段固定时长的动画，快速连点时定时器互相覆盖、
 *    状态错乱，而且动画播到一半改主意它也不理你。这里改成指数逼近：
 *    每帧朝目标高度靠近一个比例，目标随时可改，当前高度就从现在的位置平滑掉头。
 */
interface Drawer {
  parentId: string
  nodes: TreeNode[]
  /** 容器在树里的起始 y。 */
  top: number
  /** 完全展开时的高度。 */
  full: number
  /** 这些节点此刻是否还在 visibleNodes 里：展开时在，折叠时已被移除。 */
  inList: boolean
}

/** 参与动画的最大行数：折叠上千行的子树时，没必要真去动几万像素，
    那样滚动条会剧烈变形，观感反而更差。 */
const MAX_ANIM_ROWS = 40

const drawer = shallowRef<Drawer | null>(null)
const drawerH = ref(0)
let drawerTarget = 0
let rafId = 0

function stepDrawer() {
  const diff = drawerTarget - drawerH.value
  if (Math.abs(diff) < 0.5) {
    drawerH.value = drawerTarget
    rafId = 0
    // 收完了就把抽屉撤掉；展开完成则保留由正常列表接管。
    if (drawerTarget === 0) drawer.value = null
    else drawer.value = null
    return
  }
  // 越接近目标越慢，收尾自然；任何时候改 target 都能平滑转向。
  drawerH.value += diff * 0.26
  rafId = requestAnimationFrame(stepDrawer)
}

function runDrawer(target: number) {
  drawerTarget = target
  if (!rafId) rafId = requestAnimationFrame(stepDrawer)
}

onBeforeUnmount(() => {
  if (rafId) cancelAnimationFrame(rafId)
})

/** 某个节点在当前可见列表里的后代：紧随其后、层级更深的那一段。 */
function descendantsOf(index: number): TreeNode[] {
  const list = nodes.value
  const base = list[index]?.level ?? 0
  const out: TreeNode[] = []
  for (let i = index + 1; i < list.length; i++) {
    if (list[i].level <= base) break
    out.push(list[i])
  }
  return out
}

async function animatedToggle(node: TreeNode) {
  if (!node.expandable) return
  const willCollapse = conns.expanded.has(node.id)
  const index = nodes.value.findIndex((n) => n.id === node.id)
  if (index < 0) {
    await conns.toggle(node)
    return
  }
  const top = (index + 1) * ROW_H
  // 在同一个节点上反复点：沿用当前高度，动画直接掉头而不是从头再来。
  const continuing = drawer.value?.parentId === node.id

  if (willCollapse) {
    const subtree = descendantsOf(index)
    await conns.toggle(node)
    if (!subtree.length) return
    const shown = subtree.slice(0, MAX_ANIM_ROWS)
    const full = shown.length * ROW_H
    if (!continuing) drawerH.value = full
    drawer.value = { parentId: node.id, nodes: shown, top, full, inList: false }
    runDrawer(0)
  } else {
    await conns.toggle(node)
    const at = nodes.value.findIndex((n) => n.id === node.id)
    const fresh = descendantsOf(at)
    if (!fresh.length) return
    const shown = fresh.slice(0, MAX_ANIM_ROWS)
    const full = shown.length * ROW_H
    if (!continuing) drawerH.value = 0
    drawer.value = { parentId: node.id, nodes: shown, top, full, inList: true }
    runDrawer(full)
  }
}

/** 抽屉里的节点不在正常列表中重复渲染。 */
const drawerIds = computed(() => new Set(drawer.value?.nodes.map((n) => n.id) ?? []))

/**
 * 抽屉带来的位移。
 *
 * 折叠时子树已不在列表里，抽屉是额外多出来的高度；
 * 展开时子树已经在列表里，抽屉替它们占位，要把它们原本占的高度减掉。
 */
const drawerOffset = computed(() => {
  const d = drawer.value
  if (!d) return 0
  return drawerH.value - (d.inList ? d.full : 0)
})

/** 抽屉之后的节点整体让位。 */
function yOf(index: number): number {
  const d = drawer.value
  const base = index * ROW_H
  if (!d) return base
  return base >= d.top ? base + drawerOffset.value : base
}

const totalHeight = computed(() => nodes.value.length * ROW_H + drawerOffset.value)

async function onRowDblClick(node: TreeNode) {
  // 收藏视图里的节点可能属于未打开的连接，先连上再执行原动作。
  if (node.offline) {
    await connectNode(node)
    if (!conns.isOpen(node.connId)) return
  }
  doRowDblClick(node)
}

const mcpDialog = ref<{ node: TreeNode } | null>(null)

function doRowDblClick(node: TreeNode) {
  if (node.kind === 'object' && (node.objectKind === 'tool' || node.objectKind === 'resource')) {
    mcpDialog.value = { node }
    return
  }
  if (node.kind === 'object') {
    // 表/视图双击直接看数据，与 Navicat 一致。
    if (node.objectKind === 'table' || node.objectKind === 'view') {
      emit('action', 'open-table', node)
    } else {
      emit('action', 'view-ddl', node)
    }
    return
  }
  if (node.kind === 'database') {
    conns.setCurrentDatabase(node.connId, node.database!)
    // Redis 的库下面没有对象树，键在键浏览器里看；双击直接开它，别对着空节点展开
    if (node.engine === 'redis') {
      emit('action', 'open-database', node)
      return
    }
  }
  toggleNode(node)
}

function onChevron(node: TreeNode, e: MouseEvent) {
  e.stopPropagation()
  toggleNode(node)
}

/** 分组的展开/收起要记下来，下次启动还是这个样子。 */
function toggleNode(node: TreeNode) {
  animatedToggle(node)
  if (node.kind === 'group') conns.toggleGroupCollapsed(node.label, !conns.expanded.has(node.id))
}

// --- 拖放：连接可以拖到别的连接前后、拖进分组、拖到空白处回到最外层；分组之间也可以拖 ---
const dragging = ref<{ kind: 'connection' | 'group'; id: string; label: string } | null>(null)
const dropHint = ref<{ id: string; place: 'before' | 'after' | 'into' } | null>(null)

function onDragStart(node: TreeNode, e: DragEvent) {
  if (node.kind !== 'connection' && node.kind !== 'group') {
    e.preventDefault()
    return
  }
  dragging.value = { kind: node.kind, id: node.kind === 'connection' ? node.connId : node.label, label: node.label }
  e.dataTransfer?.setData('text/plain', node.label)
  if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
}

function onDragOver(node: TreeNode, e: DragEvent) {
  const d = dragging.value
  if (!d) return
  // 分组只能落在分组之间；连接可以落在连接前后或分组里
  if (d.kind === 'group' && node.kind !== 'group') return (dropHint.value = null)
  if (d.kind === 'connection' && node.kind !== 'connection' && node.kind !== 'group') return (dropHint.value = null)
  e.preventDefault()
  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
  const y = e.clientY - rect.top
  let place: 'before' | 'after' | 'into'
  if (d.kind === 'connection' && node.kind === 'group') place = 'into'
  else place = y < rect.height / 2 ? 'before' : 'after'
  dropHint.value = { id: node.id, place }
}

async function onDrop(node: TreeNode | null) {
  const d = dragging.value
  const hint = dropHint.value
  dragging.value = null
  dropHint.value = null
  if (!d) return
  try {
    if (!node) {
      // 空白处：连接回到最外层
      if (d.kind === 'connection') await conns.moveConnection(d.id, '')
      return
    }
    if (!hint || hint.id !== node.id) return
    if (d.kind === 'group' && node.kind === 'group' && hint.place !== 'into') {
      await conns.dropGroup(d.id, node.label, hint.place)
    } else if (d.kind === 'connection') {
      await conns.dropConnection(d.id, node, hint.place)
    }
  } catch (e) {
    app.reportError(e, tr('调整顺序失败'))
  }
}

function dropClass(node: TreeNode) {
  const h = dropHint.value
  if (!h || h.id !== node.id) return {}
  return { 'drop-before': h.place === 'before', 'drop-after': h.place === 'after', 'drop-into': h.place === 'into' }
}

// --- 右键菜单 ---
const menu = ref<{ x: number; y: number; node: TreeNode | null } | null>(null)

/**
 * 插件连接的节点：右键先问插件有哪些动作，再和内置菜单拼在一起。
 * 动作是异步取的，菜单先开、动作项随后补上；插件很少慢到能察觉。
 */
const pluginActions = ref<{ nodeId: string; items: PluginAction[] } | null>(null)
function isPluginNode(node: TreeNode | null) {
  return !!node && String(node.engine).startsWith('plugin:')
}
function pluginNodeOf(node: TreeNode): PluginNode | null {
  if (node.kind === 'connection') return { scope: 'connection' }
  if (node.kind === 'database') return { scope: 'database', database: node.database }
  if (node.kind === 'object') return { scope: 'table', database: node.database, name: node.label }
  return null
}
async function fetchPluginActions(node: TreeNode) {
  pluginActions.value = null
  const pn = pluginNodeOf(node)
  if (!pn || !conns.isOpen(node.connId)) return
  try {
    const items = (await api.PluginNodeActions(node.connId, pn)) ?? []
    pluginActions.value = { nodeId: node.id, items }
  } catch {
    pluginActions.value = null
  }
}
const actionForm = ref<{ node: TreeNode; action: PluginAction } | null>(null)
async function runPluginAction(node: TreeNode, action: PluginAction, params: Record<string, string>) {
  const pn = pluginNodeOf(node)
  if (!pn) return
  try {
    const res = await api.RunPluginAction(node.connId, action.id, pn, params)
    if (res.message) app.toast(res.level || 'success', res.message)
    if (res.refresh) {
      // 动作改的是插件自己的树：刷新所在连接，连接以下整体重取
      const root =
        node.kind === 'connection'
          ? node
          : conns.visibleNodes.find((n) => n.kind === 'connection' && n.connId === node.connId)
      if (root) await conns.refresh(root)
    }
  } catch (e) {
    app.reportError(e, action.label)
  }
}

function onActionSubmit(params: Record<string, string>) {
  const f = actionForm.value
  actionForm.value = null
  if (f) void runPluginAction(f.node, f.action, params)
}

function onContextMenu(node: TreeNode | null, e: MouseEvent) {
  e.preventDefault()
  if (node) conns.select(node.id)
  menu.value = { x: e.clientX, y: e.clientY, node }
  if (node && isPluginNode(node)) void fetchPluginActions(node)
}

const menuItems = computed<MenuItem[]>(() => {
  const base = baseMenuItems.value
  const node = menu.value?.node
  const pa = pluginActions.value
  if (!node || !pa || pa.nodeId !== node.id || !pa.items.length) return base
  const items: MenuItem[] = pa.items.map((a) => ({
    key: 'plugin-action:' + a.id,
    label: a.label,
    danger: a.danger,
    icon: 'sparkle',
  }))
  return [...items, { key: 'sep-plugin', separator: true }, ...base]
})
const baseMenuItems = computed<MenuItem[]>(() => {
  const node = menu.value?.node
  if (!node) {
    return [
      { key: 'new-connection', label: tr('新建连接…'), icon: 'plus' },
      { key: 'new-group', label: tr('新建分组…'), icon: 'folder' },
      { key: 'sep1', separator: true },
      { key: 'refresh-all', label: tr('刷新全部'), icon: 'refresh' },
    ]
  }
  const open = conns.isOpen(node.connId)
  const moveTargets: MenuItem[] = [
    { key: 'move-to:', label: tr('无分组') },
    ...conns.groups.map((g) => ({ key: 'move-to:' + g.name, label: g.name })),
  ]

  switch (node.kind) {
    case 'group':
      return [
        { key: 'new-connection-in-group', label: tr('新建连接…'), icon: 'plus' },
        { key: 'sep1', separator: true },
        { key: 'rename-group', label: tr('重命名分组…'), icon: 'edit' },
        { key: 'delete-group', label: tr('删除分组'), icon: 'trash', danger: true },
      ]
    case 'connection':
      return [
        { key: 'move-to', label: tr('移动到分组'), icon: 'folder', children: moveTargets },
        { key: 'sepm', separator: true },
        open
          ? { key: 'close-connection', label: tr('断开连接'), icon: 'unlink' }
          : { key: 'open-connection', label: tr('打开连接'), icon: 'link' },
        { key: 'sep1', separator: true },
        { key: 'new-query', label: tr('新建查询'), icon: 'sql', disabled: !open },
        { key: 'new-database', label: tr('新建数据库…'), icon: 'database', disabled: !open },
        { key: 'sep2', separator: true },
        { key: 'edit-connection', label: tr('编辑连接…'), icon: 'edit' },
        { key: 'duplicate-connection', label: tr('复制连接'), icon: 'copy' },
        { key: 'refresh', label: tr('刷新'), icon: 'refresh', disabled: !open },
        { key: 'sep3', separator: true },
        { key: 'server-monitor', label: tr('服务器监控'), icon: 'chart', disabled: !open },
        { key: 'sep4', separator: true },
        { key: 'delete-connection', label: tr('删除连接'), icon: 'trash', danger: true },
      ]

    case 'database':
      return [
        { key: 'open-database', label: tr('打开数据库'), icon: 'database' },
        { key: 'scope-filter', label: tr('在此库中筛选表…'), icon: 'search' },
        {
          key: 'toggle-favorite',
          label: conns.isFavorite(node) ? tr('取消收藏') : tr('收藏此库'),
          icon: 'star',
        },
        { key: 'sepf', separator: true },
        { key: 'new-query', label: tr('新建查询'), icon: 'sql' },
        { key: 'sep1', separator: true },
        { key: 'new-table', label: tr('新建表…'), icon: 'plus' },
        { key: 'refresh', label: tr('刷新'), icon: 'refresh' },
        { key: 'sep2', separator: true },
        {
          key: 'drop-database',
          label: tr('删除数据库'),
          icon: 'trash',
          danger: true,
          disabled: node.engine === 'sqlite' || node.engine === 'redis',
        },
      ]

    case 'schema':
      return [
        { key: 'new-query', label: tr('新建查询'), icon: 'sql' },
        { key: 'new-table', label: tr('新建表…'), icon: 'plus' },
        { key: 'refresh', label: tr('刷新'), icon: 'refresh' },
        { key: 'sep1', separator: true },
        { key: 'drop-schema', label: tr('删除 schema'), icon: 'trash', danger: true },
      ]

    case 'folder':
      return [
        { key: 'scope-filter', label: tr('在此分组中筛选…'), icon: 'search' },
        { key: 'sepf', separator: true },
        node.objectKind === 'table'
          ? { key: 'new-table', label: tr('新建表…'), icon: 'plus' }
          : { key: 'noop', label: tr('新建对象需在 SQL 编辑器中完成'), disabled: true },
        { key: 'refresh', label: tr('刷新'), icon: 'refresh' },
      ]

    case 'object': {
      if (node.objectKind === 'tool' || node.objectKind === 'resource') {
        return [{ key: 'open-mcp', label: node.objectKind === 'tool' ? tr('调用工具…') : tr('读取资源'), icon: 'sparkle' }]
      }
      const isTable = node.objectKind === 'table'
      const isTableLike = isTable || node.objectKind === 'view'
      return [
        { key: 'open-table', label: tr('打开表数据'), icon: 'grid', disabled: !isTableLike },
        { key: 'design-table', label: tr('设计表'), icon: 'edit', disabled: !isTable },
        {
          key: 'toggle-favorite',
          label: conns.isFavorite(node) ? tr('取消收藏') : tr('收藏此表'),
          icon: 'star',
        },
        { key: 'view-ddl', label: tr('查看 DDL'), icon: 'code' },
        { key: 'sep1', separator: true },
        {
          key: 'generate',
          label: tr('生成 SQL'),
          icon: 'sql',
          children: [
            { key: 'gen-select', label: tr('SELECT 语句'), disabled: !isTableLike },
            { key: 'gen-insert', label: tr('INSERT 语句'), disabled: !isTable },
            { key: 'gen-update', label: tr('UPDATE 语句'), disabled: !isTable },
          ],
        },
        { key: 'export-table', label: tr('导出数据…'), icon: 'download', disabled: !isTableLike },
        { key: 'sep2', separator: true },
        { key: 'copy-name', label: tr('复制名称'), icon: 'copy' },
        { key: 'rename-object', label: tr('重命名…'), icon: 'edit' },
        { key: 'sep3', separator: true },
        { key: 'truncate-table', label: tr('清空表'), icon: 'minus', danger: true, disabled: !isTable },
        { key: 'drop-object', label: tr('删除对象'), icon: 'trash', danger: true },
      ]
    }
  }
  return []
})

function onMenuSelect(key: string) {
  const node = menu.value?.node ?? null
  menu.value = null
  // 这两个是纯界面状态，不必绕一圈回到 App。
  if (key === 'toggle-favorite' && node) {
    conns.toggleFavorite(node)
    return
  }
  if (key === 'scope-filter' && node) {
    conns.setFilterScope(node)
    return
  }
  if (key === 'open-mcp' && node) {
    mcpDialog.value = { node }
    return
  }
  if (key.startsWith('plugin-action:') && node) {
    const action = pluginActions.value?.items.find((a) => 'plugin-action:' + a.id === key)
    if (!action) return
    if (action.fields && action.fields.length) actionForm.value = { node, action }
    else void runPluginAction(node, action, {})
    return
  }
  if (key.startsWith('move-to:') && node?.kind === 'connection') {
    conns.moveConnection(node.connId, key.slice('move-to:'.length)).catch((e) => app.reportError(e, tr('移动失败')))
    return
  }
  emit('action', key, node)
}

const hasConnections = computed(() => conns.states.length > 0)
</script>

<template>
  <aside class="sidebar">
    <div class="sidebar-search">
      <div class="search-row">
        <Icon name="search" :size="12" class="search-icon" />
        <input
          v-model="conns.filter"
          class="search-input"
          type="search"
          :placeholder="
          conns.filterScopeLabel
            ? tr('在 {scope} 内筛选', { scope: conns.filterScopeLabel })
            : tr('筛选对象')
        "
          spellcheck="false"
        />
        <button
          class="fav-toggle"
          :class="{ 'is-on': conns.onlyFavorites }"
          :title="tr('只看收藏')"
          @click="conns.onlyFavorites = !conns.onlyFavorites"
        >
          <Icon name="star" :size="13" :filled="conns.onlyFavorites" />
        </button>
      </div>
      <div v-if="conns.filterScopeLabel" class="scope-chip">
        <Icon name="database" :size="10" />
        <span class="scope-name">{{ conns.filterScopeLabel }}</span>
        <button :title="tr('取消范围限定')" @click="conns.setFilterScope(null)">
          <Icon name="close" :size="9" />
        </button>
      </div>
    </div>

    <div
      ref="scroller"
      class="tree"
      :class="{ 'is-animating': !!drawer }"
      @scroll="onScroll"
      @contextmenu="onContextMenu(null, $event)"
      @dragover.prevent
      @drop.prevent="onDrop(null)"
    >
      <div v-if="!hasConnections" class="tree-empty">
        <Icon name="database" :size="26" />
        <div class="empty-state-title">{{ tr('还没有连接') }}</div>
        <button class="btn btn-primary" @click="emit('action', 'new-connection', null)">{{ tr('新建连接') }}</button>
      </div>

      <!-- 收藏为空，或被搜索词滤空：都要说清楚是哪种情况，
           否则「点了收藏却看不到」会被当成功能坏了。 -->
      <div v-else-if="conns.onlyFavorites && !nodes.length" class="tree-empty">
        <Icon name="star" :size="24" />
        <template v-if="conns.filter.trim()">
          <div class="empty-state-title">{{ tr('没有匹配的收藏') }}</div>
          <div class="muted">
            {{ tr('搜索词「{kw}」过滤掉了全部收藏项', { kw: conns.filter.trim() }) }}
          </div>
          <button class="btn" @click="conns.filter = ''">{{ tr('清除搜索') }}</button>
        </template>
        <template v-else-if="conns.favoriteList.length">
          <div class="empty-state-title">{{ tr('收藏项所属的连接已被删除') }}</div>
        </template>
        <template v-else>
          <div class="empty-state-title">{{ tr('还没有收藏') }}</div>
          <div class="muted">{{ tr('把鼠标移到库或表上，点左侧的星标即可收藏') }}</div>
          <button class="btn" @click="conns.onlyFavorites = false">{{ tr('返回全部') }}</button>
        </template>
      </div>

      <div v-else-if="!nodes.length && conns.filter.trim()" class="tree-empty">
        <Icon name="search" :size="24" />
        <div class="empty-state-title">{{ tr('没有匹配的对象') }}</div>
        <div class="muted">
          {{
              conns.filterScopeLabel
                ? tr('在「{scope}」范围内没有找到「{kw}」', {
                    scope: conns.filterScopeLabel,
                    kw: conns.filter.trim(),
                  })
                : tr('当前已加载的节点中没有找到「{kw}」', { kw: conns.filter.trim() })
            }}
        </div>
        <div class="muted">{{ tr('未展开的库需要先展开才能被搜到') }}</div>
      </div>

      <div v-else-if="nodes.length" class="tree-inner" :style="{ height: totalHeight + 'px' }">
        <div
          v-for="item in rendered"
          :key="item.node.id"
          class="tree-row"
          :class="{
            'is-selected': conns.selectedId === item.node.id,
            'is-offline': item.node.offline,
            'is-conn': item.node.kind === 'connection',
            'is-group': item.node.kind === 'group',
            ...dropClass(item.node),
          }"
          :draggable="item.node.kind === 'connection' || item.node.kind === 'group'"
          @dragstart="onDragStart(item.node, $event)"
          @dragover="onDragOver(item.node, $event)"
          @dragleave="dropHint = null"
          @drop.prevent.stop="onDrop(item.node)"
          :style="{
            '--y': yOf(item.index) + 'px',
            paddingLeft: 24 + item.node.level * 14 + 'px',
            '--engine': engineColor(item.node),
          }"
          @click="onRowClick(item.node)"
          @dblclick="onRowDblClick(item.node)"
          @contextmenu.stop="onContextMenu(item.node, $event)"
        >
          <!-- 收藏钉在行首固定列：位置不随层级缩进变化，一竖排下来好点也好扫 -->
          <button
            v-if="item.node.kind === 'database' || item.node.kind === 'object'"
            class="star"
            :class="{ 'is-on': conns.isFavorite(item.node) }"
            :title="conns.isFavorite(item.node) ? tr('取消收藏') : tr('收藏')"
            @click.stop="conns.toggleFavorite(item.node)"
          >
            <Icon name="star" :size="14" :filled="conns.isFavorite(item.node)" />
          </button>

          <span
            class="tree-chevron"
            :class="{ 'is-hidden': !item.node.expandable }"
            @click="onChevron(item.node, $event)"
          >
            <Icon
              v-if="item.node.loading"
              name="refresh"
              :size="11"
              class="spin"
            />
            <Icon
              v-else
              :name="conns.expanded.has(item.node.id) ? 'chevronDown' : 'chevronRight'"
              :size="11"
            />
          </span>

          <span
            class="tree-icon"
            :style="
              item.node.kind === 'connection' ? { color: engineColor(item.node) } : undefined
            "
          >
            <Icon :name="iconFor(item.node)" :size="item.node.kind === 'connection' ? 15 : 13" />
          </span>

          <span class="tree-label" :style="item.node.color ? { color: item.node.color } : undefined">{{
            item.node.label
          }}</span>

          <!-- 加载中要让人一眼看见。只把 11px 的箭头换成转圈太隐蔽了，
               用户会以为点了没反应。 -->
          <span v-if="item.node.loading" class="tree-loading">
            <span class="tree-dots"><i></i><i></i><i></i></span>{{ tr('加载中') }}</span>
          <span
            v-else-if="item.node.error"
            class="badge badge-error"
            :title="item.node.error"
            >{{ tr('加载失败') }}</span
          >
          <span
            v-else-if="item.node.kind === 'connection' && conns.stateOf(item.node.connId)?.config.readOnly"
            class="badge badge-readonly"
            >{{ tr('只读') }}</span
          >
          <span
            v-else-if="item.node.kind === 'connection' && !conns.isOpen(item.node.connId)"
            class="tree-detail"
            >{{ tr('未连接') }}</span
          >
          <span v-else-if="item.node.detail" class="tree-detail">{{ item.node.detail }}</span>
        </div>

        <!-- 抽屉：子树被卷进这个 overflow:hidden 的容器，
             容器高度收到 0 就是「收纳进去」的效果 -->
        <div
          v-if="drawer"
          class="tree-drawer"
          :style="{ top: drawer.top + 'px', height: drawerH + 'px' }"
        >
          <div
            v-for="n in drawer.nodes"
            :key="'d-' + n.id"
            class="tree-row in-drawer"
            :class="{ 'is-conn': n.kind === 'connection', 'is-offline': n.offline }"
            :style="{ paddingLeft: 24 + n.level * 14 + 'px', '--engine': engineColor(n) }"
          >
            <span class="tree-chevron" :class="{ 'is-hidden': !n.expandable }">
              <Icon :name="conns.expanded.has(n.id) ? 'chevronDown' : 'chevronRight'" :size="11" />
            </span>
            <span
              class="tree-icon"
              :style="n.kind === 'connection' ? { color: engineColor(n) } : undefined"
            >
              <Icon :name="iconFor(n)" :size="13" />
            </span>
            <span class="tree-label">{{ n.label }}</span>
            <span v-if="n.detail" class="tree-detail">{{ n.detail }}</span>
          </div>
        </div>
      </div>
    </div>

    <McpToolDialog v-if="mcpDialog" :node="mcpDialog.node" @close="mcpDialog = null" />
    <ActionFormDialog
      v-if="actionForm"
      :title="actionForm.action.label"
      :fields="actionForm.action.fields ?? []"
      :danger="actionForm.action.danger"
      @submit="onActionSubmit"
      @close="actionForm = null"
    />
    <ContextMenu
      v-if="menu"
      :x="menu.x"
      :y="menu.y"
      :items="menuItems"
      @select="onMenuSelect"
      @close="menu = null"
    />
  </aside>
</template>

<style scoped>
.sidebar {
  display: flex;
  flex-direction: column;
  height: 100%;
  background: var(--c-sidebar);
  border-right: 1px solid var(--c-border-soft);
}

.sidebar-search {
  flex: none;
  padding: var(--sp-2) var(--sp-3);
  border-bottom: 1px solid var(--c-border-soft);
}

.search-row {
  position: relative;
  display: flex;
  align-items: center;
  gap: 4px;
}

.fav-toggle {
  display: flex;
  align-items: center;
  justify-content: center;
  flex: none;
  width: 24px;
  height: 24px;
  padding: 0;
  border: 1px solid transparent;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--c-text-tertiary);
}
.fav-toggle:hover {
  background: var(--c-chrome-active);
}
.fav-toggle.is-on {
  background: var(--c-accent-soft);
  border-color: var(--c-accent-border);
  color: #e0a030;
}

.scope-chip {
  display: flex;
  align-items: center;
  gap: 4px;
  margin-top: 5px;
  padding: 2px 4px 2px 7px;
  border-radius: 10px;
  background: var(--c-accent-soft);
  color: var(--c-accent);
  font-size: var(--font-size-sm);
}
.scope-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.scope-chip button {
  display: flex;
  align-items: center;
  justify-content: center;
  flex: none;
  width: 14px;
  height: 14px;
  padding: 0;
  border: none;
  border-radius: 50%;
  background: transparent;
  color: inherit;
}
.scope-chip button:hover {
  background: var(--c-accent);
  color: #fff;
}
.search-icon {
  position: absolute;
  left: var(--sp-3);
  top: 6px;
  z-index: 1;
  color: var(--c-text-tertiary);
  pointer-events: none;
}
.search-input {
  flex: 1;
  min-width: 0;
  height: 24px;
  padding: 0 var(--sp-3) 0 24px;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  background: var(--c-canvas);
  color: var(--c-text);
  outline: none;
  font-size: var(--font-size);
}
.search-input:focus {
  border-color: var(--c-accent);
  box-shadow: 0 0 0 2px var(--c-accent-soft);
}
.search-input::-webkit-search-cancel-button {
  cursor: default;
}

.tree {
  flex: 1;
  /* 允许横向滚动：表名 + 行数体积经常比侧栏宽，
     截断了还不如让它能滚，配合双击分隔条一键适配宽度。 */
  overflow: auto;
  position: relative;
  /* 给滚动条留出固定的槽：树高度在开合过程中一直在变，
     滚动条反复出现/消失会让内容横向抖一下，比滚动条本身更扰人。 */
  scrollbar-gutter: stable;
}

/* 开合过程中把滚动条调暗：这段时间里内容高度本来就在变，
   滑块跟着快速伸缩很抓眼睛，压下去之后整体安静很多。 */
.tree.is-animating::-webkit-scrollbar-thumb {
  background: transparent;
}

.tree-inner {
  position: relative;
  transition: height 190ms cubic-bezier(0.25, 0.8, 0.35, 1);
}

.tree-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--sp-3);
  padding: 56px var(--sp-5);
  color: var(--c-text-tertiary);
  text-align: center;
  line-height: 1.6;
}

.tree-row {
  position: absolute;
  left: 0;
  border-left: 2px solid transparent;
  /* 宽度由内容决定，这样容器的 scrollWidth 才是真实内容宽度，
     「双击分隔条自适应」才有依据可算。 */
  width: max-content;
  min-width: 100%;
  display: flex;
  align-items: center;
  gap: 4px;
  height: 24px;
  padding-right: 10px;
  white-space: nowrap;
  transform: translateY(var(--y));
  /* 位置完全由 JS 每帧写入，这里不要再挂 transition——
     两套动画同时作用在 transform 上会互相拖拽，反而出现滞后和抖动。 */
}

/* 拖放落点提示：前/后一条线，进分组整行着色 */
.tree-row.drop-before { box-shadow: inset 0 2px 0 var(--c-accent); }
.tree-row.drop-after { box-shadow: inset 0 -2px 0 var(--c-accent); }
.tree-row.drop-into { background: var(--c-accent-soft); }
.tree-row.is-group .tree-label { font-weight: 600; }
.tree-row.is-group .tree-detail { opacity: 0.7; }

/* 抽屉里的行跟着容器走，自己不定位 */
.tree-row.in-drawer {
  position: relative;
  transform: none;
  pointer-events: none;
}

.tree-drawer {
  position: absolute;
  left: 0;
  right: 0;
  overflow: hidden;
  /* 高度由 rAF 逐帧写入，这里同样不能加 transition */
  will-change: height;
}

.tree-row:hover {
  background: var(--c-chrome-active);
}
/* 连接节点带引擎色条：一眼分清这是哪种库，
   挂着生产库时尤其重要 */
.tree-row.is-conn {
  border-left-color: var(--engine);
  font-weight: 500;
}
.tree-row.is-selected {
  background: var(--c-accent);
  color: #fff;
}

/* 所属连接没打开：整行压暗，提示它还不能直接用 */
.tree-row.is-offline .tree-label,
.tree-row.is-offline .tree-icon {
  opacity: 0.45;
}
.tree-row.is-offline.is-selected .tree-label,
.tree-row.is-offline.is-selected .tree-icon {
  opacity: 0.75;
}

.connect-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  flex: none;
  width: 18px;
  height: 18px;
  margin-left: auto;
  padding: 0;
  border: none;
  border-radius: 3px;
  background: transparent;
  color: var(--c-text-tertiary);
}
.connect-btn:hover {
  background: var(--c-accent);
  color: #fff;
}
.tree-row.is-selected .connect-btn {
  color: rgba(255, 255, 255, 0.8);
}
.tree-row.is-selected .tree-detail,
.tree-row.is-selected .tree-chevron {
  color: rgba(255, 255, 255, 0.8);
}
.tree-row.is-selected .tree-icon {
  color: #fff !important;
}

.tree-chevron {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 14px;
  flex: none;
  color: var(--c-text-tertiary);
}
.tree-chevron.is-hidden {
  visibility: hidden;
}

.tree-icon {
  display: flex;
  flex: none;
  color: var(--c-text-secondary);
}

.tree-label {
  overflow: hidden;
  text-overflow: ellipsis;
}

.star {
  position: absolute;
  left: 4px;
  top: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 17px;
  height: 17px;
  padding: 0;
  border: none;
  border-radius: 3px;
  background: transparent;
  color: var(--c-border-strong);
  opacity: 0;
}
.tree-row:hover .star,
.star.is-on {
  opacity: 1;
}
.star.is-on {
  color: #e0a030;
}
.star:hover {
  background: var(--c-chrome-active);
}
.tree-row.is-selected .star {
  color: rgba(255, 255, 255, 0.85);
}
.tree-row.is-selected .star.is-on {
  color: #ffd166;
}

.tree-detail {
  margin-left: auto;
  padding-left: var(--sp-3);
  font-size: var(--font-size-sm);
  color: var(--c-text-tertiary);
}
.tree-row.is-selected .tree-detail {
  color: rgba(255, 255, 255, 0.75);
}

.tree-loading {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  margin-left: auto;
  padding-left: 8px;
  font-size: var(--font-size-sm);
  color: var(--c-accent);
  white-space: nowrap;
}
.tree-row.is-selected .tree-loading {
  color: #fff;
}

.tree-dots {
  display: inline-flex;
  gap: 2px;
}
.tree-dots i {
  width: 3px;
  height: 3px;
  border-radius: 50%;
  background: currentColor;
  animation: blink 1.1s ease-in-out infinite;
}
.tree-dots i:nth-child(2) {
  animation-delay: 0.18s;
}
.tree-dots i:nth-child(3) {
  animation-delay: 0.36s;
}
@keyframes blink {
  0%,
  60%,
  100% {
    opacity: 0.25;
  }
  30% {
    opacity: 1;
  }
}

.spin {
  animation: spin 0.9s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
