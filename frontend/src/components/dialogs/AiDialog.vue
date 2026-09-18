<script setup lang="ts">
/**
 * AI 能力的规划说明。
 *
 * 功能还没做，但入口先占上——免得这条需求被忘掉，
 * 也让点进来的人清楚它现在是什么状态、将来会是什么样。
 */
import { computed } from 'vue'
import Modal from '../common/Modal.vue'
import Icon from '../common/Icon.vue'
import { tr } from '../../i18n'

const emit = defineEmits<{ (e: 'close'): void }>()

const planned = computed(() => [
  {
    title: tr('自然语言查询'),
    desc: tr('用中文描述要查什么，生成 SQL 后先给你过目，确认了才执行。不做「自动执行」——查错库的代价太大。'),
  },
  {
    title: tr('MCP 数据源'),
    desc: tr('已可用：新建连接选「MCP 服务器」，stdio 或 Streamable HTTP 都行。工具用表单手动调用、结果表格化进网格，资源可直接读——不经过 AI。'),
  },
  {
    title: tr('结果解读与执行计划分析'),
    desc: tr('把 EXPLAIN 的输出翻成人话，指出缺哪个索引、为什么走了全表扫描。'),
  },
  {
    title: tr('错误诊断'),
    desc: tr('SQL 报错时给出可能原因与改法，而不是把数据库的原始错误码直接甩给你。'),
  },
])
</script>

<template>
  <Modal :title="tr('AI 助手')" :width="560" @close="emit('close')">
    <div class="head">
      <Icon name="sparkle" :size="22" />
      <div>
        <div class="head-title">{{ tr('规划中') }}</div>
        <div class="muted">{{ tr('这个入口先占位，功能尚未实现') }}</div>
      </div>
    </div>

    <div class="divider"></div>

    <div v-for="p in planned" :key="p.title" class="item">
      <div class="item-title">{{ p.title }}</div>
      <div class="item-desc muted">{{ p.desc }}</div>
    </div>

    <div class="note">
      <Icon name="info" :size="13" />
      <span>
        {{
          tr(
            '接入方式与边界还没定（本地模型还是走 API、SQL 要不要强制人工确认、生产库连接是否一律禁用）。这些要先定下来再动手。',
          )
        }}
      </span>
    </div>

    <template #footer>
      <button class="btn btn-primary" @click="emit('close')">{{ tr('知道了') }}</button>
    </template>
  </Modal>
</template>

<style scoped>
.head {
  display: flex;
  align-items: center;
  gap: 12px;
  color: var(--c-accent);
}
.head-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--c-text);
}

.item {
  margin-bottom: 11px;
}
.item-title {
  font-weight: 600;
  margin-bottom: 2px;
}
.item-desc {
  line-height: 1.6;
}

.note {
  display: flex;
  gap: 7px;
  margin-top: 6px;
  padding: 9px 11px;
  border-radius: var(--radius-sm);
  background: var(--c-info-soft);
  color: var(--c-text-secondary);
  line-height: 1.6;
}
</style>
