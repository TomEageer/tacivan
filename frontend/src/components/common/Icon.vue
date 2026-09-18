<script setup lang="ts">
import { ENGINE_LOGOS } from './engineLogos'
/**
 * 图标集。
 *
 * 全部内联 SVG：界面里不出现 emoji，图标风格统一为 16×16 线性描边，
 * 颜色跟随 currentColor，因此在浅色与深色主题下都不需要额外处理。
 */
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{ name: string; size?: number; strokeWidth?: number; filled?: boolean }>(),
  { size: 16, strokeWidth: 1.4, filled: false },
)

/** 描边路径，viewBox 统一为 0 0 16 16。 */
const STROKE: Record<string, string> = {
  // 对象
  database: 'M3 4c0-1.1 2.2-2 5-2s5 .9 5 2-2.2 2-5 2-5-.9-5-2zM3 4v8c0 1.1 2.2 2 5 2s5-.9 5-2V4M3 8c0 1.1 2.2 2 5 2s5-.9 5-2',
  table: 'M2.5 3.5h11v9h-11zM2.5 6.5h11M6.5 6.5v6M10 6.5v6',
  view: 'M1.5 8s2.5-4 6.5-4 6.5 4 6.5 4-2.5 4-6.5 4S1.5 8 1.5 8zM8 9.8a1.8 1.8 0 100-3.6 1.8 1.8 0 000 3.6z',
  function: 'M9.5 3h-1a2 2 0 00-2 2v6a2 2 0 01-2 2M5 8h5M11.5 5.5l2 5M13.5 5.5l-2 5',
  procedure: 'M4 2.5h6l2.5 2.5v8.5h-8.5zM10 2.5V5h2.5M5.5 8.5h5M5.5 11h3',
  trigger: 'M8.5 1.5L3 9h4l-.5 5.5L12 7H8z',
  event: 'M8 2a6 6 0 100 12A6 6 0 008 2zM8 4.5V8l2.5 1.5',
  sequence: 'M2.5 4h3M2.5 8h3M2.5 12h3M8 4h5.5M8 8h5.5M8 12h5.5',
  index: 'M3 3.5h10M3 8h10M3 12.5h6M11.5 11l1.5 1.5-1.5 1.5',
  column: 'M5 2.5h6v11H5zM5 5.5h6',
  key: 'M10.2 2.5a3.3 3.3 0 00-3.1 4.4L2.5 11.5v2h2l.5-1h1.5v-1.5h1.5l1.1-1.1a3.3 3.3 0 101.1-7.4zM11 5.2a.8.8 0 110 1.6.8.8 0 010-1.6z',
  folder: 'M1.5 3.5h4.5l1.5 2h7v7.5h-13z',
  folderOpen: 'M1.5 3.5h4.5l1.5 2h7V7M1.5 5.5L3 13h11l1.5-6z',

  // 引擎
  server: 'M2.5 2.5h11v4.5h-11zM2.5 9h11v4.5h-11zM5 4.8h.01M5 11.3h.01',

  // 操作
  play: 'M4.5 3L12 8l-7.5 5z',
  stop: 'M4 4h8v8H4z',
  plus: 'M8 3.5v9M3.5 8h9',
  minus: 'M3.5 8h9',
  close: 'M4 4l8 8M12 4l-8 8',
  check: 'M3.5 8.5l3 3 6-7',
  refresh: 'M13.5 7a5.5 5.5 0 10-1.2 4.3M13.5 3.5V7H10',
  save: 'M3 3h7.5L13 5.5V13H3zM5.5 3v3.5h5V3M5.5 13v-3.5h5V13',
  trash: 'M3.5 4.5h9M6.5 4.5V3h3v1.5M4.5 4.5l.6 8.5h5.8l.6-8.5M6.8 7v3.5M9.2 7v3.5',
  edit: 'M11.5 2.5l2 2L6 12l-2.8.8.8-2.8zM10 4l2 2',
  copy: 'M5.5 5.5h7v7h-7zM3.5 10.5v-7h7',
  search: 'M7.2 2.5a4.7 4.7 0 100 9.4 4.7 4.7 0 000-9.4zM10.8 10.8l2.7 2.7',
  filter: 'M2.5 3.5h11l-4.2 5v4.5l-2.6-1.3V8.5z',
  sort: 'M5 3v10M5 3L3 5.5M5 3l2 2.5M11 13V3M11 13l-2-2.5M11 13l2-2.5',
  download: 'M8 2.5v8M8 10.5L5 7.5M8 10.5l3-3M3 13h10',
  upload: 'M8 11.5v-8M8 3.5L5 6.5M8 3.5l3 3M3 13h10',
  settings:
    'M12.84 6.76 L14.53 7.07 L14.53 8.93 L12.84 9.24 L12.30 10.55 L13.28 11.96 L11.96 13.28 L10.55 12.30 L9.24 12.84 L8.93 14.53 L7.07 14.53 L6.76 12.84 L5.45 12.30 L4.04 13.28 L2.72 11.96 L3.70 10.55 L3.16 9.24 L1.47 8.93 L1.47 7.07 L3.16 6.76 L3.70 5.45 L2.72 4.04 L4.04 2.72 L5.45 3.70 L6.76 3.16 L7.07 1.47 L8.93 1.47 L9.24 3.16 L10.55 3.70 L11.96 2.72 L13.28 4.04 L12.30 5.45Z M10.30 8a2.3 2.3 0 1 1-4.6 0a2.3 2.3 0 1 1 4.6 0',
  code: 'M5.5 4L2 8l3.5 4M10.5 4L14 8l-3.5 4',
  terminal: 'M3 4l3 3-3 3M8 11h5',
  chart: 'M2.5 13V3M13.5 13h-11M5 11V7.5M8 11V4.5M11 11V8.5',
  info: 'M8 2a6 6 0 100 12A6 6 0 008 2zM8 7.2v4M8 4.9h.01',
  warning: 'M8 2.2L14.2 13H1.8zM8 6.5v3.2M8 11.4h.01',
  link: 'M6.5 9.5l3-3M6.8 4.6l1.4-1.4a2.6 2.6 0 013.6 3.6L10.4 8.2M5.6 7.8L4.2 9.2a2.6 2.6 0 003.6 3.6l1.4-1.4',
  unlink: 'M6.8 4.6l1.4-1.4a2.6 2.6 0 013.6 3.6L10.4 8.2M5.6 7.8L4.2 9.2a2.6 2.6 0 003.6 3.6l1.4-1.4M2.5 2.5l11 11',
  lock: 'M4.5 7V5.2a3.5 3.5 0 017 0V7M3.5 7h9v6.5h-9z',
  memory: 'M4 4h8v8H4zM6.2 1.8v2M9.8 1.8v2M6.2 12.2v2M9.8 12.2v2M1.8 6.2h2M1.8 9.8h2M12.2 6.2h2M12.2 9.8h2',
  layers: 'M8 2l6 3-6 3-6-3zM2 8l6 3 6-3M2 11l6 3 6-3',
  history: 'M2.5 8a5.5 5.5 0 111.6 3.9M2.5 4.5V8h3.5M8 5.5V8l2 1.5',
  chevronRight: 'M6 3.5L10.5 8 6 12.5',
  chevronDown: 'M3.5 6L8 10.5 12.5 6',
  chevronLeft: 'M10 3.5L5.5 8 10 12.5',
  chevronUp: 'M3.5 10L8 5.5 12.5 10',
  more: 'M3.5 8h.01M8 8h.01M12.5 8h.01',
  grid: 'M2.5 2.5h11v11h-11zM2.5 6h11M2.5 9.5h11M6 2.5v11',
  file: 'M4 2h5l3 3v9H4zM9 2v3h3',
  panelLeft: 'M2 3h12v10H2zM6 3v10',
  sun: 'M8 5a3 3 0 100 6 3 3 0 000-6zM8 1.5v2M8 12.5v2M1.5 8h2M12.5 8h2M3.4 3.4l1.4 1.4M11.2 11.2l1.4 1.4M12.6 3.4l-1.4 1.4M4.8 11.2l-1.4 1.4',
  moon: 'M13.2 9.6A5.6 5.6 0 016.4 2.8a5.8 5.8 0 106.8 6.8z',
  auto: 'M8 2a6 6 0 100 12A6 6 0 008 2zM8 2v12a6 6 0 000-12z',
  sparkle: 'M6 2.2l1 2.6 2.6 1-2.6 1-1 2.6-1-2.6-2.6-1 2.6-1zM12 8.4l.6 1.6 1.6.6-1.6.6-.6 1.6-.6-1.6-1.6-.6 1.6-.6zM11 1.8l.4 1.1 1.1.4-1.1.4-.4 1.1-.4-1.1-1.1-.4 1.1-.4z',
  star: 'M8 1.8l1.9 3.9 4.3.6-3.1 3 .7 4.3L8 11.6l-3.8 2 .7-4.3-3.1-3 4.3-.6z',
  sql: 'M2.5 4.2c0-.9 1.4-1.7 3.2-1.7s3.2.8 3.2 1.7-1.4 1.7-3.2 1.7-3.2-.8-3.2-1.7zM2.5 4.2v7.6c0 .9 1.4 1.7 3.2 1.7M15 8.5a2 2 0 00-2-2h-1a1.5 1.5 0 000 3h1a1.5 1.5 0 010 3h-1a2 2 0 01-2-2',
}

/** 需要填充而非描边的图标。 */
const FILL = new Set(['play', 'stop'])

const path = computed(() => STROKE[props.name] ?? STROKE.info)
// 有些图标（如收藏星）在「选中」状态下要填充，用 filled 显式控制。
const filled = computed(() => props.filled || FILL.has(props.name))
</script>

<template>
  <svg
    v-if="name.startsWith('logo:') && ENGINE_LOGOS[name.slice(5)]"
    :width="size"
    :height="size"
    :viewBox="ENGINE_LOGOS[name.slice(5)].viewBox"
    fill="currentColor"
    aria-hidden="true"
  >
    <path :d="ENGINE_LOGOS[name.slice(5)].d" fill-rule="evenodd" />
  </svg>
  <svg
    v-else
    class="icon"
    :width="size"
    :height="size"
    viewBox="0 0 16 16"
    aria-hidden="true"
    focusable="false"
  >
    <path
      :d="path"
      :fill="filled ? 'currentColor' : 'none'"
      :stroke="filled ? 'none' : 'currentColor'"
      :stroke-width="strokeWidth"
      stroke-linecap="round"
      stroke-linejoin="round"
    />
  </svg>
</template>

<style scoped>
.icon {
  flex: none;
  display: block;
}
</style>
