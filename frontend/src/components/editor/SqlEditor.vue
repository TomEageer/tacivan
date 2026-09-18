<script setup lang="ts">
/**
 * SQL 编辑器。
 *
 * 基于 CodeMirror 6：语法高亮、行号、括号匹配、搜索都由它提供，
 * 这里补上方言切换、库表结构补全，以及「执行」相关的快捷键。
 */
import { onMounted, onBeforeUnmount, ref, shallowRef, watch } from 'vue'
import { EditorState, Compartment } from '@codemirror/state'
import { EditorView, keymap, placeholder as cmPlaceholder } from '@codemirror/view'
import { basicSetup } from 'codemirror'
import { sql, MySQL, PostgreSQL, SQLite, type SQLNamespace } from '@codemirror/lang-sql'
import { oneDark } from '@codemirror/theme-one-dark'
import { indentWithTab } from '@codemirror/commands'
import { useAppStore } from '../../stores/app'
import type { Engine } from '../../types'
import { tr } from '../../i18n'

const props = withDefaults(
  defineProps<{
    modelValue: string
    engine: Engine
    /** 表名 -> 列名，供自动补全。 */
    schema?: SQLNamespace
    placeholder?: string
    readonly?: boolean
  }>(),
  { placeholder: tr('在此输入 SQL，⌘R 执行全部，⇧⌘R 执行当前语句'), readonly: false },
)

const emit = defineEmits<{
  (e: 'update:modelValue', v: string): void
  (e: 'run'): void
  (e: 'run-current'): void
  (e: 'cursor', offset: number): void
}>()

const app = useAppStore()
const host = ref<HTMLElement | null>(null)
const view = shallowRef<EditorView | null>(null)

const themeComp = new Compartment()
const langComp = new Compartment()
const fontComp = new Compartment()

function dialectFor(engine: Engine) {
  switch (engine) {
    case 'postgres':
      return PostgreSQL
    case 'sqlite':
      return SQLite
    default:
      return MySQL
  }
}

function languageExt() {
  return sql({
    dialect: dialectFor(props.engine),
    schema: props.schema,
    upperCaseKeywords: true,
  })
}

function fontTheme() {
  return EditorView.theme({
    '&': {
      fontSize: `${app.settings.fontSize}px`,
      height: '100%',
    },
    '.cm-scroller': {
      fontFamily: app.settings.editorFont,
      lineHeight: '1.55',
    },
    '.cm-content': { padding: '6px 0' },
    '.cm-gutters': {
      background: 'var(--c-bg-sunken)',
      borderRight: '1px solid var(--c-border-soft)',
      color: 'var(--c-text-tertiary)',
    },
    '.cm-activeLineGutter': { background: 'var(--c-chrome-active)' },
  })
}

onMounted(() => {
  if (!host.value) return
  const state = EditorState.create({
    doc: props.modelValue,
    extensions: [
      basicSetup,
      langComp.of(languageExt()),
      themeComp.of(app.effectiveTheme === 'dark' ? oneDark : []),
      fontComp.of(fontTheme()),
      keymap.of([
        indentWithTab,
        // ⌘R / ⇧⌘R 与菜单里的「运行」保持一致。
        { key: 'Mod-r', preventDefault: true, run: () => (emit('run'), true) },
        { key: 'Shift-Mod-r', preventDefault: true, run: () => (emit('run-current'), true) },
        { key: 'Mod-Enter', preventDefault: true, run: () => (emit('run'), true) },
      ]),
      cmPlaceholder(props.placeholder),
      EditorView.editable.of(!props.readonly),
      EditorView.updateListener.of((u) => {
        if (u.docChanged) emit('update:modelValue', u.state.doc.toString())
        if (u.selectionSet) emit('cursor', u.state.selection.main.head)
      }),
    ],
  })
  view.value = new EditorView({ state, parent: host.value })
})

onBeforeUnmount(() => {
  view.value?.destroy()
  view.value = null
})

// 外部改动同步进编辑器（例如从历史里填入一条 SQL）。
watch(
  () => props.modelValue,
  (v) => {
    const ed = view.value
    if (!ed) return
    if (v === ed.state.doc.toString()) return
    ed.dispatch({ changes: { from: 0, to: ed.state.doc.length, insert: v } })
  },
)

watch(
  () => [props.engine, props.schema] as const,
  () => {
    view.value?.dispatch({ effects: langComp.reconfigure(languageExt()) })
  },
)

watch(
  () => app.effectiveTheme,
  (t) => {
    view.value?.dispatch({ effects: themeComp.reconfigure(t === 'dark' ? oneDark : []) })
  },
)

watch(
  () => [app.settings.fontSize, app.settings.editorFont] as const,
  () => {
    view.value?.dispatch({ effects: fontComp.reconfigure(fontTheme()) })
  },
)

/** 当前选中的文本，空串表示没有选区。 */
function selectedText(): string {
  const ed = view.value
  if (!ed) return ''
  const { from, to } = ed.state.selection.main
  return from === to ? '' : ed.state.sliceDoc(from, to)
}

function cursorOffset(): number {
  return view.value?.state.selection.main.head ?? 0
}

function focus() {
  view.value?.focus()
}

function setValue(v: string) {
  const ed = view.value
  if (!ed) return
  ed.dispatch({ changes: { from: 0, to: ed.state.doc.length, insert: v } })
}

/** 在光标处插入文本，用于从对象树拖入表名。 */
function insert(text: string) {
  const ed = view.value
  if (!ed) return
  const { from, to } = ed.state.selection.main
  ed.dispatch({ changes: { from, to, insert: text }, selection: { anchor: from + text.length } })
  ed.focus()
}

defineExpose({ selectedText, cursorOffset, focus, setValue, insert })
</script>

<template>
  <div ref="host" class="sqleditor"></div>
</template>

<style scoped>
.sqleditor {
  height: 100%;
  min-height: 0;
  overflow: hidden;
  background: var(--c-bg);
}

.sqleditor :deep(.cm-editor) {
  height: 100%;
}
.sqleditor :deep(.cm-editor.cm-focused) {
  outline: none;
}
.sqleditor :deep(.cm-scroller) {
  overflow: auto;
}
</style>
