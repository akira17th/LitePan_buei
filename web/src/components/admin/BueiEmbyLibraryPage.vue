<script setup lang="ts">
import { computed, onActivated, onBeforeUnmount, onMounted, ref } from "vue";
import { getApiErrorMessage } from "@/api/client";
import {
  fetchBueiEmbyArchiveRuleSummary,
  runBueiEmbyArchiveNow,
  type BueiEmbyArchiveRuleView,
  type BueiEmbyArchiveRun,
} from "@/api/bueiEmbyArchive";
import { fetchEmbyConfigs } from "@/api/emby";
import {
  archiveBueiEmbyLibraryItem,
  fetchBueiEmbyLibraryState,
  fetchBueiEmbyLibrarySyncState,
  restoreBueiEmbyLibraryItem,
  setBueiEmbyLibraryTVState,
  setBueiEmbyLibraryUserPlayed,
  syncBueiEmbyLibrary,
  type BueiEmbyLibrary,
  type BueiEmbyLibraryDetail,
  type BueiEmbyLibraryItem,
  type BueiEmbyLibrarySyncItem,
} from "@/api/bueiEmbyLibrary";
import { fetchStrmTasks, type StrmTask } from "@/api/strm";
import { fetchStrmScrapeItems, refreshStrmScrapeIndex, type StrmScrapeItem } from "@/api/strmScrape";
import { matchBueiEmbyLibraryStrmTasks } from "@/utils/bueiEmbyLibraryTasks";
import { formatTimeShort } from "@/utils/format";
import { confirm } from "@/composables/useConfirm";
import { toast } from "@/composables/useToast";
import AppDropdown from "@/components/base/AppDropdown.vue";
import AppEmptyState from "@/components/admin/AdminEmptyState.vue";
import AppButton from "@/components/base/AppButton.vue";
import BusySpinner from "@/components/base/BusySpinner.vue";
import BueiEmbyArchiveLogModal from "@/components/buei/BueiEmbyArchiveLogModal.vue";
import BueiEmbyArchiveRuleModal from "@/components/buei/BueiEmbyArchiveRuleModal.vue";
import BueiEmbyLibraryIcon from "@/components/buei/BueiEmbyLibraryIcon.vue";
import { bueiEmbyLibraryKind, bueiEmbyLibraryKindLabel, type BueiEmbyLibraryKind } from "@/components/buei/bueiEmbyLibraryIcons";
import SvgIcon from "@/components/icons/SvgIcon.vue";
import type { DropdownMenuItem } from "@/types/menu";

const libraries = ref<BueiEmbyLibrary[]>([]);
const items = ref<BueiEmbyLibraryItem[]>([]);
const matchedTasks = ref<StrmTask[]>([]);
const configId = ref("");
const selectedLibraryId = ref("");
const selectedTaskFilter = ref<number | "all">("all");
const taskFilterQuery = ref("");
const selectedItem = ref<BueiEmbyLibraryDetail | null>(null);
const loadingState = ref(true);
const loadingItems = ref(false);
const loadingDetail = ref(false);
const syncingEmby = ref(false);
const archivingItemID = ref("");
const errorMessage = ref("");
const menuOpenID = ref("");
const loaded = ref(false);
const hasConfig = ref(false);
const archiveMenuOpen = ref(false);
const archiveRunStarting = ref(false);
const archiveRuleOpen = ref(false);
const archiveLogOpen = ref(false);
const userPlayedPending = ref<Set<string>>(new Set());
const tvStatePending = ref<Set<string>>(new Set());
const archiveRule = ref<BueiEmbyArchiveRuleView | null>(null);
// 各媒体库最近一次「Emby 同步」成功时间；超过 12 小时会在进入媒体库时自动同步。
const syncTimes = ref<Record<string, string>>({});
const autoSyncedLibraryIDs = ref<Set<string>>(new Set());
const archiveRuleLoaded = ref(false);

const autoSyncIntervalMs = 12 * 60 * 60 * 1000;

const selectedSyncTime = computed(() => syncTimes.value[selectedLibraryId.value] || "");

const archiveSchedule = computed(() => {
  const rule = archiveRule.value?.rule;
  const sources = rule?.source_libraries?.length || 0;
  if (!rule || !sources) return null;
  const hours = rule.interval_hours || 1;
  return {
    interval: hours === 24 ? "每 24 小时" : hours === 6 ? "每 6 小时" : "每小时",
    next: archiveRule.value?.next_run_at || rule.next_run_at || "",
    sources,
    enabled: rule.enabled !== false,
  };
});

const selectedLibrary = computed(() => libraries.value.find((item) => item.id === selectedLibraryId.value) || null);
const isArchiveLibrary = computed(() => selectedLibrary.value?.name.includes("归档") || false);
const hasItems = computed(() => items.value.length > 0);
const visibleTaskList = computed(() => {
  const query = taskFilterQuery.value.trim().toLocaleLowerCase();
  const filtered = matchedTasks.value.filter((task) => !query || task.name.toLocaleLowerCase().includes(query));
  if (selectedTaskFilter.value !== "all" && !filtered.some((task) => (task.id || 0) === selectedTaskFilter.value)) {
    const active = matchedTasks.value.find((task) => (task.id || 0) === selectedTaskFilter.value);
    if (active) filtered.unshift(active);
  }
  return filtered;
});
const taskItemCounts = computed(() => {
  const counts = new Map<number, number>();
  for (const item of items.value) {
    if (item.strm_task_id) counts.set(item.strm_task_id, (counts.get(item.strm_task_id) || 0) + 1);
  }
  return counts;
});
const visibleItems = computed(() => {
  if (selectedTaskFilter.value === "all") return items.value;
  return items.value.filter((item) => item.strm_task_id === selectedTaskFilter.value);
});
const activeTask = computed(() => {
  if (selectedTaskFilter.value === "all") return null;
  return matchedTasks.value.find((task) => (task.id || 0) === selectedTaskFilter.value) || null;
});
const hasVisibleItems = computed(() => visibleItems.value.length > 0);
const isConfigured = computed(() => hasConfig.value);

const archiveMenu = computed<DropdownMenuItem[]>(() => [
  { key: "run", label: archiveRunStarting.value ? "正在启动…" : "立即执行", icon: "bolt", disabled: archiveRunStarting.value, type: "action" },
  { key: "config", label: "配置", icon: "sliders", type: "action" },
  { key: "logs", label: "日志", icon: "file-lines", type: "action" },
]);

const itemMenu = (itemID = ""): DropdownMenuItem[] => {
  const loading = archivingItemID.value === itemID;
  if (isArchiveLibrary.value) {
    return [{ key: "restore", label: loading ? "移回中…" : "移回原处", icon: "box-archive", disabled: loading, type: "action" }];
  }
  return [{ key: "archive", label: loading ? "归档中…" : "归档", icon: "box-archive", disabled: loading, type: "action" }];
};

function imageURL(path: string | undefined) {
  return path || "";
}

function libraryKind(library: BueiEmbyLibrary): BueiEmbyLibraryKind {
  return bueiEmbyLibraryKind(library.collection_type, library.name);
}

function libraryKindLabel(library: BueiEmbyLibrary): string {
  return bueiEmbyLibraryKindLabel(library.collection_type, library.name);
}

function taskID(task: StrmTask) {
  return task.id || 0;
}

function taskItemCount(task: StrmTask) {
  return taskItemCounts.value.get(taskID(task)) || 0;
}

function selectTaskFilter(task: number | "all") {
  selectedTaskFilter.value = task;
}

function tmdbURL(item: BueiEmbyLibraryItem) {
  const kind = item.media_type === "tv" || item.type === "Series" ? "tv" : "movie";
  return `https://www.themoviedb.org/${kind}/${encodeURIComponent(item.tmdb_id || "")}`;
}

function episodeLabel(item: BueiEmbyLibraryItem) {
  if (item.type !== "Series" || !item.total_episodes) return "";
  const values = item.users.map((user) => user.remaining_episodes).filter((value): value is number => value != null);
  if (!values.length) return "";
  // 取各用户剩余集数的最小值，表示所有用户中观看进度最高者还剩多少集。
  return `未看 ${Math.min(...values)} / ${item.total_episodes} 集`;
}

function isTVStatePending(itemID: string) {
  return tvStatePending.value.has(itemID);
}

async function toggleTVState(item: BueiEmbyLibraryItem) {
  if (tvStatePending.value.has(item.id)) return;
  const ended = item.tv_state !== "ended";
  tvStatePending.value = new Set([...tvStatePending.value, item.id]);
  try {
    const result = await setBueiEmbyLibraryTVState({ item_id: item.id, ended });
    item.tv_state = result.tv_state;
  } catch (error) {
    toast.error(getApiErrorMessage(error, "切换完结状态失败"));
  } finally {
    const pending = new Set(tvStatePending.value);
    pending.delete(item.id);
    tvStatePending.value = pending;
  }
}

function userPlayedKey(itemID: string, userID: string) {
  return `${itemID}::${userID}`;
}

function isUserPlayedPending(itemID: string, userID: string) {
  return userPlayedPending.value.has(userPlayedKey(itemID, userID));
}

async function toggleUserPlayed(item: BueiEmbyLibraryItem, user: BueiEmbyLibraryItem["users"][number]) {
  const key = userPlayedKey(item.id, user.user_id);
  if (userPlayedPending.value.has(key)) return;
  const nextPlayed = !user.played;
  userPlayedPending.value = new Set([...userPlayedPending.value, key]);
  try {
    const result = await setBueiEmbyLibraryUserPlayed({
      item_id: item.id,
      user_id: user.user_id,
      played: nextPlayed,
      config_id: configId.value || undefined,
    });
    const index = item.users.findIndex((current) => current.user_id === user.user_id);
    if (index >= 0) item.users[index] = { ...item.users[index], ...result.user };
  } catch (error) {
    toast.error(getApiErrorMessage(error, "更新观看状态失败"));
  } finally {
    const pending = new Set(userPlayedPending.value);
    pending.delete(key);
    userPlayedPending.value = pending;
  }
}

function userStateTitle(user: BueiEmbyLibraryItem["users"][number]) {
  return `${user.user_name}：${userStatusLabel(user)}（点击标记${user.played ? "未看" : "已看完"}）`;
}

function userStatusLabel(user: BueiEmbyLibraryItem["users"][number]) {
  if (user.total_episodes && user.watched_episodes != null) {
    return user.played ? "已看完" : `已看 ${user.watched_episodes} / ${user.total_episodes} 集`;
  }
  return user.played ? "已看" : "未看";
}

function formatRuntime(minutes?: number) {
  if (!minutes) return "";
  const hours = Math.floor(minutes / 60);
  const rest = minutes % 60;
  return hours ? `${hours} 小时 ${rest} 分钟` : `${rest} 分钟`;
}

function formatRating(rating?: number) {
  return rating && rating > 0 ? rating.toFixed(1) : "";
}

async function loadState(showLoading = true) {
  if (showLoading) loadingState.value = true;
  errorMessage.value = "";
  try {
    const state = await fetchBueiEmbyLibraryState();
    configId.value = state.config.id;
    hasConfig.value = true;
    libraries.value = state.libraries || [];
    syncTimes.value = state.syncs || {};
    if (!selectedLibraryId.value || !libraries.value.some((library) => library.id === selectedLibraryId.value)) {
      selectedLibraryId.value = libraries.value[0]?.id || "";
    }
    loaded.value = true;
    if (selectedLibraryId.value) {
      await loadItems(selectedLibraryId.value);
      void maybeAutoSync(selectedLibraryId.value);
    }
  } catch (error) {
    errorMessage.value = getApiErrorMessage(error, "读取 Emby 媒体库失败");
    configId.value = "";
    libraries.value = [];
    items.value = [];
    matchedTasks.value = [];
    try {
      const state = await fetchEmbyConfigs();
      hasConfig.value = (state.items || []).length > 0;
    } catch {
      hasConfig.value = false;
    }
  } finally {
    loadingState.value = false;
  }
}

function toLibraryItem(task: StrmTask, item: StrmScrapeItem, synced?: BueiEmbyLibrarySyncItem): BueiEmbyLibraryItem {
  const taskID = task.id || 0;
  const name = item.title || item.folder_name || item.strm_name?.replace(/\.strm$/i, "") || "未命名项目";
  return {
    id: `${taskID}:${item.id}`,
    name,
    type: item.media_type === "tv" ? "Series" : "Movie",
    production_year: item.year,
    poster_url: item.poster_url,
    total_episodes: synced?.total_episodes,
    users: synced?.users || [],
    strm_task_id: taskID,
    strm_task_name: task.name,
    strm_item_id: item.id,
    rel_dir: item.rel_dir,
    strm_name: item.strm_name,
    media_type: item.media_type,
    status: item.status,
    has_nfo: item.has_nfo,
    has_poster: item.has_poster,
    file_count: item.file_count,
    ep_local: item.ep_local,
    ep_tmdb: item.ep_tmdb,
    ep_scraped: item.ep_scraped,
    tv_state: item.tv_state,
    tmdb_id: item.tmdb_id,
  };
}

async function fetchAllTaskItems(task: StrmTask, refresh = false) {
  if (!task.id) return [] as BueiEmbyLibraryItem[];
  const out: BueiEmbyLibraryItem[] = [];
  const limit = 200;
  let offset = 0;
  let first = true;
  while (true) {
    const page = refresh && first
      ? await refreshStrmScrapeIndex(task.id, { offset, limit, sort: "title_asc" })
      : await fetchStrmScrapeItems(task.id, { offset, limit, sort: "title_asc" });
    out.push(...(page.items || []).map((item) => toLibraryItem(task, item)));
    if (!page.has_more || !page.items?.length) break;
    const next = offset + page.items.length;
    if (next <= offset) break;
    offset = next;
    first = false;
  }
  return out;
}

async function loadArchiveRule(libraryId: string) {
  const library = libraries.value.find((item) => item.id === libraryId);
  archiveRule.value = null;
  archiveRuleLoaded.value = false;
  if (!library || !library.name.includes("归档")) {
    archiveRuleLoaded.value = true;
    return;
  }
  try {
    archiveRule.value = await fetchBueiEmbyArchiveRuleSummary(libraryId);
  } catch {
    archiveRule.value = null;
  } finally {
    archiveRuleLoaded.value = true;
  }
}

async function loadItems(libraryId: string, refresh = false) {
  void loadArchiveRule(libraryId);
  if (!libraryId) {
    items.value = [];
    matchedTasks.value = [];
    return;
  }
  loadingItems.value = true;
  errorMessage.value = "";
  selectedTaskFilter.value = "all";
  taskFilterQuery.value = "";
  matchedTasks.value = [];
  try {
    const tasks = await fetchStrmTasks();
    const library = libraries.value.find((current) => current.id === libraryId);
    matchedTasks.value = matchBueiEmbyLibraryStrmTasks(tasks || [], library?.name || "");
    if (!matchedTasks.value.length) {
      items.value = [];
      return;
    }
    // 同步状态属于附加信息，与项目列表并行读取，读取失败时仍展示 STRM 项目本身。
    const syncStatePromise = fetchBueiEmbyLibrarySyncState(libraryId, configId.value, library?.name || "").catch(() => null);
    const allItems: BueiEmbyLibraryItem[] = [];
    const taskErrors: string[] = [];
    // 每个任务单独、并行读取：归档目录可能尚未创建（还没归档过），
    // 一个任务读取失败不能影响其它任务的项目展示。
    const batches = await Promise.all(
      matchedTasks.value.map(async (task) => {
        try {
          return { items: await fetchAllTaskItems(task, refresh), error: "" };
        } catch (error) {
          const message = getApiErrorMessage(error, `读取任务「${task.name}」失败`);
          // 归档目录尚未创建（还没归档过）时按“暂无项目”处理，不当作错误提示。
          return { items: [] as BueiEmbyLibraryItem[], error: message.includes("输出目录不存在") ? "" : message };
        }
      }),
    );
    for (const batch of batches) {
      allItems.push(...batch.items);
      if (batch.error) taskErrors.push(batch.error);
    }
    if (!allItems.length && taskErrors.length) {
      errorMessage.value = taskErrors[0];
    }
    const syncState = await syncStatePromise;
    if (syncState) {
      const syncedByKey = new Map(syncState.items.map((item) => [item.key, item]));
      items.value = allItems.map((item) => {
        const synced = syncedByKey.get(item.id);
        return {
          ...item,
          total_episodes: synced?.total_episodes,
          users: synced?.users || [],
        };
      });
    } else {
      items.value = allItems;
    }
  } catch (error) {
    items.value = [];
    matchedTasks.value = [];
    errorMessage.value = getApiErrorMessage(error, refresh ? "刷新 STRM 项目失败" : "读取 STRM 项目失败");
  } finally {
    loadingItems.value = false;
  }
}

async function chooseLibrary(library: BueiEmbyLibrary) {
  if (library.id === selectedLibraryId.value && (loadingItems.value || hasItems.value)) return;
  archiveRuleOpen.value = false;
  archiveLogOpen.value = false;
  archiveMenuOpen.value = false;
  selectedLibraryId.value = library.id;
  await loadItems(library.id);
  void maybeAutoSync(library.id);
}

async function refreshCurrentLibrary() {
  if (!selectedLibrary.value || loadingItems.value || syncingEmby.value) return;
  await loadItems(selectedLibrary.value.id, true);
}

async function syncCurrentLibrary(auto = false) {
  if (!selectedLibrary.value || syncingEmby.value) return;
  if (!matchedTasks.value.length) return;
  const libraryId = selectedLibrary.value.id;
  const libraryName = selectedLibrary.value.name;
  syncingEmby.value = true;
  try {
    const result = await syncBueiEmbyLibrary({
      config_id: configId.value || undefined,
      library_id: libraryId,
      library_name: libraryName,
    });
    if (result.last_synced_at) {
      syncTimes.value = { ...syncTimes.value, [libraryId]: result.last_synced_at };
    }
    const failed = result.failed_items?.length || 0;
    await loadItems(libraryId);
    if (!auto) {
      toast.success(`Emby 同步完成：已匹配 ${result.matched_items} 项，保存 ${result.saved_items} 项${failed ? `，失败 ${failed} 项` : ""}`);
    }
  } catch (error) {
    if (!auto) toast.error(getApiErrorMessage(error, "Emby 同步失败"));
  } finally {
    syncingEmby.value = false;
  }
}

function shouldAutoSync(libraryId: string) {
  const last = syncTimes.value[libraryId];
  if (!last) return true;
  const time = Date.parse(last);
  if (!Number.isFinite(time)) return true;
  return Date.now() - time > autoSyncIntervalMs;
}

// 进入媒体库时检查最近同步时间，超过 12 小时自动同步一次；同一媒体库本次会话只尝试一次。
async function maybeAutoSync(libraryId: string) {
  if (!libraryId || libraryId !== selectedLibraryId.value) return;
  if (autoSyncedLibraryIDs.value.has(libraryId)) return;
  if (!matchedTasks.value.length || loadingItems.value || syncingEmby.value) return;
  if (!shouldAutoSync(libraryId)) return;
  autoSyncedLibraryIDs.value = new Set([...autoSyncedLibraryIDs.value, libraryId]);
  await syncCurrentLibrary(true);
}

function closeDetail() {
  selectedItem.value = null;
}

async function archiveItem(item: BueiEmbyLibraryItem) {
  menuOpenID.value = "";
  const ok = await confirm({
    title: "归档媒体项目？",
    message: `将「${item.name}」移动到对应的归档媒体库。源文件夹会从当前网盘目录移出，请确认。`,
    confirmText: "确认归档",
    cancelText: "取消",
  }).catch(() => false);
  if (!ok || archivingItemID.value) return;
  archivingItemID.value = item.id;
  try {
    const result = await archiveBueiEmbyLibraryItem(item.id, configId.value, selectedLibraryId.value);
    if (selectedLibrary.value) await loadItems(selectedLibrary.value.id);
    toast.success(`已归档「${result.item_name}」：${result.target_path}`);
  } catch (error) {
    toast.error(getApiErrorMessage(error, "归档媒体项目失败"));
  } finally {
    archivingItemID.value = "";
  }
}

async function restoreItem(item: BueiEmbyLibraryItem) {
  menuOpenID.value = "";
  const ok = await confirm({
    title: "移回原处？",
    message: `将「${item.name}」按归档时保存的原始位置移回，并刷新原媒体库。`,
    confirmText: "确认移回",
    cancelText: "取消",
  }).catch(() => false);
  if (!ok || archivingItemID.value) return;
  archivingItemID.value = item.id;
  try {
    const result = await restoreBueiEmbyLibraryItem(item.id, configId.value);
    if (selectedLibrary.value) await loadItems(selectedLibrary.value.id);
    toast.success(`已移回「${result.item_name}」：${result.source_path}`);
  } catch (error) {
    toast.error(getApiErrorMessage(error, "移回原处失败"));
  } finally {
    archivingItemID.value = "";
  }
}

function handleMenuSelect(item: BueiEmbyLibraryItem, key: string) {
  if (key === "archive") void archiveItem(item);
  else if (key === "restore") void restoreItem(item);
}

function handleArchiveMenu(key: string) {
  if (key === "run") void runArchiveNow();
  else if (key === "config") archiveRuleOpen.value = true;
  else if (key === "logs") archiveLogOpen.value = true;
}

async function runArchiveNow() {
  if (!selectedLibrary.value || archiveRunStarting.value) return;
  archiveRunStarting.value = true;
  try {
    await runBueiEmbyArchiveNow({
      config_id: configId.value || undefined,
      archive_library_id: selectedLibrary.value.id,
    });
    archiveLogOpen.value = true;
  } catch (error) {
    toast.error(getApiErrorMessage(error, "启动自动归档失败"));
  } finally {
    archiveRunStarting.value = false;
  }
}

// 立即执行结束后，若有项目被归档，重新读取当前媒体库项目（后端已刷新索引与 Emby 媒体库）。
async function handleArchiveRunFinished(run: BueiEmbyArchiveRun) {
  if (!run.archived || !selectedLibrary.value) return;
  await loadItems(selectedLibrary.value.id);
}

function handleArchiveRuleSaved() {
  archiveRuleOpen.value = false;
  void loadArchiveRule(selectedLibraryId.value);
  toast.success("自动归档配置已保存");
}

function handleKeydown(event: KeyboardEvent) {
  if (event.key === "Escape" && selectedItem.value) closeDetail();
}

onMounted(() => {
  window.addEventListener("keydown", handleKeydown);
  void loadState();
});
onBeforeUnmount(() => window.removeEventListener("keydown", handleKeydown));
onActivated(() => {
  if (loaded.value) void loadState(false);
});
</script>

<template>
  <div class="emby-library-page">
    <div v-if="loadingState" class="emby-library-loading" aria-live="polite">
      <BusySpinner variant="notch" :size="26" />
      <span>正在连接 Emby…</span>
    </div>

    <AppEmptyState
      v-else-if="!isConfigured"
      icon="film"
      title="还没有配置 Emby"
      description="请先到「辅助工具 → 增强工具 → Emby/Jellyfin 反代」填写地址和 API Key。保存后即可在这里浏览媒体库。"
    />

    <AppEmptyState v-else-if="errorMessage && !libraries.length" icon="triangle-exclamation" title="Emby 暂时不可用" :description="errorMessage">
      <AppButton variant="secondary" @click="loadState()">重新连接</AppButton>
    </AppEmptyState>

    <div v-else class="emby-library-shell">
      <aside class="emby-library-sidebar" aria-label="Emby 媒体库">
        <div class="emby-library-sidebar__head">
          <div>
            <p class="emby-library-eyebrow">{{ selectedLibrary ? "正在浏览" : "内容空间" }}</p>
            <h1>Emby 媒体库</h1>
          </div>
          <span class="emby-library-status"><i />已连接</span>
        </div>
        <div class="emby-library-sidebar__rule" />
        <p class="emby-library-sidebar__label">所有媒体库</p>
        <nav class="emby-library-list" aria-label="所有媒体库">
          <button
            v-for="library in libraries"
            :key="library.id"
            type="button"
            class="emby-library-nav-item"
            :class="{ 'is-active': library.id === selectedLibraryId }"
            :aria-current="library.id === selectedLibraryId ? 'page' : undefined"
            :title="`${library.name} · ${libraryKindLabel(library)}`"
            @click="chooseLibrary(library)"
          >
            <span class="emby-library-nav-item__icon" :class="`is-kind-${libraryKind(library)}`">
              <BueiEmbyLibraryIcon :kind="libraryKind(library)" :size="16" />
            </span>
            <span class="emby-library-nav-item__copy">
              <strong>{{ library.name }}</strong>
            </span>
          </button>
        </nav>
        <div class="emby-library-sidebar__foot">
          <span class="emby-library-dot" />
          <span>{{ matchedTasks.length }} 个 STRM 任务已匹配</span>
        </div>
      </aside>

      <main class="emby-library-content">
        <header class="emby-library-content__head">
          <div class="emby-library-breadcrumb">
            <span>媒体库</span>
            <span class="emby-library-breadcrumb__sep" aria-hidden="true" />
            <strong>{{ selectedLibrary?.name || "未选择" }}</strong>
          </div>
          <div class="emby-library-title-row">
            <h2>{{ selectedLibrary?.name || "选择一个媒体库" }}</h2>
            <div class="emby-library-toolbar">
              <button type="button" class="emby-library-refresh" :disabled="loadingItems || syncingEmby" aria-label="刷新当前媒体库" title="刷新当前媒体库" @click="refreshCurrentLibrary">
                <SvgIcon name="rotate" :size="15" />
                <span>刷新</span>
              </button>
              <AppButton
                class="emby-library-sync"
                variant="secondary"
                size="sm"
                :disabled="syncingEmby || loadingItems || !matchedTasks.length"
                @click="syncCurrentLibrary()"
              >
                <BusySpinner v-if="syncingEmby" variant="notch" :size="13" />
                <SvgIcon v-else name="cloud-arrow-down" :size="14" />
                <span>Emby 同步</span>
              </AppButton>
              <AppDropdown
                v-if="isArchiveLibrary"
                v-model:open="archiveMenuOpen"
                :items="archiveMenu"
                trigger="click"
                align="right"
                :min-width="150"
                @select="handleArchiveMenu"
              >
                <template #trigger="{ open, toggle }">
                  <button
                    type="button"
                    class="emby-library-archive-menu"
                    :aria-expanded="open"
                    aria-haspopup="menu"
                    title="自动归档"
                    @click.stop="toggle()"
                  >
                    <SvgIcon name="clock-rotate-left" :size="14" />
                    <span>自动归档</span>
                    <SvgIcon name="chevron-down" :size="11" />
                  </button>
                </template>
              </AppDropdown>
            </div>
          </div>
          <p v-if="selectedLibrary" class="emby-library-meta">
            <span class="emby-library-meta__group">
              <SvgIcon name="cloud-arrow-down" :size="12" />
              <template v-if="selectedSyncTime">
                <span>上次 Emby 同步 <strong>{{ formatTimeShort(selectedSyncTime) }}</strong></span>
                <span class="emby-library-meta__sep" aria-hidden="true" />
                <span>超过 12 小时自动同步</span>
              </template>
              <span v-else>尚无 Emby 同步记录 · 进入媒体库超过 12 小时会自动同步</span>
            </span>
            <template v-if="isArchiveLibrary && archiveRuleLoaded">
              <span class="emby-library-meta__divider" aria-hidden="true" />
              <span class="emby-library-meta__group">
                <SvgIcon name="clock-rotate-left" :size="12" />
                <template v-if="archiveSchedule">
                  <span>自动归档 <strong>{{ archiveSchedule.interval }}</strong> 检查一次</span>
                  <span class="emby-library-meta__sep" aria-hidden="true" />
                  <span v-if="archiveSchedule.enabled">下次执行 <strong>{{ formatTimeShort(archiveSchedule.next) || "未排期" }}</strong></span>
                  <span v-else>已停用</span>
                  <span class="emby-library-meta__sep" aria-hidden="true" />
                  <span>来源 {{ archiveSchedule.sources }} 个媒体库</span>
                </template>
                <span v-else>自动归档未配置，可在「自动归档 → 配置」选择来源媒体库</span>
              </span>
            </template>
          </p>
        </header>

        <div v-if="errorMessage" class="emby-library-inline-error" role="alert">
          <SvgIcon name="circle-exclamation" :size="15" />{{ errorMessage }}
        </div>

        <div v-if="loadingItems" class="emby-library-grid-state" aria-live="polite">
          <BusySpinner variant="notch" :size="28" />
          <span>正在载入媒体项目…</span>
        </div>

        <section v-if="!loadingItems && selectedLibrary && matchedTasks.length" class="emby-task-filter" aria-label="STRM 任务筛选">
          <div class="emby-task-filter__head">
            <div>
              <span class="emby-task-filter__eyebrow">STRM 来源</span>
              <strong>按任务筛选媒体项目</strong>
              <span class="emby-task-filter__summary">{{ visibleItems.length }} / {{ items.length }} 个项目</span>
            </div>
            <label v-if="matchedTasks.length > 2" class="emby-task-filter__search">
              <SvgIcon name="magnifying-glass" :size="13" />
              <input v-model="taskFilterQuery" type="search" aria-label="筛选 STRM 任务" placeholder="筛选任务名称" />
              <button v-if="taskFilterQuery" type="button" aria-label="清除任务筛选" @click="taskFilterQuery = ''"><SvgIcon name="xmark" :size="12" /></button>
            </label>
          </div>
          <div class="emby-task-filter__chips" role="tablist" aria-label="STRM 任务列表">
            <button
              type="button"
              class="emby-task-chip"
              :class="{ 'is-active': selectedTaskFilter === 'all' }"
              role="tab"
              :aria-selected="selectedTaskFilter === 'all'"
              @click="selectTaskFilter('all')"
            >
              <span class="emby-task-chip__icon"><SvgIcon name="list" :size="13" /></span>
              <span>全部任务</span>
              <b>{{ items.length }}</b>
            </button>
            <button
              v-for="task in visibleTaskList"
              :key="taskID(task)"
              type="button"
              class="emby-task-chip"
              :class="{ 'is-active': selectedTaskFilter === taskID(task) }"
              role="tab"
              :aria-selected="selectedTaskFilter === taskID(task)"
              :title="task.name"
              @click="selectTaskFilter(taskID(task))"
            >
              <span class="emby-task-chip__icon"><SvgIcon name="folder" :size="13" /></span>
              <span>{{ task.name }}</span>
              <b>{{ taskItemCount(task) }}</b>
            </button>
          </div>
          <p v-if="taskFilterQuery && !visibleTaskList.length" class="emby-task-filter__empty">没有匹配的 STRM 任务，请换个关键词。</p>
        </section>

        <AppEmptyState v-if="!loadingItems && !selectedLibrary" icon="folder" title="从左侧选择媒体库" description="选择一个媒体库后，这里会显示名称匹配的 STRM 项目。" />
        <AppEmptyState v-else-if="!loadingItems && !matchedTasks.length" icon="folder" title="没有匹配的 STRM 任务" :description="`没有找到名称包含“${selectedLibrary?.name || ''}”的 STRM 任务，请先创建或重命名任务。`" />
        <AppEmptyState v-else-if="!loadingItems && matchedTasks.length && !hasVisibleItems" icon="film" :title="activeTask ? `「${activeTask.name}」没有项目` : '匹配的 STRM 任务没有项目'" :description="activeTask ? '可以切换上方任务，或点击刷新重建该媒体库的 STRM 索引。' : '已匹配到 STRM 任务，但暂时没有可展示的刮削项目；可以点击刷新重建索引。'" />

        <template v-else-if="!loadingItems && hasVisibleItems">
          <div class="emby-library-list-head">
            <span>{{ selectedTaskFilter === 'all' ? `${matchedTasks.length} 个 STRM 任务 · ` : `${activeTask?.name || '当前任务'} · ` }}{{ visibleItems.length }} 个项目</span>
            <span class="emby-library-list-head__hint">可勾选项目进行批量操作</span>
          </div>
          <section class="emby-media-grid" aria-label="媒体项目列表">
            <article
              v-for="item in visibleItems"
              :key="item.id"
              class="emby-media-card"
            >
              <div class="emby-media-card__poster">
                <img v-if="imageURL(item.poster_url)" :src="imageURL(item.poster_url)" :alt="item.name" loading="lazy" />
                <div v-else class="emby-media-card__poster-fallback"><SvgIcon name="film" :size="28" /></div>
                <div class="emby-media-card__poster-shade" />
                <span class="emby-media-card__type">
                  <span>{{ item.type === "Series" ? "剧集" : "电影" }}</span>
                  <span v-if="item.production_year" class="emby-media-card__year">{{ item.production_year }}</span>
                  <button
                    v-if="item.type === 'Series'"
                    type="button"
                    class="emby-media-card__state"
                    :class="{ 'is-ended': item.tv_state === 'ended' }"
                    :disabled="isTVStatePending(item.id)"
                    :aria-pressed="item.tv_state === 'ended'"
                    :title="item.tv_state === 'ended' ? '点击标记为未完结（会继续追更）' : '点击标记为已完结'"
                    @click.stop="toggleTVState(item)"
                  >{{ item.tv_state === "ended" ? "已完结" : "未完结" }}</button>
                </span>
                <a
                  v-if="item.tmdb_id"
                  class="emby-media-card__tmdb"
                  :href="tmdbURL(item)"
                  target="_blank"
                  rel="noopener noreferrer"
                  title="在 TMDB 打开"
                  aria-label="在 TMDB 打开"
                  @click.stop
                >TMDB<SvgIcon name="arrow-up-right-from-square" :size="9" /></a>
                <span v-if="episodeLabel(item)" class="emby-media-card__episodes">{{ episodeLabel(item) }}</span>
                <div class="emby-media-card__actions" @click.stop>
                  <AppDropdown
                    :open="menuOpenID === item.id"
                    trigger="click"
                    align="right"
                    :items="itemMenu(item.id)"
                    @update:open="menuOpenID = $event ? item.id : ''"
                    @select="handleMenuSelect(item, $event)"
                  >
                    <template #trigger="{ open, toggle }">
                      <button type="button" class="emby-media-card__more" :aria-expanded="open" :disabled="archivingItemID === item.id" aria-label="更多操作" @click.stop="menuOpenID = open ? '' : item.id; toggle()">
                        <BusySpinner v-if="archivingItemID === item.id" variant="notch" :size="15" />
                        <svg v-else viewBox="0 0 24 24" aria-hidden="true"><circle cx="5" cy="12" r="1.8" /><circle cx="12" cy="12" r="1.8" /><circle cx="19" cy="12" r="1.8" /></svg>
                      </button>
                    </template>
                  </AppDropdown>
                </div>
              </div>
              <div class="emby-media-card__body">
                <h3 :title="item.name">{{ item.name }}</h3>
                <div v-if="item.strm_task_name" class="emby-media-card__task" :title="`所属 STRM 任务：${item.strm_task_name}`">
                  <SvgIcon name="folder" :size="10" />
                  <span>{{ item.strm_task_name }}</span>
                </div>
                <div v-if="item.users.length" class="emby-media-card__users" aria-label="用户观看状态">
                  <button
                    v-for="user in item.users"
                    :key="user.user_id"
                    type="button"
                    class="emby-user-state"
                    :class="{ 'is-played': user.played, 'is-progress': !user.played && (user.watched_episodes || 0) > 0 }"
                    :disabled="isUserPlayedPending(item.id, user.user_id)"
                    :aria-pressed="user.played"
                    :title="userStateTitle(user)"
                    @click.stop="toggleUserPlayed(item, user)"
                  >
                    {{ user.user_name }}
                    <SvgIcon v-if="user.played" name="check" :size="8" />
                  </button>
                </div>
              </div>
            </article>
          </section>
        </template>
      </main>
    </div>

    <Teleport to="body">
      <div v-if="loadingDetail && !selectedItem" class="emby-detail-overlay emby-detail-overlay--loading" aria-live="polite">
        <div class="emby-detail-loading"><BusySpinner variant="notch" :size="24" />正在读取详情…</div>
      </div>
      <div v-if="selectedItem" class="emby-detail-overlay" @click.self="closeDetail">
        <section class="emby-detail-modal" role="dialog" aria-modal="true" :aria-label="selectedItem.item.name">
          <div class="emby-detail-hero" :style="selectedItem.item.backdrop_url ? { backgroundImage: `url(${selectedItem.item.backdrop_url})` } : undefined">
            <div class="emby-detail-hero__shade" />
            <button type="button" class="emby-detail-back" @click="closeDetail"><SvgIcon name="arrow-left" :size="14" />返回媒体库</button>
            <button type="button" class="emby-detail-close" aria-label="关闭详情" @click="closeDetail"><SvgIcon name="xmark" :size="16" /></button>
            <div class="emby-detail-hero__copy">
              <span class="emby-detail-kicker">{{ selectedItem.item.type === "Series" ? "剧集" : "电影" }} · Emby 媒体库</span>
              <h2>{{ selectedItem.item.name }}</h2>
              <p v-if="selectedItem.item.production_year">{{ selectedItem.item.production_year }}<span v-if="selectedItem.item.type === 'Movie' && formatRuntime(selectedItem.runtime_minutes)"> · {{ formatRuntime(selectedItem.runtime_minutes) }}</span><span v-if="selectedItem.official_rating"> · {{ selectedItem.official_rating }}</span><span v-if="formatRating(selectedItem.community_rating)"> · ★ {{ formatRating(selectedItem.community_rating) }}</span></p>
            </div>
          </div>
          <div class="emby-detail-body">
            <div v-if="selectedItem.tagline" class="emby-detail-tagline">{{ selectedItem.tagline }}</div>
            <p v-if="selectedItem.item.overview" class="emby-detail-overview">{{ selectedItem.item.overview }}</p>
            <div class="emby-detail-meta">
              <div v-if="selectedItem.genres?.length"><span>类型</span><strong>{{ selectedItem.genres.join(" · ") }}</strong></div>
              <div v-if="selectedItem.studios?.length"><span>制作</span><strong>{{ selectedItem.studios.join(" · ") }}</strong></div>
              <div v-if="selectedItem.premiere_date"><span>首播</span><strong>{{ selectedItem.premiere_date }}</strong></div>
            </div>
            <div v-if="selectedItem.item.users.length" class="emby-detail-progress">
              <h3>观看状态</h3>
              <div class="emby-detail-progress__list">
                <div v-for="user in selectedItem.item.users" :key="user.user_id" class="emby-detail-progress__user">
                  <span class="emby-detail-progress__avatar">{{ user.user_name.slice(0, 1) }}</span>
                  <span>{{ user.user_name }}</span>
                  <strong :class="{ 'is-done': user.played }">{{ user.played ? "已看完" : (selectedItem.item.type === 'Series' && user.remaining_episodes != null ? `剩余 ${user.remaining_episodes} 集` : "未看完") }}</strong>
                </div>
              </div>
            </div>
            <div v-if="selectedItem.seasons?.length" class="emby-detail-seasons">
              <h3>剧集季</h3>
              <div class="emby-season-grid">
                <div v-for="season in selectedItem.seasons" :key="season.id" class="emby-season-card">
                  <img v-if="season.poster_url" :src="season.poster_url" :alt="season.name" loading="lazy" />
                  <div v-else class="emby-season-card__fallback"><SvgIcon name="film" :size="18" /></div>
                  <div><strong>{{ season.name }}</strong><small v-if="season.index_number != null">第 {{ season.index_number }} 季</small></div>
                </div>
              </div>
            </div>
            <div v-if="selectedItem.people?.length" class="emby-detail-people">
              <h3>演职员</h3>
              <div class="emby-detail-people__list">
                <span v-for="person in selectedItem.people" :key="`${person.name}-${person.role}`"><strong>{{ person.name }}</strong><small v-if="person.role">{{ person.role }}</small></span>
              </div>
            </div>
          </div>
          <div v-if="loadingDetail" class="emby-detail-loading"><BusySpinner variant="notch" :size="24" />正在读取详情…</div>
        </section>
      </div>
    </Teleport>

    <BueiEmbyArchiveRuleModal
      :open="archiveRuleOpen"
      :library-id="selectedLibraryId"
      :library-name="selectedLibrary?.name || ''"
      :config-id="configId"
      @close="archiveRuleOpen = false"
      @saved="handleArchiveRuleSaved"
    />
    <BueiEmbyArchiveLogModal
      :open="archiveLogOpen"
      :library-id="selectedLibraryId"
      :library-name="selectedLibrary?.name || ''"
      @close="archiveLogOpen = false"
      @finished="handleArchiveRunFinished"
    />
  </div>
</template>

<style scoped>
.emby-library-page {
  min-height: calc(100vh - 92px);
  color: var(--text);
}
.emby-library-loading,
.emby-library-grid-state {
  min-height: 300px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  color: var(--text-muted);
}
.emby-library-shell {
  min-height: calc(100vh - 92px);
  display: grid;
  grid-template-columns: 248px minmax(0, 1fr);
  border: 1px solid var(--border);
  border-radius: var(--radius-xl);
  overflow: hidden;
  background: var(--surface);
  box-shadow: var(--shadow-card);
}
.emby-library-sidebar {
  display: flex;
  flex-direction: column;
  min-width: 0;
  padding: 22px 16px 16px;
  background: linear-gradient(180deg, color-mix(in srgb, var(--brand) 7%, var(--surface)), var(--surface-sunken));
  border-right: 1px solid var(--border);
}
.emby-library-sidebar__head { display: flex; align-items: flex-start; justify-content: space-between; gap: 8px; padding: 0 8px; }
.emby-library-eyebrow { margin: 0 0 4px; color: var(--brand); font-size: 11px; font-weight: 700; letter-spacing: .08em; }
.emby-library-sidebar h1 { margin: 0; font-size: 19px; letter-spacing: -.03em; }
.emby-library-status { display: inline-flex; align-items: center; gap: 5px; padding: 4px 7px; border-radius: var(--radius-pill); background: color-mix(in srgb, var(--success) 10%, var(--surface)); color: var(--success); font-size: 10px; white-space: nowrap; }
.emby-library-status i { width: 6px; height: 6px; border-radius: 50%; background: currentColor; }
.emby-library-sidebar__rule { height: 1px; margin: 20px 8px 16px; background: var(--border); }
.emby-library-sidebar__label { margin: 0 8px 8px; color: var(--text-muted); font-size: 11px; font-weight: 700; }
.emby-library-list { display: grid; gap: 5px; }
.emby-library-nav-item { width: 100%; min-height: 56px; display: flex; align-items: center; gap: 10px; padding: 9px 10px; border: 1px solid transparent; border-radius: var(--radius-control); color: var(--text-muted); background: transparent; text-align: left; cursor: pointer; transition: background .16s ease, border-color .16s ease, color .16s ease; }
.emby-library-nav-item:hover { color: var(--text); background: var(--surface); border-color: var(--border); }
.emby-library-nav-item.is-active { color: var(--text); background: var(--surface); border-color: color-mix(in srgb, var(--brand) 26%, var(--border)); box-shadow: 0 4px 12px color-mix(in srgb, #0f172a 7%, transparent); }
.emby-library-nav-item__icon { --lib-tone: var(--brand); display: grid; place-items: center; width: 32px; height: 32px; border-radius: 10px; color: var(--lib-tone); background: color-mix(in srgb, var(--lib-tone) 11%, var(--surface)); box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--lib-tone) 16%, transparent); flex-shrink: 0; transition: color .16s ease, background .16s ease, box-shadow .16s ease; }
.emby-library-nav-item.is-active .emby-library-nav-item__icon { color: #fff; background: var(--lib-tone); box-shadow: 0 4px 10px color-mix(in srgb, var(--lib-tone) 32%, transparent); }
.emby-library-nav-item__icon.is-kind-movies { --lib-tone: #4c74df; }
.emby-library-nav-item__icon.is-kind-series { --lib-tone: #7c5cd6; }
.emby-library-nav-item__icon.is-kind-music { --lib-tone: #0e9b8c; }
.emby-library-nav-item__icon.is-kind-photos { --lib-tone: #d9852b; }
.emby-library-nav-item__icon.is-kind-homevideos { --lib-tone: #1590c4; }
.emby-library-nav-item__icon.is-kind-books { --lib-tone: #a1703f; }
.emby-library-nav-item__icon.is-kind-boxsets { --lib-tone: #c15c86; }
.emby-library-nav-item__icon.is-kind-mixed { --lib-tone: #6b7a90; }
.emby-library-nav-item__copy { min-width: 0; flex: 1; }
.emby-library-nav-item__copy strong { display: block; overflow: hidden; font-size: 14px; font-weight: 600; line-height: 1.35; letter-spacing: -.01em; text-overflow: ellipsis; white-space: nowrap; }
.emby-library-sidebar__foot { display: flex; align-items: center; gap: 7px; margin: auto 8px 0; padding-top: 16px; color: var(--text-muted); font-size: 11px; line-height: 1.45; border-top: 1px dashed var(--border); }
.emby-library-dot { width: 7px; height: 7px; border-radius: 50%; background: var(--success); box-shadow: 0 0 0 4px color-mix(in srgb, var(--success) 12%, transparent); flex-shrink: 0; }
.emby-library-content { min-width: 0; padding: 30px clamp(18px, 3vw, 38px) 42px; background: var(--bg); }
.emby-library-content__head { margin-bottom: 25px; }
.emby-library-breadcrumb { display: flex; align-items: center; gap: 9px; margin-bottom: 10px; color: var(--text-muted); font-size: 11px; letter-spacing: .02em; }
.emby-library-breadcrumb__sep { width: 1px; height: 10px; background: color-mix(in srgb, var(--text-muted) 42%, transparent); transform: rotate(16deg); }
.emby-library-breadcrumb strong { color: var(--brand); font-weight: 650; }
.emby-library-content__head p.emby-library-meta { display: flex; align-items: center; flex-wrap: wrap; gap: 9px 12px; font-size: 11px; line-height: 1.5; }
.emby-library-meta__group { display: inline-flex; align-items: center; flex-wrap: wrap; gap: 3px 8px; }
.emby-library-meta strong { color: var(--text); font-weight: 700; }
.emby-library-meta__sep { width: 1px; height: 9px; background: color-mix(in srgb, var(--text-muted) 42%, transparent); }
.emby-library-meta__divider { width: 1px; height: 12px; background: color-mix(in srgb, var(--text-muted) 58%, transparent); }
.emby-library-title-row { display: flex; align-items: center; justify-content: space-between; gap: 18px; }
.emby-library-title-row h2 { min-width: 0; }
.emby-library-content h2 { margin: 0; font-size: clamp(24px, 3vw, 33px); line-height: 1.1; letter-spacing: -.05em; }
.emby-library-content__head p { margin: 8px 0 0; color: var(--text-muted); font-size: 13px; }
.emby-library-toolbar { display: flex; align-items: center; gap: 9px; flex-shrink: 0; }
.emby-library-refresh { display: inline-flex; align-items: center; gap: 7px; height: 34px; min-height: 34px; padding: 0 11px; border: 1px solid var(--border); border-radius: var(--radius-control); background: var(--surface); color: var(--text-muted); font-size: 12px; cursor: pointer; transition: .15s ease; }
.emby-library-refresh:hover:not(:disabled) { border-color: var(--brand); color: var(--brand); background: color-mix(in srgb, var(--brand) 6%, var(--surface)); }
.emby-library-refresh:disabled { opacity: .55; cursor: not-allowed; }
.emby-library-toolbar :deep(.emby-library-sync) { min-height: 34px; }
.emby-library-archive-menu { display: inline-flex; align-items: center; gap: 6px; height: 34px; min-height: 34px; padding: 0 10px; border: 1px solid var(--border); border-radius: var(--radius-control); color: var(--text-muted); background: var(--surface); font-size: 12px; cursor: pointer; transition: border-color .15s ease, color .15s ease, background .15s ease; }
.emby-library-archive-menu:hover, .emby-library-archive-menu[aria-expanded="true"] { border-color: var(--brand); color: var(--brand); background: color-mix(in srgb, var(--brand) 6%, var(--surface)); }
.emby-task-filter { margin: -7px 0 22px; padding: 14px 15px 12px; border: 1px solid var(--border); border-radius: var(--radius-card); background: color-mix(in srgb, var(--surface) 82%, var(--brand) 18%); box-shadow: 0 8px 20px color-mix(in srgb, var(--brand) 7%, transparent); }
.emby-task-filter__head { display: flex; align-items: center; justify-content: space-between; gap: 14px; margin-bottom: 12px; }
.emby-task-filter__head > div:first-child { display: flex; align-items: baseline; flex-wrap: wrap; gap: 7px 10px; min-width: 0; }
.emby-task-filter__eyebrow { color: var(--brand); font-size: 10px; font-weight: 800; letter-spacing: .08em; text-transform: uppercase; }
.emby-task-filter__head strong { color: var(--text); font-size: 13px; }
.emby-task-filter__summary { color: var(--text-muted); font-size: 11px; }
.emby-task-filter__search { display: flex; align-items: center; gap: 6px; width: min(220px, 42%); min-height: 30px; padding: 0 9px; border: 1px solid var(--border); border-radius: var(--radius-pill); color: var(--text-muted); background: var(--surface); }
.emby-task-filter__search:focus-within { border-color: var(--brand); box-shadow: 0 0 0 2px color-mix(in srgb, var(--brand) 14%, transparent); }
.emby-task-filter__search input { min-width: 0; flex: 1; border: 0; outline: 0; color: var(--text); background: transparent; font-size: 11px; }
.emby-task-filter__search input::placeholder { color: var(--text-muted); }
.emby-task-filter__search button { display: grid; place-items: center; width: 20px; height: 20px; padding: 0; border: 0; color: var(--text-muted); background: transparent; cursor: pointer; }
.emby-task-filter__search button:hover { color: var(--text); }
.emby-task-filter__chips { display: flex; gap: 7px; overflow-x: auto; padding: 1px 1px 3px; scrollbar-width: thin; }
.emby-task-chip { display: inline-flex; align-items: center; gap: 7px; min-width: max-content; min-height: 34px; padding: 0 10px 0 7px; border: 1px solid var(--border); border-radius: var(--radius-control); color: var(--text-muted); background: var(--surface); font-size: 11px; cursor: pointer; transition: border-color .15s ease, background .15s ease, color .15s ease, transform .15s ease; }
.emby-task-chip:hover { color: var(--text); border-color: color-mix(in srgb, var(--brand) 45%, var(--border)); transform: translateY(-1px); }
.emby-task-chip.is-active { border-color: color-mix(in srgb, var(--brand) 55%, var(--border)); color: var(--brand); background: color-mix(in srgb, var(--brand) 11%, var(--surface)); box-shadow: 0 3px 10px color-mix(in srgb, var(--brand) 10%, transparent); }
.emby-task-chip__icon { display: grid; place-items: center; width: 22px; height: 22px; border-radius: 7px; color: inherit; background: color-mix(in srgb, currentColor 10%, transparent); }
.emby-task-chip > span:not(.emby-task-chip__icon) { max-width: 190px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.emby-task-chip b { min-width: 18px; padding: 2px 5px; border-radius: var(--radius-pill); color: inherit; background: color-mix(in srgb, currentColor 10%, transparent); font-size: 10px; font-weight: 700; text-align: center; }
.emby-task-filter__empty { margin: 10px 0 0; color: var(--text-muted); font-size: 11px; }
.emby-library-inline-error { display: flex; align-items: center; gap: 7px; margin-bottom: 15px; padding: 10px 12px; border: 1px solid color-mix(in srgb, var(--danger) 25%, var(--border)); border-radius: var(--radius-control); background: color-mix(in srgb, var(--danger) 7%, var(--surface)); color: var(--danger); font-size: 12px; }
.emby-library-list-head { display: flex; align-items: center; gap: 9px; margin-bottom: 13px; color: var(--text-muted); font-size: 12px; }
.emby-library-list-head__hint { margin-left: auto; font-size: 11px; }
.emby-media-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(172px, 1fr)); gap: 18px 14px; }
.emby-media-card { position: relative; min-width: 0; overflow: visible; border: 1px solid transparent; border-radius: var(--radius-card); background: var(--surface); cursor: default; transition: transform .18s ease, box-shadow .18s ease, border-color .18s ease; }
.emby-media-card:hover { transform: translateY(-3px); border-color: color-mix(in srgb, var(--brand) 42%, var(--border)); box-shadow: 0 12px 24px color-mix(in srgb, #0f172a 12%, transparent); }
.emby-media-card:focus-visible { outline: 2px solid var(--brand); outline-offset: 3px; }
.emby-media-card__poster { position: relative; aspect-ratio: 2 / 3; overflow: hidden; border-radius: var(--radius-card) var(--radius-card) 0 0; background: linear-gradient(145deg, color-mix(in srgb, var(--brand) 22%, var(--surface-sunken)), var(--surface-sunken)); }
.emby-media-card__poster img { display: block; width: 100%; height: 100%; object-fit: cover; transition: transform .35s ease; }
.emby-media-card:hover .emby-media-card__poster img { transform: scale(1.035); }
.emby-media-card__poster-fallback { display: grid; place-items: center; width: 100%; height: 100%; color: color-mix(in srgb, var(--brand) 55%, var(--text-muted)); }
.emby-media-card__poster-shade { position: absolute; inset: 0; background: linear-gradient(180deg, rgba(2,6,23,.22), transparent 34%, rgba(2,6,23,.7)); pointer-events: none; }
.emby-media-card__type { position: absolute; left: 10px; bottom: 10px; display: inline-flex; align-items: center; padding: 3px 8px; border-radius: 6px; color: #fff; background: rgba(2,6,23,.62); backdrop-filter: blur(6px); box-shadow: inset 0 0 0 1px rgba(255,255,255,.1); font-size: 10px; font-weight: 700; line-height: 1.5; white-space: nowrap; }
.emby-media-card__year, .emby-media-card__state { display: inline-flex; align-items: center; color: rgba(255,255,255,.76); font-weight: 600; font-variant-numeric: tabular-nums; letter-spacing: .01em; }
.emby-media-card__state { padding: 0; border: 0; background: none; font: inherit; font-weight: 600; color: #ffd08a; cursor: pointer; }
.emby-media-card__state:hover:not(:disabled) { text-decoration: underline; text-underline-offset: 2px; }
.emby-media-card__state:disabled { opacity: .6; cursor: progress; }
.emby-media-card__state:focus-visible { outline: 2px solid rgba(255,255,255,.85); outline-offset: 2px; border-radius: 3px; }
.emby-media-card__state.is-ended { color: #9ce3c0; }
.emby-media-card__year::before, .emby-media-card__state::before { content: ""; width: 3px; height: 3px; margin: 0 6px 0 5px; border-radius: 50%; background: currentColor; opacity: .85; }
.emby-media-card__tmdb { position: absolute; top: 10px; left: 10px; z-index: 2; display: inline-flex; align-items: center; gap: 3px; padding: 3px 7px; border-radius: 5px; color: rgba(255,255,255,.82); background: rgba(2,6,23,.68); backdrop-filter: blur(5px); font-size: 10px; font-weight: 750; letter-spacing: .02em; line-height: 1.5; text-decoration: none; transition: background .15s ease, color .15s ease; }
.emby-media-card__tmdb:hover { color: #06202a; background: #01b4e4; }
.emby-media-card__tmdb:focus-visible { outline: 2px solid #01b4e4; outline-offset: 2px; }
.emby-media-card__episodes { position: absolute; top: 10px; right: 10px; z-index: 2; padding: 3px 7px; border-radius: 5px; color: #ffe9b0; background: rgba(2,6,23,.68); backdrop-filter: blur(5px); font-size: 10px; font-weight: 750; }
.emby-media-card__actions { position: absolute; right: 7px; bottom: 7px; z-index: 3; }
.emby-media-card__more { display: grid; place-items: center; width: 30px; height: 30px; padding: 0; border: 0; border-radius: 50%; background: transparent; color: var(--text-muted); cursor: pointer; transition: background .15s ease, color .15s ease; }
.emby-media-card__more svg { display: block; width: 18px; height: 18px; fill: currentColor; }
.emby-media-card__more:hover:not(:disabled), .emby-media-card__more[aria-expanded="true"] { background: var(--surface-sunken); color: var(--brand); }
.emby-media-card__more:disabled { opacity: .75; color: var(--brand); cursor: wait; }
.emby-media-card__body { min-width: 0; padding: 10px 10px 12px; }
.emby-media-card__body h3 { margin: 0; overflow: hidden; color: var(--text); font-size: 13px; font-weight: 700; line-height: 1.35; text-overflow: ellipsis; white-space: nowrap; }
.emby-media-card__task { display: flex; align-items: center; gap: 4px; min-width: 0; margin-top: 6px; color: var(--brand); font-size: 10px; font-weight: 650; line-height: 1.2; }
.emby-media-card__task span { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.emby-media-card__users { display: flex; flex-wrap: wrap; gap: 4px; min-height: 20px; margin-top: 8px; }
.emby-user-state { position: relative; display: inline-flex; align-items: center; justify-content: center; width: auto; min-width: 20px; max-width: 100%; height: 22px; padding: 0 7px; border: 1px solid var(--border); border-radius: var(--radius-pill); color: var(--text-muted); background: var(--surface-sunken); font-family: inherit; font-size: 9px; font-weight: 700; line-height: 1; white-space: nowrap; cursor: pointer; transition: box-shadow .15s ease, transform .15s ease, opacity .15s ease; }
.emby-user-state:hover:not(:disabled) { box-shadow: 0 2px 6px color-mix(in srgb, #0f172a 14%, transparent); transform: translateY(-1px); }
.emby-user-state:focus-visible { outline: 2px solid var(--brand); outline-offset: 2px; }
.emby-user-state:disabled { opacity: .65; cursor: progress; }
.emby-user-state.is-played { border-color: color-mix(in srgb, var(--success) 62%, var(--border)); color: #fff; background: var(--success); }
.emby-user-state.is-progress { border-color: color-mix(in srgb, var(--warning) 65%, var(--border)); color: var(--warning); background: color-mix(in srgb, var(--warning) 12%, var(--surface-sunken)); }
.emby-user-state :deep(.lp-svg-icon) { position: absolute; right: -2px; bottom: -2px; padding: 1px; border-radius: 50%; color: #fff; background: var(--success); }

.emby-detail-overlay { position: fixed; inset: 0; z-index: var(--z-modal); display: flex; align-items: center; justify-content: center; padding: 28px; overflow-y: auto; background: rgba(2,6,23,.58); backdrop-filter: blur(4px); }
.emby-detail-modal { position: relative; width: min(780px, 100%); max-height: min(900px, 92vh); overflow: auto; border: 1px solid var(--border); border-radius: var(--radius-xl); background: var(--surface); box-shadow: 0 26px 70px rgba(2,6,23,.3); }
.emby-detail-hero { position: relative; min-height: 260px; display: flex; align-items: flex-end; overflow: hidden; background: linear-gradient(135deg, color-mix(in srgb, var(--brand) 35%, var(--surface-sunken)), var(--surface-sunken)); background-position: center; background-size: cover; }
.emby-detail-hero__shade { position: absolute; inset: 0; background: linear-gradient(90deg, rgba(2,6,23,.86), rgba(2,6,23,.35) 62%, rgba(2,6,23,.3)), linear-gradient(0deg, rgba(2,6,23,.82), transparent 55%); }
.emby-detail-back { position: absolute; top: 16px; left: 18px; z-index: 2; display: inline-flex; align-items: center; gap: 6px; min-height: 32px; padding: 0 10px; border: 1px solid rgba(255,255,255,.28); border-radius: var(--radius-pill); color: #fff; background: rgba(2,6,23,.38); font-size: 11px; cursor: pointer; }
.emby-detail-back:hover, .emby-detail-close:hover { background: rgba(2,6,23,.75); border-color: rgba(255,255,255,.7); }
.emby-detail-close { position: absolute; top: 14px; right: 14px; z-index: 2; display: grid; place-items: center; width: 34px; height: 34px; border: 1px solid rgba(255,255,255,.28); border-radius: 50%; color: #fff; background: rgba(2,6,23,.38); cursor: pointer; }
.emby-detail-hero__copy { position: relative; z-index: 1; max-width: 72%; padding: 28px 30px; color: #fff; }
.emby-detail-kicker { color: #9bd8ff; font-size: 11px; font-weight: 700; letter-spacing: .08em; }
.emby-detail-hero h2 { margin: 8px 0 6px; font-size: clamp(27px, 5vw, 43px); line-height: 1.05; letter-spacing: -.055em; }
.emby-detail-hero p { margin: 0; color: rgba(255,255,255,.78); font-size: 13px; }
.emby-detail-body { padding: 25px 30px 30px; }
.emby-detail-tagline { margin-bottom: 8px; color: var(--brand); font-size: 14px; font-weight: 700; }
.emby-detail-overview { margin: 0; color: var(--text-regular); font-size: 14px; line-height: 1.75; }
.emby-detail-meta { display: flex; flex-wrap: wrap; gap: 16px 30px; margin-top: 20px; padding: 15px 0; border-top: 1px solid var(--border); border-bottom: 1px solid var(--border); }
.emby-detail-meta div { display: grid; gap: 3px; }
.emby-detail-meta span, .emby-detail-progress h3, .emby-detail-seasons h3, .emby-detail-people h3 { color: var(--text-muted); font-size: 11px; font-weight: 700; }
.emby-detail-meta strong { font-size: 12px; }
.emby-detail-progress, .emby-detail-seasons, .emby-detail-people { margin-top: 22px; }
.emby-detail-progress h3, .emby-detail-seasons h3, .emby-detail-people h3 { margin: 0 0 10px; }
.emby-detail-progress__list { display: grid; grid-template-columns: repeat(auto-fit, minmax(190px, 1fr)); gap: 7px; }
.emby-detail-progress__user { display: flex; align-items: center; gap: 8px; min-height: 40px; padding: 7px 9px; border-radius: var(--radius-control); background: var(--surface-sunken); font-size: 12px; }
.emby-detail-progress__avatar { display: grid; place-items: center; width: 25px; height: 25px; border-radius: 50%; color: #fff; background: var(--brand); font-size: 11px; font-weight: 700; }
.emby-detail-progress__user strong { margin-left: auto; color: var(--warning); font-size: 11px; }
.emby-detail-progress__user strong.is-done { color: var(--success); }
.emby-season-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(130px, 1fr)); gap: 9px; }
.emby-season-card { display: flex; align-items: center; gap: 8px; min-width: 0; overflow: hidden; padding: 7px; border: 1px solid var(--border); border-radius: var(--radius-control); }
.emby-season-card img, .emby-season-card__fallback { width: 34px; height: 48px; flex-shrink: 0; object-fit: cover; border-radius: 5px; background: var(--surface-sunken); }
.emby-season-card__fallback { display: grid; place-items: center; color: var(--text-muted); }
.emby-season-card div:last-child { min-width: 0; }
.emby-season-card strong, .emby-season-card small { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.emby-season-card strong { font-size: 11px; }
.emby-season-card small { margin-top: 3px; color: var(--text-muted); font-size: 10px; }
.emby-detail-people__list { display: flex; flex-wrap: wrap; gap: 7px 15px; }
.emby-detail-people__list span { display: grid; gap: 1px; }
.emby-detail-people__list strong { font-size: 12px; }
.emby-detail-people__list small { color: var(--text-muted); font-size: 10px; }
.emby-detail-loading { position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; gap: 8px; color: var(--text-muted); background: color-mix(in srgb, var(--surface) 78%, transparent); backdrop-filter: blur(2px); }

@media (max-width: 900px) { .emby-library-shell { grid-template-columns: 210px minmax(0, 1fr); } .emby-media-grid { grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); } }
@media (max-width: 680px) { .emby-library-shell { display: block; } .emby-library-sidebar { border-right: 0; border-bottom: 1px solid var(--border); } .emby-library-sidebar__foot { margin-top: 18px; } .emby-library-list { grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); } .emby-library-content { padding: 22px 14px 30px; } .emby-library-title-row { display: block; } .emby-library-toolbar { margin-top: 16px; justify-content: flex-end; flex-wrap: wrap; } .emby-task-filter__head { display: block; } .emby-task-filter__search { width: 100%; margin-top: 11px; } .emby-task-chip > span:not(.emby-task-chip__icon) { max-width: 150px; } .emby-detail-overlay { align-items: flex-start; padding: 10px; } .emby-detail-hero__copy, .emby-detail-body { padding-left: 20px; padding-right: 20px; } .emby-detail-hero__copy { max-width: 88%; } }
@media (prefers-reduced-motion: reduce) { .emby-library-nav-item, .emby-media-card, .emby-media-card__poster img { transition: none; } }
</style>
