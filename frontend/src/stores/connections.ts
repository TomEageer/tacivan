import { defineStore } from 'pinia'
import { ref, computed, shallowRef, watch } from 'vue'
import * as api from '../api'
import { useAppStore } from './app'
import type {
  McpResource,
  McpTool,
  ConnectionState,
  DatabaseInfo,
  Engine,
  Favorite,
  Group,
  ObjectKind,
  ObjectNode,
} from '../types'
import { tr } from '../i18n'

export type NodeKind =
  'group' | 'connection' | 'database' | 'schema' | 'folder' | 'object'
/** 对象树上的一个节点。 */
export interface TreeNode {
  /** 全路径构成的唯一键，节点重建后仍然稳定，用来记忆展开状态。 */
  id: string
  kind: NodeKind
  label: string
  /** 右侧的次要信息，如行数、体积。 */
  detail?: string
  connId: string
  engine: Engine
  database?: string
  schema?: string
  objectKind?: ObjectKind
  object?: ObjectNode
  level: number
  /** 是否可能有子节点。 */
  expandable: boolean
  children: TreeNode[]
  loaded: boolean
  loading: boolean
  error?: string
  /** 所属连接尚未打开：节点置灰，点一下就连。 */
  offline?: boolean
  /** 来自连接的自定义数据库列表：连上后自动展开。 */
  autoOpen?: boolean
  /** 插件附加的状态与颜色（核验过/出错）。 */
  status?: string
  color?: string
  /** MCP 工具或资源的原始描述，双击时用。 */
  mcp?: McpTool | McpResource
}

/**
 * 各引擎在对象树里展示的分组，顺序即界面顺序。
 *
 * 写成函数而不是常量：分组名要翻译，模块加载时算一次的话，
 * 切换语言后树上还是旧语言，只有重开应用才会变。
 */
function folderTable(): Record<string, { kind: ObjectKind; label: string }[]> {
  return {
    mysql: [
      { kind: 'table', label: tr('表') },
      { kind: 'view', label: tr('视图') },
      { kind: 'function', label: tr('函数') },
      { kind: 'procedure', label: tr('存储过程') },
      { kind: 'trigger', label: tr('触发器') },
      { kind: 'event', label: tr('事件') },
    ],
    postgres: [
      { kind: 'table', label: tr('表') },
      { kind: 'view', label: tr('视图') },
      { kind: 'function', label: tr('函数') },
      { kind: 'procedure', label: tr('存储过程') },
      { kind: 'sequence', label: tr('序列') },
      { kind: 'trigger', label: tr('触发器') },
    ],
    sqlite: [
      { kind: 'table', label: tr('表') },
      { kind: 'view', label: tr('视图') },
      { kind: 'trigger', label: tr('触发器') },
      { kind: 'index', label: tr('索引') },
    ],
  }
}

function foldersFor(engine: Engine) {
  const all = folderTable()
  // 插件连接只有"表"这一层：库表都是插件自己定义的，没有视图/函数那些
  if (String(engine).startsWith('plugin:')) return [all.mysql[0]]
  if (engine === 'mariadb') return all.mysql
  return all[engine] ?? all.mysql
}

export const useConnectionsStore = defineStore('connections', () => {
  const app = useAppStore()
  const states = ref<ConnectionState[]>([])
  const roots = shallowRef<TreeNode[]>([])
  /** 连接分组，按顺序。 */
  const groups = ref<Group[]>([])
  /** 记住用户收起过哪些分组；分组默认是展开的，只记例外。 */
  const collapsedGroups = new Set<string>(
    (() => {
      try {
        return JSON.parse(localStorage.getItem('tacivan.groups.collapsed') || '[]') as string[]
      } catch {
        return []
      }
    })(),
  )
  function persistCollapsed() {
    try {
      localStorage.setItem('tacivan.groups.collapsed', JSON.stringify([...collapsedGroups]))
    } catch {
      // 存不了就算了，只是个便利
    }
  }
  /** 展开的节点 id 集合，与节点对象分离，刷新树后仍能恢复展开状态。 */
  const expanded = ref<Set<string>>(new Set())
  const selectedId = ref<string>('')
  const filter = ref('')
  const loading = ref(false)
  /**
   * 筛选范围：限定在某个节点的子树内搜索。
   *
   * 实例里有三千多个库、每库上百张表时，全局搜一个表名会命中一大片同名表
   * （按月分表尤其如此）。限定到某个库里搜，才是真正要的。
   */
  const filterScope = ref<string>('')
  const filterScopeLabel = ref<string>('')
  /** 收藏的节点 key 集合，key 与后端的 Favorite.Key 对应。 */
  const favorites = ref<Set<string>>(new Set())
  /** 收藏的完整条目，用来在连接未打开时也能把收藏列出来。 */
  const favoriteList = ref<Favorite[]>([])
  const onlyFavorites = ref(false)
  /** 节点对应的收藏 key。 */
  function favKeyOf(node: TreeNode): string {
    const name = node.kind === 'object' ? node.label : ''
    return [node.connId, node.database ?? '', node.schema ?? '', name].join(
      "\x1f",
    )
  }

  function isFavorite(node: TreeNode): boolean {
    if (node.kind !== 'database' && node.kind !== 'object') return false
    return favorites.value.has(favKeyOf(node))
  }

  /**
   * 已经自动展开过的收藏节点。
   *
   * 只在节点「首次出现」时替它展开一次：这样默认是摊开的（收藏本来就不多），
   * 而用户主动折叠之后不会在下一次刷新时又被强行展开回去。
   */
  const favAutoExpanded = new Set<string>()
  function ensureFavoritesExpanded() {
    const next = new Set(expanded.value)
    let changed = false
    const walk = (nodes: TreeNode[]) => {
      for (const n of nodes) {
        if (n.children.length && !favAutoExpanded.has(n.id)) {
          favAutoExpanded.add(n.id)
          next.add(n.id)
          changed = true
        }
        walk(n.children)
      }
    }
    walk(favoriteTree.value)
    if (changed) expanded.value = next
  }

  // 切进收藏视图、或收藏内容有变动时，为新出现的节点预置展开。
  watch(
    [onlyFavorites, favoriteList, states],
    () => {
      if (onlyFavorites.value) ensureFavoritesExpanded()
    },
    { immediate: true },
  )
  async function loadFavorites() {
    try {
      const list = await api.ListFavorites()
      favoriteList.value = list
      favorites.value = new Set(
        list.map((f) =>
          [f.connectionId, f.database, f.schema, f.name].join("\x1f"),
        ),
      )
    } catch {
      // 收藏读不到不影响浏览。
    }
  }

  async function toggleFavorite(node: TreeNode) {
    if (node.kind !== 'database' && node.kind !== 'object') return
    const fav: Favorite = {
      connectionId: node.connId,
      database: node.database ?? '',
      schema: node.schema ?? '',
      name: node.kind === 'object' ? node.label : '',
      kind: node.kind === 'object' ? (node.objectKind ?? 'table') : 'database',
    }
    try {
      const added = await api.ToggleFavorite(fav)
      const key = favKeyOf(node)
      const next = new Set(favorites.value)
      if (added) {
        next.add(key)
        favoriteList.value = [...favoriteList.value, fav]
      } else {
        next.delete(key)
        favoriteList.value = favoriteList.value.filter(
          (f) =>
            [f.connectionId, f.database, f.schema, f.name].join("\x1f") !== key,
        )
      }
      favorites.value = next
    } catch (e) {
      app.reportError(e, tr('收藏失败'))
    }
  }

  /** 把搜索限定到某个节点的子树内。 */
  function setFilterScope(node: TreeNode | null) {
    filterScope.value = node?.id ?? ''
    filterScopeLabel.value = node?.label ?? ''
  }

  function stateOf(connId: string) {
    return states.value.find((s) => s.config.id === connId)
  }

  /** 触发一次 shallowRef 更新：树是大对象，用整体替换代替深响应。 */
  function touch() {
    roots.value = [...roots.value]
  }

  async function loadConnections() {
    loading.value = true
    try {
      // 分组读不到不该让连接列表也消失——它只是个整理方式
      const [st, gs] = await Promise.all([
        api.ListConnections(),
        Promise.resolve()
          .then(() => (typeof api.ListGroups === 'function' ? api.ListGroups() : []))
          .catch(() => [] as Group[]),
      ])
      states.value = st
      groups.value = gs ?? []
      rebuildRoots()
    } catch (e) {
      app.reportError(e, tr('读取连接列表失败'))
    } finally {
      loading.value = false
    }
  }

  /** 按连接列表重建根节点，尽量复用已有节点以保住已加载的子树。 */
  function rebuildRoots() {
    // 之前的连接节点可能挂在根上也可能挂在分组下，都要能复用
    const prev = new Map<string, TreeNode>()
    for (const n of roots.value) {
      prev.set(n.id, n)
      if (n.kind === 'group') for (const c of n.children) prev.set(c.id, c)
    }
    const connNode = (s: ConnectionState, level: number): TreeNode => {
      const node = buildConnNode(s)
      node.level = level
      for (const c of node.children) c.level = level + 1 // 库节点层级跟着变
      return node
    }
    const byGroup = new Map<string, ConnectionState[]>()
    const rootConns: ConnectionState[] = []
    for (const s of states.value) {
      const g = s.config.group?.trim() ?? ''
      if (g) {
        if (!byGroup.has(g)) byGroup.set(g, [])
        byGroup.get(g)!.push(s)
      } else rootConns.push(s)
    }
    const order = groups.value.map((g) => g.name)
    for (const g of byGroup.keys()) if (!order.includes(g)) order.push(g)
    const out: TreeNode[] = []
    for (const name of order) {
      const id = `g:${name}`
      // 分组默认展开：用户收起过的才收着
      if (!collapsedGroups.has(name) && !expanded.value.has(id)) expanded.value.add(id)
      out.push({
        id,
        kind: 'group',
        label: name,
        connId: '',
        engine: 'mysql',
        level: 0,
        expandable: true,
        children: (byGroup.get(name) ?? []).map((s) => connNode(s, 1)),
        loaded: true,
        loading: false,
        detail: String((byGroup.get(name) ?? []).length),
      })
    }
    for (const s of rootConns) out.push(connNode(s, 0))
    roots.value = out

    function buildConnNode(s: ConnectionState): TreeNode {
      const id = `c:${s.config.id}`
      const existing = prev.get(id)
      if (existing && s.open) {
        existing.label = s.config.name
        existing.engine = s.config.engine
        existing.detail = s.serverInfo?.version
          ? shortVersion(s.serverInfo.version)
          : undefined
        return existing
      }
      return {
        id,
        kind: 'connection' as NodeKind,
        label: s.config.name,
        connId: s.config.id,
        engine: s.config.engine,
        level: 0,
        expandable: true,
        children: [],
        loaded: false,
        loading: false,
        detail:
          s.open && s.serverInfo?.version
            ? shortVersion(s.serverInfo.version)
            : undefined,
      }
    }
  }

  /** 连接节点可能在根上也可能在分组下。 */
  function findConnNode(connId: string): TreeNode | undefined {
    for (const n of roots.value) {
      if (n.kind === 'connection' && n.connId === connId) return n
      if (n.kind === 'group') {
        const hit = n.children.find((c) => c.connId === connId)
        if (hit) return hit
      }
    }
    return undefined
  }

  // --- 分组与排序 ---
  async function createGroup(name: string) {
    groups.value = await api.SaveGroup(name)
    rebuildRoots()
  }
  async function renameGroup(oldName: string, newName: string) {
    await api.RenameGroup(oldName, newName)
    if (collapsedGroups.delete(oldName)) {
      collapsedGroups.add(newName)
      persistCollapsed()
    }
    await loadConnections()
  }
  async function deleteGroup(name: string) {
    await api.DeleteGroup(name)
    collapsedGroups.delete(name)
    persistCollapsed()
    await loadConnections()
  }
  async function moveConnection(id: string, group: string) {
    await api.MoveConnection(id, group)
    await loadConnections()
  }
  /**
   * 拖放落点：把 dragId 放到 targetId 之前/之后（同时并入目标所在分组），
   * 或放进某个分组末尾。排序用全量 id 顺序回写，分组顺序单独回写。
   */
  async function dropConnection(dragId: string, target: TreeNode, place: 'before' | 'after' | 'into') {
    const st = states.value.find((s) => s.config.id === dragId)
    if (!st) return
    if (target.kind === 'group') {
      await api.MoveConnection(dragId, target.label)
      await loadConnections()
      return
    }
    if (target.kind !== 'connection' || target.connId === dragId) return
    const tst = states.value.find((s) => s.config.id === target.connId)
    const group = tst?.config.group ?? ''
    if ((st.config.group ?? '') !== group) await api.MoveConnection(dragId, group)
    const ids = states.value.map((s) => s.config.id).filter((id) => id !== dragId)
    const at = ids.indexOf(target.connId)
    ids.splice(place === 'after' ? at + 1 : at, 0, dragId)
    await api.ReorderConnections(ids)
    await loadConnections()
  }
  async function dropGroup(dragName: string, targetName: string, place: 'before' | 'after') {
    const names = groups.value.map((g) => g.name).filter((n) => n !== dragName)
    const at = names.indexOf(targetName)
    if (at < 0) return
    names.splice(place === 'after' ? at + 1 : at, 0, dragName)
    await api.ReorderGroups(names)
    await loadConnections()
  }
  function toggleGroupCollapsed(name: string, collapsed: boolean) {
    if (collapsed) collapsedGroups.add(name)
    else collapsedGroups.delete(name)
    persistCollapsed()
  }

  function shortVersion(v: string) {
    const m = v.match(/(\d+\.\d+(\.\d+)?)/)
    return m ? m[1] : v.slice(0, 20)
  }

  function isOpen(connId: string) {
    return stateOf(connId)?.open === true
  }

  /** 正在建立的连接，避免连接慢时重复双击发起多条连接。 */
  const connecting = new Map<string, Promise<void>>()
  function openConnection(connId: string): Promise<void> {
    const pending = connecting.get(connId)
    if (pending) return pending
    const p = doOpenConnection(connId).finally(() => connecting.delete(connId))
    connecting.set(connId, p)
    return p
  }

  async function doOpenConnection(connId: string) {
    const node = findConnNode(connId)
    if (node) {
      node.loading = true
      touch()
    }
    try {
      const st = await api.OpenConnection(connId)
      const i = states.value.findIndex((s) => s.config.id === connId)
      if (i >= 0) states.value[i] = st
      rebuildRoots()
      const fresh = findConnNode(connId)
      if (fresh) {
        await loadChildren(fresh)
        expanded.value.add(fresh.id)
      }
    } catch (e) {
      app.reportError(e, tr('连接失败'))
      if (node) node.loading = false
    } finally {
      if (node) node.loading = false
      touch()
    }
  }

  async function closeConnection(connId: string) {
    try {
      await api.CloseConnection(connId)
    } catch (e) {
      app.reportError(e, tr('断开连接失败'))
    }
    const i = states.value.findIndex((s) => s.config.id === connId)
    if (i >= 0) states.value[i] = { ...states.value[i], open: false }
    // 折叠该连接下的全部展开状态，下次打开从头加载。
    for (const id of [...expanded.value]) {
      if (id.startsWith(`c:${connId}`)) expanded.value.delete(id)
    }
    const node = findConnNode(connId)
    if (node) {
      node.children = []
      node.loaded = false
      node.detail = undefined
    }
    touch()
  }

  /**
   * 正在进行的子节点加载，按节点 id 去重。
   *
   * 这个去重不能靠 node.loading：那是界面上的「转圈」标志，
   * 打开连接时也会把它置起来，结果加载子节点的调用被当成重复请求挡掉，
   * 表现为「第一次双击展开了却什么都没有，第三次才出来」。
   * 一个标志同时承担界面状态和并发控制，迟早撞车。
   */
  const loadingChildren = new Map<string, Promise<void>>()
  /** 加载一个节点的子节点；同一节点的并发调用会合并成一次请求。 */
  function loadChildren(node: TreeNode): Promise<void> {
    const pending = loadingChildren.get(node.id)
    if (pending) return pending
    const p = doLoadChildren(node).finally(() =>
      loadingChildren.delete(node.id),
    )
    loadingChildren.set(node.id, p)
    return p
  }

  async function doLoadChildren(node: TreeNode) {
    node.loading = true
    node.error = undefined
    touch()
    try {
      switch (node.kind) {
        case 'group':
          break
        case 'connection':
          node.children = await buildDatabaseNodes(node)
          // 自定义列表里勾了「自动打开」的库，连上就展开。
          void autoOpenDatabases(node)
          break
        case 'database':
          node.children = await buildDatabaseChildren(node)
          break
        case 'schema':
          node.children = buildFolderNodes(node, node.database!, node.label)
          break
        case 'folder':
          node.children = await buildObjectNodes(node)
          // 行数与体积属于统计信息，取它比列目录贵得多。
          // 先把表名显示出来，数字随后异步补上，展开就不会卡住。
          if (node.objectKind === 'table') void loadTableStats(node)
          break
      }
      node.loaded = true
    } catch (e) {
      node.error = e instanceof Error ? e.message : String(e)
      app.reportError(e, tr('加载「{name}」失败', { name: node.label }))
    } finally {
      node.loading = false
      touch()
    }
  }

  async function buildDatabaseNodes(node: TreeNode): Promise<TreeNode[]> {
    // MCP 连接没有库：直接给两个分组，工具和资源
    if (node.engine === 'mcp') {
      return (
        [
          { kind: 'tool' as ObjectKind, label: tr('工具') },
          { kind: 'resource' as ObjectKind, label: tr('资源') },
        ] as const
      ).map((f) => ({
        id: `${node.id}/f:${f.kind}`,
        kind: 'folder' as NodeKind,
        label: f.label,
        connId: node.connId,
        engine: node.engine,
        objectKind: f.kind,
        level: 1,
        expandable: true,
        children: [],
        loaded: false,
        loading: false,
      }))
    }
    const dbs: DatabaseInfo[] = await api.ListDatabases(node.connId)
    return dbs.map((d) => ({
      id: `${node.id}/d:${d.name}`,
      kind: 'database' as NodeKind,
      label: d.name,
      detail: d.detail || d.collation || undefined,
      color: d.color || undefined,
      status: d.status || undefined,
      connId: node.connId,
      engine: node.engine,
      database: d.name,
      level: 1,
      // Redis 的库没有子树（键在键浏览器里），箭头就不该画出来
      expandable: node.engine !== 'redis',
      autoOpen: d.autoOpen === true,
      children: [],
      loaded: false,
      loading: false,
    }))
  }

  /**
   * 展开连接配置里标了「自动打开」的库。
   *
   * 不 await：每个库都要向服务端要一次对象列表，串在连接展开里会把
   * 整棵树的出现时间拖长。库名先显示出来，内容随后自己填进去。
   */
  async function autoOpenDatabases(node: TreeNode) {
    for (const child of node.children) {
      if (!child.autoOpen) continue
      expanded.value.add(child.id)
      void loadChildren(child)
    }
  }

  async function buildDatabaseChildren(node: TreeNode): Promise<TreeNode[]> {
    // Redis 的库下直接就是键，没有对象分组。
    if (node.engine === 'redis') return []
    // PostgreSQL 的库下先是 schema 层。
    if (node.engine === 'postgres') {
      const schemas = await api.ListSchemas(node.connId, node.database!)
      return schemas.map((s) => ({
        id: `${node.id}/s:${s.name}`,
        kind: 'schema' as NodeKind,
        label: s.name,
        detail: s.owner || undefined,
        connId: node.connId,
        engine: node.engine,
        database: node.database,
        schema: s.name,
        level: 2,
        expandable: true,
        children: [],
        loaded: false,
        loading: false,
      }))
    }
    return buildFolderNodes(node, node.database!, '')
  }

  function buildFolderNodes(
    node: TreeNode,
    database: string,
    schema: string,
  ): TreeNode[] {
    return foldersFor(node.engine).map((f) => ({
      id: `${node.id}/f:${f.kind}`,
      kind: 'folder' as NodeKind,
      label: f.label,
      connId: node.connId,
      engine: node.engine,
      database,
      schema: node.kind === 'schema' ? schema : node.schema,
      objectKind: f.kind,
      level: node.level + 1,
      expandable: true,
      children: [],
      loaded: false,
      loading: false,
    }))
  }

  async function buildObjectNodes(node: TreeNode): Promise<TreeNode[]> {
    if (node.objectKind === 'tool' || node.objectKind === 'resource') {
      const items =
        node.objectKind === 'tool'
          ? (await api.McpTools(node.connId)).map((t) => ({ label: t.name, detail: t.title || t.description || '', payload: t }))
          : ((await api.McpResources(node.connId)) ?? []).map((r) => ({ label: r.name || r.uri, detail: r.uri, payload: r }))
      return items.map((it) => ({
        id: `${node.id}/o:${it.label}`,
        kind: 'object' as NodeKind,
        label: it.label,
        detail: it.detail || undefined,
        connId: node.connId,
        engine: node.engine,
        objectKind: node.objectKind,
        mcp: it.payload,
        level: node.level + 1,
        expandable: false,
        children: [],
        loaded: true,
        loading: false,
      }))
    }
    const objs = await api.ListObjects(
      node.connId,
      node.database ?? '',
      node.schema ?? '',
      [node.objectKind!],
    )
    return objs.map((o) => ({
      id: `${node.id}/o:${o.name}`,
      kind: 'object' as NodeKind,
      label: o.name,
      detail: o.detail || detailOf(o),
      color: o.color || undefined,
      status: o.status || undefined,
      connId: node.connId,
      engine: node.engine,
      database: node.database,
      schema: node.schema,
      objectKind: o.kind,
      object: o,
      level: node.level + 1,
      expandable: false,
      children: [],
      loaded: true,
      loading: false,
    }))
  }

  /** 异步补齐某个「表」分组下各表的行数与体积。 */
  async function loadTableStats(folder: TreeNode) {
    try {
      const stats = await api.GetTableStats(
        folder.connId,
        folder.database ?? '',
      )
      let changed = false
      for (const child of folder.children) {
        const st = stats[child.label]
        if (!st) continue
        child.object = { ...(child.object as ObjectNode), ...st }
        child.detail = detailOf(st)
        changed = true
      }
      if (changed) touch()
    } catch {
      // 统计信息取不到不影响浏览，树上不显示数字即可。
    }
  }

  function detailOf(o: ObjectNode) {
    const parts: string[] = []
    if (o.rowsText) parts.push(tr('{n} 行', { n: o.rowsText }))
    if (o.sizeText) parts.push(o.sizeText)
    return parts.join(' · ') || undefined
  }

  async function toggle(node: TreeNode) {
    if (!node.expandable) return
    if (expanded.value.has(node.id)) {
      // 标记为展开却没有子节点（刷新过父节点之后就是这种状态）：
      // 用户看到的是"收着的"，这一下他是想展开，不能把它当成收起。
      if (!node.loaded && !node.loading) {
        await loadChildren(node)
        return
      }
      expanded.value.delete(node.id)
      touch()
      return
    }
    if (node.kind === 'connection' && !isOpen(node.connId)) {
      await openConnection(node.connId)
      return
    }
    expanded.value.add(node.id)
    if (!node.loaded) await loadChildren(node)
    else touch()
  }

  /** 刷新节点：丢弃已加载的子树后重新拉取，原来展开着的后代也一并展开回来。 */
  async function refresh(node: TreeNode) {
    node.loaded = false
    node.children = []
    if (expanded.value.has(node.id)) {
      await loadChildren(node)
      await restoreExpanded(node)
    } else touch()
  }

  /**
   * 重新加载后把之前展开的后代补回来。
   *
   * 子节点 id 是按路径算的，刷新前后不变，所以 expanded 里还记着它们；
   * 但新建出来的子节点是空壳，不把它们的孩子重新拉一遍，树上就是
   * "标记为展开、实际什么都没有"——用户看到的是全收起了，再点一下又被当成收起，
   * 于是得点两轮才展得开。
   */
  async function restoreExpanded(node: TreeNode) {
    for (const child of node.children) {
      if (!child.expandable || !expanded.value.has(child.id)) continue
      if (!child.loaded) await loadChildren(child)
      await restoreExpanded(child)
    }
  }

  /** 找到某个连接/库下的对象分组节点，用于结构变更后局部刷新。 */
  function findFolder(
    connId: string,
    database: string,
    schema: string,
    kind: ObjectKind,
  ) {
    const walk = (nodes: TreeNode[]): TreeNode | undefined => {
      for (const n of nodes) {
        if (
          n.kind === 'folder' &&
          n.connId === connId &&
          (n.database ?? '') === database &&
          (n.schema ?? '') === (schema ?? '') &&
          n.objectKind === kind
        ) {
          return n
        }
        const found = walk(n.children)
        if (found) return found
      }
      return undefined
    }
    return walk(roots.value)
  }

  async function refreshObjectFolder(
    connId: string,
    database: string,
    schema: string,
    kind: ObjectKind,
  ) {
    const folder = findFolder(connId, database, schema ?? '', kind)
    if (folder) await refresh(folder)
  }

  /**
   * 扁平化的可见节点列表。
   *
   * 树用扁平数组渲染而不是递归组件：一个库里有几千张表时，
   * 递归组件的更新开销会让展开操作明显卡顿，扁平数组配合虚拟滚动则是常数级。
   */
  /**
   * 收藏视图。
   *
   * 直接用收藏条目搭出来，不走主树——否则连接没打开时主树里根本没有这些节点，
   * 「只看收藏」会是一片空白。未打开的连接下节点置灰，点一下就连上。
   */
  const favoriteTree = computed<TreeNode[]>(() => {
    const byConn = new Map<string, Favorite[]>()
    for (const f of favoriteList.value) {
      const arr = byConn.get(f.connectionId) ?? []
      arr.push(f)
      byConn.set(f.connectionId, arr)
    }

    const out: TreeNode[] = []
    for (const [connId, favs] of byConn) {
      const st = stateOf(connId)
      // 连接已被删除时，它名下的收藏不再展示。
      if (!st) continue
      const offline = !st.open
      const connNode: TreeNode = {
        id: `fav:c:${connId}`,
        kind: 'connection',
        label: st.config.name,
        detail:
          !offline && st.serverInfo?.version
            ? shortVersion(st.serverInfo.version)
            : undefined,
        connId,
        engine: st.config.engine,
        level: 0,
        expandable: true,
        children: [],
        loaded: true,
        loading: false,
        offline,
      }
      const byDb = new Map<string, Favorite[]>()
      for (const f of favs) {
        const arr = byDb.get(f.database) ?? []
        arr.push(f)
        byDb.set(f.database, arr)
      }

      for (const [database, items] of byDb) {
        const dbNode: TreeNode = {
          id: `fav:c:${connId}/d:${database}`,
          kind: 'database',
          label: database,
          connId,
          engine: st.config.engine,
          database,
          level: 1,
          expandable: items.some((f) => f.name !== ''),
          children: [],
          loaded: true,
          loading: false,
          offline,
        }
        for (const f of items) {
          if (!f.name) continue
          dbNode.children.push({
            id: `fav:c:${connId}/d:${database}/o:${f.name}`,
            kind: 'object',
            label: f.name,
            connId,
            engine: st.config.engine,
            database,
            schema: f.schema,
            objectKind: (f.kind || 'table') as ObjectKind,
            level: 2,
            expandable: false,
            children: [],
            loaded: true,
            loading: false,
            offline,
          })
        }
        connNode.children.push(dbNode)
      }
      out.push(connNode)
    }
    return out
  })
  const visibleNodes = computed<TreeNode[]>(() => {
    const out: TreeNode[] = []
    const kw = filter.value.trim().toLowerCase()
    const scope = filterScope.value
    const favOnly = onlyFavorites.value
    if (favOnly) {
      // 按 expanded 走，折叠才会真的生效。
      // 「默认展开」由 ensureFavoritesExpanded 在节点首次出现时预置，
      // 之前图省事在这里无条件递归，结果箭头收起了、子节点还在。
      const walk = (nodes: TreeNode[]) => {
        for (const n of nodes) {
          const selfMatch = !kw || n.label.toLowerCase().includes(kw)
          const childMatch = kw ? hasMatch(n.children, kw) : false
          if (kw && !selfMatch && !childMatch) continue
          out.push(n)
          // 搜索时自动打开命中路径，其余情况完全听 expanded 的。
          if (expanded.value.has(n.id) || childMatch) walk(n.children)
        }
      }
      walk(favoriteTree.value)
      return out
    }

    /** 子树里是否存在收藏项，用来决定这条路径要不要保留。 */
    const hasFavorite = (nodes: TreeNode[]): boolean => {
      for (const n of nodes) {
        if (isFavorite(n)) return true
        if (hasFavorite(n.children)) return true
      }
      return false
    }
    // forced 表示祖先已经命中关键词，此时子树整体放行。
    // 否则会出现「筛选到某个库、展开后里面空空如也」——因为「表」「视图」
    // 这些分组节点的名字不含关键词，被连同它们下面的对象一起滤掉了。
    const push = (nodes: TreeNode[], forced: boolean) => {
      for (const n of nodes) {
        // 只看收藏：保留收藏项本身，以及通往收藏项的路径。
        if (favOnly && !isFavorite(n) && !hasFavorite(n.children)) continue
        if (!kw || forced) {
          out.push(n)
          if (
            expanded.value.has(n.id) ||
            (favOnly && hasFavorite(n.children))
          ) {
            push(n.children, forced)
          }
          continue
        }
        const selfMatch = n.label.toLowerCase().includes(kw)
        const childMatches = hasMatch(n.children, kw)
        if (!selfMatch && !childMatches) continue
        out.push(n)
        // 后代命中时自动展开这条路径；自身命中则其子树不再过滤。
        if (childMatches || expanded.value.has(n.id))
          push(n.children, selfMatch)
      }
    }
    // 限定了范围时，范围外的节点只作为路径展示，不参与关键词过滤。
    if (scope && kw) {
      const target = findById(roots.value, scope)
      if (target) {
        const path = pathTo(roots.value, scope)
        for (const p of path) out.push(p)
        push(target.children, false)
        return out
      }
    }

    push(roots.value, false)
    return out
  })
  /** 按 id 找节点。 */
  function findById(nodes: TreeNode[], id: string): TreeNode | undefined {
    for (const n of nodes) {
      if (n.id === id) return n
      const found = findById(n.children, id)
      if (found) return found
    }
    return undefined
  }

  /** 从根到目标节点的路径（含目标自身）。 */
  function pathTo(nodes: TreeNode[], id: string): TreeNode[] {
    for (const n of nodes) {
      if (n.id === id) return [n]
      const sub = pathTo(n.children, id)
      if (sub.length) return [n, ...sub]
    }
    return []
  }

  function hasMatch(nodes: TreeNode[], kw: string): boolean {
    for (const n of nodes) {
      if (n.label.toLowerCase().includes(kw)) return true
      if (hasMatch(n.children, kw)) return true
    }
    return false
  }

  function select(id: string) {
    selectedId.value = id
  }

  const selectedNode = computed(() =>
    visibleNodes.value.find((n) => n.id === selectedId.value),
  )
  async function setCurrentDatabase(connId: string, database: string) {
    try {
      await api.SetCurrentDatabase(connId, database)
      const i = states.value.findIndex((s) => s.config.id === connId)
      if (i >= 0) states.value[i] = { ...states.value[i], currentDb: database }
    } catch {
      // 当前库只影响新开标签页的默认值，失败不值得打断用户。
    }
  }

  return {
    states,
    roots,
    expanded,
    selectedId,
    selectedNode,
    filter,
    filterScope,
    filterScopeLabel,
    favorites,
    favoriteList,
    onlyFavorites,
    loading,
    visibleNodes,
    groups,
    createGroup,
    renameGroup,
    deleteGroup,
    moveConnection,
    dropConnection,
    dropGroup,
    toggleGroupCollapsed,
    stateOf,
    isOpen,
    loadConnections,
    openConnection,
    closeConnection,
    toggle,
    refresh,
    refreshObjectFolder,
    select,
    setCurrentDatabase,
    setFilterScope,
    isFavorite,
    toggleFavorite,
    loadFavorites,
    touch,
  }
})