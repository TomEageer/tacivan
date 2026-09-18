<script setup lang="ts">
/**
 * 资源监视器。
 *
 * 这个面板是刻意做出来的：客户端占了多少内存、哪个结果集占的、
 * 有没有落盘、回收过几次，用户应该能直接看到而不是靠猜。
 */
import { computed, onMounted, onBeforeUnmount, ref } from 'vue'
import { useAppStore } from '../../stores/app'
import Modal from '../common/Modal.vue'
import Icon from '../common/Icon.vue'
import { formatBytes } from '../grid/cell'
import { tr } from '../../i18n'

const emit = defineEmits<{ (e: 'close'): void }>()
const app = useAppStore()

/** 最近的堆占用采样，画成迷你折线。 */
const samples = ref<number[]>([])
let timer: number | undefined

onMounted(() => {
  tick()
  timer = window.setInterval(tick, 1000)
})
onBeforeUnmount(() => {
  if (timer) window.clearInterval(timer)
})

async function tick() {
  await app.refreshStats()
  const v = app.stats?.processHeapMb ?? 0
  samples.value.push(v)
  if (samples.value.length > 90) samples.value.shift()
}

const stats = computed(() => app.stats)
const rs = computed(() => app.stats?.resultSets)

const pressure = computed(() => {
  const s = rs.value
  if (!s?.memoryLimit) return 0
  return Math.min(1, s.memoryBytes / s.memoryLimit)
})

/** 迷你折线图的路径。 */
const sparkline = computed(() => {
  const data = samples.value
  if (data.length < 2) return ''
  const w = 420
  const h = 56
  const max = Math.max(...data, 1) * 1.15
  return data
    .map((v, i) => {
      const x = (i / (data.length - 1)) * w
      const y = h - (v / max) * h
      return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(' ')
})

const peakHeap = computed(() => (samples.value.length ? Math.max(...samples.value) : 0))

async function release() {
  await app.releaseMemory()
  samples.value.push(app.stats?.processHeapMb ?? 0)
}
</script>

<template>
  <Modal :title="tr('资源监视器')" :width="760" @close="emit('close')">
    <div v-if="!stats" class="muted">{{ tr('正在采集…') }}</div>

    <template v-else>
      <!-- 概览 -->
      <div class="cards">
        <div class="card">
          <div class="card-label">{{ tr('进程堆内存') }}</div>
          <div class="card-value">{{ stats.processHeapMb.toFixed(1) }} <small>MB</small></div>
          <div class="card-sub">
          {{
            tr('峰值 {peak} MB · 系统 {sys} MB', {
              peak: peakHeap.toFixed(1),
              sys: stats.processSysMb.toFixed(0),
            })
          }}
        </div>
        </div>
        <div class="card">
          <div class="card-label">{{ tr('结果集常驻') }}</div>
          <div class="card-value">
            {{ formatBytes(rs!.memoryBytes) }}
          </div>
          <div class="card-sub">{{ tr('上限 {size}', { size: formatBytes(rs!.memoryLimit) }) }}</div>
        </div>
        <div class="card">
          <div class="card-label">{{ tr('已溢出到磁盘') }}</div>
          <div class="card-value">{{ formatBytes(rs!.spillBytes) }}</div>
          <div class="card-sub">{{ tr('回收 {n} 次', { n: rs!.trimCount }) }}</div>
        </div>
        <div class="card">
          <div class="card-label">{{ tr('打开的结果集') }}</div>
          <div class="card-value">{{ rs!.openResultSets }}</div>
          <div class="card-sub">
          {{
            tr('共 {rows} 行 · {conns} 个连接', {
              rows: rs!.totalRows.toLocaleString(),
              conns: stats.openConnections,
            })
          }}
        </div>
        </div>
      </div>

      <!-- 水位 -->
      <div class="gauge-row">
        <span class="gauge-label">{{ tr('结果集内存水位') }}</span>
        <div class="gauge-track">
          <div
            class="gauge-bar"
            :class="{ 'is-mid': pressure > 0.6, 'is-high': pressure > 0.85 }"
            :style="{ width: pressure * 100 + '%' }"
          ></div>
        </div>
        <span class="gauge-pct">{{ (pressure * 100).toFixed(0) }}%</span>
      </div>

      <!-- 趋势 -->
      <div class="chart">
        <svg viewBox="0 0 420 56" preserveAspectRatio="none">
          <path :d="sparkline" fill="none" stroke="var(--c-accent)" stroke-width="1.5" />
        </svg>
        <div class="chart-caption muted">
        {{ tr('最近 {n} 秒的堆内存走势', { n: samples.length }) }}
      </div>
      </div>

      <div class="divider"></div>

      <!-- 明细 -->
      <div class="section-title">{{ tr('各结果集占用') }}</div>
      <table class="rtable">
        <thead>
          <tr>
            <th>{{ tr('结果集') }}</th>
            <th class="num">{{ tr('已加载行') }}</th>
            <th class="num">{{ tr('常驻内存') }}</th>
            <th class="num">{{ tr('峰值') }}</th>
            <th class="num">{{ tr('溢出') }}</th>
            <th class="num">{{ tr('缓存块') }}</th>
            <th class="num">{{ tr('闲置') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="c in rs!.cursors" :key="c.id">
            <td class="mono">{{ c.id }}</td>
            <td class="num">{{ c.loaded.toLocaleString() }}</td>
            <td class="num">{{ formatBytes(c.memoryBytes) }}</td>
            <td class="num muted">{{ formatBytes(c.peakMemoryBytes) }}</td>
            <td class="num">{{ c.spillBytes ? formatBytes(c.spillBytes) : '—' }}</td>
            <td class="num">{{ c.residentChunks }}</td>
            <td class="num muted">{{ c.idleSeconds }}s</td>
          </tr>
          <tr v-if="!rs!.cursors.length">
            <td colspan="7" class="muted center">{{ tr('当前没有打开的结果集') }}</td>
          </tr>
        </tbody>
      </table>

      <div class="note">
        <Icon name="info" :size="13" />
        <div>
          {{
            tr(
              '结果集的行数据按块缓存，超过上限后最久未用的块会被丢弃并转存到磁盘，需要时再读回来。因此打开超大表时内存不随行数增长，代价是滚回旧位置可能有一次磁盘读取。上限可在「选项 → 性能」中调整。',
            )
          }}
        </div>
      </div>
    </template>

    <template #footer>
      <button class="btn" @click="release">
        <Icon name="memory" :size="13" />{{ tr('立即回收内存') }}</button>
      <span class="spacer"></span>
      <button class="btn btn-primary" @click="emit('close')">{{ tr('关闭') }}</button>
    </template>
  </Modal>
</template>

<style scoped>
.cards {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 8px;
  margin-bottom: 12px;
}

.card {
  padding: 9px 11px;
  border: 1px solid var(--c-border-soft);
  border-radius: var(--radius);
  background: var(--c-bg-sunken);
}

.card-label {
  font-size: var(--font-size-sm);
  color: var(--c-text-secondary);
}

.card-value {
  margin: 3px 0 2px;
  font-size: 19px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}
.card-value small {
  font-size: 11px;
  font-weight: 400;
  color: var(--c-text-secondary);
}

.card-sub {
  font-size: var(--font-size-sm);
  color: var(--c-text-tertiary);
}

.gauge-row {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 12px;
}

.gauge-label {
  width: 110px;
  color: var(--c-text-secondary);
}

.gauge-track {
  flex: 1;
  height: 8px;
  border-radius: 4px;
  background: var(--c-border-soft);
  overflow: hidden;
}

.gauge-bar {
  height: 100%;
  background: var(--c-success);
  transition: width 0.4s ease;
}
.gauge-bar.is-mid {
  background: var(--c-warning);
}
.gauge-bar.is-high {
  background: var(--c-danger);
}

.gauge-pct {
  width: 40px;
  text-align: right;
  font-variant-numeric: tabular-nums;
}

.chart {
  margin-bottom: 4px;
}
.chart svg {
  width: 100%;
  height: 56px;
  border: 1px solid var(--c-border-soft);
  border-radius: var(--radius-sm);
  background: var(--c-bg-sunken);
}
.chart-caption {
  margin-top: 3px;
  font-size: var(--font-size-sm);
  text-align: right;
}

.section-title {
  margin-bottom: 6px;
  font-weight: 600;
}

.rtable {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--font-size);
}
.rtable th {
  padding: 4px 6px;
  text-align: left;
  font-weight: 600;
  color: var(--c-text-secondary);
  background: var(--c-chrome);
  border-bottom: 1px solid var(--c-border);
}
.rtable td {
  padding: 3px 6px;
  border-bottom: 1px solid var(--c-border-soft);
}
.rtable .num {
  text-align: right;
  font-variant-numeric: tabular-nums;
}
.rtable .center {
  text-align: center;
  padding: 14px;
}

.note {
  display: flex;
  gap: 7px;
  margin-top: 12px;
  padding: 9px 11px;
  border-radius: var(--radius-sm);
  background: var(--c-info-soft);
  color: var(--c-text-secondary);
  line-height: 1.6;
}

.spacer {
  flex: 1;
}
</style>
