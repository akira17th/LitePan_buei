<script setup lang="ts">
import { computed, onActivated, onMounted, onUnmounted, ref } from "vue";
import { getApiErrorMessage } from "@/api/client";
import {
  fetchBueiDoubanScoreItems,
  fetchBueiDoubanScoreState,
  fetchBueiDoubanScoreTasks,
  queryBueiDoubanScore,
  stopBueiDoubanScore,
  type BueiDoubanFilter,
  type BueiDoubanItemStats,
  type BueiDoubanScoreTask,
  type BueiDoubanSort,
  type BueiDoubanState,
  type BueiDoubanWallItem,
} from "@/api/bueiDoubanScore";
import AdminEmptyState from "@/components/admin/AdminEmptyState.vue";
import AppButton from "@/components/base/AppButton.vue";
import AppSelect from "@/components/base/AppSelect.vue";
import BusySpinner from "@/components/base/BusySpinner.vue";
import { useAdminPageLoading } from "@/composables/useAdminLoadingBar";
import { confirm } from "@/composables/useConfirm";
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

async function runTask() {
  if (!activeTaskId.value || running.value) return;
  try {
    const result = await queryBueiDoubanScore({ strm_task_id: activeTaskId.value });
    state.value = state.value ? { ...state.value, progress: result } : state.value;
    pendingRefresh.value = true;
    await loadState();
    schedulePoll();
  } catch (e) {
    toast.error(getApiErrorMessage(e, "启动查询失败"));
  }
}

async function runAll() {
  if (running.value) return;
  if (!tasks.value.length) return;
  const ok = await confirm({
    title: "查询全部任务",
    message: "按规则查询所有 STRM 任务的豆瓣评分：没有评分、未完结、完结不满 3 个月的作品会重新查询，其余跳过。",
    confirmText: "开始查询",
    danger: false,
  }).catch(() => false);
  if (!ok) return;
  try {
    const result = await queryBueiDoubanScore({ all_tasks: true });
    state.value = state.value ? { ...state.value, progress: result } : state.value;
    pendingRefresh.value = true;
    await loadState();
    schedulePoll();
  } catch (e) {
    toast.error(getApiErrorMessage(e, "启动查询失败"));
  }
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
  await Promise.all([loadState(), loadTasks()]);
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
        <AppButton v-if="running" type="button" variant="secondary" @click="stopRun">停止查询</AppButton>
        <template v-else>
          <AppButton type="button" variant="secondary" :disabled="!activeTaskId" @click="runTask">
            查询本任务
          </AppButton>
          <AppButton type="button" variant="primary" @click="runAll">全部任务</AppButton>
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
