<script setup lang="ts">
/** 选项对话框。 */
import { computed, onMounted, ref } from 'vue'
import * as api from '../../api'
import { useAppStore } from '../../stores/app'
import Modal from '../common/Modal.vue'
import markDay from '../../assets/mark-day.svg'
import markNight from '../../assets/mark-night.svg'
import type { PluginInfo, Settings } from '../../types'
import { tr } from '../../i18n'

const emit = defineEmits<{ (e: 'close'): void }>()
const app = useAppStore()

/**
 * 换页带方向：往右点的页从右边滑进来，往左点的从左边。
 * out-in 配很短的时长，配合对话框顶部锚定，看起来是"平移过去"而不是"闪一下"。
 */
const slideName = ref('slide-left')
const PAGE_ORDER = ['general', 'editor', 'performance', 'plugins', 'about'] as const
function goPage(next: typeof page.value) {
  const order: readonly string[] = PAGE_ORDER
  slideName.value = order.indexOf(next) > order.indexOf(page.value) ? 'slide-left' : 'slide-right'
  page.value = next
}
// --- 插件 ---
const plugins = ref<PluginInfo[]>([])
const pluginBusy = ref(false)
async function loadPlugins() {
  try {
    plugins.value = await api.ListPlugins()
  } catch (e) {
    app.reportError(e, tr('读取插件列表失败'))
  }
}
async function importPlugin() {
  pluginBusy.value = true
  try {
    const p = await api.ImportPluginDialog()
    if (p) {
      app.toast('success', tr('插件已导入：{name}', { name: p.name }), tr('新建连接时可以选到它了'))
      await loadPlugins()
    }
  } catch (e) {
    app.reportError(e, tr('导入插件失败'))
  } finally {
    pluginBusy.value = false
  }
}
/** 移除是破坏性的：第一次点只是"武装"，五秒内再点才真删。 */
const armedRemove = ref('')
let armTimer = 0
async function removePlugin(p: PluginInfo) {
  if (armedRemove.value !== p.id) {
    armedRemove.value = p.id
    window.clearTimeout(armTimer)
    armTimer = window.setTimeout(() => (armedRemove.value = ''), 5000)
    return
  }
  armedRemove.value = ''
  try {
    await api.RemovePlugin(p.id)
    app.toast('success', tr('已移除插件 {name}', { name: p.name }), tr('用它建的连接配置还在，只是打不开了'))
    await loadPlugins()
  } catch (e) {
    app.reportError(e, tr('移除插件失败'))
  }
}
onMounted(loadPlugins)

const aboutMark = computed(() => (app.effectiveTheme === 'dark' ? markNight : markDay))
const page = ref<'general' | 'editor' | 'performance' | 'plugins' | 'about'>('general')
const draft = ref<Settings>({ ...app.settings })

const memoryMb = computed({
  get: () => draft.value.memoryLimitMb,
  set: (v: number) => (draft.value.memoryLimitMb = Math.max(64, Math.min(8192, v || 64))),
})

async function save() {
  await app.saveSettings(draft.value)
  emit('close')
}

function reset() {
  draft.value = {
    theme: 'system',
    language: 'system',
    gridPageSize: 200,
    rowLimit: 1000,
    memoryLimitMb: 512,
    cellPreviewLimit: 4096,
    maxResultRows: 5000000,
    autoCommit: true,
    confirmOnDelete: true,
    fontSize: 13,
    editorFont: draft.value.editorFont,
    showSystemObjects: false,
    historyLimit: 500,
    diagnosticLog: true,
    slowQueryMs: 300,
  }
}

const info = computed(() => app.info)

async function revealLog() {
  try {
    await api.RevealLogFile()
  } catch (e) {
    app.reportError(e, tr('打开日志失败'))
  }
}
</script>

<template>
  <Modal :title="tr('选项')" :width="580" @close="emit('close')">
    <nav class="pager">
      <button
        v-for="p in [
          { key: 'general', label: tr('常规') },
          { key: 'editor', label: tr('编辑器') },
          { key: 'performance', label: tr('性能') },
          { key: 'plugins', label: tr('插件') },
          { key: 'about', label: tr('关于') },
        ]"
        :key="p.key"
        class="pager-btn"
        :class="{ 'is-active': page === p.key }"
        @click="goPage(p.key as typeof page)"
      >
        {{ p.label }}
      </button>
    </nav>

    <Transition :name="slideName" mode="out-in">
    <section v-if="page === 'general'" key="general">
      <div class="field">
        <label>{{ tr('外观') }}</label>
        <select v-model="draft.theme" class="select">
          <option value="system">{{ tr('跟随系统') }}</option>
          <option value="light">{{ tr('浅色') }}</option>
          <option value="dark">{{ tr('深色') }}</option>
        </select>
      </div>
      <div class="field">
        <label>{{ tr('语言') }}</label>
        <!-- 语言名一律用它自己的语言写，这是通行做法：
             界面已经切成英文时，"Chinese" 反而不如"简体中文"好认。 -->
        <select v-model="draft.language" class="select">
          <option value="system">{{ tr('跟随系统') }}</option>
          <option value="zh-CN">简体中文</option>
          <option value="en-US">English</option>
        </select>
      </div>
      <div class="field-hint">{{ tr('菜单栏的文字在重启后才会跟着变') }}</div>
      <div class="field">
        <label>{{ tr('网格每页') }}</label>
        <input v-model.number="draft.gridPageSize" class="input" type="number" min="20" max="2000" />
      </div>
      <div class="field-hint">{{ tr('首屏与每次滚动向服务端请求的行数') }}</div>
      <div class="field">
        <label>{{ tr('取数上限') }}</label>
        <input v-model.number="draft.rowLimit" class="input" type="number" min="0" />
      </div>
      <div class="field-hint">
        {{ tr('打开表数据时默认给 SELECT 加的 LIMIT，0 表示不加。每个标签页还能在工具条上单独调整。') }}
      </div>
      <div class="divider"></div>
      <label class="checkbox">
        <input v-model="draft.showSystemObjects" type="checkbox" />{{ tr('在对象树中显示系统库') }}</label>
      <label class="checkbox">
        <input v-model="draft.confirmOnDelete" type="checkbox" />{{ tr('删除数据前二次确认') }}</label>
      <label class="checkbox">
        <input v-model="draft.autoCommit" type="checkbox" />{{ tr('SQL 编辑器默认自动提交') }}</label>
      <div class="divider"></div>
      <div class="field">
        <label>{{ tr('历史条数') }}</label>
        <input v-model.number="draft.historyLimit" class="input" type="number" min="50" max="5000" />
      </div>
    </section>

    <section v-else-if="page === 'editor'" key="editor">
      <div class="field">
        <label>{{ tr('字号') }}</label>
        <input v-model.number="draft.fontSize" class="input" type="number" min="9" max="24" />
      </div>
      <div class="field">
        <label>{{ tr('等宽字体') }}</label>
        <input v-model="draft.editorFont" class="input mono" />
      </div>
      <div class="field-hint">{{ tr('CSS 字体族写法，可写多个候选，逗号分隔') }}</div>
      <div class="preview" :style="{ fontFamily: draft.editorFont, fontSize: draft.fontSize + 'px' }">
        SELECT id, name FROM users WHERE age &gt; 18 ORDER BY created_at DESC;
      </div>
    </section>

    <section v-else-if="page === 'performance'" key="performance">
      <div class="field">
        <label>{{ tr('内存上限') }}</label>
        <input v-model.number="memoryMb" class="input" type="number" min="64" max="8192" />
      </div>
      <div class="field-hint">
        {{ tr('MB。所有结果集的常驻内存总量上限，超过后最久未用的数据会转存到磁盘。调小更省内存但回滚查看旧数据时会多一次磁盘读取。') }}
      </div>

      <div class="field">
        <label>{{ tr('单元格预览') }}</label>
        <input
          v-model.number="draft.cellPreviewLimit"
          class="input"
          type="number"
          min="256"
          max="1048576"
        />
      </div>
      <div class="field-hint">
        {{ tr('字节。大字段只把前这么多字节读进网格，完整值在双击单元格时按主键回查。这是大表不吃内存的关键，不建议调得过大。') }}
      </div>

      <div class="field">
        <label>{{ tr('结果行数上限') }}</label>
        <input v-model.number="draft.maxResultRows" class="input" type="number" min="0" />
      </div>
      <div class="field-hint">
        {{ tr('单个结果集最多拉取的行数，0 表示不限。这是一道保护闸，防止误发的全表查询把磁盘写满。') }}
      </div>

      <div class="divider"></div>

      <label class="checkbox">
        <input v-model="draft.diagnosticLog" type="checkbox" />{{ tr('记录诊断日志') }}</label>
      <div class="field-hint">
        {{ tr('把每条语句的耗时、返回行数写进日志文件。开销只有一行文本，但排查「为什么慢」时能直接定位到具体是哪一步，建议保持开启。') }}
      </div>
      <div class="field">
        <label>{{ tr('慢语句阈值') }}</label>
        <input v-model.number="draft.slowQueryMs" class="input" type="number" min="50" />
      </div>
      <div class="field-hint">{{ tr('毫秒。超过该值的语句在日志里标记 slow=true，便于筛选') }}</div>
      <div v-if="info?.logPath" class="log-row">
        <code class="mono selectable">{{ info.logPath }}</code>
        <button class="btn" @click="revealLog">{{ tr('在访达中显示') }}</button>
      </div>

      <div class="divider"></div>
      <button class="btn" @click="reset">{{ tr('恢复默认值') }}</button>
    </section>

    <section v-else-if="page === 'plugins'" key="plugins">
      <div class="plugins-head">
        <div class="muted">{{ tr('插件是独立进程，一个插件就是一种新的连接类型；导入后新建连接时可以选到它。') }}</div>
        <button class="btn btn-primary" :disabled="pluginBusy" @click="importPlugin">{{ tr('导入插件…') }}</button>
      </div>
      <div v-if="!plugins.length" class="muted plugins-empty">{{ tr('还没有插件。选一个含 plugin.json 的目录导入。') }}</div>
      <ul v-else class="plugin-list">
        <li v-for="p in plugins" :key="p.id" class="plugin-item">
          <div class="plugin-main">
            <div class="plugin-name">
              {{ p.name }} <span class="mono muted">{{ p.version }}</span>
              <span v-if="p.readOnly" class="badge badge-readonly">{{ tr('只读') }}</span>
            </div>
            <div v-if="p.description" class="muted plugin-desc">{{ p.description }}</div>
            <div class="mono muted plugin-dir selectable">{{ p.dir }}</div>
          </div>
          <div class="plugin-ops">
            <button class="btn btn-ghost" @click="api.RevealPlugin(p.id)">{{ tr('在访达中显示') }}</button>
            <button class="btn danger" :class="{ 'btn-ghost': armedRemove !== p.id }" @click="removePlugin(p)">
              {{ armedRemove === p.id ? tr('再点一次确认移除') : tr('移除') }}
            </button>
          </div>
        </li>
      </ul>
      <div class="field-hint plugins-hint">{{ tr('写自己的插件：见仓库 docs/plugins.md，协议是 stdin/stdout 上按行的 JSON-RPC，任何语言都行。') }}</div>
    </section>
    <section v-else-if="page === 'about'" key="about" class="about">
      <div class="about-hero">
        <img class="about-mark" :src="aboutMark" alt="" draggable="false" />
        <div class="about-id">
          <div class="about-name">Tacivan</div>
          <div class="about-ver mono" v-if="info">{{ info.version }} · {{ info.platform }}</div>
          <div class="about-tag">{{ tr('开源数据库客户端 · 支持 MySQL / MariaDB / PostgreSQL / SQLite / Redis') }}</div>
        </div>
      </div>
      <dl v-if="info" class="about-facts">
        <dt>{{ tr('运行时') }}</dt><dd>{{ info.goVersion }}</dd>
        <dt>{{ tr('密码存储') }}</dt><dd>{{ info.keyringOk ? tr('系统钥匙串') : tr('不可用（密码将无法保存）') }}</dd>
        <dt>{{ tr('配置目录') }}</dt><dd class="mono selectable">{{ info.configDir }}</dd>
        <dt>{{ tr('溢出目录') }}</dt><dd class="mono selectable">{{ info.spillDir }}</dd>
        <dt v-if="info.logPath">{{ tr('诊断日志') }}</dt><dd v-if="info.logPath" class="mono selectable">{{ info.logPath }}</dd>
      </dl>
    </section>
    </Transition>

    <template #footer>
      <template v-if="page === 'about' || page === 'plugins'">
        <span class="spacer"></span>
        <button class="btn" @click="emit('close')">{{ tr('关闭') }}</button>
      </template>
      <template v-else>
        <button class="btn" @click="emit('close')">{{ tr('取消') }}</button>
        <button class="btn btn-primary" @click="save">{{ tr('保存') }}</button>
      </template>
    </template>
  </Modal>
</template>

<style scoped>
.plugins-head { display: flex; align-items: center; gap: var(--sp-3); margin-bottom: var(--sp-3); }
.plugins-head .muted { flex: 1; }
.plugins-empty { padding: var(--sp-4) 0; }
.plugin-list { list-style: none; margin: 0; padding: 0; border: 1px solid var(--c-border); border-radius: var(--radius-sm); }
.plugin-item { display: flex; align-items: flex-start; gap: var(--sp-3); padding: var(--sp-3); border-bottom: 1px solid var(--c-border-soft); }
.plugin-item:last-child { border-bottom: none; }
.plugin-main { flex: 1; min-width: 0; }
.plugin-name { display: flex; align-items: center; gap: var(--sp-2); font-weight: 600; }
.plugin-desc { margin-top: 2px; }
.plugin-dir { margin-top: 4px; font-size: var(--font-size-xs); overflow-wrap: anywhere; }
.plugin-ops { display: flex; gap: var(--sp-1); flex: none; }
.plugins-hint { grid-column: auto; margin-top: var(--sp-3); }

.slide-left-enter-active,
.slide-left-leave-active,
.slide-right-enter-active,
.slide-right-leave-active {
  transition: opacity 0.07s ease, transform 0.1s cubic-bezier(0.2, 0.7, 0.2, 1);
}
.slide-left-enter-from { opacity: 0; transform: translateX(12px); }
.slide-left-leave-to { opacity: 0; transform: translateX(-12px); }
.slide-right-enter-from { opacity: 0; transform: translateX(-12px); }
.slide-right-leave-to { opacity: 0; transform: translateX(12px); }

.pager {
  display: flex;
  gap: 2px;
  margin-bottom: 12px;
  border-bottom: 1px solid var(--c-border);
}

.pager-btn {
  height: 26px;
  padding: 0 14px;
  border: none;
  border-bottom: 2px solid transparent;
  background: transparent;
  color: var(--c-text-secondary);
  font-size: var(--font-size);
}
.pager-btn:hover {
  color: var(--c-text);
}
.pager-btn.is-active {
  border-bottom-color: var(--c-accent);
  color: var(--c-accent);
  font-weight: 600;
}

.checkbox {
  display: flex;
  margin-bottom: 7px;
}

.preview {
  margin-top: 10px;
  padding: 10px;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  background: var(--c-bg-sunken);
  overflow-x: auto;
  white-space: nowrap;
}

.log-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 8px;
}
.log-row code {
  flex: 1;
  min-width: 0;
  padding: 4px 7px;
  border-radius: var(--radius-sm);
  background: var(--c-bg-sunken);
  font-size: var(--font-size-sm);
  overflow-x: auto;
  white-space: nowrap;
}

.about {
  padding-top: var(--sp-2);
}
.about-hero {
  display: flex;
  align-items: center;
  gap: var(--sp-4);
  padding-bottom: var(--sp-4);
  border-bottom: 1px solid var(--c-border-soft);
}
.about-mark {
  width: 72px;
  height: 72px;
  flex: none;
}
.about-name {
  font-size: 20px;
  font-weight: 600;
  letter-spacing: -0.01em;
}
.about-ver {
  margin-top: 2px;
  color: var(--c-text-tertiary);
  font-size: var(--font-size-sm);
}
.about-tag {
  margin-top: 6px;
  color: var(--c-text-secondary);
}
.about-facts {
  display: grid;
  grid-template-columns: max-content 1fr;
  column-gap: var(--sp-4);
  row-gap: 6px;
  margin: var(--sp-4) 0 0;
}
.about-facts dt {
  color: var(--c-text-tertiary);
  text-align: right;
}
.about-facts dd {
  margin: 0;
  min-width: 0;
  overflow-wrap: anywhere;
  color: var(--c-text-secondary);
}
.kv > span:first-child {
  width: 88px;
  color: var(--c-text-secondary);
  flex: none;
}
.kv > span:last-child {
  word-break: break-all;
}
</style>
