<script setup lang="ts">
/** 新建 / 编辑连接。分页结构对齐 Navicat：常规、SSL、SSH、高级。 */
import { computed, onMounted, ref, watch } from 'vue'
import * as api from '../../api'
import { useAppStore } from '../../stores/app'
import { useConnectionsStore } from '../../stores/connections'
import Modal from '../common/Modal.vue'
import Icon from '../common/Icon.vue'
import { logoFor } from '../common/engineLogos'
import type { ConnectionConfig, Engine, EngineOption } from '../../types'
import { tr } from '../../i18n'

const props = defineProps<{ existing?: ConnectionConfig | null; group?: string }>()
const emit = defineEmits<{ (e: 'saved', cfg: ConnectionConfig): void; (e: 'close'): void }>()

const app = useAppStore()
const engines = ref<EngineOption[]>([])
const page = ref<'general' | 'databases' | 'ssl' | 'ssh' | 'advanced'>('general')
const PAGE_ORDER = ['general', 'databases', 'ssl', 'ssh', 'advanced'] as const
const slideName = ref('slide-left')
function goPage(next: typeof page.value) {
  const order: readonly string[] = PAGE_ORDER
  slideName.value = order.indexOf(next) > order.indexOf(page.value) ? 'slide-left' : 'slide-right'
  page.value = next
}
const testing = ref(false)
const saving = ref(false)
const testResult = ref<{ ok: boolean; text: string } | null>(null)

/** 编辑已有连接时，密码框留空表示「保持不变」。 */
const passwordTouched = ref(false)

const COLORS = ['', '#d13438', '#e68a00', '#1a8c46', '#0a6cff', '#8b5cf6', '#6b6c70']

function blank(): ConnectionConfig {
  return {
    id: '',
    name: '',
    engine: 'mysql',
    color: '',
    group: props.group ?? '',
    host: '127.0.0.1',
    port: 3306,
    user: 'root',
    password: '',
    database: '',
    tls: { enabled: false, caFile: '', certFile: '', keyFile: '', insecureSkipVerify: false },
    ssh: {
      enabled: false,
      host: '',
      port: 22,
      user: '',
      authMethod: 'key',
      password: '',
      keyFile: '',
      passphrase: '',
    },
    readOnly: false,
    keepAlive: 0,
    connectTimeout: 10,
    useDatabaseList: false,
    databaseList: [],
    compress: false,
    sortOrder: 0,
  }
}

const cfg = ref<ConnectionConfig>(props.existing ? { ...props.existing, password: '' } : blank())
const conns = useConnectionsStore()
const groupNames = computed(() => conns.groups.map((g) => g.name))
/** 可以当结构来源的连接：内置引擎的、且不是自己。 */
const linkableConns = computed(() =>
  conns.states.filter((c) => !String(c.config.engine).startsWith('plugin:') && c.config.id !== cfg.value.id),
)

const isEdit = computed(() => !!cfg.value.id)
const engineInfo = computed(() => engines.value.find((e) => e.engine === cfg.value.engine))
const fileBased = computed(() => engineInfo.value?.fileBased === true)
const isRedis = computed(() => cfg.value.engine === 'redis')
/** MCP 连接：没有主机端口，参数是传输方式 + 命令/URL。 */
const isMcp = computed(() => cfg.value.engine === 'mcp')
const mcpTransport = computed({
  get: () => cfg.value.params?.transport || 'stdio',
  set: (v: string) => setParam('transport', v),
})
/** 插件引擎：没有主机端口那套，表单完全按清单里的字段来。 */
const isPlugin = computed(() => engineInfo.value?.plugin === true)
const pluginFields = computed(() => engineInfo.value?.fields ?? [])
function paramOf(key: string): string {
  return cfg.value.params?.[key] ?? ''
}
function setParam(key: string, v: string) {
  if (!cfg.value.params) cfg.value.params = {}
  cfg.value.params[key] = v
}

onMounted(async () => {
  try {
    engines.value = await api.ListEngines()
  } catch (e) {
    app.reportError(e, tr('读取引擎列表失败'))
  }
})

function onEngineChange(e: Engine) {
  cfg.value.engine = e
  const info = engines.value.find((x) => x.engine === e)
  if (info && !info.fileBased) {
    cfg.value.port = info.defaultPort
    if (e === 'postgres' && cfg.value.user === 'root') cfg.value.user = 'postgres'
    if ((e === 'mysql' || e === 'mariadb') && cfg.value.user === 'postgres') cfg.value.user = 'root'
  }
  if (e === 'redis') cfg.value.user = ''
  if (info?.plugin) {
    cfg.value.params = cfg.value.params ?? {}
    for (const f of info.fields ?? []) {
      if (f.default && !cfg.value.params[f.key]) cfg.value.params[f.key] = f.default
    }
    // 只读插件：写路径在主程序里根本不存在，这里只是把开关同步过去别让人误会
    if (info.readOnly) cfg.value.readOnly = true
  }
  testResult.value = null
}

async function pickFile() {
  try {
    const p = await api.OpenDatabaseFileDialog()
    if (p) {
      cfg.value.database = p
      if (!cfg.value.name) {
        cfg.value.name = p.split('/').pop()?.replace(/\.[^.]+$/, '') ?? ''
      }
    }
  } catch (e) {
    app.reportError(e, tr('选择文件失败'))
  }
}

async function createFile() {
  try {
    const p = await api.CreateDatabaseFileDialog()
    if (p) cfg.value.database = p
  } catch (e) {
    app.reportError(e, tr('创建文件失败'))
  }
}

async function pickKeyFile() {
  try {
    const p = await api.OpenDatabaseFileDialog()
    if (p) cfg.value.ssh.keyFile = p
  } catch {
    // 用户取消了选择，无需处理。
  }
}

/** 提交前把「密码未改动」转成不传该字段，避免把已保存的口令清掉。 */
function payload(): ConnectionConfig {
  const out = { ...cfg.value }
  if (isEdit.value && !passwordTouched.value) {
    delete (out as Partial<ConnectionConfig>).password
  }
  return out
}

async function test() {
  testing.value = true
  testResult.value = null
  try {
    const info = await api.TestConnection(payload())
    testResult.value = { ok: true, text: tr('连接成功 · {version}', { version: info.version }) }
  } catch (e) {
    testResult.value = { ok: false, text: e instanceof Error ? e.message : String(e) }
  } finally {
    testing.value = false
  }
}

async function save() {
  saving.value = true
  try {
    const saved = await api.SaveConnection(payload())
    emit('saved', saved)
  } catch (e) {
    app.reportError(e, tr('保存连接失败'))
  } finally {
    saving.value = false
  }
}

watch(
  () => cfg.value.password,
  () => {
    passwordTouched.value = true
  },
)

const pages = computed(() => {
  const list: { key: typeof page.value; label: string }[] = [{ key: 'general', label: tr('常规') }]
  if (!fileBased.value && !isRedis.value && !isMcp.value) list.push({ key: 'databases', label: tr('数据库') })
  if (!fileBased.value && !isPlugin.value && !isMcp.value) {
    list.push({ key: 'ssl', label: 'SSL' })
    list.push({ key: 'ssh', label: 'SSH' })
  }
  list.push({ key: 'advanced', label: tr('高级') })
  return list
})

// --- 自定义数据库列表 ---
//
// 超大实例上，对象树每次展开都要把几千个库拉回来再渲染。勾上之后
// 只列这里面的，连 SHOW DATABASES 都不发。

/** 从服务器实拉回来的全部库名，用于挑选；为空表示还没拉过。 */
const serverDbs = ref<string[]>([])
const loadingDbs = ref(false)
const dbFilter = ref('')
const newDb = ref('')

const chosen = computed(() => new Set((cfg.value.databaseList ?? []).map((d) => d.name)))

const dbCandidates = computed(() => {
  const kw = dbFilter.value.trim().toLowerCase()
  return serverDbs.value
    .filter((n) => !chosen.value.has(n))
    .filter((n) => !kw || n.toLowerCase().includes(kw))
    .slice(0, 300)
})

async function loadServerDbs() {
  if (!cfg.value.id) {
    app.toast('info', tr('请先保存并连接'), tr('要先连上服务器才能列出它有哪些库'))
    return
  }
  loadingDbs.value = true
  try {
    const list = await api.ListAllDatabases(cfg.value.id)
    serverDbs.value = list.map((d) => d.name)
    if (!serverDbs.value.length) app.toast('info', tr('没有拿到库名'), '')
  } catch (e) {
    app.reportError(e, tr('读取数据库列表失败，请先双击连接把它打开'))
  } finally {
    loadingDbs.value = false
  }
}

function addDb(name: string) {
  const n = name.trim()
  if (!n || chosen.value.has(n)) return
  if (!cfg.value.databaseList) cfg.value.databaseList = []
  cfg.value.databaseList.push({ name: n, autoOpen: false })
  newDb.value = ''
}

function removeDb(index: number) {
  cfg.value.databaseList?.splice(index, 1)
}
</script>

<template>
  <Modal :title="isEdit ? tr('编辑连接') : tr('新建连接')" :width="580" @close="emit('close')">
    <div class="engines">
      <button
        v-for="e in engines"
        :key="e.engine"
        class="engine-btn"
        :class="{ 'is-active': cfg.engine === e.engine }"
        :style="{ '--engine-color': `var(--c-engine-${e.engine})` }"
        @click="onEngineChange(e.engine)"
      >
        <Icon v-if="logoFor(e.engine)" :name="'logo:' + logoFor(e.engine)" :size="14" class="engine-logo" />
        <span v-else class="engine-dot"></span>
        {{ e.displayName }}
      </button>
    </div>

    <nav class="pager">
      <button
        v-for="p in pages"
        :key="p.key"
        class="pager-btn"
        :class="{ 'is-active': page === p.key }"
        @click="goPage(p.key)"
      >
        {{ p.label }}
      </button>
    </nav>

    <!-- 常规 -->
    <Transition :name="slideName" mode="out-in">
    <section v-if="page === 'general'" key="general">
      <div class="field">
        <label>{{ tr('连接名') }}</label>
        <input v-model="cfg.name" class="input" :placeholder="tr('例如：本地开发库')" />
      </div>

      <div class="field">
        <label>{{ tr('颜色') }}</label>
        <div class="colors">
          <button
            v-for="c in COLORS"
            :key="c || 'none'"
            class="color-dot"
            :class="{ 'is-active': cfg.color === c, 'is-none': !c }"
            :style="c ? { background: c } : undefined"
            :title="c ? tr('标记颜色') : tr('无颜色')"
            @click="cfg.color = c"
          ></button>
        </div>
      </div>
      <div class="field">
        <label>{{ tr('分组') }}</label>
        <input v-model="cfg.group" class="input" list="conn-groups" :placeholder="tr('例如：生产环境；留空不分组')" />
        <datalist id="conn-groups"><option v-for="g in groupNames" :key="g" :value="g" /></datalist>
      </div>
      <div class="field-hint">{{ tr('给生产库标上醒目颜色，能少一次误操作') }}</div>

      <template v-if="fileBased">
        <div class="field">
          <label>{{ tr('数据库文件') }}</label>
          <div class="path-row">
            <input v-model="cfg.database" class="input mono" :placeholder="tr('选择 .db / .sqlite 文件')" />
            <button class="btn" @click="pickFile">{{ tr('打开…') }}</button>
            <button class="btn" @click="createFile">{{ tr('新建…') }}</button>
          </div>
        </div>
      </template>

      <template v-else>
        <template v-if="isMcp">
        <div class="field-hint plugin-desc">{{ tr('把一个 MCP server 当成连接：树上列出它的工具和资源，工具用表单手动调用，不经过 AI。') }}</div>
        <div class="field">
          <label>{{ tr('传输方式') }}</label>
          <select v-model="mcpTransport" class="select">
            <option value="stdio">stdio（本机启动子进程）</option>
            <option value="http">Streamable HTTP</option>
          </select>
        </div>
        <template v-if="mcpTransport === 'stdio'">
          <div class="field">
            <label>{{ tr('启动命令') }}</label>
            <input class="input mono" :value="paramOf('command')" placeholder="npx -y @modelcontextprotocol/server-filesystem /tmp" @input="setParam('command', ($event.target as HTMLInputElement).value)" />
          </div>
          <div class="field-hint">{{ tr('和 Claude Desktop 配置里的 command + args 一样，写成一行；含空格的路径用引号') }}</div>
          <div class="field">
            <label>{{ tr('环境变量') }}</label>
            <textarea class="input mono plugin-textarea" :value="paramOf('env')" placeholder="KEY=VALUE，每行一个" @input="setParam('env', ($event.target as HTMLTextAreaElement).value)"></textarea>
          </div>
        </template>
        <template v-else>
          <div class="field">
            <label>URL</label>
            <input class="input mono" :value="paramOf('url')" placeholder="https://host/mcp" @input="setParam('url', ($event.target as HTMLInputElement).value)" />
          </div>
          <div class="field">
            <label>{{ tr('请求头') }}</label>
            <textarea class="input mono plugin-textarea" :value="paramOf('headers')" placeholder="Authorization: Bearer …，每行一个" @input="setParam('headers', ($event.target as HTMLTextAreaElement).value)"></textarea>
          </div>
        </template>
      </template>
      <template v-else-if="isPlugin">
        <div v-if="engineInfo?.description" class="field-hint plugin-desc">{{ engineInfo.description }}</div>
        <div v-for="f in pluginFields" :key="f.key" class="field">
          <label>{{ f.label }}</label>
          <input
            v-if="f.type === 'text' || f.type === 'password' || f.type === 'number'"
            class="input"
            :class="{ mono: f.type === 'text' }"
            :type="f.type"
            :placeholder="f.placeholder"
            :value="paramOf(f.key)"
            autocomplete="off"
            @input="setParam(f.key, ($event.target as HTMLInputElement).value)"
          />
          <textarea
            v-else-if="f.type === 'textarea'"
            class="input mono plugin-textarea"
            :placeholder="f.placeholder"
            :value="paramOf(f.key)"
            @input="setParam(f.key, ($event.target as HTMLTextAreaElement).value)"
          ></textarea>
          <select
            v-else-if="f.type === 'connection'"
            class="select"
            :value="paramOf(f.key)"
            @change="setParam(f.key, ($event.target as HTMLSelectElement).value)"
          >
            <option value="">{{ tr('不关联') }}</option>
            <option v-for="c in linkableConns" :key="c.config.id" :value="c.config.id">{{ c.config.name }}</option>
          </select>
          <select
            v-else-if="f.type === 'select'"
            class="select"
            :value="paramOf(f.key)"
            @change="setParam(f.key, ($event.target as HTMLSelectElement).value)"
          >
            <option v-for="o in f.options ?? []" :key="o" :value="o">{{ o }}</option>
          </select>
          <label v-else-if="f.type === 'bool'" class="checkbox">
            <input type="checkbox" :checked="paramOf(f.key) === 'true'" @change="setParam(f.key, ($event.target as HTMLInputElement).checked ? 'true' : '')" />
          </label>
          <div v-if="f.hint" class="field-hint">{{ f.hint }}</div>
        </div>
      </template>
      <template v-else>
      <div class="field">
          <label>{{ tr('主机') }}</label>
          <input v-model="cfg.host" class="input" placeholder="127.0.0.1" />
        </div>
        <div class="field">
          <label>{{ tr('端口') }}</label>
          <input v-model.number="cfg.port" class="input" type="number" min="1" max="65535" />
        </div>
        <div v-if="!isRedis" class="field">
          <label>{{ tr('用户名') }}</label>
          <input v-model="cfg.user" class="input" autocomplete="off" />
        </div>
        <div class="field">
          <label>{{ tr('密码') }}</label>
          <input
            v-model="cfg.password"
            class="input"
            type="password"
            autocomplete="new-password"
            :placeholder="isEdit ? tr('留空表示不修改已保存的密码') : ''"
          />
        </div>
        <div class="field">
          <label>{{ isRedis ? tr('默认库') : tr('默认数据库') }}</label>
          <input
            v-model="cfg.database"
            class="input"
            :placeholder="isRedis ? '0' : tr('可留空，连接后再选择')"
          />
        </div>
      </template>
      </template>
    </section>

    <!-- SSL -->
    <section v-else-if="page === 'ssl'" key="ssl">
      <label class="checkbox">
        <input v-model="cfg.tls.enabled" type="checkbox" />{{ tr('使用 SSL / TLS 加密连接') }}</label>
      <div class="divider"></div>
      <template v-if="cfg.tls.enabled">
        <div class="field">
          <label>{{ tr('CA 证书') }}</label>
          <input v-model="cfg.tls.caFile" class="input mono" :placeholder="tr('ca.pem 路径')" />
        </div>
        <div class="field">
          <label>{{ tr('客户端证书') }}</label>
          <input v-model="cfg.tls.certFile" class="input mono" :placeholder="tr('client-cert.pem 路径')" />
        </div>
        <div class="field">
          <label>{{ tr('客户端密钥') }}</label>
          <input v-model="cfg.tls.keyFile" class="input mono" :placeholder="tr('client-key.pem 路径')" />
        </div>
        <label class="checkbox">
          <input v-model="cfg.tls.insecureSkipVerify" type="checkbox" />{{ tr('跳过服务器证书校验') }}</label>
        <div class="field-hint warn">{{ tr('跳过校验会让加密失去防中间人的意义，仅在自签名证书的内网测试环境使用') }}</div>
      </template>
      <div v-else class="muted">{{ tr('未启用加密，数据将以明文在网络上传输') }}</div>
    </section>

    <!-- SSH -->
    <section v-else-if="page === 'ssh'" key="ssh">
      <label class="checkbox">
        <input v-model="cfg.ssh.enabled" type="checkbox" />{{ tr('通过 SSH 隧道连接') }}</label>
      <div class="divider"></div>
      <template v-if="cfg.ssh.enabled">
        <div class="field">
          <label>{{ tr('跳板机') }}</label>
          <input v-model="cfg.ssh.host" class="input" placeholder="bastion.example.com" />
        </div>
        <div class="field">
          <label>{{ tr('端口') }}</label>
          <input v-model.number="cfg.ssh.port" class="input" type="number" />
        </div>
        <div class="field">
          <label>{{ tr('用户名') }}</label>
          <input v-model="cfg.ssh.user" class="input" />
        </div>
        <div class="field">
          <label>{{ tr('认证方式') }}</label>
          <select v-model="cfg.ssh.authMethod" class="select">
            <option value="key">{{ tr('私钥') }}</option>
            <option value="agent">ssh-agent</option>
            <option value="password">{{ tr('密码') }}</option>
          </select>
        </div>
        <div v-if="cfg.ssh.authMethod === 'key'" class="field">
          <label>{{ tr('私钥文件') }}</label>
          <div class="path-row">
            <input v-model="cfg.ssh.keyFile" class="input mono" placeholder="~/.ssh/id_rsa" />
            <button class="btn" @click="pickKeyFile">{{ tr('浏览…') }}</button>
          </div>
        </div>
        <div v-if="cfg.ssh.authMethod === 'key'" class="field">
          <label>{{ tr('私钥口令') }}</label>
          <input v-model="cfg.ssh.passphrase" class="input" type="password" autocomplete="off" />
        </div>
        <div v-if="cfg.ssh.authMethod === 'password'" class="field">
          <label>{{ tr('SSH 密码') }}</label>
          <input v-model="cfg.ssh.password" class="input" type="password" autocomplete="off" />
        </div>
        <div class="field-hint">{{ tr('主机密钥按 ~/.ssh/known_hosts 校验；首次连接请先在终端 ssh 一次确认指纹') }}</div>
      </template>
      <div v-else class="muted">{{ tr('不使用跳板机，直接连接数据库主机') }}</div>
    </section>

    <!-- 数据库：自定义列表 -->
    <section v-else-if="page === 'databases'" key="databases">
      <label class="checkbox">
        <input v-model="cfg.useDatabaseList" type="checkbox" />{{ tr('使用自定义数据库列表') }}</label>
      <div class="field-hint">
        {{
          tr(
            '只列下面这些库，并且跳过向服务端问库名这一步。实例里库特别多时，对象树不必每次都把几千个库拉回来再渲染一遍。',
          )
        }}
      </div>

      <template v-if="cfg.useDatabaseList">
        <div class="divider"></div>

        <div v-if="!cfg.databaseList?.length" class="muted">{{ tr('还没有加任何库。下面手动输入，或从服务器获取后挑选。') }}</div>
        <ul v-else class="db-list">
          <li v-for="(d, i) in cfg.databaseList" :key="d.name" class="db-row">
            <label class="db-auto" :title="d.autoOpen ? tr('连上后自动展开') : tr('连上后不展开')">
              <input v-model="d.autoOpen" type="checkbox" />{{ tr('自动打开') }}</label>
            <span class="db-name mono">{{ d.name }}</span>
            <button class="btn btn-ghost db-del" :title="tr('移除')" @click="removeDb(i)">
              <Icon name="close" :size="11" />
            </button>
          </li>
        </ul>

        <div class="db-add">
          <input
            v-model="newDb"
            class="input mono"
            :placeholder="tr('输入库名后回车')"
            @keydown.enter.prevent="addDb(newDb)"
          />
          <button class="btn" :disabled="!newDb.trim()" @click="addDb(newDb)">{{ tr('添加') }}</button>
          <button class="btn" :disabled="loadingDbs" @click="loadServerDbs">
            {{ loadingDbs ? tr('获取中…') : tr('从服务器获取') }}
          </button>
        </div>

        <template v-if="serverDbs.length">
          <div class="divider"></div>
          <div class="field">
            <label>{{ tr('服务器上共 {n} 个库', { n: serverDbs.length }) }}</label>
            <input v-model="dbFilter" class="input" :placeholder="tr('筛选…')" />
          </div>
          <ul class="db-pick">
            <li v-for="n in dbCandidates" :key="n">
              <button class="db-pick-btn mono" @click="addDb(n)">
                <Icon name="plus" :size="10" />
                {{ n }}
              </button>
            </li>
          </ul>
          <div v-if="dbCandidates.length >= 300" class="field-hint">{{ tr('只显示前 300 个，用上面的筛选框缩小范围') }}</div>
        </template>
      </template>
    </section>

    <!-- 高级 -->
    <section v-else-if="page === 'advanced'" key="advanced">
      <label class="checkbox">
        <input v-model="cfg.readOnly" type="checkbox" />{{ tr('只读连接') }}</label>
      <div class="field-hint">{{ tr('开启后拦截一切写语句（含数据网格编辑、结构变更），挂生产库时建议打开') }}</div>
      <div class="divider"></div>
      <div class="field">
        <label>{{ tr('连接超时') }}</label>
        <input v-model.number="cfg.connectTimeout" class="input" type="number" min="1" max="300" />
      </div>
      <div class="field-hint">{{ tr('秒。网络不稳时适当调大') }}</div>
      <div class="field">
        <label>{{ tr('保活间隔') }}</label>
        <input v-model.number="cfg.keepAlive" class="input" type="number" min="0" />
      </div>
      <div class="field-hint">{{ tr('秒，0 表示关闭') }}</div>
      <template v-if="cfg.engine === 'mysql' || cfg.engine === 'mariadb'">
        <div class="divider"></div>
        <label class="checkbox">
          <input v-model="cfg.compress" type="checkbox" />{{ tr('使用压缩') }}</label>
        <div class="field-hint">
          {{
            tr(
              '协议级 zlib 压缩。跨公网或 VPN 读宽表时能明显减少传输量；本机或同机房直连开了反而白费 CPU。',
            )
          }}
        </div>
      </template>
    </section>
    </Transition>

    <div v-if="testResult" class="test-result" :class="testResult.ok ? 'is-ok' : 'is-err'">
      <Icon :name="testResult.ok ? 'check' : 'warning'" :size="13" />
      <span class="selectable">{{ testResult.text }}</span>
    </div>

    <template #footer>
      <button class="btn" :disabled="testing" @click="test">
        {{ testing ? tr('测试中…') : tr('测试连接') }}
      </button>
      <span class="spacer"></span>
      <button class="btn" @click="emit('close')">{{ tr('取消') }}</button>
      <button class="btn btn-primary" :disabled="saving" @click="save">{{ tr('保存') }}</button>
    </template>
  </Modal>
</template>

<style scoped>
.engine-logo { color: var(--engine-color); flex: none; }

.plugin-desc { grid-column: 1 / -1; margin: 0 0 var(--sp-3); }
.plugin-textarea { min-height: 72px; resize: vertical; max-width: 420px; }

.slide-left-enter-active, .slide-left-leave-active,
.slide-right-enter-active, .slide-right-leave-active {
  transition: opacity 0.07s ease, transform 0.1s cubic-bezier(0.2, 0.7, 0.2, 1);
}
.slide-left-enter-from { opacity: 0; transform: translateX(12px); }
.slide-left-leave-to { opacity: 0; transform: translateX(-12px); }
.slide-right-enter-from { opacity: 0; transform: translateX(-12px); }
.slide-right-leave-to { opacity: 0; transform: translateX(12px); }

.db-list {
  list-style: none;
  margin: var(--sp-2) 0;
  padding: 0;
  max-height: 180px;
  overflow-y: auto;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
}
.db-row {
  display: flex;
  align-items: center;
  gap: var(--sp-3);
  padding: 3px var(--sp-2);
  border-bottom: 1px solid var(--c-border-soft);
}
.db-row:last-child {
  border-bottom: none;
}
.db-auto {
  flex: none;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: var(--font-size-sm);
  color: var(--c-text-tertiary);
}
.db-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}
.db-del {
  flex: none;
  padding: 2px;
}

.db-add {
  display: flex;
  gap: var(--sp-2);
}
.db-add .input {
  flex: 1;
}

.db-pick {
  list-style: none;
  margin: var(--sp-2) 0 0;
  padding: 0;
  max-height: 160px;
  overflow-y: auto;
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.db-pick-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px var(--sp-2);
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--c-text-secondary);
  font-size: var(--font-size-sm);
}
.db-pick-btn:hover {
  border-color: var(--c-accent);
  color: var(--c-text);
}

.engines {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-bottom: 12px;
}

.engine-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 26px;
  padding: 0 11px;
  border: 1px solid var(--c-border);
  border-radius: 13px;
  background: var(--c-bg);
  color: var(--c-text);
  font-size: var(--font-size);
}
.engine-btn:hover {
  background: var(--c-bg-sunken);
}
.engine-btn.is-active {
  border-color: var(--engine-color);
  background: var(--c-accent-soft);
  font-weight: 600;
}

.engine-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--engine-color);
}

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

.colors {
  display: flex;
  gap: 5px;
}

.color-dot {
  width: 16px;
  height: 16px;
  padding: 0;
  border: 1px solid var(--c-border);
  border-radius: 50%;
  background: transparent;
}
.color-dot.is-none {
  background: linear-gradient(
    45deg,
    transparent 45%,
    var(--c-border-strong) 45%,
    var(--c-border-strong) 55%,
    transparent 55%
  );
}
.color-dot.is-active {
  box-shadow: 0 0 0 2px var(--c-accent);
}

.path-row {
  display: flex;
  gap: 5px;
}
.path-row .input {
  flex: 1;
}

.field-hint.warn {
  color: var(--c-warning);
}

.test-result {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  margin-top: 12px;
  padding: 7px 10px;
  border-radius: var(--radius-sm);
  word-break: break-word;
}
.test-result.is-ok {
  background: var(--c-success-soft);
  color: var(--c-success);
}
.test-result.is-err {
  background: var(--c-danger-soft);
  color: var(--c-danger);
}

.spacer {
  flex: 1;
}
</style>
