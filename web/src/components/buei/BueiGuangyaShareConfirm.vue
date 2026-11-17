<script setup lang="ts">
import AppButton from "@/components/base/AppButton.vue";
import AppModal from "@/components/base/AppModal.vue";
import SvgIcon from "@/components/icons/SvgIcon.vue";

// buei 定制：光鸭分享收藏的转存确认弹窗，附带重命名预览。

export interface RenamePreviewRow {
  oldName: string;
  newName: string;
  path: string;
}

const props = defineProps<{
  open: boolean;
  target: string;
  count: number;
  renames: RenamePreviewRow[];
  unchangedCount: number;
  loading?: boolean;
}>();

const emit = defineEmits<{ confirm: []; cancel: [] }>();
</script>

<template>
  <AppModal
    :open="open"
    title="确认转存"
    size="lg"
    body-flush
    @close="!loading && emit('cancel')"
  >
    <div class="gysc">
      <p class="gysc__summary">
        将 <b>{{ count }}</b> 项内容转存到「{{ target }}」。
        目标文件夹不存在时会自动创建；文件夹会连同其中的文件一起转存。
      </p>

      <section class="gysc__preview">
        <header class="gysc__preview-head">
          <span class="gysc__preview-title">
            <SvgIcon name="pen" :size="13" />
            重命名预览
          </span>
          <span class="gysc__preview-count">
            <template v-if="renames.length">
              {{ renames.length }} 项改名
              <template v-if="unchangedCount"> · {{ unchangedCount }} 项保持原名</template>
            </template>
            <template v-else>未启用重命名规则</template>
          </span>
        </header>
        <p v-if="!renames.length" class="gysc__empty">
          当前没有需要修改的文件名，转存后保留原名称。
        </p>
        <ul v-else class="gysc__list">
          <li v-for="row in renames" :key="`${row.path}|${row.oldName}`" class="gysc__row">
            <span class="gysc__old" :title="row.oldName">{{ row.oldName }}</span>
            <SvgIcon name="arrow-right-long" :size="12" class="gysc__arrow" />
            <span class="gysc__new" :title="row.newName">{{ row.newName }}</span>
          </li>
        </ul>
      </section>
    </div>

    <template #footer>
      <AppButton variant="cancel" :disabled="loading" @click="emit('cancel')">再检查一下</AppButton>
      <AppButton variant="primary" :disabled="loading" @click="emit('confirm')">
        {{ loading ? "转存中…" : "开始转存" }}
      </AppButton>
    </template>
  </AppModal>
</template>

<style scoped>
.gysc {
  display: flex;
  flex-direction: column;
  min-height: 0;
  padding: 16px 20px 14px;
}

.gysc__summary {
  margin: 0 0 12px;
  color: var(--text-regular);
  font-size: 13px;
  line-height: 1.6;
}

.gysc__summary b {
  color: var(--brand);
}

.gysc__preview {
  display: flex;
  flex-direction: column;
  min-height: 0;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  overflow: hidden;
}

.gysc__preview-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 8px 12px;
  background: var(--surface-sunken);
  border-bottom: 1px solid var(--border-soft);
  font-size: 12px;
}

.gysc__preview-title {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-weight: 600;
  color: var(--text-regular);
}

.gysc__preview-count {
  color: var(--text-muted);
  font-size: 11px;
  white-space: nowrap;
}

.gysc__empty {
  margin: 0;
  padding: 28px 16px;
  text-align: center;
  color: var(--text-muted);
  font-size: 12px;
}

.gysc__list {
  max-height: min(46vh, 360px);
  overflow-y: auto;
  margin: 0;
  padding: 4px;
  list-style: none;
}

.gysc__row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr);
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  border-radius: var(--radius-sm);
  font-size: 12px;
}

.gysc__row:hover {
  background: var(--surface-hover);
}

.gysc__old,
.gysc__new {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.gysc__old {
  color: var(--text-muted);
  text-decoration: line-through;
  text-decoration-color: color-mix(in srgb, var(--text-muted) 55%, transparent);
}

.gysc__new {
  color: var(--text);
  font-weight: 500;
}

.gysc__arrow {
  color: var(--text-muted);
  flex-shrink: 0;
}
</style>
