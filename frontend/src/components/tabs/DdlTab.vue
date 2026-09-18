<script setup lang="ts">
/** 对象 DDL 查看页。 */
import { onMounted, ref } from 'vue'
import * as api from '../../api'
import { useAppStore } from '../../stores/app'
import type { Tab } from '../../stores/tabs'
import Icon from '../common/Icon.vue'
import { tr } from '../../i18n'

const props = defineProps<{ tab: Tab }>()
const app = useAppStore()

const ddl = ref('')
const loading = ref(false)

onMounted(load)

async function load() {
  loading.value = true
  try {
    ddl.value = await api.GetObjectDDL(props.tab.connId, props.tab.ref!)
  } catch (e) {
    app.reportError(e, tr('读取 DDL 失败'))
  } finally {
    loading.value = false
  }
}

function copy() {
  navigator.clipboard?.writeText(ddl.value)
  app.toast('success', tr('已复制 DDL'))
}

defineExpose({ refresh: load })
</script>

<template>
  <div class="tabpane">
    <div class="ddlbar">
      <span class="muted mono">{{ tab.ref?.database }}.{{ tab.ref?.name }}</span>
      <div class="spacer"></div>
      <button class="btn btn-ghost" :disabled="loading" @click="load">
        <Icon name="refresh" :size="13" />{{ tr('刷新') }}</button>
      <button class="btn" :disabled="!ddl" @click="copy">
        <Icon name="copy" :size="13" />{{ tr('复制') }}</button>
    </div>
    <pre v-if="ddl" class="ddl mono selectable">{{ ddl }}</pre>
    <div v-else class="empty-state">
      <div class="empty-state-title">{{ loading ? tr('读取中…') : tr('没有可显示的 DDL') }}</div>
    </div>
  </div>
</template>

<style scoped>
.tabpane {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}

.ddlbar {
  flex: none;
  display: flex;
  align-items: center;
  gap: 6px;
  height: 32px;
  padding: 0 8px;
  background: var(--c-bg-sunken);
  border-bottom: 1px solid var(--c-border-soft);
}

.spacer {
  flex: 1;
}

.ddl {
  flex: 1;
  min-height: 0;
  margin: 0;
  padding: 12px;
  overflow: auto;
  background: var(--c-bg);
  font-size: var(--font-size-mono);
  line-height: 1.6;
  white-space: pre;
}
</style>
