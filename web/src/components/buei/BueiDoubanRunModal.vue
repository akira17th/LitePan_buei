<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { getApiErrorMessage } from "@/api/client";
import {
  fetchBueiDoubanScorePlan,
  fetchBueiDoubanScoreState,
  queryBueiDoubanScore,
  stopBueiDoubanScore,
  type BueiDoubanPlan,
  type BueiDoubanPlanItem,
  type BueiDoubanProgress,
} from "@/api/bueiDoubanScore";
import AppButton from "@/components/base/AppButton.vue";
import AppModal from "@/components/base/AppModal.vue";
import BusySpinner from "@/components/base/BusySpinner.vue";
import SvgIcon from "@/components/icons/SvgIcon.vue";
import { toast } from "@/composables/useToast";

// buei:douban-score —— 「立即查询」弹窗：
// 先展示按规则筛选出的「将查询 / 无需查询」清单（无需查询默认收起），确认后执行；
// 执行中逐项显示查询状态与真实结果（风控 / 限流 / 无匹配分开说明），结束后保持结果视图。

const props = defineProps<{
  strmTaskId: number;
  taskName: string;
}>();

const emit = defineEmits<{
  (e: "close"): void;
  (e: "finished"): void;
}>();

const loading = ref(false);
const loadError = ref("");
const plan = ref<BueiDoubanPlan | null>(null);
const running = ref(false);
const progress = ref<BueiDoubanProgress | null>(null);
const stopping = ref(false);
// plan=确认清单 → running=执行中 → done=结束（保持显示逐项结果）
const phase = ref<"plan" | "running" | "done">("plan");
const skipOpen = ref(false);
let pollTimer: ReturnType<typeof setTimeout> | null = null;

const ready = computed(() => !loading.value && !loadError.value);

const runItems = computed<BueiDoubanPlanItem[]>(() => plan.value?.run ?? []);
const skipItems = computed<BueiDoubanPlanItem[]>(() => plan.value?.skip ?? []);

// 执行中按「正在查 → 已完成 → 失败 → 等待」合并展示。
type RunRowState = "pending" | "running" | "queried" | "failed";
interface RunRow {
  item: BueiDoubanPlanItem;
  state: RunRowState;
  detail: string;
}

const runRows = computed<RunRow[]>(() => {
  const status = progress.value?.items ?? {};
  const current = progress.value?.current_item_id ?? "";
  return runItems.value.map((item) => {
    const st = status[item.id];
    let state: RunRowState = "pending";
    let detail = item.reason;
    if (st) {
      state = (st.state === "failed" ? "failed" : "queried") as RunRowState;
      detail = st.detail || "";
    } else if (current === item.id) {
      state = "running";
      detail = "正在查询…";
    }
    return { item, state, detail };
  });
});

const doneRows = computed(() => runRows.value.filter((r) => r.state === "queried" || r.state === "failed").length);

const failedCount = computed(() => runRows.value.filter((r) => r.state === "failed").length);

const finishedWithError = computed(() => Boolean(progress.value && !progress.value.running && progress.value.error));

watch(
  () => props.strmTaskId,
  () => {
    phase.value = "plan";
    skipOpen.value = false;
    progress.value = null;
    void loadPlan();
  },
  { immediate: true },
);

async function loadPlan() {
  loading.value = true;
  loadError.value = "";
  try {
    plan.value = await fetchBueiDoubanScorePlan(props.strmTaskId);
  } catch (e) {
    loadError.value = getApiErrorMessage(e, "读取查询计划失败");
  } finally {
    loading.value = false;
  }
}

async function startRun() {
  if (running.value) return;
  running.value = true;
  phase.value = "running";
  try {
    progress.value = await queryBueiDoubanScore({ strm_task_id: props.strmTaskId });
    schedulePoll();
  } catch (e) {
    toast.error(getApiErrorMessage(e, "启动查询失败"));
    running.value = false;
    phase.value = "plan";
  }
}

function schedulePoll() {
  if (pollTimer) clearTimeout(pollTimer);
  pollTimer = setTimeout(async () => {
    pollTimer = null;
    await refreshProgress();
    if (!running.value) return;
    if (progress.value?.running) {
      schedulePoll();
      return;
    }
    running.value = false;
    phase.value = "done";
    emit("finished");
  }, 2000);
}

async function refreshProgress() {
  try {
    const st = await fetchBueiDoubanScoreState();
    progress.value = st.progress;
  } catch {
    // 轮询失败不打断，等下一轮。
  }
}

async function stopRun() {
  if (stopping.value) return;
  stopping.value = true;
  try {
    const st = await stopBueiDoubanScore();
    progress.value = st.progress;
  } catch (e) {
    toast.error(getApiErrorMessage(e, "停止查询失败"));
  } finally {
    stopping.value = false;
  }
}

interface rowMeta {
  icon: string;
  label: string;
}

function rowMeta(row: RunRow): rowMeta {
  switch (row.state) {
    case "running":
      return { icon: "", label: "查询中" };
    case "queried":
      return { icon: "circle-check", label: "完成" };
    case "failed":
      return { icon: "circle-exclamation", label: "失败" };
    default:
      return { icon: "", label: "等待" };
  }
}

onBeforeUnmount(() => {
  if (pollTimer) clearTimeout(pollTimer);
});
</script>

<template>
  <AppModal open size="lg" :title="`立即查询 · ${taskName}`" @close="emit('close')">
    <div class="brun">
      <!-- 阶段一：查询计划确认 -->
      <template v-if="phase === 'plan'">
        <div v-if="loading" class="brun__state">
          <BusySpinner variant="notch" :size="20" />正在分析查询计划…
        </div>
        <div v-else-if="loadError" class="brun__state brun__state--error">
          <span>{{ loadError }}</span>
          <AppButton type="button" variant="secondary" size="sm" @click="loadPlan">重试</AppButton>
        </div>
        <template v-else>
          <p class="brun__lead">
            按调度规则筛选出 <strong>将查询 {{ runItems.length }}</strong> 项<template v-if="skipItems.length">，另有 {{ skipItems.length }} 项无需查询</template>。
          </p>

          <div v-if="!runItems.length" class="brun__empty">
            <SvgIcon name="circle-check" :size="18" class-name="brun__empty-icon" />
            <span>没有需要查询的作品：全部都已查到评分且上映 / 完结超过 1 个月，或尚未刮削 TMDB。</span>
          </div>

          <div v-else class="brun__list brun__list--run">
            <div v-for="row in runItems" :key="row.id" class="brun__row brun__row--plan">
              <span class="brun__name">{{ row.title }}</span>
              <span class="brun__reason">{{ row.reason }}</span>
            </div>
          </div>

          <div v-if="skipItems.length" class="brun__skip">
            <button type="button" class="brun__skip-toggle" @click="skipOpen = !skipOpen">
              <SvgIcon name="chevron-down" :size="12" :class-name="skipOpen ? '' : 'is-collapsed'" />
              无需查询 · {{ skipItems.length }} 项
            </button>
            <div v-if="skipOpen" class="brun__skip-body">
              <div v-for="row in skipItems" :key="row.id" class="brun__row brun__row--skip">
                <span class="brun__name">{{ row.title }}</span>
                <span class="brun__why">{{ row.reason }}</span>
              </div>
            </div>
          </div>
        </template>
      </template>

      <!-- 阶段二：执行中与结束后的逐项结果（结束后保持显示） -->
      <template v-else>
        <div class="brun__head">
          <template v-if="progress?.running">
            <BusySpinner variant="notch" :size="16" />
            <span class="brun__head-text">正在查询 {{ Math.min(progress.done + 1, progress.total) }}/{{ progress.total }}</span>
            <span class="brun__spacer" />
            <AppButton type="button" variant="secondary" size="sm" :disabled="stopping" @click="stopRun">
              停止
            </AppButton>
          </template>
          <template v-else>
            <span class="brun__head-text" :class="{ 'brun__head-text--error': finishedWithError }">
              {{ finishedWithError ? progress?.error || "查询失败" : "查询结束" }}
            </span>
            <span class="brun__counters">
              <span class="brun__counter brun__counter--ok">完成 {{ doneRows - failedCount }}</span>
              <span v-if="failedCount" class="brun__counter brun__counter--fail">失败 {{ failedCount }}</span>
              <span v-if="skipItems.length" class="brun__counter">跳过 {{ skipItems.length }}</span>
            </span>
          </template>
        </div>

        <div class="brun__list brun__list--run">
          <div
            v-for="row in runRows"
            :key="row.item.id"
            class="brun__row brun__row--result"
            :class="[`brun__row--${row.state}`]"
          >
            <span class="brun__cell-icon">
              <BusySpinner v-if="row.state === 'running'" variant="notch" :size="13" />
              <SvgIcon v-else-if="rowMeta(row).icon" :name="rowMeta(row).icon" :size="13" />
              <i v-else class="brun__dot" aria-hidden="true" />
            </span>
            <span class="brun__name">{{ row.item.title }}</span>
            <span class="brun__detail" :class="{ 'brun__detail--fail': row.state === 'failed' }" :title="row.detail">
              {{ row.detail }}
            </span>
          </div>
        </div>
      </template>
    </div>

    <template #footer>
      <AppButton type="button" variant="ghost" @click="emit('close')">{{ phase === "plan" ? "取消" : "关闭" }}</AppButton>
      <AppButton
        v-if="phase === 'plan'"
        type="button"
        variant="primary"
        :disabled="!ready || !runItems.length"
        @click="startRun"
      >
        开始查询（{{ runItems.length }}）
      </AppButton>
    </template>
  </AppModal>
</template>

<style scoped>
.brun {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: 220px;
  max-height: 62vh;
  /* 兜底：任意区块（如展开的跳过清单）把内容撑高时，整体内滚不溢出弹窗 */
  overflow-y: auto;
  min-height: 0;
  scrollbar-width: thin;
}

.brun__state {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  min-height: 140px;
  color: var(--text-muted);
  font-size: 12px;
}

.brun__state--error {
  color: var(--danger);
}

.brun__lead {
  margin: 0;
  font-size: 12px;
  color: var(--text-muted);
}

.brun__lead strong {
  color: var(--text);
}

.brun__empty {
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 8px 0;
  padding: 22px 16px;
  border: 1px dashed var(--border);
  border-radius: var(--radius-card);
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.6;
}

.brun__empty-icon {
  flex-shrink: 0;
  color: var(--success);
}

/* 列表容器 */
.brun__list {
  display: flex;
  flex-direction: column;
  gap: 5px;
  overflow-y: auto;
  min-height: 0;
  padding: 2px;
  scrollbar-width: thin;
}

/* 将查询：主视图，行卡片醒目 */
.brun__row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 12px;
  border-radius: var(--radius-control);
  font-size: 13px;
}

.brun__row--plan {
  background: color-mix(in srgb, var(--brand) 6%, var(--surface));
  border: 1px solid color-mix(in srgb, var(--brand) 16%, var(--border));
}

.brun__row--plan .brun__name {
  font-weight: 600;
}

.brun__reason {
  margin-left: auto;
  flex-shrink: 0;
  max-width: 50%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  padding: 2px 8px;
  border-radius: var(--radius-pill);
  background: color-mix(in srgb, var(--brand) 10%, transparent);
  color: var(--brand);
  font-size: 11px;
}

/* 无需查询：收起的安静区块 */
.brun__skip {
  display: flex;
  flex-direction: column;
  gap: 0;
  border-top: 1px solid var(--border);
  padding-top: 8px;
  margin-top: 2px;
}

.brun__skip-toggle {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  align-self: flex-start;
  padding: 4px 0;
  border: none;
  background: none;
  color: var(--text-muted);
  font-size: 12px;
  cursor: pointer;
}

.brun__skip-toggle:hover {
  color: var(--text-regular);
}

.brun__skip-toggle :deep(.lp-svg-icon) {
  transition: transform 0.15s ease;
}

.brun__skip-toggle :deep(.lp-svg-icon.is-collapsed) {
  transform: rotate(-90deg);
}

.brun__skip-body {
  display: flex;
  flex-direction: column;
  gap: 3px;
  margin-top: 6px;
  /* 跳过项很多时限制高度并出纵向滚动条，避免把弹窗撑出屏幕 */
  max-height: 30vh;
  overflow-y: auto;
  min-height: 0;
  scrollbar-width: thin;
}

.brun__row--skip {
  padding: 5px 10px;
  color: var(--text-muted);
  font-size: 12px;
  background: var(--surface-sunken);
}

.brun__row--skip .brun__name {
  font-weight: 400;
}

.brun__row--skip .brun__why {
  margin-left: auto;
  flex-shrink: 0;
  font-size: 11px;
  color: var(--text-muted);
  opacity: 0.85;
}

/* 执行/结果视图 */
.brun__head {
  display: flex;
  align-items: center;
  gap: 10px;
  min-height: 28px;
  font-size: 13px;
}

.brun__head-text {
  font-weight: 600;
  color: var(--text);
}

.brun__head-text--error {
  color: var(--danger);
}

.brun__spacer {
  flex: 1;
}

.brun__counters {
  display: inline-flex;
  gap: 6px;
}

.brun__counter {
  padding: 2px 9px;
  border-radius: var(--radius-pill);
  background: var(--surface-sunken);
  color: var(--text-muted);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}

.brun__counter--ok {
  color: var(--success);
  background: color-mix(in srgb, var(--success) 10%, transparent);
}

.brun__counter--fail {
  color: var(--danger);
  background: color-mix(in srgb, var(--danger) 10%, transparent);
}

.brun__list--run .brun__row--result {
  gap: 8px;
  padding: 7px 10px;
  border: 1px solid var(--border);
  background: var(--surface);
}

.brun__row--running {
  border-color: color-mix(in srgb, var(--brand) 40%, var(--border));
  background: color-mix(in srgb, var(--brand) 6%, var(--surface));
}

.brun__row--failed {
  border-color: color-mix(in srgb, var(--danger) 30%, var(--border));
  background: color-mix(in srgb, var(--danger) 5%, var(--surface));
}

.brun__cell-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 16px;
  flex-shrink: 0;
}

.brun__cell-icon :deep(.lp-svg-icon) {
  display: inline-flex;
}

.brun__row--queried .brun__cell-icon {
  color: var(--success);
}

.brun__row--failed .brun__cell-icon {
  color: var(--danger);
}

.brun__dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  border: 1.5px solid var(--border);
}

.brun__row--result .brun__name {
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.brun__row--pending .brun__name {
  color: var(--text-muted);
}

.brun__detail {
  margin-left: auto;
  flex-shrink: 0;
  max-width: 52%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
  color: var(--text-muted);
}

.brun__detail--fail {
  color: var(--danger);
}
</style>
