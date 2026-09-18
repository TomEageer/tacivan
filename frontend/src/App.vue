<script setup lang="ts">
/** 应用主壳：工具栏 + 对象树 + 标签页 + 状态栏，并负责把树上的操作分发出去。 */
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { EventsOn } from '../wailsjs/runtime/runtime'
import * as api from './api'
import { useAppStore } from './stores/app'
import markDay from './assets/mark-day.svg'
import markNight from './assets/mark-night.svg'
import { useConnectionsStore, type TreeNode } from './stores/connections'
import { useTabsStore } from './stores/tabs'
import type { ConnectionConfig, ObjectRef, SearchHit } from './types'

import Toolbar from './components/layout/Toolbar.vue'
import Sidebar from './components/layout/Sidebar.vue'
import TabBar from './components/layout/TabBar.vue'
import StatusBar from './components/layout/StatusBar.vue'
import Icon from './components/common/Icon.vue'
import TooltipLayer from './components/common/TooltipLayer.vue'

import TableTab from './components/tabs/TableTab.vue'
import QueryTab from './components/tabs/QueryTab.vue'
import DesignerTab from './components/tabs/DesignerTab.vue'
import RedisTab from './components/tabs/RedisTab.vue'
import DdlTab from './components/tabs/DdlTab.vue'

import ConnectionDialog from './components/dialogs/ConnectionDialog.vue'
import SettingsDialog from './components/dialogs/SettingsDialog.vue'
import ResourceMonitor from './components/dialogs/ResourceMonitor.vue'
import SearchDialog from './components/dialogs/SearchDialog.vue'
import AiDialog from './components/dialogs/AiDialog.vue'
import HistoryDialog from './components/dialogs/HistoryDialog.vue'
import PromptDialog from './components/dialogs/PromptDialog.vue'
import { tr } from './i18n'

const app = useAppStore()
/** 空状态用的标志，和程序坞图标同一个，夜间换成夜版。 */
const welcomeMark = computed(() => (app.effectiveTheme === 'dark' ? markNight : markDay))
const conns = useConnectionsStore()
const tabs = useTabsStore()

/** 侧栏宽度记在本地，下次打开保持不变。 */
const SIDEBAR_KEY = 'tacivan.sidebarWidth'
const sidebarWidth = ref(Number(localStorage.getItem(SIDEBAR_KEY)) || 250)

watch(sidebarWidth, (v) => localStorage.setItem(SIDEBAR_KEY, String(Math.round(v))))

/**
 * 按树的内容宽度自适应侧栏。
 *
 * 树行用了 width: max-content，所以容器的 scrollWidth 就是最长那行的真实宽度。
 * 双击分隔条触发，和调整表格列宽的习惯一致；不做成随内容自动变化，
 * 那样展开一个长表名就会让整个侧栏跳一下，比截断更烦人。
 */
function autoFitSidebar() {
  const tree = document.querySelector('.tree') as HTMLElement | null
  if (!tree) return
  const content = tree.scrollWidth
  if (content <= 0) return
  sidebarWidth.value = Math.max(180, Math.min(560, content + 14))
}
const statusMessage = ref('')

// 对话框状态
const connDialog = ref<{ existing: ConnectionConfig | null; group?: string } | null>(null)
const showSettings = ref(false)
const showMonitor = ref(false)
const showAi = ref(false)
const showHistory = ref(false)
/** 全库搜索对话框；记下发起时的连接与库，作为搜索范围的默认值。 */
const search = ref<{ connId: string; database: string } | null>(null)
const prompt = ref<{
  title: string
  message?: string
  label?: string
  value?: string
  confirmLabel?: string
  danger?: boolean
  input?: boolean
  requireTyping?: string
  onConfirm: (v: string) => void
} | null>(null)

/** 当前激活标签页的组件实例，菜单动作转发给它。 */
const activePane = ref<any>(null)

onMounted(async () => {
  await app.load()
  await conns.loadConnections()
  await conns.loadFavorites()
  await restoreDrafts()
  bindMenuEvents()
  window.addEventListener('keydown', onGlobalKey)
})

/**
 * 把上次留下的查询草稿恢复成标签页。
 *
 * 草稿在标签页被正常关掉时就删了，所以还留着的恰好是「上次退出时开着的」
 * 或者「进程没能正常退出」的那些——两种情况用户都希望它们回来。
 * 恢复时不去自动连库：编辑器本身不需要连接就能开，
 * 用户可能只是想看看上次写到哪儿，不该一启动就去敲一堆服务器。
 */
async function restoreDrafts() {
  let drafts
  try {
    drafts = await api.ListDrafts()
  } catch {
    return
  }
  if (!drafts?.length) return
  for (const d of drafts) {
    const st = conns.stateOf(d.connId)
    tabs.open({
      id: d.id,
      type: 'query',
      title: d.title || tr('查询'),
      connId: d.connId,
      connName: d.connName || st?.config.name || '',
      engine: st?.config.engine ?? 'mysql',
      database: d.database,
      sql: d.sql,
    })
  }
  app.toast('info', tr('已恢复 {n} 个未保存的查询', { n: drafts.length }), '')
}

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onGlobalKey)
})

// --- 原生菜单事件 ---
function bindMenuEvents() {
  const map: Record<string, () => void> = {
    'menu:new-connection': () => (connDialog.value = { existing: null }),
    'menu:new-query': () => newQuery(conns.selectedNode),
    'menu:open-sql': () => activePane.value?.openFile?.(),
    'menu:save': () => activePane.value?.save?.(),
    'menu:save-as': () => activePane.value?.saveAs?.(),
    'menu:export': () => activePane.value?.exportData?.(),
    'menu:close-tab': () => tabs.activeId && tabs.close(tabs.activeId),
    'menu:run': () => activePane.value?.run?.(),
    'menu:run-current': () => activePane.value?.runCurrent?.(),
    'menu:stop': () => activePane.value?.stop?.(),
    'menu:explain': () => activePane.value?.explain?.(),
    'menu:format-sql': () => activePane.value?.format?.(),
    'menu:refresh': () => refreshCurrent(),
    'menu:new-table': () => newTable(conns.selectedNode),
    'menu:design-table': () => designTable(conns.selectedNode),
    'menu:open-table': () => openTable(conns.selectedNode),
    'menu:resource-monitor': () => (showMonitor.value = true),
    'menu:history': () => (showHistory.value = true),
    'menu:find-in-database': openSearch,
    'menu:server-monitor': () => app.toast('info', tr('服务器监控'), tr('可在连接右键菜单中打开')),
    'menu:settings': () => (showSettings.value = true),
    'menu:find': () => app.toast('info', tr('在编辑器中按 ⌘F 可搜索')),
  }
  for (const [event, handler] of Object.entries(map)) {
    EventsOn(event, handler)
  }
}

/**
 * 网页层的快捷键。
 *
 * 菜单栏里已经绑了同样的组合键，这里再接一层是因为焦点可能落在
 * 数据网格、筛选框这些地方，而用户期望随时按下就执行。
 * ⌘R 在 WebView 里默认是刷新页面，必须挡住。
 */
function onGlobalKey(e: KeyboardEvent) {
  const meta = e.metaKey || e.ctrlKey
  if (!meta) return

  switch (e.key.toLowerCase()) {
    case '[':
      e.preventDefault()
      tabs.activateNext(-1)
      return
    case ']':
      e.preventDefault()
      tabs.activateNext(1)
      return
    case 'r':
      e.preventDefault()
      if (e.shiftKey) activePane.value?.runCurrent?.()
      else if (activePane.value?.run) activePane.value.run()
      else activePane.value?.refresh?.()
      return
    case 'enter':
      if (activePane.value?.run) {
        e.preventDefault()
        activePane.value.run()
      }
      return
    case '.':
      if (activePane.value?.stop) {
        e.preventDefault()
        activePane.value.stop()
      }
      return
    case 'f':
      // ⌘⇧F 全库搜索；不带 Shift 留给编辑器里的查找。
      if (e.shiftKey) {
        e.preventDefault()
        openSearch()
      }
      return
  }
}

/**
 * 打开全库搜索。
 *
 * 范围默认取当前上下文（选中的连接与库），而不是"全部"——
 * 大实例上默认全选等于一按就开始几千个库的扫描。
 */
function openSearch() {
  const node = conns.selectedNode
  const t = tabs.active
  const connId = node?.connId ?? t?.connId ?? ''
  if (!connId || !conns.isOpen(connId)) {
    app.toast('warning', tr('请先在左侧打开一个连接'))
    return
  }
  search.value = { connId, database: node?.database ?? t?.database ?? '' }
}

/** 从搜索结果跳到对象上：表和视图直接打开，列和数据命中也落到所在表。 */
function onSearchHit(hit: SearchHit) {
  const connId = search.value?.connId
  if (!connId) return
  const st = conns.stateOf(connId)
  tabs.open({
    type: 'table',
    title: hit.table,
    connId,
    connName: st?.config.name ?? '',
    engine: st?.config.engine ?? 'mysql',
    database: hit.database,
    ref: {
      database: hit.database,
      schema: hit.schema,
      name: hit.table,
      kind: hit.kind === 'view' ? 'view' : 'table',
    },
  })
}

function refreshCurrent() {
  if (activePane.value?.refresh) activePane.value.refresh()
  else if (conns.selectedNode) conns.refresh(conns.selectedNode)
}

// --- 打开各类标签页 ---
function refOf(node: TreeNode): ObjectRef {
  return {
    database: node.database ?? '',
    schema: node.schema ?? '',
    name: node.label,
    kind: node.objectKind ?? 'table',
  }
}

function connMeta(node: TreeNode) {
  const st = conns.stateOf(node.connId)
  return { connId: node.connId, connName: st?.config.name ?? '', engine: node.engine }
}

function openTable(node: TreeNode | null | undefined) {
  if (!node || node.kind !== 'object') return
  tabs.open({
    type: 'table',
    title: node.label,
    ...connMeta(node),
    database: node.database ?? '',
    ref: refOf(node),
    estimatedRows: node.object?.rows ?? 0,
  })
}

function designTable(node: TreeNode | null | undefined) {
  if (!node || node.kind !== 'object' || node.objectKind !== 'table') return
  tabs.open({
    type: 'designer',
    title: tr('设计: {name}', { name: node.label }),
    ...connMeta(node),
    database: node.database ?? '',
    ref: refOf(node),
  })
}

function viewDdl(node: TreeNode) {
  tabs.open({
    type: 'ddl',
    title: `DDL: ${node.label}`,
    ...connMeta(node),
    database: node.database ?? '',
    ref: refOf(node),
  })
}

function newQuery(node: TreeNode | null | undefined, sql = '') {
  const target = node ?? conns.selectedNode
  if (!target) {
    app.toast('warning', tr('请先在左侧选择一个已打开的连接'))
    return
  }
  if (!conns.isOpen(target.connId)) {
    app.toast('warning', tr('请先打开该连接'))
    return
  }
  if (target.engine === 'redis') {
    openRedis(target)
    return
  }
  const st = conns.stateOf(target.connId)
  const t = tabs.open({
    type: 'query',
    title: tr('查询'),
    connId: target.connId,
    connName: st?.config.name ?? '',
    engine: target.engine,
    database: target.database ?? st?.currentDb ?? '',
    sql,
  })
  if (sql) t.sql = sql
}

function openRedis(node: TreeNode) {
  tabs.open({
    type: 'redis',
    title: node.database ? tr('键浏览器 ({db})', { db: node.database }) : tr('键浏览器'),
    ...connMeta(node),
    database: node.database ?? 'db0',
    pattern: '*',
  })
}

function newTable(node: TreeNode | null | undefined) {
  if (!node) return
  const database = node.database ?? conns.stateOf(node.connId)?.currentDb ?? ''
  if (!database) {
    app.toast('warning', tr('请先选择一个数据库'))
    return
  }
  tabs.open({
    type: 'designer',
    title: tr('新建表'),
    ...connMeta(node),
    database,
    ref: { database, schema: node.schema ?? '', name: '', kind: 'table' },
  })
}

// --- 树上的操作分发 ---
async function onTreeAction(action: string, node: TreeNode | null) {
  switch (action) {
    case 'new-connection':
      connDialog.value = { existing: null }
      return
    case 'refresh-all':
      await conns.loadConnections()
      return
  }
  if (!node) return

  switch (action) {
    case 'open-connection':
      await conns.openConnection(node.connId)
      break

    case 'close-connection':
      tabs.closeByConnection(node.connId)
      await conns.closeConnection(node.connId)
      break

    case 'edit-connection': {
      const st = conns.stateOf(node.connId)
      if (st) connDialog.value = { existing: st.config }
      break
    }

    case 'duplicate-connection':
      try {
        await api.DuplicateConnection(node.connId)
        await conns.loadConnections()
        app.toast('success', tr('已复制连接'))
      } catch (e) {
        app.reportError(e, tr('复制连接失败'))
      }
      break

    case 'new-group':
      prompt.value = {
        title: tr('新建分组'),
        label: tr('分组名'),
        value: '',
        input: true,
        confirmLabel: tr('创建'),
        onConfirm: async (v) => {
          try {
            await conns.createGroup(v)
          } catch (e) {
            app.reportError(e, tr('新建分组失败'))
          }
        },
      }
      return
    case 'new-connection-in-group':
      connDialog.value = { existing: null, group: node?.label ?? '' }
      return
    case 'rename-group':
      if (!node) return
      prompt.value = {
        title: tr('重命名分组'),
        label: tr('分组名'),
        value: node.label,
        input: true,
        confirmLabel: tr('重命名'),
        onConfirm: async (v) => {
          try {
            await conns.renameGroup(node.label, v)
          } catch (e) {
            app.reportError(e, tr('重命名失败'))
          }
        },
      }
      return
    case 'delete-group':
      if (!node) return
      prompt.value = {
        title: tr('删除分组'),
        message: tr('将删除分组「{name}」，里面的连接会回到最外层，不会被删。', { name: node.label }),
        danger: true,
        input: false,
        confirmLabel: tr('删除'),
        onConfirm: async () => {
          try {
            await conns.deleteGroup(node.label)
          } catch (e) {
            app.reportError(e, tr('删除分组失败'))
          }
        },
      }
      return
    case 'delete-connection': {
      const st = conns.stateOf(node.connId)
      prompt.value = {
        title: tr('删除连接'),
        message: tr('将删除连接「{name}」及其保存的密码。数据库本身不受影响。', { name: st?.config.name ?? '' }),
        danger: true,
        input: false,
        confirmLabel: tr('删除'),
        onConfirm: async () => {
          try {
            tabs.closeByConnection(node.connId)
            await api.DeleteConnection(node.connId)
            await conns.loadConnections()
            app.toast('success', tr('连接已删除'))
          } catch (e) {
            app.reportError(e, tr('删除连接失败'))
          }
        },
      }
      break
    }

    case 'refresh':
      await conns.refresh(node)
      break

    case 'open-database':
      if (node.engine === 'redis') openRedis(node)
      else {
        await conns.setCurrentDatabase(node.connId, node.database!)
        await conns.toggle(node)
      }
      break

    case 'new-query':
      newQuery(node)
      break

    case 'new-table':
      newTable(node)
      break

    case 'open-table':
      openTable(node)
      break

    case 'design-table':
      designTable(node)
      break

    case 'view-ddl':
      viewDdl(node)
      break

    case 'copy-name':
      navigator.clipboard?.writeText(node.label)
      app.toast('success', tr('已复制名称'))
      break

    case 'gen-select':
    case 'gen-insert':
    case 'gen-update':
      await generateSql(action, node)
      break

    case 'rename-object':
      prompt.value = {
        title: tr('重命名'),
        label: tr('新名称'),
        value: node.label,
        onConfirm: async (v) => {
          try {
            await api.RenameObject(node.connId, refOf(node), v)
            app.toast('success', tr('已重命名为 {name}', { name: v }))
            await conns.refreshObjectFolder(
              node.connId,
              node.database ?? '',
              node.schema ?? '',
              node.objectKind ?? 'table',
            )
          } catch (e) {
            app.reportError(e, tr('重命名失败'))
          }
        },
      }
      break

    case 'truncate-table':
      prompt.value = {
        title: tr('清空表'),
        message: tr('将删除表「{name}」中的全部数据，表结构保留。', { name: node.label }),
        danger: true,
        input: false,
        confirmLabel: tr('清空'),
        requireTyping: app.settings.confirmOnDelete ? node.label : undefined,
        onConfirm: async () => {
          try {
            await api.TruncateTable(node.connId, refOf(node))
            app.toast('success', tr('表 {name} 已清空', { name: node.label }))
          } catch (e) {
            app.reportError(e, tr('清空表失败'))
          }
        },
      }
      break

    case 'drop-object':
      prompt.value = {
        title: tr('删除对象'),
        message: tr('将永久删除「{name}」及其全部数据。', { name: node.label }),
        danger: true,
        input: false,
        confirmLabel: tr('删除'),
        requireTyping: app.settings.confirmOnDelete ? node.label : undefined,
        onConfirm: async () => {
          try {
            await api.DropObject(node.connId, refOf(node))
            app.toast('success', tr('已删除 {name}', { name: node.label }))
            await conns.refreshObjectFolder(
              node.connId,
              node.database ?? '',
              node.schema ?? '',
              node.objectKind ?? 'table',
            )
          } catch (e) {
            app.reportError(e, tr('删除失败'))
          }
        },
      }
      break

    case 'drop-database':
      prompt.value = {
        title: tr('删除数据库'),
        message: tr('将永久删除数据库「{name}」及其中全部对象与数据。', { name: node.database ?? '' }),
        danger: true,
        input: false,
        confirmLabel: tr('删除'),
        requireTyping: node.database,
        onConfirm: async () => {
          try {
            await api.DropDatabase(node.connId, node.database!)
            app.toast('success', tr('数据库 {name} 已删除', { name: node.database ?? '' }))
            const root = conns.roots.find((r) => r.connId === node.connId)
            if (root) await conns.refresh(root)
          } catch (e) {
            app.reportError(e, tr('删除数据库失败'))
          }
        },
      }
      break

    case 'new-database':
      prompt.value = {
        title: tr('新建数据库'),
        label: tr('数据库名'),
        onConfirm: async (v) => {
          try {
            const isMy = node.engine === 'mysql' || node.engine === 'mariadb'
            await api.CreateDatabase(node.connId, v, isMy ? 'utf8mb4' : '', '')
            app.toast('success', tr('数据库 {name} 已创建', { name: v }))
            const root = conns.roots.find((r) => r.connId === node.connId)
            if (root) await conns.refresh(root)
          } catch (e) {
            app.reportError(e, tr('新建数据库失败'))
          }
        },
      }
      break

    case 'drop-schema':
      prompt.value = {
        title: tr('删除 schema'),
        message: tr('将删除 schema「{name}」。若其中仍有对象，需要级联删除。', { name: node.schema ?? '' }),
        danger: true,
        input: false,
        confirmLabel: tr('级联删除'),
        requireTyping: node.schema,
        onConfirm: async () => {
          try {
            await api.DropSchema(node.connId, node.database!, node.schema!, true)
            app.toast('success', tr('schema {name} 已删除', { name: node.schema ?? '' }))
          } catch (e) {
            app.reportError(e, tr('删除 schema 失败'))
          }
        },
      }
      break

    case 'export-table':
      openTable(node)
      // 表数据页挂载后再触发导出，避免拿不到结果集。
      setTimeout(() => activePane.value?.exportData?.(), 400)
      break

    case 'server-monitor':
      await showServerVariables(node)
      break
  }
}

async function generateSql(action: string, node: TreeNode) {
  try {
    const ref = refOf(node)
    let sql = ''
    if (action === 'gen-select') sql = await api.GenerateSelectSQL(node.connId, ref, 100)
    else if (action === 'gen-insert') sql = await api.GenerateInsertSQL(node.connId, ref)
    else sql = await api.GenerateUpdateSQL(node.connId, ref)
    newQuery(node, sql)
  } catch (e) {
    app.reportError(e, tr('生成 SQL 失败'))
  }
}

async function showServerVariables(node: TreeNode) {
  try {
    const vars = await api.ServerVariables(node.connId)
    const lines = Object.entries(vars)
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([k, v]) => `${k} = ${v}`)
      .join('\n')
    newQuery(node, `-- ${tr('服务器变量快照（只读，供参考）')}\n/*\n${lines}\n*/\n`)
  } catch (e) {
    app.reportError(e, tr('读取服务器变量失败'))
  }
}

// --- 工具栏动作 ---
function onToolbarAction(action: string) {
  const node = conns.selectedNode
  switch (action) {
    case 'new-connection':
      connDialog.value = { existing: null }
      break
    case 'settings':
      showSettings.value = true
      break
    case 'resource-monitor':
      showMonitor.value = true
      break
    case 'ai':
      showAi.value = true
      break
    case 'history':
      showHistory.value = true
      break
    case 'export':
      activePane.value?.exportData?.()
      break
    default:
      onTreeAction(action, node ?? null)
  }
}

async function onConnectionSaved(cfg: ConnectionConfig) {
  connDialog.value = null
  await conns.loadConnections()
  app.toast('success', tr('连接「{name}」已保存', { name: cfg.name }))
}

function onHistoryUse(sql: string) {
  const t = tabs.active
  if (t?.type === 'query') {
    t.sql = sql
    activePane.value?.insertText?.('')
    // 直接替换编辑器内容。
    t.sql = sql
  } else {
    newQuery(conns.selectedNode, sql)
  }
}

// --- 侧栏拖拽 ---
let dragX = 0
let dragW = 0
function onSplitDown(e: MouseEvent) {
  dragX = e.clientX
  dragW = sidebarWidth.value
  window.addEventListener('mousemove', onSplitMove)
  window.addEventListener('mouseup', onSplitUp)
}
function onSplitMove(e: MouseEvent) {
  sidebarWidth.value = Math.max(180, Math.min(520, dragW + e.clientX - dragX))
}
function onSplitUp() {
  window.removeEventListener('mousemove', onSplitMove)
  window.removeEventListener('mouseup', onSplitUp)
}

const activeTab = computed(() => tabs.active)
</script>

<template>
  <div class="shell">
    <Toolbar @action="onToolbarAction" />

    <div class="body">
      <div class="sidebar-wrap" :style="{ width: sidebarWidth + 'px' }">
        <Sidebar @action="onTreeAction" />
      </div>
      <div
        class="vsplit"
        :title="tr('拖动调整宽度，双击自动适配内容')"
        @mousedown="onSplitDown"
        @dblclick="autoFitSidebar"
      ></div>

      <main class="main">
        <TabBar />

        <div class="panes">
          <template v-if="activeTab">
            <!-- 每个标签页独立实例，切换时用 v-show 保住内部状态（滚动位置、编辑内容） -->
            <div
              v-for="t in tabs.tabs"
              v-show="t.id === tabs.activeId"
              :key="t.id"
              class="pane"
            >
              <TableTab
                v-if="t.type === 'table'"
                :ref="(el) => t.id === tabs.activeId && (activePane = el)"
                :tab="t"
              />
              <QueryTab
                v-else-if="t.type === 'query'"
                :ref="(el) => t.id === tabs.activeId && (activePane = el)"
                :tab="t"
              />
              <DesignerTab
                v-else-if="t.type === 'designer'"
                :ref="(el) => t.id === tabs.activeId && (activePane = el)"
                :tab="t"
                :is-new="!t.ref?.name"
              />
              <RedisTab
                v-else-if="t.type === 'redis'"
                :ref="(el) => t.id === tabs.activeId && (activePane = el)"
                :tab="t"
              />
              <DdlTab
                v-else-if="t.type === 'ddl'"
                :ref="(el) => t.id === tabs.activeId && (activePane = el)"
                :tab="t"
              />
            </div>
          </template>

          <div v-else class="welcome">
            <img class="welcome-mark" :src="welcomeMark" alt="" draggable="false" />
            <div class="welcome-title">Tacivan</div>
            <div class="welcome-sub">{{ tr('在左侧双击一个连接开始，或新建一个连接') }}</div>
            <div class="welcome-actions">
              <button class="btn btn-primary" @click="connDialog = { existing: null }">
                <Icon name="plus" :size="13" />{{ tr('新建连接') }}</button>
              <button
                class="btn"
                :disabled="!conns.selectedNode"
                @click="newQuery(conns.selectedNode)"
              >
                <Icon name="sql" :size="13" />{{ tr('新建查询') }}</button>
            </div>
            <div class="welcome-tips">
              <div><kbd>⌘N</kbd>{{ tr('新建连接') }}</div>
              <div><kbd>⌥⌘T</kbd>{{ tr('新建查询') }}</div>
              <div><kbd>⌘R</kbd>{{ tr('运行') }}</div>
              <div><kbd>⇧⌘M</kbd>{{ tr('资源监视器') }}</div>
            </div>
          </div>
        </div>
      </main>
    </div>

    <StatusBar :message="statusMessage" @action="onToolbarAction" />

    <TooltipLayer />

    <!-- 通知 -->
    <div class="toasts">
      <div
        v-for="t in app.toasts"
        :key="t.id"
        class="toast"
        :class="`is-${t.kind}`"
        @click="app.dismiss(t.id)"
      >
        <Icon
          :name="t.kind === 'error' ? 'warning' : t.kind === 'success' ? 'check' : 'info'"
          :size="14"
        />
        <div class="toast-text">
          <div class="toast-title">{{ t.title }}</div>
          <div v-if="t.detail" class="toast-detail selectable">{{ t.detail }}</div>
        </div>
      </div>
    </div>

    <!-- 对话框 -->
    <ConnectionDialog
      v-if="connDialog"
      :group="connDialog.group"
      :existing="connDialog.existing"
      @saved="onConnectionSaved"
      @close="connDialog = null"
    />
    <SettingsDialog v-if="showSettings" @close="showSettings = false" />
    <ResourceMonitor v-if="showMonitor" @close="showMonitor = false" />
    <AiDialog v-if="showAi" @close="showAi = false" />
    <HistoryDialog v-if="showHistory" @use="onHistoryUse" @close="showHistory = false" />
    <SearchDialog
      v-if="search"
      :conn-id="search.connId"
      :database="search.database"
      @open-hit="onSearchHit"
      @close="search = null"
    />
    <PromptDialog
      v-if="prompt"
      :title="prompt.title"
      :message="prompt.message"
      :label="prompt.label"
      :value="prompt.value"
      :confirm-label="prompt.confirmLabel"
      :danger="prompt.danger"
      :input="prompt.input !== false"
      :require-typing="prompt.requireTyping"
      @confirm="
        (v) => {
          const fn = prompt!.onConfirm
          prompt = null
          fn(v)
        }
      "
      @close="prompt = null"
    />
  </div>
</template>

<style scoped>
.shell {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}

.body {
  flex: 1;
  display: flex;
  min-height: 0;
}

.sidebar-wrap {
  flex: none;
  min-width: 180px;
  height: 100%;
}

.vsplit {
  flex: none;
  width: 4px;
  margin-left: -2px;
  background: transparent;
  cursor: col-resize;
  z-index: 5;
}
.vsplit:hover {
  background: var(--c-accent);
  opacity: 0.4;
}

.main {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  background: var(--c-bg);
}

.panes {
  flex: 1;
  position: relative;
  min-height: 0;
}

.pane {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
}

.welcome {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  gap: 8px;
  color: var(--c-text-tertiary);
}

.welcome-mark {
  width: 112px;
  height: 112px;
  /* 标志自带圆角本体和投影，这里不再加任何修饰 */
  user-select: none;
  -webkit-user-select: none;
}

.welcome-title {
  font-size: 21px;
  font-weight: 600;
  color: var(--c-text);
}

.welcome-sub {
  color: var(--c-text-secondary);
}

.welcome-actions {
  display: flex;
  gap: 8px;
  margin-top: 10px;
}

.welcome-tips {
  display: flex;
  gap: 18px;
  margin-top: 26px;
  font-size: var(--font-size-sm);
}
.welcome-tips kbd {
  display: inline-block;
  min-width: 15px;
  padding: 1px 5px;
  margin-right: 4px;
  border: 1px solid var(--c-border);
  border-bottom-width: 2px;
  border-radius: 3px;
  background: var(--c-bg-sunken);
  font-family: var(--font-ui);
  font-size: 10px;
}

.toasts {
  position: fixed;
  right: 14px;
  bottom: 34px;
  z-index: 300;
  display: flex;
  flex-direction: column;
  gap: 7px;
  max-width: 400px;
}

.toast {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 9px 12px;
  border: 1px solid var(--c-border);
  border-left-width: 3px;
  border-radius: var(--radius);
  background: var(--c-bg);
  box-shadow: var(--shadow-popup);
}
.toast.is-success {
  border-left-color: var(--c-success);
  color: var(--c-success);
}
.toast.is-error {
  border-left-color: var(--c-danger);
  color: var(--c-danger);
}
.toast.is-warning {
  border-left-color: var(--c-warning);
  color: var(--c-warning);
}
.toast.is-info {
  border-left-color: var(--c-accent);
  color: var(--c-accent);
}

.toast-text {
  min-width: 0;
}

.toast-title {
  font-weight: 600;
}

.toast-detail {
  margin-top: 2px;
  color: var(--c-text-secondary);
  word-break: break-word;
  max-height: 140px;
  overflow: auto;
}
</style>
