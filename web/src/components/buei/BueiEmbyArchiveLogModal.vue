<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { getApiErrorMessage } from "@/api/client";
import { fetchBueiEmbyArchiveLogs, type BueiEmbyArchiveRun } from "@/api/bueiEmbyArchive";
import AppButton from "@/components/base/AppButton.vue";
import AppModal from "@/components/base/AppModal.vue";
import BusySpinner from "@/components/base/BusySpinner.vue";
import SvgIcon from "@/components/icons/SvgIcon.vue";
import { formatTime } from "@/utils/format";

// buei 定制：自动归档执行日志弹窗（默认展示最新一次，执行中自动轮询）。

const props = withDefaults(
  defineProps<{
    open: boolean;
    libraryId: string;
    libraryName: string;
  }>(),
  {},
);

const emit = defineEmits<{ close: []; finished: [run: BueiEmbyArchiveRun] }>();

const loading = ref(false);
const errorMessage = ref("");
const runs = ref<BueiEmbyArchiveRun[]>([]);
const selectedRunId = ref(0);
const finishedRunID = ref(0);
let pollTimer: ReturnType<typeof setTimeout> | null = null;

const selectedRun = computed(() => runs.value.find((run) => run.id === selectedRunId.value) || runs.value[0] || null);
const running = computed(() => selectedRun.value?.status === "running");
const archivedItems = computed(() => selectedRun.value?.items.filter((item) => item.result === "archived") || []);
const failedItems = computed(() => selectedRun.value?.items.filter((item) => item.result === "failed") || []);

const statusLabels: Record<string, string> = {
  running: "执行中",
  success: "已完成",
  partial: "部分失败",
  failed: "执行失败",
};

const triggerLabels: Record<string, string> = {
  auto: "自动执行",
  manual: "手动执行",
};

function statusLabel(status?: string) {
  return status ? statusLabels[status] || status : "";
}

function resultLabel(result: string) {
  switch (result) {
    case "archived":
      return "已归档";
    case "failed":
      return "失败";
    case "skipped":
      return "已跳过";
    default:
      return result;
  }
}

function mediaTypeLabel(item: { media_type?: string }) {
  if (item.media_type === "tv") return "剧集";
  if (item.media_type === "movie") return "电影";
  return "";
}

function runDuration(run: BueiEmbyArchiveRun) {
  if (!run.started_at) return "";
  const start = new Date(run.started_at).getTime();
  const end = run.finished_at ? new Date(run.finished_at).getTime() : Date.now();
  if (!Number.isFinite(start) || !Number.isFinite(end) || end < start) return "";
  const seconds = Math.max(0, Math.round((end - start) / 1000));
  if (seconds < 60) return `${seconds} 秒`;
  return `${Math.floor(seconds / 60)} 分 ${seconds % 60} 秒`;
}

function clearPoll() {
  if (pollTimer) {
    clearTimeout(pollTimer);
    pollTimer = null;
  }
}

async function load(silent = false) {
  if (!props.libraryId) return;
  if (!silent) loading.value = true;
  errorMessage.value = "";
  try {
    const result = await fetchBueiEmbyArchiveLogs(props.libraryId, 10);
    const nextRuns = result.items || [];
    const keepId = selectedRunId.value;
    runs.value = nextRuns;
    if (!nextRuns.some((run) => run.id === keepId)) {
      selectedRunId.value = nextRuns[0]?.id || 0;
    }
    // 最新一次执行结束且有项目归档时通知外部刷新媒体库项目。
    const latest = nextRuns[0];
    if (latest && latest.status !== "running" && latest.id !== finishedRunID.value) {
      finishedRunID.value = latest.id;
      if (latest.archived > 0) emit("finished", latest);
    }
  } catch (error) {
    if (!silent) errorMessage.value = getApiErrorMessage(error, "读取执行日志失败");
  } finally {
    loading.value = false;
    schedulePoll();
  }
}

function schedulePoll() {
  clearPoll();
  if (!props.open || !running.value) return;
  pollTimer = setTimeout(() => {
    void load(true);
  }, 2000);
}

function reset() {
  clearPoll();
  runs.value = [];
  selectedRunId.value = 0;
  finishedRunID.value = 0;
  errorMessage.value = "";
  loading.value = false;
}

watch(
  () => [props.open, props.libraryId],
  ([open]) => {
    if (!open) {
      reset();
      return;
    }
    selectedRunId.value = runs.value[0]?.id || 0;
    void load();
  },
);

onBeforeUnmount(clearPoll);
</script>

<template>
  <AppModal :open="open" title="自动归档日志" size="md" @close="emit('close')">
    <div class="archive-log">
      <p class="archive-log__lead">
        归档媒体库 <strong>{{ libraryName || "当前归档媒体库" }}</strong> 的最近执行记录。
      </p>

      <div v-if="loading && !runs.length" class="archive-log__state">
        <BusySpinner variant="notch" :size="20" />正在读取日志…
      </div>

      <p v-else-if="!runs.length && !errorMessage" class="archive-log__empty">
        <SvgIcon name="file-lines" :size="20" />
        <span>还没有执行记录。保存配置后会按频率执行，也可以点“立即执行”。</span>
      </p>

      <template v-else>
        <div v-if="runs.length > 1" class="archive-log__history" role="tablist" aria-label="执行记录">
          <button
            v-for="run in runs.slice(0, 5)"
            :key="run.id"
            type="button"
            class="archive-log__history-item"
            :class="{ 'is-active': selectedRun?.id === run.id, [`is-${run.status}`]: true }"
            role="tab"
            :aria-selected="selectedRun?.id === run.id"
            @click="selectedRunId = run.id; schedulePoll()"
          >
            <i aria-hidden="true" />
            {{ formatTime(run.started_at) }}
          </button>
        </div>

        <div v-if="selectedRun" class="archive-log__run">
          <header class="archive-log__run-head">
            <div>
              <span class="archive-log__status" :class="`is-${selectedRun.status}`">{{ statusLabel(selectedRun.status) }}</span>
              <span class="archive-log__trigger">{{ triggerLabels[selectedRun.trigger] || selectedRun.trigger }}</span>
            </div>
            <span class="archive-log__time">{{ formatTime(selectedRun.started_at) }}<template v-if="runDuration(selectedRun)"> · {{ runDuration(selectedRun) }}</template></span>
          </header>

          <div class="archive-log__stats">
            <div><span>扫描</span><strong>{{ selectedRun.scanned }}</strong></div>
            <div><span>归档</span><strong class="is-archived">{{ selectedRun.archived }}</strong></div>
            <div><span>跳过</span><strong>{{ selectedRun.skipped }}</strong></div>
            <div><span>失败</span><strong :class="{ 'is-failed': selectedRun.failed > 0 }">{{ selectedRun.failed }}</strong></div>
          </div>

          <p v-if="selectedRun.message" class="archive-log__message">
            <SvgIcon name="circle-info" :size="13" />{{ selectedRun.message }}
          </p>

          <div v-if="archivedItems.length || failedItems.length" class="archive-log__items">
            <div v-for="item in [...archivedItems, ...failedItems]" :key="`${item.result}-${item.name}-${item.target_path || item.message || ''}`" class="archive-log__item">
              <span class="archive-log__item-result" :class="`is-${item.result}`">{{ resultLabel(item.result) }}</span>
              <div class="archive-log__item-copy">
                <strong>{{ item.name }}</strong>
                <small>
                  <template v-if="mediaTypeLabel(item)">{{ mediaTypeLabel(item) }} · </template>
                  <template v-if="item.source_library">{{ item.source_library }}<template v-if="item.source_task"> / {{ item.source_task }}</template></template>
                  <template v-if="item.message"> · {{ item.message }}</template>
                </small>
              </div>
            </div>
          </div>
          <p v-else-if="!running" class="archive-log__none">本次没有项目满足归档规则。</p>

          <p v-if="running" class="archive-log__running">
            <BusySpinner variant="notch" :size="13" />正在执行，页面会自动刷新…
          </p>
        </div>

        <p v-if="errorMessage" class="archive-log__error" role="alert">
          <SvgIcon name="circle-exclamation" :size="14" />{{ errorMessage }}
        </p>
      </template>
    </div>

    <template #footer>
      <div class="archive-log__footer">
        <button type="button" class="archive-log__refresh" :disabled="loading" @click="load()">
          <SvgIcon name="rotate" :size="13" />刷新
        </button>
        <AppButton variant="secondary" @click="emit('close')">关闭</AppButton>
      </div>
    </template>
  </AppModal>
</template>

<style scoped>
.archive-log { display: grid; gap: 14px; }
.archive-log__lead { margin: 0; color: var(--text-muted); font-size: 12px; }
.archive-log__lead strong { color: var(--text); }
.archive-log__state { display: flex; align-items: center; justify-content: center; gap: 8px; min-height: 120px; color: var(--text-muted); font-size: 12px; }
.archive-log__empty { display: flex; align-items: center; gap: 10px; margin: 0; padding: 22px 16px; border: 1px dashed var(--border); border-radius: var(--radius-card); color: var(--text-muted); font-size: 12px; line-height: 1.6; }
.archive-log__history { display: flex; gap: 6px; overflow-x: auto; padding-bottom: 2px; scrollbar-width: thin; }
.archive-log__history-item { display: inline-flex; align-items: center; gap: 6px; min-width: max-content; min-height: 28px; padding: 0 9px; border: 1px solid var(--border); border-radius: var(--radius-control); color: var(--text-muted); background: var(--surface); font-size: 10px; cursor: pointer; }
.archive-log__history-item i { width: 6px; height: 6px; border-radius: 50%; background: var(--text-muted); }
.archive-log__history-item.is-success i { background: var(--success); }
.archive-log__history-item.is-partial i { background: var(--warning); }
.archive-log__history-item.is-failed i { background: var(--danger); }
.archive-log__history-item.is-running i { background: var(--brand); }
.archive-log__history-item.is-active { border-color: color-mix(in srgb, var(--brand) 55%, var(--border)); color: var(--text); background: color-mix(in srgb, var(--brand) 8%, var(--surface)); }
.archive-log__run { display: grid; gap: 12px; padding: 14px; border: 1px solid var(--border); border-radius: var(--radius-card); background: var(--surface-sunken); }
.archive-log__run-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; }
.archive-log__run-head > div { display: flex; align-items: center; gap: 7px; }
.archive-log__status { display: inline-flex; align-items: center; min-height: 20px; padding: 0 8px; border-radius: var(--radius-pill); font-size: 10px; font-weight: 700; }
.archive-log__status.is-running { color: var(--brand); background: color-mix(in srgb, var(--brand) 12%, var(--surface)); }
.archive-log__status.is-success { color: var(--success); background: color-mix(in srgb, var(--success) 12%, var(--surface)); }
.archive-log__status.is-partial { color: var(--warning); background: color-mix(in srgb, var(--warning) 12%, var(--surface)); }
.archive-log__status.is-failed { color: var(--danger); background: color-mix(in srgb, var(--danger) 12%, var(--surface)); }
.archive-log__trigger, .archive-log__time { color: var(--text-muted); font-size: 10px; }
.archive-log__stats { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 7px; }
.archive-log__stats div { display: grid; gap: 2px; padding: 8px 10px; border-radius: var(--radius-control); background: var(--surface); }
.archive-log__stats span { color: var(--text-muted); font-size: 10px; }
.archive-log__stats strong { font-size: 15px; font-variant-numeric: tabular-nums; }
.archive-log__stats strong.is-archived { color: var(--success); }
.archive-log__stats strong.is-failed { color: var(--danger); }
.archive-log__message { display: flex; align-items: flex-start; gap: 6px; margin: 0; padding: 8px 10px; border-radius: var(--radius-control); background: color-mix(in srgb, var(--warning) 10%, var(--surface)); color: var(--warning); font-size: 11px; line-height: 1.5; }
.archive-log__items { display: grid; gap: 6px; max-height: 300px; overflow-y: auto; }
.archive-log__item { display: flex; align-items: flex-start; gap: 8px; padding: 7px 9px; border: 1px solid var(--border); border-radius: var(--radius-control); background: var(--surface); }
.archive-log__item-result { flex-shrink: 0; margin-top: 1px; padding: 1px 6px; border-radius: 5px; font-size: 9px; font-weight: 700; }
.archive-log__item-result.is-archived { color: var(--success); background: color-mix(in srgb, var(--success) 12%, transparent); }
.archive-log__item-result.is-failed { color: var(--danger); background: color-mix(in srgb, var(--danger) 12%, transparent); }
.archive-log__item-copy { display: grid; gap: 2px; min-width: 0; }
.archive-log__item-copy strong { overflow: hidden; font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.archive-log__item-copy small { color: var(--text-muted); font-size: 10px; line-height: 1.45; word-break: break-all; }
.archive-log__none { margin: 0; color: var(--text-muted); font-size: 11px; }
.archive-log__running { display: flex; align-items: center; gap: 6px; margin: 0; color: var(--brand); font-size: 11px; }
.archive-log__error { display: flex; align-items: center; gap: 6px; margin: 0; padding: 8px 10px; border: 1px solid color-mix(in srgb, var(--danger) 25%, var(--border)); border-radius: var(--radius-control); color: var(--danger); background: color-mix(in srgb, var(--danger) 7%, var(--surface)); font-size: 11px; }
.archive-log__footer { display: flex; align-items: center; justify-content: space-between; gap: 12px; width: 100%; }
.archive-log__refresh { display: inline-flex; align-items: center; gap: 6px; min-height: 30px; padding: 0 10px; border: 1px solid var(--border); border-radius: var(--radius-control); color: var(--text-muted); background: var(--surface); font-size: 11px; cursor: pointer; }
.archive-log__refresh:hover:not(:disabled) { border-color: var(--brand); color: var(--brand); }
.archive-log__refresh:disabled { opacity: .6; cursor: not-allowed; }
</style>
