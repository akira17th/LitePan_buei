<script setup lang="ts">
import { computed, onActivated, onMounted, onUnmounted, ref } from "vue";
import { getApiErrorMessage } from "@/api/client";
import {
  fetchBueiDoubanLibraries,
  fetchBueiDoubanScoreItems,
  fetchBueiDoubanScoreState,
  fetchBueiDoubanScoreTasks,
  queryBueiDoubanScore,
  saveBueiDoubanLibrary,
  stopBueiDoubanScore,
  type BueiDoubanFilter,
  type BueiDoubanItemStats,
  type BueiDoubanLibraryOption,
  type BueiDoubanRunLog,
  type BueiDoubanScoreTask,
  type BueiDoubanSort,
  type BueiDoubanState,
  type BueiDoubanWallItem,
} from "@/api/bueiDoubanScore";
import AdminEmptyState from "@/components/admin/AdminEmptyState.vue";
import BueiDoubanRunModal from "@/components/buei/BueiDoubanRunModal.vue";
import AppButton from "@/components/base/AppButton.vue";
import AppModal from "@/components/base/AppModal.vue";
import AppSelect from "@/components/base/AppSelect.vue";
import BusySpinner from "@/components/base/BusySpinner.vue";
import { useAdminPageLoading } from "@/composables/useAdminLoadingBar";
import { toast } from "@/composables/useToast";

const PAGE_LIMIT = 60;
const TASK_STORAGE_KEY = "litepan:buei-douban-score:task";
const POLL_INTERVAL = 3000;

const tasks = ref<BueiDoubanScoreTask[]>([]);
const activeTaskId = ref<number | null>(null);
const items = ref<BueiDoubanWallItem[]>([]);
const stats = ref<BueiDoubanItemStats>({ total: 0, scored: 0, unscored: 0, pending: 0, tv: 0, movie: 0 });
const totalMatched = ref(0);
const hasMore = ref(false);
const loading = ref(false);
const booted = ref(false);
const loadingMore = ref(false);
const state = ref<BueiDoubanState | null>(null);
const busyItemId = ref("");
// 有查询在进行中：等它真正结束后再刷新列表/清掉卡片上的「查询中」。
const pendingRefresh = ref(false);
const filter = ref<BueiDoubanFilter>("all");
const sortKey = ref<BueiDoubanSort>("default");
const keyword = ref("");
// buei:douban-library —— 任务 ↔ Emby 媒体库关联（评分联动刷库）。
const embyLibraries = ref<BueiDoubanLibraryOption[]>([]);
const taskBindings = ref<Record<string, BueiDoubanLibraryOption>>({});
const librarySaving = ref(false);
// buei:run-log —— 每日自动查询日志弹窗。
const showRunLogModal = ref(false);
const runLog = computed<BueiDoubanRunLog | null>(() => state.value?.run_log ?? null);
let pollTimer: ReturnType<typeof setTimeout> | null = null;
let loadSeq = 0;
let searchTimer: ReturnType<typeof setTimeout> | null = null;

useAdminPageLoading("tools", loading);

const sortOptions = [
  { value: "default", label: "评分从高到低" },
  { value: "rating_asc", label: "评分从低到高" },
  { value: "title_asc", label: "名称" },
  { value: "year_desc", label: "年份" },
  { value: "added_desc", label: "添加时间" },
];

const activeTask = computed(() => tasks.value.find((t) => t.id === activeTaskId.value) ?? null);
const progress = computed(() => state.value?.progress ?? null);
const running = computed(() => Boolean(progress.value?.running));
const scheduleText = computed(() => {
  const runAt = state.value?.run_at || "08:00";
  return `每天 ${runAt} 自动查询`;
});
const lastRunText = computed(() => {
  const last = state.value?.last_run_at;
  if (!last) return "还没有查询记录";
  return `上次 ${formatDateTime(last)}`;
});
const progressText = computed(() => {
  const p = progress.value;
  if (!p || !p.running) return "";
  const parts = [`${p.done}/${p.total}`];
  if (p.queried) parts.push(`已查 ${p.queried}`);
  if (p.skipped) parts.push(`跳过 ${p.skipped}`);
  if (p.failed) parts.push(`失败 ${p.failed}`);
  return parts.join(" · ");
});

const emptyTitle = computed(() => {
  if (!tasks.value.length) return "还没有 STRM 任务";
  if (!stats.value.total) return "这部任务还没有索引项目";
  return "没有符合筛选的条目";
});
const emptyDescription = computed(() => {
  if (!tasks.value.length) return "先在「STRM 刮削」里跑一次任务，索引生成后这里就能按海报墙展示作品。";
  if (!stats.value.total) return "这个 STRM 任务的索引是空的，先在「STRM 刮削」里刮一次再回来查评分。";
  return "试试切换筛选或清空搜索。";
});

const filterChips = computed(() => [
  { key: "all" as BueiDoubanFilter, label: "全部", count: stats.value.total },
  { key: "unscored" as BueiDoubanFilter, label: "未评分", count: stats.value.unscored },
  { key: "pending" as BueiDoubanFilter, label: "待更新", count: stats.value.pending },
  { key: "scored" as BueiDoubanFilter, label: "已评分", count: stats.value.scored },
]);

function formatDuration(ms: number) {
  if (!ms || ms < 0) return "—";
  const totalSec = Math.round(ms / 1000);
  if (totalSec < 60) return `${totalSec} 秒`;
  const m = Math.floor(totalSec / 60);
  const s = totalSec % 60;
  return s ? `${m} 分 ${s} 秒` : `${m} 分钟`;
}

function formatDateTime(raw?: string) {
  if (!raw) return "—";
  const d = new Date(raw);
  if (Number.isNaN(d.getTime())) return "—";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function readSavedTaskID(): number | null {
  try {
    const raw = localStorage.getItem(TASK_STORAGE_KEY);
    const id = raw ? Number(raw) : NaN;
    return Number.isFinite(id) && id > 0 ? id : null;
  } catch {
    return null;
  }
}

function saveTaskID(id: number) {
  try {
    localStorage.setItem(TASK_STORAGE_KEY, String(id));
  } catch {}
}

function letterOf(title: string) {
  return (title || "?").trim().slice(0, 1);
}

function doubanURL(item: BueiDoubanWallItem) {
  const id = item.douban?.douban_id;
  return id ? `https://movie.douban.com/subject/${id}/` : "";
}

function doubanLinkTitle(item: BueiDoubanWallItem) {
  const name = item.douban?.douban_title;
  return name ? `在豆瓣打开：${name}` : "在豆瓣打开";
}

function scoreOf(item: BueiDoubanWallItem) {
  return item.douban?.rating ?? null;
}

function scoreText(item: BueiDoubanWallItem) {
  const rating = scoreOf(item);
  return rating == null ? "" : rating.toFixed(1);
}

function scoreTone(rating: number | null) {
  if (rating == null) return "none";
  if (rating >= 8) return "high";
  if (rating >= 7) return "mid";
  return "low";
}

function scoreTitle(item: BueiDoubanWallItem) {
  const douban = item.douban;
  if (!douban || douban.rating == null) return "";
  const parts = [`豆瓣 ${douban.rating.toFixed(1)}`];
  if (douban.votes) parts.push(`${douban.votes} 人评价`);
  if (douban.douban_title) parts.push(douban.douban_title);
  if (douban.checked_at) parts.push(`查询于 ${formatDateTime(douban.checked_at)}`);
  return parts.join(" · ");
}

function itemHint(item: BueiDoubanWallItem) {
  const douban = item.douban;
  if (!douban) return "未查询";
  if (douban.rating == null) {
    if (douban.status === "error") return "查询失败";
    if (douban.status === "not_found") return "豆瓣无此条目";
    return douban.message || "待查询";
  }
  return "";
}

function tvStateText(item: BueiDoubanWallItem) {
  if (item.media_type !== "tv") return "";
  if (item.tv_state === "ended") return "已完结";
  if (item.tv_state === "updating") return "追更中";
  return "";
}


async function loadState() {
  try {
    state.value = await fetchBueiDoubanScoreState();
  } catch (e) {
    // 状态失败不打扰用户，面板主内容仍可用。
  }
}

async function loadTasks() {
  try {
    const data = await fetchBueiDoubanScoreTasks();
    tasks.value = data.tasks ?? [];
  } catch (e) {
    toast.error(getApiErrorMessage(e, "读取 STRM 任务失败"));
    return;
  }
  if (!tasks.value.length) {
    activeTaskId.value = null;
    return;
  }
  const saved = readSavedTaskID();
  if (activeTaskId.value && tasks.value.some((t) => t.id === activeTaskId.value)) return;
  activeTaskId.value = saved && tasks.value.some((t) => t.id === saved) ? saved : tasks.value[0].id;
}

async function loadItems(reset: boolean) {
  const taskId = activeTaskId.value;
  if (!taskId) {
    items.value = [];
    totalMatched.value = 0;
    hasMore.value = false;
    return;
  }
  const seq = ++loadSeq;
  if (reset) {
    loading.value = true;
  } else {
    loadingMore.value = true;
  }
  try {
    const data = await fetchBueiDoubanScoreItems(taskId, {
      offset: reset ? 0 : items.value.length,
      limit: PAGE_LIMIT,
      keyword: keyword.value.trim(),
      filter: filter.value,
      sort: sortKey.value,
    });
    if (seq !== loadSeq) return;
    items.value = reset ? data.items : [...items.value, ...data.items];
    stats.value = data.stats;
    totalMatched.value = data.total;
    hasMore.value = data.has_more;
  } catch (e) {
    if (seq !== loadSeq) return;
    toast.error(getApiErrorMessage(e, "读取海报墙失败"));
  } finally {
    if (seq === loadSeq) {
      loading.value = false;
      loadingMore.value = false;
    }
  }
}

async function selectTask(task: BueiDoubanScoreTask) {
  if (activeTaskId.value === task.id) return;
  activeTaskId.value = task.id;
  saveTaskID(task.id);
  keyword.value = "";
  await loadItems(true);
}

function setFilter(next: BueiDoubanFilter) {
  if (filter.value === next) return;
  filter.value = next;
  void loadItems(true);
}

function onSortChange(value: string) {
  sortKey.value = value as BueiDoubanSort;
  void loadItems(true);
}

// buei:douban-library —— 当前任务的关联媒体库选项（未关联时显示占位）。
const libraryOptions = computed(() => [
  { value: "", label: embyLibraries.value.length ? "未关联媒体库" : "未配置 Emby" },
  ...embyLibraries.value.map((lib) => ({ value: lib.library_id, label: lib.library_name })),
]);
const activeBinding = computed(() => {
  if (!activeTaskId.value) return null;
  return taskBindings.value[String(activeTaskId.value)] ?? null;
});
const libraryPickerValue = computed(() => activeBinding.value?.library_id ?? "");

async function loadLibraries() {
  try {
    const data = await fetchBueiDoubanLibraries();
    embyLibraries.value = data.libraries ?? [];
    taskBindings.value = data.bindings ?? {};
  } catch {
    // 读取失败不打扰：选择器回落为「未关联」。
  }
}

async function onLibraryChange(value: string) {
  const taskId = activeTaskId.value;
  if (!taskId || librarySaving.value) return;
  const name = embyLibraries.value.find((lib) => lib.library_id === value)?.library_name ?? "";
  librarySaving.value = true;
  try {
    await saveBueiDoubanLibrary(taskId, value, name);
    const next = { ...taskBindings.value };
    if (value) {
      next[String(taskId)] = { library_id: value, library_name: name };
    } else {
      delete next[String(taskId)];
    }
    taskBindings.value = next;
  } catch (e) {
    toast.error(getApiErrorMessage(e, "保存媒体库关联失败"));
  } finally {
    librarySaving.value = false;
  }
}

function onKeywordInput() {
  if (searchTimer) clearTimeout(searchTimer);
  searchTimer = setTimeout(() => {
    void loadItems(true);
  }, 300);
}

function schedulePoll() {
  if (pollTimer) clearTimeout(pollTimer);
  pollTimer = setTimeout(async () => {
    pollTimer = null;
    await loadState();
    if (running.value) {
      schedulePoll();
      return;
    }
    if (!pendingRefresh.value) return;
    // 查询真的结束了：统一刷新任务计数与海报墙，并结束卡片上的「查询中」。
    pendingRefresh.value = false;
    busyItemId.value = "";
    await Promise.all([loadTasks(), loadItems(true)]);
    toast.success("豆瓣评分查询完成");
  }, POLL_INTERVAL);
}

// 立即查询：打开弹窗展示「将查询 / 无需查询」清单，确认后才执行。
const showRunModal = ref(false);

function openRunModal() {
  if (!activeTaskId.value || running.value) return;
  showRunModal.value = true;
}

// 查询进行中关掉弹窗（后台运行）：面板接管轮询，结束后刷新列表。
function closeRunModal() {
  showRunModal.value = false;
  void (async () => {
    await loadState();
    if (running.value) {
      pendingRefresh.value = true;
      schedulePoll();
    }
  })();
}

// 弹窗里的查询结束后：刷新任务计数与海报墙，并同步面板顶部状态。
async function onRunFinished() {
  pendingRefresh.value = false;
  busyItemId.value = "";
  await Promise.all([loadState(), loadTasks(), loadItems(true)]);
  toast.success("豆瓣评分查询结束");
}

async function refreshItem(item: BueiDoubanWallItem) {
  if (running.value || busyItemId.value) return;
  busyItemId.value = item.id;
  try {
    await queryBueiDoubanScore({ strm_task_id: item.strm_task_id, item_ids: [item.id], force: true });
    pendingRefresh.value = true;
    await loadState();
    schedulePoll();
  } catch (e) {
    busyItemId.value = "";
    toast.error(getApiErrorMessage(e, "查询失败"));
  }
}

async function stopRun() {
  try {
    state.value = await stopBueiDoubanScore();
    pendingRefresh.value = true;
    schedulePoll();
  } catch (e) {
    toast.error(getApiErrorMessage(e, "停止查询失败"));
  }
}

async function boot() {
  if (booted.value) return;
  booted.value = true;
  loading.value = true;
  await Promise.all([loadState(), loadTasks(), loadLibraries()]);
  await loadItems(true);
  if (running.value) {
    pendingRefresh.value = true;
    schedulePoll();
  }
}

onMounted(boot);
onActivated(boot);
onUnmounted(() => {
  if (pollTimer) clearTimeout(pollTimer);
  if (searchTimer) clearTimeout(searchTimer);
});
</script>

<template>
  <div class="douban">
    <header class="douban__head">
      <div class="douban__headline">
        <span class="douban__schedule">{{ scheduleText }}</span>
        <span class="douban__sep" aria-hidden="true">·</span>
        <span class="douban__muted">{{ lastRunText }}</span>
        <template v-if="running">
          <span class="douban__sep" aria-hidden="true">·</span>
          <span class="douban__running">
            <span class="douban__pulse" aria-hidden="true" />
            {{ progressText }}
          </span>
        </template>
      </div>
      <div class="douban__actions">
        <!-- #region buei:run-log —— 查看最近一次每日自动查询日志 -->
        <AppButton
          v-if="runLog"
          type="button"
          variant="ghost"
          @click="showRunLogModal = true"
        >
          自动日志
        </AppButton>
        <!-- #endregion buei:run-log -->
        <AppButton v-if="running" type="button" variant="secondary" @click="stopRun">停止查询</AppButton>
        <template v-else>
          <AppButton type="button" variant="primary" :disabled="!activeTaskId" @click="openRunModal">
            立即查询
          </AppButton>
        </template>
      </div>
    </header>

    <div class="douban__body">
      <aside class="douban__rail" aria-label="STRM 任务">
        <div class="douban__rail-title">STRM 任务</div>
        <div v-if="!tasks.length" class="douban__rail-empty">还没有 STRM 任务</div>
        <button
          v-for="task in tasks"
          :key="task.id"
          type="button"
          class="douban__task"
          :class="{ 'douban__task--active': task.id === activeTaskId }"
          @click="selectTask(task)"
        >
          <span class="douban__task-name" :title="task.name">{{ task.name }}</span>
          <span v-if="task.scored" class="douban__task-badge">{{ task.scored }}</span>
        </button>
      </aside>

      <section class="douban__main">
        <div class="douban__toolbar">
          <div class="douban__chips">
            <button
              v-for="chip in filterChips"
              :key="chip.key"
              type="button"
              class="douban__chip"
              :class="{ 'douban__chip--active': filter === chip.key }"
              @click="setFilter(chip.key)"
            >
              {{ chip.label }}<span class="douban__chip-count">{{ chip.count }}</span>
            </button>
          </div>
          <div class="douban__toolbar-right">
            <!-- #region buei:douban-library —— 当前任务关联的 Emby 媒体库（评分联动刷库） -->
            <AppSelect
              :model-value="libraryPickerValue"
              :options="libraryOptions"
              class="douban__library"
              :disabled="!activeTaskId || librarySaving"
              :title="activeBinding ? `查询到评分后自动刷新媒体库：${activeBinding.library_name}` : '选择查询到评分后要刷新的 Emby 媒体库'"
              @update:model-value="onLibraryChange(String($event))"
            />
            <!-- #endregion buei:douban-library -->
            <input
              v-model="keyword"
              class="douban__search"
              type="search"
              placeholder="搜索片名"
              @input="onKeywordInput"
            />
            <AppSelect
              :model-value="sortKey"
              :options="sortOptions"
              class="douban__sort"
              @update:model-value="onSortChange(String($event))"
            />
          </div>
        </div>

        <AdminEmptyState
          v-if="!items.length && !loading"
          icon="image"
          :title="emptyTitle"
          :description="emptyDescription"
        />

        <div v-else class="douban__grid">
          <article
            v-for="item in items"
            :key="`${item.strm_task_id}:${item.id}`"
            class="dcard"
            :class="{ 'dcard--busy': busyItemId === item.id }"
          >
            <div class="dcard__poster">
              <img v-if="item.poster_url" :src="item.poster_url" :alt="item.title" loading="lazy" />
              <div v-else class="dcard__ph">{{ letterOf(item.title) }}</div>
              <span
                v-if="scoreOf(item) != null"
                class="dcard__score"
                :class="`dcard__score--${scoreTone(scoreOf(item))}`"
                :title="scoreTitle(item)"
              >
                {{ scoreText(item) }}
              </span>
              <span v-else class="dcard__score dcard__score--none" :title="itemHint(item)">
                {{ item.douban?.status === "not_found" ? "—" : "待查" }}
              </span>
              <!-- 类型 · 年份 · 完结状态（参照 Emby 媒体库卡片） -->
              <span class="dcard__pill">
                <span class="dcard__kind">{{ item.media_type === "tv" ? "剧集" : "电影" }}</span>
                <span v-if="item.year" class="dcard__year">{{ item.year }}</span>
                <span
                  v-if="tvStateText(item)"
                  class="dcard__state"
                  :class="{ 'is-ended': item.tv_state === 'ended' }"
                >{{ tvStateText(item) }}</span>
              </span>
              <a
                v-if="doubanURL(item)"
                class="dcard__douban"
                :href="doubanURL(item)"
                target="_blank"
                rel="noopener noreferrer"
                :title="doubanLinkTitle(item)"
                aria-label="在豆瓣打开"
                @click.stop
              >
                豆瓣<span class="dcard__douban-arrow" aria-hidden="true">↗</span>
              </a>
              <!-- 查询中：整张海报盖一层 loading，直到后台真的查完 -->
              <div v-if="busyItemId === item.id" class="dcard__busy" role="status" aria-live="polite">
                <BusySpinner :size="26" />
                <span class="dcard__busy-text">查询中</span>
              </div>
              <div v-else class="dcard__actions">
                <button
                  type="button"
                  class="dcard__act"
                  :disabled="running"
                  @click.stop="refreshItem(item)"
                >
                  {{ scoreOf(item) == null ? "查询" : "重查" }}
                </button>
              </div>
            </div>
            <div class="dcard__meta">
              <div class="dcard__title" :title="item.title">{{ item.title }}</div>
              <div class="dcard__sub">
                <span v-if="itemHint(item)" class="dcard__hint" :title="item.douban?.message || ''">
                  {{ itemHint(item) }}
                </span>
              </div>
            </div>
          </article>
        </div>

        <div v-if="items.length" class="douban__foot">
          <span class="douban__foot-count">已加载 {{ items.length }} / {{ totalMatched }}</span>
          <AppButton
            v-if="hasMore"
            type="button"
            variant="secondary"
            :disabled="loadingMore"
            @click="loadItems(false)"
          >
            {{ loadingMore ? "加载中…" : "加载更多" }}
          </AppButton>
        </div>
      </section>
    </div>

    <!-- #region buei:douban-score —— 立即查询弹窗：先展示将查/跳过清单，再执行并逐项显示结果 -->
    <BueiDoubanRunModal
      v-if="showRunModal && activeTaskId"
      :strm-task-id="activeTaskId"
      :task-name="activeTask?.name ?? ''"
      @close="closeRunModal"
      @finished="onRunFinished"
    />
    <!-- #endregion buei:douban-score -->

    <!-- #region buei:run-log —— 每日自动查询日志（只保留最近一次） -->
    <AppModal :open="showRunLogModal" size="md" title="自动查询日志" @close="showRunLogModal = false">
      <div v-if="runLog" class="rlog">
        <div class="rlog__meta">
          <span>{{ formatDateTime(runLog.finished_at) }} 结束</span>
          <span class="rlog__sep">·</span>
          <span>耗时 {{ formatDuration(runLog.duration_ms) }}</span>
        </div>
        <div class="rlog__summary">
          <span class="rlog__pill">待查 {{ runLog.total }}</span>
          <span class="rlog__pill rlog__pill--ok">成功 {{ runLog.success }}</span>
          <span class="rlog__pill" :class="{ 'rlog__pill--fail': runLog.failed > 0 }">失败 {{ runLog.failed }}</span>
        </div>
        <div v-if="!runLog.tasks.length" class="rlog__empty">这次没有需要查询的作品。</div>
        <div v-else class="rlog__list">
          <div v-for="task in runLog.tasks" :key="task.task_id" class="rlog__task">
            <div class="rlog__task-head">
              <span class="rlog__task-name" :title="task.task_name">{{ task.task_name }}</span>
              <span class="rlog__task-stats">
                待查 {{ task.pending }} · 成功 {{ task.success }}<template v-if="task.failed"> · 失败 {{ task.failed }}</template>
              </span>
            </div>
            <div v-if="task.reasons?.length" class="rlog__reasons">
              <span v-for="reason in task.reasons" :key="reason" class="rlog__reason">{{ reason }}</span>
            </div>
          </div>
        </div>
      </div>
      <div v-else class="rlog rlog--empty">还没有自动查询记录。</div>
      <template #footer>
        <AppButton type="button" variant="ghost" @click="showRunLogModal = false">关闭</AppButton>
      </template>
    </AppModal>
    <!-- #endregion buei:run-log -->
  </div>
</template>

<style scoped>
.douban {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.douban__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

.douban__headline {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
  font-size: 12px;
  color: var(--text-muted);
}

.douban__schedule {
  font-weight: 600;
  color: var(--text-regular);
}

.douban__sep {
  opacity: 0.5;
}

.douban__running {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--accent-text);
}

.douban__pulse {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--accent-text);
  animation: douban-pulse 1.4s ease-in-out infinite;
}

@keyframes douban-pulse {
  0%,
  100% {
    opacity: 0.35;
    transform: scale(0.85);
  }
  50% {
    opacity: 1;
    transform: scale(1);
  }
}

.douban__actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.douban__body {
  display: grid;
  grid-template-columns: 190px minmax(0, 1fr);
  gap: 16px;
  align-items: start;
}

.douban__rail {
  position: sticky;
  top: 56px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  max-height: calc(100vh - 140px);
  overflow-y: auto;
  padding: 10px 8px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
}

.douban__rail-title {
  padding: 2px 8px 8px;
  font-size: 12px;
  font-weight: 600;
  color: var(--text-muted);
}

.douban__rail-empty {
  padding: 6px 8px;
  font-size: 12px;
  color: var(--text-muted);
}

.douban__task {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 8px 10px;
  border: 0;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-regular);
  font-size: 13px;
  text-align: left;
  cursor: pointer;
  transition: background var(--transition), color var(--transition);
}

.douban__task:hover {
  background: var(--surface-hover);
}

.douban__task--active {
  background: var(--tab-active-bg);
  color: var(--accent-text);
  font-weight: 600;
}

.douban__task-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.douban__task-badge {
  flex-shrink: 0;
  min-width: 20px;
  padding: 0 6px;
  border-radius: var(--radius-pill);
  background: rgba(0, 181, 29, 0.1);
  color: #0a8f1e;
  font-size: 11px;
  font-weight: 600;
  line-height: 18px;
  text-align: center;
}

.douban__main {
  display: flex;
  flex-direction: column;
  gap: 14px;
  min-width: 0;
}

.douban__toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
}

.douban__chips {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}

.douban__chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 5px 10px;
  border: 1px solid transparent;
  border-radius: var(--radius-pill);
  background: var(--surface);
  color: var(--text-regular);
  font-size: 12px;
  cursor: pointer;
  transition: background var(--transition), color var(--transition), border-color var(--transition);
}

.douban__chip:hover {
  background: var(--surface-hover);
}

.douban__chip--active {
  background: var(--tab-active-bg);
  border-color: var(--tab-active-border);
  color: var(--accent-text);
  font-weight: 600;
}

.douban__chip-count {
  font-size: 11px;
  opacity: 0.7;
}

.douban__toolbar-right {
  display: flex;
  align-items: center;
  gap: 8px;
}

.douban__search {
  width: 160px;
  height: 30px;
  padding: 0 10px;
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: var(--surface);
  color: var(--text);
  font-size: 13px;
}

.douban__search:focus {
  outline: none;
  border-color: var(--accent-text);
}

.douban__sort {
  width: 150px;
}

/* buei:douban-library —— 媒体库选择器与搜索框同高 */
.douban__library {
  width: 150px;
}

.douban__library :deep(.select__trigger) {
  height: 30px;
  padding: 0 10px;
  border-radius: var(--radius-control);
  font-size: 13px;
}

/* buei:run-log —— 自动查询日志弹窗 */
.rlog {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: 120px;
}

.rlog--empty {
  align-items: center;
  justify-content: center;
  color: var(--text-muted);
  font-size: 12px;
}

.rlog__meta {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--text-muted);
  font-size: 12px;
}

.rlog__sep {
  opacity: 0.5;
}

.rlog__summary {
  display: flex;
  gap: 6px;
}

.rlog__pill {
  padding: 2px 10px;
  border-radius: var(--radius-pill);
  background: var(--surface-sunken);
  color: var(--text-muted);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}

.rlog__pill--ok {
  color: var(--success);
  background: color-mix(in srgb, var(--success) 10%, transparent);
}

.rlog__pill--fail {
  color: var(--danger);
  background: color-mix(in srgb, var(--danger) 10%, transparent);
}

.rlog__empty {
  padding: 18px 0;
  text-align: center;
  color: var(--text-muted);
  font-size: 12px;
}

.rlog__list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-height: 46vh;
  overflow-y: auto;
  min-height: 0;
}

.rlog__task {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 9px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: var(--surface);
}

.rlog__task-head {
  display: flex;
  align-items: center;
  gap: 10px;
}

.rlog__task-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 13px;
  font-weight: 600;
  color: var(--text);
}

.rlog__task-stats {
  flex-shrink: 0;
  color: var(--text-muted);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}

.rlog__reasons {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.rlog__reason {
  padding: 2px 8px;
  border-radius: var(--radius-pill);
  background: color-mix(in srgb, var(--danger) 8%, transparent);
  color: var(--danger);
  font-size: 11px;
}

/* 下拉默认内边距比旁边的搜索框/筛选项大，压到同一行高（30px）才不显突兀 */
.douban__sort :deep(.select__trigger) {
  height: 30px;
  padding: 0 10px;
  border-radius: var(--radius-control);
  font-size: 13px;
}

.douban__grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(148px, 1fr));
  gap: 14px;
}

.dcard {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
}



.dcard__poster {
  position: relative;
  aspect-ratio: 2 / 3;
  overflow: hidden;
  border-radius: var(--radius-md);
  background: var(--surface-sunken);
  box-shadow: var(--shadow-card);
}

.dcard__poster img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.dcard__ph {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  height: 100%;
  color: var(--text-muted);
  font-size: 30px;
  font-weight: 600;
}

.dcard__score {
  position: absolute;
  top: 8px;
  left: 8px;
  min-width: 34px;
  padding: 2px 7px;
  border-radius: var(--radius-pill);
  background: rgba(0, 0, 0, 0.62);
  color: #fff;
  font-size: 12px;
  font-weight: 700;
  line-height: 18px;
  text-align: center;
  backdrop-filter: blur(4px);
}

.dcard__score--high {
  background: rgba(0, 181, 29, 0.92);
}

.dcard__score--mid {
  background: rgba(0, 150, 90, 0.9);
}

.dcard__score--low {
  background: rgba(120, 130, 145, 0.9);
}

.dcard__score--none {
  background: rgba(0, 0, 0, 0.45);
  font-size: 11px;
  font-weight: 600;
}

/* 类型 · 年份 · 完结状态：海报左下角深色胶囊，分隔点用 before 伪元素 */
.dcard__pill {
  position: absolute;
  left: 8px;
  bottom: 8px;
  z-index: 2;
  pointer-events: none;
  display: inline-flex;
  align-items: center;
  max-width: calc(100% - 16px);
  padding: 2px 8px;
  border-radius: var(--radius-xs);
  background: rgba(2, 6, 23, 0.62);
  box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.1);
  color: #fff;
  font-size: 11px;
  font-weight: 700;
  line-height: 1.5;
  white-space: nowrap;
  backdrop-filter: blur(6px);
}

.dcard__year,
.dcard__state {
  display: inline-flex;
  align-items: center;
  color: rgba(255, 255, 255, 0.76);
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.dcard__state {
  color: #ffd08a;
}

.dcard__state.is-ended {
  color: #9ce3c0;
}

.dcard__year::before,
.dcard__state::before {
  content: "";
  width: 3px;
  height: 3px;
  margin: 0 6px 0 5px;
  border-radius: 50%;
  background: currentColor;
  opacity: 0.85;
}

.dcard__actions {
  position: absolute;
  inset: 0;
  z-index: 2;
  /* 只让中间按钮可点，避免挡住底部的豆瓣入口 */
  pointer-events: none;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(2, 6, 23, 0.22);
  opacity: 0;
  transition: opacity var(--transition);
}

.dcard:hover .dcard__actions,
.dcard:focus-within .dcard__actions {
  opacity: 1;
}

.dcard__act {
  pointer-events: auto;
  padding: 4px 12px;
  border: 0;
  border-radius: var(--radius-pill);
  background: rgba(255, 255, 255, 0.92);
  color: var(--text);
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
}

.dcard__act:disabled {
  cursor: default;
  opacity: 0.7;
}

.dcard--busy {
  cursor: progress;
}

/* 整图 loading：盖住海报与角标，卡片信息仍可读 */
.dcard__busy {
  position: absolute;
  inset: 0;
  z-index: 4;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  background: rgba(15, 23, 42, 0.55);
  color: #fff;
  backdrop-filter: blur(1.5px);
}

.dcard__busy-text {
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.02em;
}

.dcard__meta {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.dcard__title {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text);
  font-size: 13px;
  font-weight: 600;
}

.dcard__sub {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  color: var(--text-muted);
  font-size: 11px;
}

.dcard__hint {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dcard__sub {
  min-height: 16px;
}

/* 豆瓣条目快捷入口：只在已匹配到条目时出现，海报右上角 */
.dcard__douban {
  position: absolute;
  right: 8px;
  top: 8px;
  z-index: 3;
  display: inline-flex;
  align-items: center;
  gap: 2px;
  padding: 2px 7px;
  border-radius: var(--radius-xs);
  background: rgba(2, 6, 23, 0.68);
  color: rgba(255, 255, 255, 0.82);
  font-size: 10px;
  font-weight: 750;
  letter-spacing: 0.02em;
  line-height: 1.5;
  text-decoration: none;
  backdrop-filter: blur(5px);
  transition: background var(--transition), color var(--transition);
}

.dcard__douban:hover {
  background: #00b51d;
  color: #fff;
}

.dcard__douban-arrow {
  font-size: 9px;
}

.douban__foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 6px 2px 16px;
}

.douban__foot-count {
  color: var(--text-muted);
  font-size: 12px;
}

@media (max-width: 900px) {
  .douban__body {
    grid-template-columns: 1fr;
  }

  .douban__rail {
    position: static;
    flex-direction: row;
    max-height: none;
    overflow-x: auto;
    padding: 8px;
  }

  .douban__rail-title {
    display: none;
  }

  .douban__task {
    width: auto;
    white-space: nowrap;
  }

  .douban__task-name {
    max-width: 120px;
  }

  .douban__search {
    width: 120px;
  }
}
</style>
