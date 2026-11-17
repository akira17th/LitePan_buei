<script setup lang="ts">
import { computed } from "vue";
import AppButton from "@/components/base/AppButton.vue";
import AppModal from "@/components/base/AppModal.vue";
import SvgIcon from "@/components/icons/SvgIcon.vue";
import type { GuangyaShareTransferPhase } from "@/api/bueiGuangyaShare";

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
  phase?: GuangyaShareTransferPhase;
  autoStrm?: boolean;
}>();

const emit = defineEmits<{ confirm: []; cancel: [] }>();

type StepState = "pending" | "active" | "done" | "skipped";

const PHASE_ORDER: GuangyaShareTransferPhase[] = ["transfer", "rename", "strm"];

const activeIndex = computed(() => {
  if (!props.loading) return -1;
  if (props.phase === "done" || props.phase === "failed") return PHASE_ORDER.length;
  const index = PHASE_ORDER.indexOf(props.phase || "transfer");
  return index < 0 ? 0 : index;
});

// 转存 → 重命名 → 生成 STRM；没有改名/未开启自动 STRM 时显示为跳过。
const steps = computed(() => {
  const skipped = [!props.renames.length, props.autoStrm === false];
  return [
    { key: "transfer", label: "转存" },
    { key: "rename", label: "重命名" },
    { key: "strm", label: "生成 STRM" },
  ].map((step, index) => {
    let state: StepState;
    if (skipped[index - 1]) state = "skipped";
    else if (activeIndex.value > index) state = "done";
    else if (activeIndex.value === index) state = "active";
    else state = "pending";
    return { ...step, state };
  });
});

const phaseText = computed(() => {
  switch (props.phase) {
    case "rename":
      return "重命名中…";
    case "strm":
      return "生成 STRM 中…";
    case "done":
      return "即将完成…";
    case "failed":
      return "处理失败";
    default:
      return "转存中…";
  }
});
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

      <div v-if="loading" class="gysc__steps">
        <div
          v-for="(step, index) in steps"
          :key="step.key"
          class="gysc__step"
          :class="`is-${step.state}`"
        >
          <span class="gysc__step-dot">
            <SvgIcon v-if="step.state === 'done'" name="check" :size="10" />
            <span v-else-if="step.state === 'active'" class="gysc__step-pulse" />
            <template v-else>{{ index + 1 }}</template>
          </span>
          <span class="gysc__step-label">{{ step.label }}</span>
        </div>
      </div>

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
        {{ loading ? phaseText : "开始转存" }}
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

/* 转存阶段进度条 */
.gysc__steps {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 0 0 12px;
  padding: 9px 12px;
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-md);
  background: var(--surface-sunken);
}

.gysc__step {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 1 1 0;
  min-width: 0;
  font-size: 12px;
  color: var(--text-muted);
}

.gysc__step + .gysc__step::before {
  content: "";
  flex: 0 0 auto;
  width: 14px;
  height: 1px;
  margin-right: 2px;
  background: var(--border);
}

.gysc__step-dot {
  display: grid;
  place-items: center;
  flex: 0 0 auto;
  width: 16px;
  height: 16px;
  border: 1px solid var(--border);
  border-radius: 50%;
  background: var(--surface);
  font-size: 9px;
  font-weight: 600;
  color: var(--text-muted);
}

.gysc__step-label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.gysc__step.is-active {
  color: var(--brand);
  font-weight: 600;
}

.gysc__step.is-active .gysc__step-dot {
  border-color: var(--brand);
  background: color-mix(in srgb, var(--brand) 12%, var(--surface));
}

.gysc__step.is-done {
  color: var(--text-regular);
}

.gysc__step.is-done .gysc__step-dot {
  border-color: var(--brand);
  background: var(--brand);
  color: #fff;
}

.gysc__step.is-skipped {
  opacity: 0.5;
}

.gysc__step.is-skipped .gysc__step-label {
  text-decoration: line-through;
}

.gysc__step-pulse {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--brand);
  animation: gysc-pulse 1s ease-in-out infinite;
}

@keyframes gysc-pulse {
  0%,
  100% {
    opacity: 0.35;
    transform: scale(0.8);
  }
  50% {
    opacity: 1;
    transform: scale(1);
  }
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
