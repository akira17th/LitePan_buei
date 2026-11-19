<script setup lang="ts">
import { computed } from "vue";
import AppButton from "@/components/base/AppButton.vue";
import AppModal from "@/components/base/AppModal.vue";
import SvgIcon from "@/components/icons/SvgIcon.vue";

// buei 定制：光鸭分享收藏的转存确认弹窗，附带重命名预览。

export interface ScrapeProgressView {
  running: boolean;
  failed: boolean;
  percent: number;
  text: string;
  message: string;
}

export interface RenamePreviewRow {
  oldName: string;
  newName: string;
  path: string;
}

const props = withDefaults(
  defineProps<{
    open: boolean;
    target: string;
    count: number;
    renames: RenamePreviewRow[];
    unchangedCount: number;
    loading?: boolean;
    phase?: string;
    autoStrm?: boolean;
    done?: boolean;
    renamed?: number;
    renameSkipped?: number;
    strmGenerated?: number;
    strmMatched?: boolean;
    strmError?: string;
    autoScrape?: boolean;
    scrapeTaskId?: number;
    scrapeStarted?: boolean;
    scrapeError?: string;
    scrapeProgress?: ScrapeProgressView | null;
    refreshEmby?: boolean;
    embyRefreshed?: boolean;
    embyScheduled?: boolean;
    embyError?: string;
    /** 动作名称：光鸭分享使用「转存」，夸克入库使用「入库」。 */
    actionLabel?: string;
    hint?: string;
  }>(),
  { actionLabel: "转存", hint: "" },
);

const emit = defineEmits<{ confirm: []; cancel: []; stay: []; open: [] }>();

type StepState = "pending" | "active" | "done" | "skipped";

const PHASE_ORDER: string[] = ["transfer", "rename", "strm", "scrape", "emby"];

// 主流程走完后，刮削与刷库仍在后台运行，不能直接把所有步骤标成完成。
const phaseIndex = computed(() => {
  if (props.phase === "done" || props.phase === "failed") return PHASE_ORDER.length;
  const index = PHASE_ORDER.indexOf(props.phase || "transfer");
  return index < 0 ? 0 : index;
});

const asyncFinished = computed(() => props.phase === "done" || props.phase === "failed");

const asyncRunning = computed(() => {
  if (!props.done || asyncFinished.value) return false;
  if (props.phase === "scrape" || props.phase === "emby") return true;
  return !!props.scrapeProgress?.running || !!props.embyScheduled;
});

const dialogTitle = computed(() => {
  if (!props.done) return `确认${props.actionLabel}`;
  return asyncRunning.value ? `${props.actionLabel}完成，联动处理中` : `${props.actionLabel}完成`;
});

function stepState(key: string, index: number, skipped: boolean): StepState {
  if (skipped) return "skipped";
  if (!props.done && !props.loading) return "pending";
  if (!props.done) {
    if (phaseIndex.value > index) return "done";
    if (phaseIndex.value === index) return "active";
    return "pending";
  }
  if (key === "transfer" || key === "rename" || key === "strm") return "done";
  if (key === "scrape") {
    if (asyncFinished.value || props.scrapeError) return "done";
    if (props.scrapeProgress?.running) return "active";
    // 刮削进度已结束，但后端可能还在等 5s 轮询切到刷库阶段。
    if (props.scrapeProgress && !props.scrapeProgress.running) return "done";
    if (props.scrapeStarted) return "active";
    return "done";
  }
  // emby：刮削未结束时保持等待，刷库阶段再显示进行中。
  if (asyncFinished.value || props.embyRefreshed || props.embyError) return "done";
  if (props.phase === "emby") return "active";
  if (props.embyScheduled) return "pending";
  return "done";
}

// 每个阶段自己的结果摘要，完成时显示在该步骤下方。
function stepDetail(key: string) {
  if (!props.done) return "";
  switch (key) {
    case "transfer":
      return `${props.count} 项已${props.actionLabel}`;
    case "rename":
      if (!props.renames.length) return "未启用";
      if (props.renameSkipped) return `已重命名 ${props.renamed || 0} 项，${props.renameSkipped} 项失败`;
      if (props.renamed) return `已重命名 ${props.renamed} 项`;
      return "无需重命名";
    case "strm":
      if (props.autoStrm === false) return "已关闭";
      if (props.strmError) return "生成失败";
      if (props.strmGenerated) return `已生成 ${props.strmGenerated} 个`;
      if (props.strmMatched) return "已是最新";
      return "未匹配 STRM 任务";
    case "scrape":
      if (props.autoStrm === false || props.autoScrape === false) return "已关闭";
      if (!props.scrapeTaskId) return "未选择任务";
      if (props.scrapeProgress?.text) return props.scrapeProgress.text;
      if (props.scrapeError) return "启动失败";
      if (props.scrapeStarted) return "已开始刮削";
      return "已跳过";
    case "emby":
      if (props.refreshEmby === false) return "已关闭";
      if (props.embyError) return `刷库失败：${props.embyError}`;
      if (props.phase === "emby") return "刷库中…";
      if (props.phase === "failed") return "刷库失败";
      if (props.embyScheduled && props.phase !== "done") {
        return props.scrapeProgress?.running || props.phase === "scrape"
          ? "等待刮削完成"
          : "准备刷库…";
      }
      if (props.embyRefreshed || props.phase === "done") return "已触发刷库";
      return "已跳过";
    default:
      return "";
  }
}

// 转存 → 重命名 → 生成 STRM；没有改名/未开启自动 STRM 时显示为跳过。
const steps = computed(() => {
  const defs = [
    { key: "transfer", label: props.actionLabel, skipped: false },
    { key: "rename", label: "重命名", skipped: !props.renames.length },
    { key: "strm", label: "生成 STRM", skipped: props.autoStrm === false },
    {
      key: "scrape",
      label: "刮削",
      skipped:
        props.autoStrm === false || props.autoScrape === false || !props.scrapeTaskId,
    },
    { key: "emby", label: "Emby 刷库", skipped: props.refreshEmby === false },
  ];
  return defs.map((step, index) => ({
    ...step,
    state: stepState(step.key, index, step.skipped),
    detail: stepDetail(step.key),
  }));
});

const phaseText = computed(() => {
  switch (props.phase) {
    case "rename":
      return "重命名中…";
    case "strm":
      return "生成 STRM 中…";
    case "scrape":
      return "刮削中…";
    case "emby":
      return "刷库中…";
    case "done":
      return "即将完成…";
    case "failed":
      return "处理失败";
    default:
      return `${props.actionLabel}中…`;
  }
});
</script>

<template>
  <AppModal
    :open="open"
    :title="dialogTitle"
    size="lg"
    body-flush
    @close="!loading && emit('cancel')"
  >
    <div class="gysc">
      <p v-if="done" class="gysc__summary is-done">
        已将 <b>{{ count }}</b> 项内容保存到「{{ target }}」。
      </p>
      <p v-else class="gysc__summary">
        将 <b>{{ count }}</b> 项内容{{ actionLabel }}到「{{ target }}」。
        目标文件夹不存在时会自动创建；文件夹会连同其中的文件一起{{ actionLabel }}。
      </p>
      <p v-if="hint && !done" class="gysc__hint">{{ hint }}</p>

      <div v-if="loading || done" class="gysc__steps">
        <div
          v-for="(step, index) in steps"
          :key="step.key"
          class="gysc__step"
          :class="`is-${step.state}`"
        >
          <span class="gysc__step-head">
            <span class="gysc__step-dot">
              <SvgIcon v-if="step.state === 'done'" name="check" :size="10" />
              <span v-else-if="step.state === 'active'" class="gysc__step-pulse" />
              <template v-else>{{ index + 1 }}</template>
            </span>
            <span class="gysc__step-label">{{ step.label }}</span>
          </span>
          <span v-if="step.detail" class="gysc__step-detail" :title="step.detail">
            {{ step.detail }}
          </span>
          <template v-if="step.key === 'scrape' && scrapeProgress?.running">
            <span class="gysc__step-bar">
              <span :style="{ width: `${scrapeProgress.percent}%` }" />
            </span>
            <span v-if="scrapeProgress.message" class="gysc__step-msg" :title="scrapeProgress.message">
              {{ scrapeProgress.message }}
            </span>
          </template>
        </div>
      </div>

      <section class="gysc__preview">
        <header class="gysc__preview-head">
          <span class="gysc__preview-title">
            <SvgIcon name="pen" :size="13" />
            {{ done ? "重命名结果" : "重命名预览" }}
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
      <template v-if="done">
        <AppButton variant="cancel" @click="emit('stay')">留在这里</AppButton>
        <AppButton variant="primary" @click="emit('open')">去前台查看</AppButton>
      </template>
      <template v-else>
        <AppButton variant="cancel" :disabled="loading" @click="emit('cancel')">再检查一下</AppButton>
        <AppButton variant="primary" :disabled="loading" @click="emit('confirm')">
          {{ loading ? phaseText : `开始${actionLabel}` }}
        </AppButton>
      </template>
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

.gysc__summary.is-done {
  color: var(--text-regular);
  font-weight: 500;
}

.gysc__summary.is-done b {
  color: var(--brand);
}

.gysc__hint {
  margin: -4px 0 12px;
  padding: 6px 9px;
  border-radius: var(--radius-sm);
  background: var(--surface-sunken);
  color: var(--text-muted);
  font-size: 11px;
  line-height: 1.5;
}

/* 转存阶段进度条：每个步骤的结果显示在自身下方 */
.gysc__steps {
  display: flex;
  align-items: stretch;
  margin: 0 0 12px;
  padding: 9px 12px;
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-md);
  background: var(--surface-sunken);
}

.gysc__step {
  display: flex;
  flex-direction: column;
  gap: 3px;
  flex: 1 1 0;
  min-width: 0;
  padding: 0 10px;
  border-left: 1px solid var(--border-soft);
  font-size: 12px;
  color: var(--text-muted);
}

.gysc__step:first-child {
  padding-left: 0;
  border-left: none;
}

.gysc__step:last-child {
  padding-right: 0;
}

.gysc__step-head {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.gysc__step-detail {
  padding-left: 22px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 11px;
  font-weight: 400;
  color: var(--text-muted);
}

/* 刮削进行中的细进度条，跟随该步骤显示 */
.gysc__step-bar {
  margin-left: 22px;
  height: 4px;
  overflow: hidden;
  border-radius: var(--radius-pill);
  background: var(--surface);
}

.gysc__step-bar span {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: var(--brand);
  transition: width 0.3s ease;
}

.gysc__step-msg {
  padding-left: 22px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 10px;
  color: var(--text-muted);
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

/* 窄屏改为纵向堆叠，结果仍跟随各自步骤 */
@media (max-width: 520px) {
  .gysc__steps {
    flex-direction: column;
    gap: 0;
  }

  .gysc__step {
    padding: 7px 0;
    border-left: none;
  }

  .gysc__step + .gysc__step {
    border-top: 1px solid var(--border-soft);
  }

  .gysc__step-detail {
    padding-left: 22px;
  }
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

/* 窄屏重命名预览改为纵向，长文件名换行完整显示，不再截断 */
@media (max-width: 640px) {
  .gysc__row {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 2px;
    padding: 8px;
  }

  .gysc__old,
  .gysc__new {
    max-width: 100%;
    overflow: visible;
    text-overflow: clip;
    white-space: normal;
    overflow-wrap: anywhere;
  }

  .gysc__arrow {
    transform: rotate(90deg);
    margin: 1px 0 1px 1px;
  }
}
</style>
