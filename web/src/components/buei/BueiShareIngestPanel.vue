<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRouter } from "vue-router";
import type { FileItem } from "@/api/types";
import {
  bueiGuangyaShareApi,
  type GuangyaShareItem,
  type GuangyaShareKind,
  type GuangyaShareParseResult,
  type GuangyaShareTransferPhase,
  type GuangyaShareBrowseCrumb,
  type GuangyaShareRenameReplace,
  type GuangyaShareSavedLink,
  type GuangyaShareTransferredFile,
} from "@/api/bueiGuangyaShare";
import {
  bueiQuarkIngestApi,
  type QuarkIngestFavorite,
  type QuarkIngestPayload,
  type QuarkIngestPreset,
} from "@/api/bueiQuarkIngest";
import { ApiError, getApiErrorMessage } from "@/api/client";
import { filesApi } from "@/api/files";
import { fetchStrmTasks, type StrmTask } from "@/api/strm";
import { fetchStrmScrapeProgress, type StrmScrapeProgress } from "@/api/strmScrape";
import {
  searchMediaOrganizeTmdb,
  type MediaOrganizeTmdbSearchHit,
} from "@/api/mediaOrganize";
import {
  fetchEmbyConfigs,
  fetchEmbyLibraries,
  type EmbyConfig,
  type EmbyLibrary,
} from "@/api/emby";
import { buildBrowserLocationQuery } from "@/utils/bueiBrowserLocation";
import { isQuarkShareInboxName } from "@/utils/bueiQuarkShare";
import {
  detectShareLinkProvider,
  type ShareLinkProvider,
} from "@/utils/bueiShareLink";
import { toast } from "@/composables/useToast";
import { confirm } from "@/composables/useConfirm";
import { formatSize, formatTime, formatTimeShort } from "@/utils/format";
import {
  INGEST_KIND_ICONS as kindIcons,
  INGEST_KIND_LABELS as kindLabels,
  ingestKindFromFile,
} from "@/utils/bueiIngestKinds";
import { useAccountsStore } from "@/stores/accounts";
import AppButton from "@/components/base/AppButton.vue";
import AppModal from "@/components/base/AppModal.vue";
import BusySpinner from "@/components/base/BusySpinner.vue";
import AccountFolderField from "@/components/admin/AccountFolderField.vue";
import AppSelect from "@/components/base/AppSelect.vue";
import FolderPickerModal from "@/components/file/FolderPickerModal.vue";
import SvgIcon from "@/components/icons/SvgIcon.vue";
import BueiGuangyaShareConfirm, {
  type RenamePreviewRow,
} from "@/components/buei/BueiGuangyaShareConfirm.vue";
import BueiIngestTargetFiles from "@/components/buei/BueiIngestTargetFiles.vue";

// buei 定制：后台「辅助工具 / 影视入库」统一工作区，同时管理光鸭与夸克。

const router = useRouter();
const accountsStore = useAccountsStore();
const guangyaAccounts = computed(() =>
  accountsStore.accounts.filter((a) => a.driver_type === "guangya" && a.is_active),
);
const quarkAccounts = computed(() =>
  accountsStore.accounts.filter((a) => a.driver_type === "quark" && a.is_active),
);

type Provider = ShareLinkProvider;
type QuarkFavoriteEntry = QuarkIngestFavorite & { accountName: string };
type UnifiedFavorite = {
  key: string;
  provider: Provider;
  id: number;
  title: string;
  subtitle: string;
  link: string;
  createdAt: string;
  accountName?: string;
};

type FolderCrumb = { id: string; name: string; path: string };
type SortKey = "name" | "time" | "size" | "kind";
type DestinationBase = {
  accountId: number;
  accountName: string;
  parentId: string;
  path: string;
  dirs: string[];
};

const QUICK_FILE_KINDS = new Set<GuangyaShareKind>(["video", "image", "subtitle", "document"]);
const FOLDER_TITLE_MAX = 100;
const FAVORITE_PROVIDER_OPTIONS: { value: Provider; label: string }[] = [
  { value: "quark", label: "夸克" },
];

const url = ref("");
const crumbListRef = ref<HTMLElement | null>(null);
const password = ref("");
const parsing = ref(false);
const parseError = ref("");
const passwordPrompt = ref(false);
const result = ref<GuangyaShareParseResult | null>(null);
const parsedURL = ref("");
let parseAbort: AbortController | null = null;

const provider = ref<Provider>("guangya");
const savedLinks = ref<GuangyaShareSavedLink[]>([]);
const savedLoading = ref(false);
const savedError = ref("");
const quarkFavorites = ref<QuarkFavoriteEntry[]>([]);
const quarkFavoritesLoading = ref(false);
const quarkFavoritesError = ref("");
const activeFavoriteKey = ref<string | null>(null);
const activeQuarkFavorite = ref<QuarkFavoriteEntry | null>(null);
const quarkSourceItems = ref<FileItem[]>([]);
const quarkSourceLoading = ref(false);
const quarkSourceError = ref("");
let quarkSourceSeq = 0;

const favoriteDialogOpen = ref(false);
const favoriteEditing = ref<UnifiedFavorite | null>(null);
const favoriteLink = ref("");
const favoritePassword = ref("");
const favoriteProviderManual = ref<Provider>("quark");
const favoriteSaving = ref(false);
const favoriteError = ref("");
const favoriteQuarkAccountId = ref<number | null>(null);
const favoriteQuarkInbox = ref<FileItem | null>(null);
const favoriteQuarkInboxAccountId = ref<number | null>(null);
const favoriteQuarkInboxLoading = ref(false);
const favoriteQuarkInboxError = ref("");
const favoriteQuarkFolderId = ref("");
const favoriteQuarkParentFolderId = ref("");
const favoriteQuarkFolderName = ref("");
const favoriteFolderPickerOpen = ref(false);
const quarkFlattening = ref(false);
const quarkDeleting = ref(false);

// 当前目录与按目录缓存的子项；只加载用户实际进入的文件夹。
const pathStack = ref<FolderCrumb[]>([{ id: "", name: "全部文件", path: "" }]);
const folderCache = ref<Record<string, GuangyaShareItem[]>>({});
const loadingFolders = ref<Record<string, boolean>>({});
const folderErrors = ref<Record<string, string>>({});
let folderGeneration = 0;

const currentFolderID = computed(() => pathStack.value[pathStack.value.length - 1]?.id ?? "");
const currentFolderPath = computed(() => pathStack.value[pathStack.value.length - 1]?.path ?? "");
const currentItems = computed(() => folderCache.value[currentFolderID.value] ?? []);
const currentFolderLoading = computed(() => !!loadingFolders.value[currentFolderID.value]);
const currentFolderError = computed(() => folderErrors.value[currentFolderID.value] ?? "");

function quarkToShareItem(item: FileItem, favorite: QuarkFavoriteEntry): GuangyaShareItem {
  return {
    id: String(item.id),
    parent_id: favorite.folder_id,
    name: item.name,
    is_dir: item.is_dir,
    size: item.size,
    mod_time: item.mod_time,
    file_type: 0,
    kind: ingestKindFromFile(item),
    path: `/${favorite.folder_name}/${item.name}`,
    depth: 1,
  };
}

const quarkMappedItems = computed(() => {
  const favorite = activeQuarkFavorite.value;
  if (!favorite) return [];
  return quarkSourceItems.value.map((item) => quarkToShareItem(item, favorite));
});

const sourceItems = computed(() =>
  provider.value === "quark" ? quarkMappedItems.value : currentItems.value,
);
const sourceLoading = computed(() =>
  provider.value === "quark" ? quarkSourceLoading.value : currentFolderLoading.value,
);
const sourceError = computed(() =>
  provider.value === "quark" ? quarkSourceError.value : currentFolderError.value,
);
const hasSource = computed(() =>
  provider.value === "quark"
    ? !!activeQuarkFavorite.value
    : !!result.value && !result.value.need_password,
);
const sourceTitle = computed(() =>
  provider.value === "quark"
    ? activeQuarkFavorite.value?.folder_name || "收藏文件夹"
    : result.value?.title || "分享文件",
);
const sourceSubtitle = computed(() =>
  provider.value === "quark"
    ? `夸克 · ${activeQuarkFavorite.value?.accountName || "未选择账号"}`
    : result.value?.owner || "光鸭用户",
);

// 当前收藏的原始链接，标题旁提供新窗口快捷跳转。
const activeFavoriteLink = computed(() => {
  if (provider.value === "quark") {
    return activeQuarkFavorite.value?.link?.trim() || "";
  }
  const key = activeFavoriteKey.value;
  if (!key?.startsWith("g:")) return "";
  const id = Number(key.slice(2));
  const link = savedLinks.value.find((item) => item.id === id);
  if (!link) return "";
  return (link.url || (link.share_id ? `https://www.guangyapan.com/s/${link.share_id}` : "")).trim();
});

const unifiedFavorites = computed<UnifiedFavorite[]>(() => {
  const list: UnifiedFavorite[] = [
    ...savedLinks.value.map((link) => ({
      key: `g:${link.id}`,
      provider: "guangya" as const,
      id: link.id,
      title: link.title || link.share_id || "未命名分享",
      subtitle: `${link.owner || "光鸭用户"} · ${formatTimeShort(link.last_used_at) || "刚刚"}`,
      link: link.url || link.share_id,
      createdAt: link.created_at || link.updated_at,
      accountName: "",
    })),
    ...quarkFavorites.value.map((favorite) => ({
      key: `q:${favorite.id}`,
      provider: "quark" as const,
      id: favorite.id,
      title: favorite.folder_name || "未命名文件夹",
      subtitle: `${favorite.accountName} · ${favorite.link}`,
      link: favorite.link,
      createdAt: favorite.created_at || favorite.updated_at || "",
      accountName: favorite.accountName,
    })),
  ];
  // 按添加时间固定排序；使用收藏 ID 兜底，避免时间相同或缺失时顺序漂移。
  return list.sort((a, b) => {
    const createdDiff = (Date.parse(a.createdAt) || 0) - (Date.parse(b.createdAt) || 0);
    if (createdDiff !== 0) return createdDiff;
    if (a.provider !== b.provider) return a.provider.localeCompare(b.provider);
    return a.id - b.id;
  });
});

const favoritesLoading = computed(() => savedLoading.value || quarkFavoritesLoading.value);
const favoritesError = computed(() => savedError.value || quarkFavoritesError.value);
const favoriteDetectedProvider = computed(() => detectShareLinkProvider(favoriteLink.value));
// 添加收藏时只识别光鸭；其余链接一律归入手动选择的云盘，不校验域名。
const showFavoriteProviderSelect = computed(
  () => !favoriteEditing.value && favoriteDetectedProvider.value !== "guangya",
);
const favoriteProvider = computed<Provider>(() => {
  if (favoriteEditing.value) return favoriteEditing.value.provider;
  return favoriteDetectedProvider.value === "guangya" ? "guangya" : favoriteProviderManual.value;
});
const favoriteQuarkAccountOptions = computed(() =>
  quarkAccounts.value.map((account) => ({ value: account.id, label: account.name })),
);

const selectedIds = ref<Set<string>>(new Set());
const selectedMap = ref<Map<string, GuangyaShareItem>>(new Map());
// Shift 连选的锚点：记录上一次点击在当前页中的位置。
const anchorIndex = ref(-1);
const keyword = ref("");
const sortKey = ref<SortKey | null>("name");
const sortDir = ref<"asc" | "desc">("asc");
const page = ref(1);
const pageSize = 100;

const destination = ref<DestinationBase>({ accountId: 0, accountName: "", parentId: "", path: "", dirs: [] });
const folderTitle = ref("");
const isSeries = ref(true);
const season = ref(1);
// 联动动作：生成 STRM 后可选刮削元数据与 Emby 刷库。
const autoStrm = ref(true);
const autoScrape = ref(true);
const scrapeTaskId = ref<number | null>(null);
const scrapeTasks = ref<StrmTask[]>([]);
const scrapeTasksLoading = ref(false);
const scrapeTasksError = ref("");
// 刮削元数据的实时进度（后端只保留一份刮削进度快照）。
const scrapeProgress = ref<StrmScrapeProgress | null>(null);
// 联动动作 3：Emby 刷库。
const embyRefresh = ref(true);
const embyConfigId = ref("");
const embyMode = ref<"global" | "library">("global");
const embyLibraryId = ref("");
const embyConfigs = ref<EmbyConfig[]>([]);
const embyLibraries = ref<EmbyLibrary[]>([]);
const embyConfigsLoading = ref(false);
const embyConfigsLoaded = ref(false);
const embyLibrariesLoading = ref(false);
const embyError = ref("");
// 恢复预设时不要触发配置/范围的 watch 清空已保存的媒体库。
let restoringEmby = false;
let scrapePollTimer: number | undefined;
let scrapeWatchId = 0;
let scrapePollDeadline = 0;
const pickerOpen = ref(false);
const transferring = ref(false);
const discarding = ref(false);

// 重命名规则：prefix=加 U- 前缀，episode=补 SxxEnn，replace=正则替换。
const SEASON_EPISODE_RE = /S\d{1,2}E\d{1,3}/i;
const renamePrefixOn = ref(false);
const renamePrefixTouched = ref(false);
const renameEpisodeOn = ref(false);
const renameEpisodeTouched = ref(false);
const renameReplaceOn = ref(false);
// 替换文本支持多条规则，按顺序执行。
type RenameReplaceRule = { find: string; replaceWith: string };
const renameReplaceRules = ref<RenameReplaceRule[]>([{ find: "", replaceWith: "" }]);
const renameStartEpisode = ref(1);

// TMDB 匹配：复用系统「目录整理」里的 TMDB 设置。
const tmdbResults = ref<MediaOrganizeTmdbSearchHit[]>([]);
const tmdbLoading = ref(false);
const tmdbError = ref("");
const tmdbSearched = ref(false);
let tmdbSearchSeq = 0;
const transferConfirmOpen = ref(false);
const pendingTransfer = ref<PendingTransfer | null>(null);
// 入库完成后的结果直接展示在同一个确认弹窗里。
const transferDone = ref(false);
const targetFilesRefreshKey = ref(0);
const transferResult = ref({
  renamed: 0,
  renameSkipped: 0,
  strmGenerated: 0,
  strmMatched: false,
  strmError: "",
  scrapeStarted: false,
  scrapeError: "",
  embyRefreshed: false,
  embyScheduled: false,
  embyError: "",
});

const scrapeAvailable = computed(() => scrapeTasks.value.length > 0);

const embyAvailable = computed(() => embyConfigs.value.length > 0);
const embyConfigOptions = computed(() =>
  embyConfigs.value.map((config) => ({
    value: config.id,
    label: config.name || config.emby_url || "未命名配置",
  })),
);
const embyLibraryOptions = computed(() =>
  embyLibraries.value.map((library) => ({ value: library.id, label: library.name || library.id })),
);
const embyModeOptions = [
  { value: "global", label: "全局扫描" },
  { value: "library", label: "指定媒体库扫描" },
];
// 未配置 Emby、未选配置，或库扫描模式未选媒体库时不刷库。
const embyEffective = computed(
  () =>
    embyRefresh.value &&
    embyAvailable.value &&
    !!embyConfigId.value &&
    (embyMode.value !== "library" || !!embyLibraryId.value),
);

function ensureEmbyConfig() {
  // 配置列表还没加载时保留已保存的配置，等加载完成再兜底。
  if (!embyConfigsLoaded.value) return;
  const current = embyConfigId.value;
  if (current && embyConfigs.value.some((config) => config.id === current)) return;
  embyConfigId.value = embyConfigs.value[0]?.id || "";
}

async function loadEmbyConfigs() {
  if (embyConfigsLoading.value) return;
  embyConfigsLoading.value = true;
  embyError.value = "";
  try {
    const state = await fetchEmbyConfigs();
    embyConfigs.value = Array.isArray(state?.items) ? state.items : [];
    embyConfigsLoaded.value = true;
    ensureEmbyConfig();
    if (embyMode.value === "library") void loadEmbyLibraries();
  } catch (error) {
    embyConfigs.value = [];
    embyConfigsLoaded.value = true;
    embyError.value = getApiErrorMessage(error, "加载 Emby/Jellyfin 配置失败");
  } finally {
    embyConfigsLoading.value = false;
  }
}

async function loadEmbyLibraries() {
  if (embyLibrariesLoading.value) return;
  const configId = embyConfigId.value;
  if (!configId) {
    embyLibraries.value = [];
    embyLibraryId.value = "";
    return;
  }
  embyLibrariesLoading.value = true;
  embyError.value = "";
  try {
    const list = await fetchEmbyLibraries(configId);
    if (embyConfigId.value !== configId) return;
    embyLibraries.value = Array.isArray(list) ? list : [];
    if (!embyLibraries.value.some((library) => library.id === embyLibraryId.value)) {
      embyLibraryId.value = embyLibraries.value[0]?.id || "";
    }
  } catch (error) {
    embyLibraries.value = [];
    embyLibraryId.value = "";
    embyError.value = getApiErrorMessage(error, "加载 Emby/Jellyfin 媒体库失败");
  } finally {
    embyLibrariesLoading.value = false;
  }
}

watch(embyConfigId, () => {
  if (restoringEmby) return;
  embyLibraries.value = [];
  embyLibraryId.value = "";
  if (embyMode.value === "library") void loadEmbyLibraries();
});

watch(embyMode, (mode) => {
  if (restoringEmby) return;
  if (mode === "library") {
    void loadEmbyLibraries();
  } else {
    embyLibraries.value = [];
    embyLibraryId.value = "";
  }
});

function stopScrapePolling() {
  if (scrapePollTimer !== undefined) {
    window.clearTimeout(scrapePollTimer);
    scrapePollTimer = undefined;
  }
}

// 轮询刮削进度；只关心本次触发的 STRM 任务，完成后保留最终结果用于展示。
async function pollScrapeProgress(taskId: number) {
  stopScrapePolling();
  if (!taskId || scrapeWatchId !== taskId) return;
  try {
    const progress = await fetchStrmScrapeProgress();
    if (scrapeWatchId !== taskId) return;
    if (Number(progress.strm_task_id) === taskId) {
      scrapeProgress.value = progress;
      if (!progress.running) {
        scrapeWatchId = 0;
        return;
      }
    }
  } catch {
    // 进度只是辅助展示，失败后继续重试。
  }
  if (scrapeWatchId !== taskId) return;
  if (Date.now() > scrapePollDeadline) {
    scrapeWatchId = 0;
    return;
  }
  scrapePollTimer = window.setTimeout(() => void pollScrapeProgress(taskId), 2000);
}

function watchScrapeProgress(taskId: number) {
  if (!taskId) return;
  scrapeWatchId = taskId;
  scrapePollDeadline = Date.now() + 30 * 60 * 1000;
  void pollScrapeProgress(taskId);
}

const scrapeStatus = computed(() => {
  const progress = scrapeProgress.value;
  const taskId = Number(scrapeTaskId.value || 0);
  if (!progress || !taskId || Number(progress.strm_task_id) !== taskId) return null;
  const total = Number(progress.total || 0);
  const done = Number(progress.done || 0);
  if (progress.running) {
    const percent = total > 0 ? Math.min(100, Math.round((done * 100) / total)) : 0;
    return {
      running: true,
      failed: false,
      percent,
      text: total > 0 ? `刮削中 ${done}/${total}` : "刮削准备中…",
      message: progress.message || "",
    };
  }
  if (progress.error) {
    return { running: false, failed: true, percent: 100, text: `刮削失败：${progress.error}`, message: "" };
  }
  const parts: string[] = [];
  const success = Math.max(0, done - Number(progress.skipped || 0) - Number(progress.failed || 0));
  if (success) parts.push(`成功 ${success}`);
  if (progress.skipped) parts.push(`跳过 ${progress.skipped}`);
  if (progress.failed) parts.push(`失败 ${progress.failed}`);
  return {
    running: false,
    failed: false,
    percent: 100,
    text: parts.length ? `刮削完成：${parts.join("，")}` : "刮削完成",
    message: "",
  };
});

const scrapeTaskValue = computed({
  get: () => (scrapeTaskId.value ? String(scrapeTaskId.value) : ""),
  set: (value: string | number | boolean) => {
    const text = String(value ?? "");
    scrapeTaskId.value = text ? Number(text) : null;
  },
});

const scrapeTaskOptions = computed(() =>
  scrapeTasks.value.map((task) => ({
    value: String(task.id),
    label: task.name || `任务 #${task.id}`,
  })),
);

function ensureScrapeTask() {
  const current = scrapeTaskId.value;
  if (current && scrapeTasks.value.some((task) => Number(task.id) === current)) return;
  const first = scrapeTasks.value[0];
  scrapeTaskId.value = first ? Number(first.id) : null;
}

async function loadScrapeTasks() {
  if (scrapeTasksLoading.value) return;
  scrapeTasksLoading.value = true;
  scrapeTasksError.value = "";
  try {
    const list = await fetchStrmTasks();
    scrapeTasks.value = Array.isArray(list) ? list : [];
    ensureScrapeTask();
  } catch (error) {
    scrapeTasks.value = [];
    scrapeTasksError.value = getApiErrorMessage(error, "加载 STRM 任务失败");
  } finally {
    scrapeTasksLoading.value = false;
  }
}

// 生成 STRM 打开时，刮削元数据跟随打开（用户仍可再单独关闭）。
watch(autoStrm, (on) => {
  if (on) autoScrape.value = true;
});
// 入库阶段进度：由后端记录，前端轮询展示「入库中 → 重命名 → 生成 STRM」。
const transferPhase = ref<GuangyaShareTransferPhase>("");
let progressTimer: number | undefined;
let progressIdRef = "";

interface PendingTransfer {
  provider: Provider;
  target: string;
  count: number;
  renames: RenamePreviewRow[];
  files: { fileId: string; originalName: string; newName: string; isDir: boolean }[];
  browsePath: GuangyaShareBrowseCrumb[];
  renameReplace: {
    enabled: boolean;
    find: string;
    replaceWith: string;
    rules: { find: string; replaceWith: string }[];
  };
  autoStrm: boolean;
  autoScrape: boolean;
  scrapeTaskId: number;
  refreshEmby: boolean;
  embyConfigId: string;
  embyMode: "global" | "library";
  embyLibraryId: string;
  unchangedCount: number;
  segments: string[];
  base: DestinationBase;
  title: string;
  season: number;
  series: boolean;
  quarkFavoriteId?: number;
  quarkAccountId?: number;
  quarkSourceParentId?: string;
  quarkSourceFolderParentId?: string;
  quarkSourceFolderName?: string;
}

const quickKinds: { key: GuangyaShareKind; label: string; icon: string }[] = [
  { key: "video", label: "视频", icon: "film" },
  { key: "image", label: "图片", icon: "image" },
  { key: "subtitle", label: "字幕", icon: "closed-captioning-regular" },
  { key: "document", label: "文档", icon: "file-lines" },
  { key: "other", label: "其他", icon: "file" },
];

function isQuickKind(item: GuangyaShareItem, kind: GuangyaShareKind) {
  if (item.is_dir) return false;
  if (kind === "other") return !QUICK_FILE_KINDS.has(item.kind);
  return item.kind === kind;
}

// 当前目录是否已被上层选中的文件夹覆盖；覆盖后子项默认“已包含”，不允许再单独勾选。
const ancestorSelected = computed(
  () =>
    provider.value === "guangya" &&
    pathStack.value.slice(1).some((crumb) => selectedIds.value.has(crumb.id)),
);

function scrollCrumbsToEnd() {
  void nextTick(() => {
    const el = crumbListRef.value;
    if (el) el.scrollLeft = el.scrollWidth;
  });
}

function setFolderLoading(id: string, loading: boolean) {
  const next = { ...loadingFolders.value };
  if (loading) next[id] = true;
  else delete next[id];
  loadingFolders.value = next;
}

function setFolderError(id: string, message: string) {
  const next = { ...folderErrors.value };
  if (message) next[id] = message;
  else delete next[id];
  folderErrors.value = next;
}

function addSelection(item: GuangyaShareItem, ids: Set<string>, map: Map<string, GuangyaShareItem>) {
  ids.add(item.id);
  map.set(item.id, item);
  if (!item.is_dir) return;
  const prefix = item.path.endsWith("/") ? item.path : `${item.path}/`;
  for (const [id, selected] of [...map]) {
    if (id !== item.id && selected.path.startsWith(prefix)) {
      ids.delete(id);
      map.delete(id);
    }
  }
}

function isAllSelected(list: GuangyaShareItem[]) {
  const selectable = list.filter(
    (item) => !isProcessed(item) && (provider.value === "guangya" || !item.is_dir),
  );
  return selectable.length > 0 && selectable.every((item) => selectedIds.value.has(item.id));
}

function selectRange(endIndex: number) {
  const anchor = anchorIndex.value;
  const list = sortedItems.value;
  if (anchor < 0 || anchor >= list.length || endIndex < 0 || endIndex >= list.length) return false;
  const start = Math.min(anchor, endIndex);
  const end = Math.max(anchor, endIndex);
  const nextIds = new Set(selectedIds.value);
  const nextMap = new Map(selectedMap.value);
  for (let index = start; index <= end; index++) {
    const item = list[index];
    if (isProcessed(item) || (provider.value === "quark" && item.is_dir)) continue;
    addSelection(item, nextIds, nextMap);
  }
  selectedIds.value = nextIds;
  selectedMap.value = nextMap;
  return true;
}

function toggleSelect(item: GuangyaShareItem, event?: MouseEvent, fromCheckbox = false) {
  if (ancestorSelected.value) return;
  if (provider.value === "quark" && item.is_dir) return;
  // 已入库文件只允许通过复选框重新选中，点击整行不触发。
  if (isProcessed(item) && !fromCheckbox) return;
  const index = sortedItems.value.findIndex((entry) => entry.id === item.id);
  // Shift + 点击：从上次点击处连选到当前位置，锚点保持不变便于继续扩展。
  if (event?.shiftKey && selectRange(index)) return;
  const nextIds = new Set(selectedIds.value);
  const nextMap = new Map(selectedMap.value);
  if (nextIds.has(item.id)) {
    nextIds.delete(item.id);
    nextMap.delete(item.id);
  } else {
    addSelection(item, nextIds, nextMap);
  }
  selectedIds.value = nextIds;
  selectedMap.value = nextMap;
  anchorIndex.value = index;
}

const kindCounts = computed(() => {
  const counts: Record<string, number> = {};
  for (const item of sourceItems.value) {
    if (item.is_dir || isProcessed(item)) continue;
    if (QUICK_FILE_KINDS.has(item.kind)) counts[item.kind] = (counts[item.kind] ?? 0) + 1;
    else counts.other = (counts.other ?? 0) + 1;
  }
  return counts;
});

function quickSelect(kind: GuangyaShareKind) {
  if (ancestorSelected.value) {
    toast.info("当前目录已包含在所选文件夹中");
    return;
  }
  const candidates = sourceItems.value.filter((item) => !isProcessed(item) && isQuickKind(item, kind));
  if (!candidates.length) {
    toast.info(`当前目录没有${kindLabels[kind]}文件`);
    return;
  }
  const remove = isAllSelected(candidates);
  const nextIds = new Set(selectedIds.value);
  const nextMap = new Map(selectedMap.value);
  for (const item of candidates) {
    if (remove) {
      nextIds.delete(item.id);
      nextMap.delete(item.id);
    } else {
      addSelection(item, nextIds, nextMap);
    }
  }
  selectedIds.value = nextIds;
  selectedMap.value = nextMap;
}

function selectAllCurrent() {
  if (ancestorSelected.value) return;
  const list = sourceItems.value;
  if (!list.length) return;
  const remove = isAllSelected(list);
  const nextIds = new Set(selectedIds.value);
  const nextMap = new Map(selectedMap.value);
  for (const item of list) {
    if (isProcessed(item)) continue;
    if (provider.value === "quark" && item.is_dir) continue;
    if (remove) {
      nextIds.delete(item.id);
      nextMap.delete(item.id);
    } else {
      addSelection(item, nextIds, nextMap);
    }
  }
  selectedIds.value = nextIds;
  selectedMap.value = nextMap;
}

function clearSelection() {
  selectedIds.value = new Set();
  selectedMap.value = new Map();
  anchorIndex.value = -1;
}

const filteredItems = computed(() => {
  const query = keyword.value.trim().toLowerCase();
  if (!query) return sourceItems.value;
  return sourceItems.value.filter((item) => item.name.toLowerCase().includes(query));
});

const sortedItems = computed(() => {
  // 已入库 / 已丢弃的文件始终排在最后；其余项按当前排序规则排列。
  return [...filteredItems.value].sort((a, b) => {
    const processedDiff = Number(isProcessed(a)) - Number(isProcessed(b));
    if (processedDiff !== 0) return processedDiff;
    if (!sortKey.value) return 0;
    const direction = sortDir.value === "asc" ? 1 : -1;
    let diff = 0;
    switch (sortKey.value) {
      case "time":
        diff = (Date.parse(a.mod_time ?? "") || 0) - (Date.parse(b.mod_time ?? "") || 0);
        break;
      case "size":
        diff = a.size - b.size;
        break;
      case "kind":
        diff = a.kind.localeCompare(b.kind);
        break;
      default:
        diff = a.name.localeCompare(b.name, "zh-CN", { numeric: true, sensitivity: "base" });
    }
    if (diff === 0) diff = a.name.localeCompare(b.name, "zh-CN", { numeric: true });
    return diff * direction;
  });
});

const totalPages = computed(() => Math.max(1, Math.ceil(sortedItems.value.length / pageSize)));
const pageItems = computed(() => sortedItems.value.slice((page.value - 1) * pageSize, page.value * pageSize));

watch([keyword, sortKey, sortDir, currentFolderID], () => {
  page.value = 1;
  anchorIndex.value = -1;
});
watch(totalPages, (total) => {
  if (page.value > total) page.value = total;
});

const selectedItems = computed(() => [...selectedMap.value.values()]);
const selectedFolderCount = computed(() => selectedItems.value.filter((item) => item.is_dir).length);
const selectedFiles = computed(() => selectedItems.value.filter((item) => !item.is_dir));
const selectedFileCount = computed(() => selectedFiles.value.length);

// 表头复选框：只统计当前目录里可批量选择的项（已入库文件需单独勾选）。
const selectableCurrentItems = computed(() =>
  sourceItems.value.filter(
    (item) => !isProcessed(item) && (provider.value === "guangya" || !item.is_dir),
  ),
);
const allCurrentSelected = computed(
  () =>
    selectableCurrentItems.value.length > 0 &&
    selectableCurrentItems.value.every((item) => selectedIds.value.has(item.id)),
);
const someCurrentSelected = computed(() =>
  selectableCurrentItems.value.some((item) => selectedIds.value.has(item.id)),
);

// 已入库文件：按 file_id 置灰，并显示持久化的新名称。
const processedMap = computed(() => {
  const map = new Map<string, GuangyaShareTransferredFile>();
  for (const file of result.value?.transferred_files ?? []) {
    if (file?.file_id) map.set(file.file_id, file);
  }
  return map;
});

function isProcessed(item: GuangyaShareItem) {
  return !item.is_dir && processedMap.value.has(item.id);
}

function processedStatus(item: GuangyaShareItem) {
  return processedMap.value.get(item.id)?.status === "discarded" ? "discarded" : "transferred";
}

function processedNewName(item: GuangyaShareItem) {
  const record = processedMap.value.get(item.id);
  if (!record) return "";
  return record.new_name && record.new_name !== item.name ? record.new_name : "";
}
const selectedFileSize = computed(() =>
  selectedItems.value.reduce((sum, item) => sum + (item.is_dir ? 0 : item.size), 0),
);

const destinationDisplay = computed(() => {
  if (!destination.value.accountId) return "";
  const path = destination.value.path && destination.value.path !== "/" ? destination.value.path : "/";
  return `${destination.value.accountName} ${path}`;
});

// 目录选择器的入参走 computed，避免模板里直接对 ref 做深层属性访问。
const pickerAccountId = computed(() => destination.value?.accountId || null);
const pickerInitialPath = computed(() => destination.value?.path || "/");
const pickerAccounts = computed(() => {
  if (provider.value === "quark") {
    const accountId = activeQuarkFavorite.value?.account_id;
    return quarkAccounts.value.filter((account) => account.id === accountId);
  }
  return guangyaAccounts.value;
});
const pickerTitle = computed(() =>
  provider.value === "quark" ? "选择当前夸克账号内的目录" : "选择入库目录",
);



function clampSeason(value: number) {
  if (String(value ?? "").trim() === "") return 1;
  const number = Number(value);
  if (!Number.isFinite(number)) return 1;
  return Math.max(0, Math.min(99, Math.trunc(number)));
}

function seasonFolderName(value: number) {
  return `Season ${String(clampSeason(value)).padStart(2, "0")}`;
}

// 分享标题常带全角括号，派生文件夹名时统一成半角，便于在媒体库里检索。
function toHalfWidthBrackets(value: string) {
  return value.replace(/（/g, "(").replace(/）/g, ")");
}

function sanitizeFolderName(value: string) {
  return String(value ?? "")
    .replace(/[\\/:*?"<>|]/g, " ")
    .replace(/[\x00-\x1f\x7f]/g, "")
    .replace(/\s+/g, " ")
    .trim()
    .slice(0, FOLDER_TITLE_MAX)
    .replace(/^\.+|\.+$/g, "")
    .trim();
}

function joinPath(base: string, segments: string[]) {
  const parts = [base, ...segments]
    .map((part) => String(part ?? "").replace(/^\/+|\/+$/g, ""))
    .filter(Boolean);
  return parts.length ? `/${parts.join("/")}` : "/";
}

const sanitizedFolderTitle = computed(() => sanitizeFolderName(folderTitle.value));
const seasonNumber = computed(() => clampSeason(season.value));

function stripTmdbTags(value: string) {
  return String(value ?? "")
    .replace(/\s*\{\s*tmdb\s*-\s*\d+\s*\}\s*/gi, " ")
    .replace(/\s+/g, " ")
    .trim();
}

function stripTrailingYear(value: string) {
  return String(value ?? "").replace(/\s*[（(]\s*\d{4}\s*[）)]\s*$/g, "").trim();
}

// TMDB 搜索名：取目标文件夹名，去掉末尾年份和 {tmdb-ID}。
const tmdbSearchTitle = computed(() => {
  let base = stripTmdbTags(sanitizedFolderTitle.value);
  let previous = "";
  while (base && base !== previous) {
    previous = base;
    base = stripTrailingYear(base);
  }
  return base;
});

const tmdbSelectedID = computed(() => {
  const match = sanitizedFolderTitle.value.match(/\{\s*tmdb\s*-\s*(\d+)\s*\}/i);
  return match?.[1] || "";
});

// #region buei:guangya-rename
// 前缀基名 U：剧集去掉“(年份)”与首尾空格；电影保留年份；两者都去掉 {tmdb-ID}。
const renamePrefixBase = computed(() => {
  let base = stripTmdbTags(sanitizedFolderTitle.value);
  if (isSeries.value) {
    base = base.replace(/\s*[（(]\s*\d{4}\s*[）)]\s*/g, " ").replace(/\s+/g, " ").trim();
  }
  return base;
});

function resetTmdbMatch() {
  tmdbSearchSeq += 1;
  tmdbResults.value = [];
  tmdbLoading.value = false;
  tmdbError.value = "";
  tmdbSearched.value = false;
}

function tmdbResultType(item: MediaOrganizeTmdbSearchHit): "movie" | "tv" {
  if (item.media_type === "movie") return "movie";
  if (item.media_type === "tv") return "tv";
  return isSeries.value ? "tv" : "movie";
}

function tmdbResultTypeLabel(item: MediaOrganizeTmdbSearchHit) {
  return tmdbResultType(item) === "tv" ? "剧集" : "电影";
}

function tmdbResultTitle(item: MediaOrganizeTmdbSearchHit) {
  return String(item.title || item.name || item.original_title || item.original_name || "未命名").trim();
}

function tmdbResultOriginal(item: MediaOrganizeTmdbSearchHit) {
  const title = tmdbResultTitle(item);
  const original = String(item.original_title || item.original_name || "").trim();
  return original && original.toLowerCase() !== title.toLowerCase() ? original : "";
}

function tmdbResultYear(item: MediaOrganizeTmdbSearchHit) {
  const type = tmdbResultType(item);
  const raw = type === "movie" ? item.release_date : item.first_air_date;
  const date = raw || item.release_date || item.first_air_date || "";
  return String(date).match(/^(\d{4})/)?.[1] || "";
}

function tmdbWebURL(item: MediaOrganizeTmdbSearchHit) {
  return `https://www.themoviedb.org/${tmdbResultType(item)}/${encodeURIComponent(String(item.id ?? ""))}`;
}

function isTmdbSelected(item: MediaOrganizeTmdbSearchHit) {
  return !!tmdbSelectedID.value && String(item.id ?? "") === tmdbSelectedID.value;
}

// 搜索结果按年份倒序；无年份的条目排在最后，同年保持接口原始顺序。
const sortedTmdbResults = computed(() =>
  [...tmdbResults.value].sort((a, b) => {
    const aYear = Number(tmdbResultYear(a)) || 0;
    const bYear = Number(tmdbResultYear(b)) || 0;
    return bYear - aYear;
  }),
);

async function searchTMDB() {
  const query = tmdbSearchTitle.value.trim();
  if (!query) {
    tmdbError.value = "请先填写目标文件夹";
    return;
  }
  if (tmdbLoading.value) return;
  const seq = ++tmdbSearchSeq;
  tmdbLoading.value = true;
  tmdbError.value = "";
  tmdbSearched.value = false;
  try {
    const results = await searchMediaOrganizeTmdb({
      query,
      media_type: isSeries.value ? "tv" : "movie",
    });
    if (seq !== tmdbSearchSeq) return;
    tmdbResults.value = results.filter(
      (item) => item && item.id !== undefined && item.id !== null && String(item.id).trim() !== "",
    );
    tmdbSearched.value = true;
  } catch (error) {
    if (seq !== tmdbSearchSeq) return;
    tmdbResults.value = [];
    const message = getApiErrorMessage(error, "TMDB 搜索失败");
    tmdbError.value = /TMDB API Key|未配置.*TMDB|TMDB.*未配置/i.test(message)
      ? "系统未配置 TMDB API Key，请先在「任务管理 / 目录整理」的 TMDB 设置中填写"
      : message;
  } finally {
    if (seq === tmdbSearchSeq) tmdbLoading.value = false;
  }
}

function selectTMDBResult(item: MediaOrganizeTmdbSearchHit) {
  const id = String(item.id ?? "").trim();
  if (!id) return;
  const base = tmdbResultTitle(item);
  if (!base || base === "未命名") {
    tmdbError.value = "该 TMDB 结果没有可用标题";
    return;
  }
  const year = tmdbResultYear(item);
  const suffix = `${year ? ` (${year})` : ""} {tmdb-${id}}`;
  const safeBase = base.slice(0, Math.max(1, FOLDER_TITLE_MAX - suffix.length)).trim();
  folderTitle.value = sanitizeFolderName(`${safeBase}${suffix}`);
  tmdbError.value = "";
}

watch(isSeries, resetTmdbMatch);

function splitFileName(name: string) {
  const index = name.lastIndexOf(".");
  if (index <= 0) return { stem: name, ext: "" };
  return { stem: name.slice(0, index), ext: name.slice(index) };
}

const PREFIX_CONNECTORS = [".", "-", "_", " "] as const;

function detectPrefixConnector(stem: string) {
  // 沿用原文件名中最先出现的连接符；没有则使用短横线。
  const match = stem.match(/[.\-_ ]/);
  return match ? match[0] : "-";
}

function connectorAfterPrefix(base: string, prefix: string) {
  if (!base.startsWith(prefix)) return "";
  const first = base.slice(prefix.length, prefix.length + 1);
  return (PREFIX_CONNECTORS as readonly string[]).includes(first) ? first : "";
}

function pad2(value: number) {
  const number = Math.trunc(Number(value));
  if (!Number.isFinite(number)) return "00";
  return String(Math.max(0, Math.min(99, number))).padStart(2, "0");
}

const renameStartEpisodeNumber = computed(() => {
  const number = Math.trunc(Number(renameStartEpisode.value));
  if (!Number.isFinite(number)) return 1;
  return Math.max(1, Math.min(99, number));
});

// 序号按当前目录的表格顺序依次递增；跨目录选中的文件追加在末尾并按路径排序。
const orderedSelectedFiles = computed(() => {
  const files: GuangyaShareItem[] = [];
  const seen = new Set<string>();
  for (const item of sortedItems.value) {
    if (item.is_dir || !selectedIds.value.has(item.id)) continue;
    files.push(item);
    seen.add(item.id);
  }
  const rest = selectedItems.value
    .filter((item) => !item.is_dir && !seen.has(item.id))
    .sort(
      (a, b) =>
        a.path.localeCompare(b.path, "zh-CN", { numeric: true }) ||
        a.name.localeCompare(b.name, "zh-CN", { numeric: true }),
    );
  return [...files, ...rest];
});

const episodeIndexByID = computed(() => {
  const map = new Map<string, number>();
  orderedSelectedFiles.value.forEach((item, index) => map.set(item.id, index));
  return map;
});

const renameRuleErrors = computed(() =>
  renameReplaceRules.value.map((rule) => {
    if (!rule.find) return "";
    try {
      new RegExp(rule.find, "g");
      return "";
    } catch (error) {
      return error instanceof Error ? error.message : "正则表达式无效";
    }
  }),
);

const renameReplaceError = computed(() => {
  if (!renameReplaceOn.value) return "";
  return renameRuleErrors.value.find(Boolean) ?? "";
});

// 启用替换时需要至少填一条“查找”。
const renameReplaceReady = computed(
  () => renameReplaceOn.value && renameReplaceRules.value.some((rule) => !!rule.find),
);

const renameEpisodeActive = computed(() => renameEpisodeOn.value && isSeries.value);

const renameActive = computed(
  () =>
    (renamePrefixOn.value && !!renamePrefixBase.value) ||
    renameEpisodeActive.value ||
    (renameReplaceReady.value && !renameReplaceError.value),
);

function renamedNameFor(item: GuangyaShareItem) {
  if (item.is_dir) return "";
  const { stem, ext } = splitFileName(item.name);
  let base = stem;
  const prefix = renamePrefixBase.value;
  if (renamePrefixOn.value && prefix && !base.startsWith(prefix)) {
    base = `${prefix}${detectPrefixConnector(stem)}${base}`;
  }
  if (renameEpisodeActive.value) {
    const token = `S${pad2(seasonNumber.value)}E${pad2(renameStartEpisodeNumber.value + (episodeIndexByID.value.get(item.id) ?? 0))}`;
    if (SEASON_EPISODE_RE.test(base)) {
      base = base.replace(SEASON_EPISODE_RE, token);
    } else {
      const sep = renamePrefixOn.value && prefix ? connectorAfterPrefix(base, prefix) : "";
      if (sep) {
        base = `${prefix}${sep}${token}${sep}${base.slice(prefix.length + 1)}`;
      } else {
        base = `${token}-${base}`;
      }
    }
  }
  if (renameReplaceOn.value && !renameReplaceError.value) {
    for (const rule of renameReplaceRules.value) {
      if (!rule.find) continue;
      try {
        base = base.replace(new RegExp(rule.find, "g"), rule.replaceWith);
      } catch {
        // 正则已在上方校验，这里只兜底。
      }
    }
  }
  // 规则可能把名称整段删空，这种情况下保留原文件名更安全。
  base = base.replace(/^[-\s]+|[-\s]+$/g, "");
  if (!base) return item.name;
  return `${base}${ext}`;
}

function displayNameParts(name: string, isDir: boolean) {
  return isDir ? { stem: name, ext: "" } : splitFileName(name);
}

function itemNewName(item: GuangyaShareItem) {
  return renamedDisplay(item) || processedNewName(item);
}

function renamedDisplay(item: GuangyaShareItem) {
  if (!renameActive.value || item.is_dir || !selectedIds.value.has(item.id)) return "";
  const name = renamedNameFor(item);
  return name && name !== item.name ? name : "";
}

const renamePreviewRows = computed<RenamePreviewRow[]>(() =>
  orderedSelectedFiles.value.map((item) => ({
    oldName: item.name,
    newName: renamedNameFor(item),
    path: item.path,
  })),
);

function applyRenameReplace(rule?: GuangyaShareRenameReplace | null) {
  if (!rule) return;
  const rawRules =
    rule.rules?.length
      ? rule.rules
      : rule.find
        ? [{ find: rule.find, replace_with: rule.replace_with }]
        : [];
  const rules = rawRules
    .filter((item) => !!item.find)
    .map((item) => ({ find: item.find, replaceWith: item.replace_with || "" }));
  renameReplaceRules.value = rules.length ? rules : [{ find: "", replaceWith: "" }];
  renameReplaceOn.value = !!rule.enabled && rules.length > 0;
}

function resetRenameRules() {
  renamePrefixOn.value = false;
  renamePrefixTouched.value = false;
  renameEpisodeOn.value = false;
  renameEpisodeTouched.value = false;
  renameReplaceOn.value = false;
  renameReplaceRules.value = [{ find: "", replaceWith: "" }];
  renameStartEpisode.value = 1;
}

// 第一个选中文件的文件名（不含扩展名），方便在“替换文本”里直接取用。
function firstSelectedFileName(): string {
  const item = orderedSelectedFiles.value[0] ?? selectedItems.value.find((it) => !it.is_dir);
  if (!item) return "";
  return splitFileName(item.name).stem || item.name;
}

function onRenameReplaceToggle() {
  if (!renameReplaceOn.value) return;
  if (!renameReplaceRules.value.length) {
    renameReplaceRules.value = [{ find: "", replaceWith: "" }];
  }
  const first = renameReplaceRules.value[0];
  if (!first.find) first.find = firstSelectedFileName();
}

function addRenameReplaceRule() {
  renameReplaceRules.value.push({ find: firstSelectedFileName(), replaceWith: "" });
}

function removeRenameReplaceRule(index: number) {
  if (renameReplaceRules.value.length <= 1) {
    renameReplaceRules.value = [{ find: "", replaceWith: "" }];
    return;
  }
  renameReplaceRules.value.splice(index, 1);
}

// 选中的文件缺前缀/缺 SxxEnn 时自动勾选对应规则；用户手动改过之后不再覆盖。
function applyAutoRenameRules() {
  const files = selectedItems.value.filter((item) => !item.is_dir);
  if (!files.length) return;
  const prefix = renamePrefixBase.value;
  if (!renamePrefixTouched.value) {
    renamePrefixOn.value = !!prefix && files.some((item) => !splitFileName(item.name).stem.startsWith(prefix));
  }
  if (!renameEpisodeTouched.value) {
    renameEpisodeOn.value = isSeries.value && files.some((item) => !SEASON_EPISODE_RE.test(item.name));
  }
}

watch([selectedItems, isSeries, renamePrefixBase], applyAutoRenameRules, { flush: "post" });
// #endregion buei:guangya-rename
const targetSegments = computed(() => {
  if (!sanitizedFolderTitle.value) return [];
  return isSeries.value ? [sanitizedFolderTitle.value, seasonFolderName(seasonNumber.value)] : [sanitizedFolderTitle.value];
});
const targetPath = computed(() => joinPath(destination.value.path, targetSegments.value));
const targetPathDisplay = computed(() => {
  const account = destination.value.accountName || "未选择账号";
  return `${account} · ${targetPath.value}`;
});

function destinationBaseFromAPI(
  accountID: number,
  parentID: string,
  path: string,
  dirs?: string[],
): DestinationBase | null {
  if (!Number.isFinite(accountID) || accountID <= 0) return null;
  return {
    accountId: accountID,
    accountName: "",
    parentId: parentID || "",
    path: path || "/",
    dirs: Array.isArray(dirs) ? [...dirs] : [],
  };
}

function applyDestinationBase(base: DestinationBase | null) {
  if (!base) return false;
  const accounts = provider.value === "quark" ? quarkAccounts.value : guangyaAccounts.value;
  const account = accounts.find((item) => item.id === base.accountId);
  if (!account) return false;
  destination.value = {
    accountId: account.id,
    accountName: account.name,
    parentId: base.parentId || "",
    path: base.path || "/",
    dirs: [...base.dirs],
  };
  return true;
}

function applyDestinationForShare(res: GuangyaShareParseResult) {
  const draft = res.last_destination ?? null;
  const draftBase = draft
    ? destinationBaseFromAPI(draft.account_id, draft.parent_id, draft.base_path, draft.base_dirs)
    : null;
  const lastBase = res.last_base
    ? destinationBaseFromAPI(
        res.last_base.account_id,
        res.last_base.parent_id,
        res.last_base.path,
        res.last_base.dirs,
      )
    : null;
  if (!applyDestinationBase(draftBase) && !applyDestinationBase(lastBase)) {
    ensureDestination();
  }
  folderTitle.value = toHalfWidthBrackets(draft?.title || res.title || res.items[0]?.name || "").trim();
  isSeries.value = draft ? draft.is_series : true;
  season.value = draft ? draft.season : 1;
  // 未保存过预设时保持默认开启。
  autoStrm.value = draft ? draft.auto_strm !== false : true;
  autoScrape.value = draft ? draft.auto_scrape !== false : true;
  scrapeTaskId.value = draft && draft.auto_scrape_task_id > 0 ? Number(draft.auto_scrape_task_id) : null;
  // 任务列表可能还没加载完成，加载完成后 loadScrapeTasks 会再兜底选中。
  if (scrapeTasks.value.length) ensureScrapeTask();
  // Emby 刷库预设：开关 / 配置 / 范围 / 媒体库。
  restoringEmby = true;
  embyRefresh.value = draft ? draft.refresh_emby !== false : true;
  embyConfigId.value = draft?.emby_config_id || "";
  embyMode.value = draft?.emby_mode === "library" ? "library" : "global";
  embyLibraryId.value = draft?.emby_library_id || "";
  if (embyConfigsLoaded.value) ensureEmbyConfig();
  if (embyMode.value === "library" && embyConfigId.value) void loadEmbyLibraries();
  void nextTick(() => {
    restoringEmby = false;
  });
  resetTmdbMatch();
}

function normalizeFolderTitle() {
  const previousSearchTitle = tmdbSearchTitle.value;
  folderTitle.value = sanitizeFolderName(toHalfWidthBrackets(folderTitle.value));
  if (tmdbSearchTitle.value !== previousSearchTitle) resetTmdbMatch();
}

function onFolderTitleInput(event: Event) {
  const input = event.target as HTMLInputElement;
  const inputEvent = event as InputEvent;
  // 中文输入法组合期间不要改写输入框，等 compositionend 后的 input 再统一清洗。
  if (inputEvent.isComposing) return;
  const raw = input.value;
  const next = toHalfWidthBrackets(raw)
    .replace(/^\s+/, "")
    .replace(/\s+$/, "");
  const previousSearchTitle = tmdbSearchTitle.value;
  folderTitle.value = next;
  if (tmdbSearchTitle.value !== previousSearchTitle) resetTmdbMatch();
  if (raw !== next) {
    const cursor = input.selectionStart ?? raw.length;
    const leadingRemoved = raw.length - raw.replace(/^\s+/, "").length;
    const nextCursor = Math.max(0, Math.min(next.length, cursor - leadingRemoved));
    input.value = next;
    input.setSelectionRange(nextCursor, nextCursor);
  }
}

function normalizeSeasonInput() {
  season.value = seasonNumber.value;
}

const interactionBusy = computed(
  () =>
    transferring.value ||
    discarding.value ||
    quarkFlattening.value ||
    quarkDeleting.value,
);

const canTransfer = computed(() => {
  if (provider.value === "quark") {
    return (
      !!activeQuarkFavorite.value &&
      selectedFileCount.value > 0 &&
      destination.value.accountId === activeQuarkFavorite.value.account_id &&
      !!sanitizedFolderTitle.value &&
      !interactionBusy.value
    );
  }
  return (
    !!result.value &&
    !result.value.need_password &&
    selectedItems.value.length > 0 &&
    destination.value.accountId > 0 &&
    !!sanitizedFolderTitle.value &&
    !interactionBusy.value
  );
});

const canDiscard = computed(
  () =>
    provider.value === "guangya" &&
    !!result.value &&
    !result.value.need_password &&
    selectedFileCount.value > 0 &&
    !interactionBusy.value,
);

function selectDestination(accountID: number, accountName: string) {
  destination.value = { accountId: accountID, accountName, parentId: "", path: "/", dirs: [] };
}

function ensureDestination() {
  if (provider.value === "quark") {
    const favorite = activeQuarkFavorite.value;
    if (!favorite) return;
    if (destination.value.accountId === favorite.account_id) return;
    selectDestination(favorite.account_id, favorite.accountName);
    return;
  }
  if (guangyaAccounts.value.some((account) => account.id === destination.value.accountId)) return;
  const first = guangyaAccounts.value[0];
  if (first) {
    selectDestination(first.id, first.name);
  } else {
    destination.value = { accountId: 0, accountName: "", parentId: "", path: "/", dirs: [] };
  }
}

function openPicker() {
  if (interactionBusy.value) return;
  if (provider.value === "quark") {
    if (!activeQuarkFavorite.value) {
      toast.info("请先从左侧选择夸克收藏");
      return;
    }
    pickerOpen.value = true;
    return;
  }
  if (!guangyaAccounts.value.length) {
    toast.info("请先在「存储管理」添加光鸭云盘账号");
    return;
  }
  pickerOpen.value = true;
}

function onFolderResolved(payload: {
  accountId: number;
  accountName: string;
  parentId: string;
  path: string;
  dirs?: string[];
}) {
  if (
    provider.value === "quark" &&
    payload.accountId !== activeQuarkFavorite.value?.account_id
  ) {
    toast.error("夸克入库只能在收藏所属账号内进行");
    return;
  }
  const accountName =
    provider.value === "quark"
      ? activeQuarkFavorite.value?.accountName || payload.accountName
      : payload.accountName;
  destination.value = {
    accountId: payload.accountId,
    accountName,
    parentId: payload.parentId,
    path: payload.path || "/",
    dirs: Array.isArray(payload.dirs) ? [...payload.dirs] : [],
  };
  pickerOpen.value = false;
}

async function loadSavedLinks() {
  savedLoading.value = true;
  savedError.value = "";
  try {
    savedLinks.value = (await bueiGuangyaShareApi.saved()).items;
  } catch (error) {
    savedError.value = getApiErrorMessage(error, "加载光鸭收藏失败");
  } finally {
    savedLoading.value = false;
  }
}

async function loadQuarkFavorites() {
  quarkFavoritesLoading.value = true;
  quarkFavoritesError.value = "";
  const entries: QuarkFavoriteEntry[] = [];
  const failedAccounts: string[] = [];
  await Promise.all(
    quarkAccounts.value.map(async (account) => {
      try {
        const res = await bueiQuarkIngestApi.favorites(account.id);
        for (const favorite of res.items) {
          entries.push({ ...favorite, accountName: account.name });
        }
      } catch {
        failedAccounts.push(account.name);
      }
    }),
  );
  quarkFavorites.value = entries;
  if (failedAccounts.length) {
    quarkFavoritesError.value = `部分夸克账号收藏加载失败：${failedAccounts.join("、")}`;
  }
  if (activeQuarkFavorite.value) {
    const current = entries.find((item) => item.id === activeQuarkFavorite.value?.id);
    if (current) activeQuarkFavorite.value = current;
  }
  quarkFavoritesLoading.value = false;
}

async function loadAllFavorites() {
  await Promise.all([loadSavedLinks(), loadQuarkFavorites()]);
}

async function selectFavorite(favorite: UnifiedFavorite) {
  if (interactionBusy.value) return;
  activeFavoriteKey.value = favorite.key;
  if (favorite.provider === "guangya") {
    const link = savedLinks.value.find((item) => item.id === favorite.id);
    if (link) await selectGuangyaFavorite(link);
    return;
  }
  const quarkFavorite = quarkFavorites.value.find((item) => item.id === favorite.id);
  if (quarkFavorite) await selectQuarkFavorite(quarkFavorite);
}

function resetQuarkSource() {
  quarkSourceSeq += 1;
  activeQuarkFavorite.value = null;
  quarkSourceItems.value = [];
  quarkSourceLoading.value = false;
  quarkSourceError.value = "";
}

async function selectGuangyaFavorite(link: GuangyaShareSavedLink) {
  resetQuarkSource();
  provider.value = "guangya";
  activeFavoriteKey.value = `g:${link.id}`;
  url.value = (link.url || link.share_id).trim();
  password.value = link.password || "";
  await parseShare(false, link.id);
}

function applyQuarkPreset(preset: QuarkIngestPreset | null, favorite: QuarkFavoriteEntry) {
  destination.value = {
    accountId: favorite.account_id,
    accountName: favorite.accountName,
    parentId: preset?.parent_id || "",
    path: preset?.base_path || "/",
    dirs: Array.isArray(preset?.base_dirs) ? [...(preset?.base_dirs ?? [])] : [],
  };
  folderTitle.value = sanitizeFolderName(
    toHalfWidthBrackets(preset?.title || favorite.folder_name || ""),
  );
  isSeries.value = preset ? preset.is_series !== false : true;
  season.value = clampSeason(preset?.season ?? 1);
  autoStrm.value = preset ? preset.auto_strm !== false : true;
  autoScrape.value = preset ? preset.auto_scrape !== false : true;
  scrapeTaskId.value = preset && preset.auto_scrape_task_id > 0 ? Number(preset.auto_scrape_task_id) : null;
  restoringEmby = true;
  embyRefresh.value = preset ? preset.refresh_emby !== false : true;
  embyConfigId.value = preset?.emby_config_id || "";
  embyMode.value = preset?.emby_mode === "library" ? "library" : "global";
  embyLibraryId.value = preset?.emby_library_id || "";
  if (embyConfigsLoaded.value) ensureEmbyConfig();
  if (embyMode.value === "library" && embyConfigId.value) void loadEmbyLibraries();
  resetRenameRules();
  applyRenameReplace(preset?.rename_replace ?? null);
  if (scrapeTasks.value.length) ensureScrapeTask();
  void nextTick(() => {
    restoringEmby = false;
  });
  resetTmdbMatch();
}

async function selectQuarkFavorite(favorite: QuarkFavoriteEntry) {
  if (interactionBusy.value) return;
  parseAbort?.abort();
  parseAbort = null;
  parsing.value = false;
  parseError.value = "";
  provider.value = "quark";
  activeFavoriteKey.value = `q:${favorite.id}`;
  activeQuarkFavorite.value = favorite;
  result.value = null;
  parsedURL.value = "";
  password.value = "";
  passwordPrompt.value = false;
  folderGeneration += 1;
  folderCache.value = {};
  loadingFolders.value = {};
  folderErrors.value = {};
  pathStack.value = [{ id: "", name: "全部文件", path: "" }];
  selectedIds.value = new Set();
  selectedMap.value = new Map();
  anchorIndex.value = -1;
  keyword.value = "";
  page.value = 1;
  sortKey.value = "name";
  sortDir.value = "asc";
  resetTmdbMatch();
  resetRenameRules();
  destination.value = {
    accountId: favorite.account_id,
    accountName: favorite.accountName,
    parentId: "",
    path: "/",
    dirs: [],
  };
  folderTitle.value = sanitizeFolderName(toHalfWidthBrackets(favorite.folder_name));
  isSeries.value = true;
  season.value = 1;
  autoStrm.value = true;
  autoScrape.value = true;
  scrapeTaskId.value = null;
  embyRefresh.value = true;
  embyMode.value = "global";
  embyLibraryId.value = "";
  embyLibraries.value = [];

  const seq = ++quarkSourceSeq;
  quarkSourceLoading.value = true;
  quarkSourceError.value = "";
  quarkSourceItems.value = [];
  try {
    const [preset, listing] = await Promise.all([
      bueiQuarkIngestApi.preset(favorite.account_id).catch(() => null),
      filesApi.list(favorite.account_id, favorite.folder_id, { forceRefresh: true }),
    ]);
    if (seq !== quarkSourceSeq || activeQuarkFavorite.value?.id !== favorite.id) return;
    applyQuarkPreset(preset, favorite);
    quarkSourceItems.value = listing.items;
  } catch (error) {
    if (seq !== quarkSourceSeq) return;
    quarkSourceItems.value = [];
    quarkSourceError.value = getApiErrorMessage(error, "读取夸克收藏文件夹失败");
  } finally {
    if (seq === quarkSourceSeq) quarkSourceLoading.value = false;
  }
}

async function refreshQuarkFavoriteFiles() {
  const favorite = activeQuarkFavorite.value;
  if (!favorite) return;
  const seq = ++quarkSourceSeq;
  quarkSourceLoading.value = true;
  quarkSourceError.value = "";
  try {
    const res = await filesApi.list(favorite.account_id, favorite.folder_id, {
      forceRefresh: true,
    });
    if (seq !== quarkSourceSeq || activeQuarkFavorite.value?.id !== favorite.id) return;
    quarkSourceItems.value = res.items;
    const valid = new Set(res.items.filter((item) => !item.is_dir).map((item) => String(item.id)));
    const nextIds = new Set(selectedIds.value);
    const nextMap = new Map(selectedMap.value);
    for (const id of [...nextIds]) {
      if (!valid.has(id)) {
        nextIds.delete(id);
        nextMap.delete(id);
      }
    }
    selectedIds.value = nextIds;
    selectedMap.value = nextMap;
  } catch (error) {
    if (seq !== quarkSourceSeq) return;
    quarkSourceItems.value = [];
    quarkSourceError.value = getApiErrorMessage(error, "读取夸克收藏文件夹失败");
  } finally {
    if (seq === quarkSourceSeq) quarkSourceLoading.value = false;
  }
}

async function removeFavorite(favorite: UnifiedFavorite) {
  if (interactionBusy.value) return;
  const isQuark = favorite.provider === "quark";
  const ok = await confirm({
    title: isQuark ? "删除收藏和文件夹？" : "取消收藏？",
    message: isQuark
      ? `确定删除收藏「${favorite.title}」吗？会同时删除夸克「来自：分享」中的收藏文件夹及其内容。`
      : `将移除「${favorite.title}」的收藏记录，不影响网盘文件和分享本身。`,
    confirmText: "删除",
    cancelText: "保留",
    danger: true,
  }).catch(() => false);
  if (!ok) return;
  try {
    if (isQuark) {
      const quarkFavorite = quarkFavorites.value.find((item) => item.id === favorite.id);
      if (!quarkFavorite) return;
      await bueiQuarkIngestApi.deleteFavorite(quarkFavorite.id, quarkFavorite.account_id);
      quarkFavorites.value = quarkFavorites.value.filter((item) => item.id !== favorite.id);
      if (activeFavoriteKey.value === favorite.key) {
        activeFavoriteKey.value = null;
        activeQuarkFavorite.value = null;
        quarkSourceItems.value = [];
        quarkSourceError.value = "";
      }
      toast.success("收藏及文件夹已删除");
      return;
    }
    await bueiGuangyaShareApi.deleteSaved(favorite.id);
    savedLinks.value = savedLinks.value.filter((item) => item.id !== favorite.id);
    if (activeFavoriteKey.value === favorite.key) {
      activeFavoriteKey.value = null;
      resetParse();
    }
    toast.success("已取消收藏");
  } catch (error) {
    toast.error(getApiErrorMessage(error, isQuark ? "删除收藏失败" : "取消收藏失败"));
  }
}

let favoriteInboxSeq = 0;

function resetFavoriteDialog() {
  favoriteEditing.value = null;
  favoriteLink.value = "";
  favoritePassword.value = "";
  favoriteProviderManual.value = "quark";
  favoriteError.value = "";
  favoriteQuarkAccountId.value = quarkAccounts.value[0]?.id ?? null;
  favoriteQuarkInbox.value = null;
  favoriteQuarkInboxAccountId.value = null;
  favoriteQuarkInboxLoading.value = false;
  favoriteQuarkInboxError.value = "";
  favoriteQuarkFolderId.value = "";
  favoriteQuarkParentFolderId.value = "";
  favoriteQuarkFolderName.value = "";
  favoriteFolderPickerOpen.value = false;
}

function openAddFavorite() {
  if (interactionBusy.value) return;
  resetFavoriteDialog();
  favoriteDialogOpen.value = true;
}

function closeFavoriteDialog() {
  if (favoriteSaving.value) return;
  favoriteDialogOpen.value = false;
  favoriteFolderPickerOpen.value = false;
}

function openEditQuarkFavorite(favorite: UnifiedFavorite) {
  if (interactionBusy.value || favorite.provider !== "quark") return;
  const entry = quarkFavorites.value.find((item) => item.id === favorite.id);
  if (!entry) return;
  resetFavoriteDialog();
  favoriteEditing.value = favorite;
  favoriteLink.value = entry.link;
  favoriteQuarkAccountId.value = entry.account_id;
  favoriteQuarkFolderId.value = entry.folder_id;
  favoriteQuarkParentFolderId.value = entry.parent_folder_id;
  favoriteQuarkFolderName.value = entry.folder_name;
  favoriteError.value = "";
  favoriteDialogOpen.value = true;
  void ensureFavoriteQuarkInbox();
}

function openEditGuangyaFavorite(favorite: UnifiedFavorite) {
  if (interactionBusy.value) return;
  const link = savedLinks.value.find((item) => item.id === favorite.id);
  if (!link) return;
  resetFavoriteDialog();
  favoriteEditing.value = favorite;
  favoriteLink.value = link.url || link.share_id;
  favoritePassword.value = link.password || "";
  favoriteError.value = "";
  favoriteDialogOpen.value = true;
}

function openEditFavorite(favorite: UnifiedFavorite) {
  if (favorite.provider === "quark") {
    openEditQuarkFavorite(favorite);
  } else {
    openEditGuangyaFavorite(favorite);
  }
}

async function ensureFavoriteQuarkInbox() {
  const accountId = favoriteQuarkAccountId.value;
  if (!accountId) {
    favoriteQuarkInbox.value = null;
    favoriteQuarkInboxError.value = "请先选择夸克账号";
    return;
  }
  if (
    favoriteQuarkInboxAccountId.value === accountId &&
    (favoriteQuarkInbox.value || favoriteQuarkInboxLoading.value)
  ) {
    // 已加载或正在加载同一账号的「来自：分享」，直接复用。
    return;
  }
  const seq = ++favoriteInboxSeq;
  favoriteQuarkInboxAccountId.value = accountId;
  favoriteQuarkInboxLoading.value = true;
  favoriteQuarkInboxError.value = "";
  try {
    const root = await filesApi.list(accountId, "", { forceRefresh: true });
    if (seq !== favoriteInboxSeq) return;
    const inbox = root.items.find((item) => item.is_dir && isQuarkShareInboxName(item.name));
    if (!inbox) {
      favoriteQuarkInbox.value = null;
      favoriteQuarkInboxError.value = "未找到「来自：分享」，请先在夸克 App 中转存分享内容";
      return;
    }
    favoriteQuarkInbox.value = inbox;
    favoriteQuarkInboxAccountId.value = accountId;
  } catch (error) {
    if (seq !== favoriteInboxSeq) return;
    favoriteQuarkInbox.value = null;
    favoriteQuarkInboxAccountId.value = null;
    favoriteQuarkInboxError.value = getApiErrorMessage(error, "读取夸克「来自：分享」失败");
  } finally {
    if (seq === favoriteInboxSeq) favoriteQuarkInboxLoading.value = false;
  }
}

function onFavoriteLinkInput(event?: Event) {
  favoriteError.value = "";
  if (favoriteEditing.value) return;
  const target = event?.target as HTMLInputElement | null;
  const value = target?.value ?? favoriteLink.value;
  if (value.trim() && detectShareLinkProvider(value) !== "guangya") void ensureFavoriteQuarkInbox();
}

function onFavoriteProviderChange(value: string | number | boolean) {
  if (value !== "quark") return;
  favoriteProviderManual.value = "quark";
  favoriteError.value = "";
  if (!favoriteEditing.value && favoriteDetectedProvider.value !== "guangya") {
    void ensureFavoriteQuarkInbox();
  }
}

function onFavoriteQuarkAccountChange(value: string | number | boolean) {
  favoriteQuarkAccountId.value = value ? Number(value) : null;
  favoriteQuarkInbox.value = null;
  favoriteQuarkInboxAccountId.value = null;
  favoriteQuarkInboxError.value = "";
  favoriteQuarkFolderId.value = "";
  favoriteQuarkParentFolderId.value = "";
  favoriteQuarkFolderName.value = "";
  void ensureFavoriteQuarkInbox();
}

function openFavoriteFolderPicker() {
  if (!favoriteQuarkInbox.value) {
    toast.info(favoriteQuarkInboxError.value || "请先加载夸克「来自：分享」");
    return;
  }
  favoriteFolderPickerOpen.value = true;
}

function onFavoriteFolderResolved(payload: {
  accountId: number;
  accountName: string;
  parentId: string;
  parentDirId?: string;
  path: string;
  dirs?: string[];
}) {
  favoriteQuarkFolderId.value = payload.parentId;
  favoriteQuarkParentFolderId.value = payload.parentDirId || favoriteQuarkInbox.value?.id || "";
  const name = payload.path.split("/").filter(Boolean).pop() || "";
  favoriteQuarkFolderName.value = name;
  favoriteFolderPickerOpen.value = false;
  favoriteError.value = "";
}

async function submitFavorite() {
  if (favoriteSaving.value) return;
  const link = favoriteLink.value.trim();
  if (!link) {
    favoriteError.value = "请填写分享链接";
    return;
  }
  const targetProvider = favoriteProvider.value;
  favoriteSaving.value = true;
  favoriteError.value = "";
  try {
    if (targetProvider === "guangya") {
      const editingFavorite = favoriteEditing.value;
      const res = editingFavorite
        ? await bueiGuangyaShareApi.updateSaved(editingFavorite.id, {
            url: link,
            password: favoritePassword.value.trim(),
          })
        : await bueiGuangyaShareApi.parse({
            url: link,
            password: favoritePassword.value.trim(),
          });
      if (res.need_password) {
        favoriteError.value = "该分享需要提取码";
        return;
      }
      applyParsedGuangya(res, link);
      await loadSavedLinks();
      if (res.saved_link_id > 0) {
        activeFavoriteKey.value = `g:${res.saved_link_id}`;
      } else if (editingFavorite) {
        activeFavoriteKey.value = `g:${editingFavorite.id}`;
      }
      favoriteDialogOpen.value = false;
      toast.success(editingFavorite ? "光鸭收藏已更新" : "已添加光鸭收藏");
      return;
    }

    const accountId = favoriteQuarkAccountId.value;
    if (!accountId) {
      favoriteError.value = "请选择夸克账号";
      return;
    }
    if (!favoriteQuarkFolderId.value || !favoriteQuarkFolderName.value) {
      favoriteError.value = "请选择「来自：分享」下的收藏文件夹";
      return;
    }
    const payload = {
      account_id: accountId,
      link,
      folder_id: favoriteQuarkFolderId.value,
      parent_folder_id: favoriteQuarkParentFolderId.value,
      folder_name: favoriteQuarkFolderName.value,
    };
    let saved: QuarkIngestFavorite;
    if (favoriteEditing.value) {
      saved = await bueiQuarkIngestApi.updateFavorite(favoriteEditing.value.id, payload);
    } else {
      saved = await bueiQuarkIngestApi.createFavorite(payload);
    }
    favoriteDialogOpen.value = false;
    await loadQuarkFavorites();
    const target = quarkFavorites.value.find((item) => item.id === saved.id);
    if (target) await selectQuarkFavorite(target);
    toast.success(favoriteEditing.value ? "收藏已更新" : "已添加夸克收藏");
  } catch (error) {
    favoriteError.value = getApiErrorMessage(error, "保存收藏失败");
  } finally {
    favoriteSaving.value = false;
  }
}

function errorText(error: unknown, fallback: string) {
  if (error instanceof ApiError && error.errorType === "aborted") return "";
  return getApiErrorMessage(error, fallback);
}

async function restoreBrowsePath(crumbs?: GuangyaShareBrowseCrumb[]) {
  if (!crumbs || crumbs.length <= 1) return;
  const root = { id: "", name: "全部文件", path: "" };
  const list: FolderCrumb[] = [];
  for (const crumb of crumbs) {
    const id = String(crumb?.id ?? "");
    if (list.length === 0 && id !== "") list.push({ ...root });
    if (!id && list.length > 0) break;
    const path = String(crumb?.path ?? "");
    const name = String(crumb?.name ?? "").trim() || path.split("/").filter(Boolean).pop() || "未命名目录";
    list.push({ id, name, path });
  }
  if (list.length <= 1) {
    pathStack.value = [{ ...root }];
    return;
  }
  const target = list[list.length - 1];
  if (!target.id) return;
  pathStack.value = list;
  scrollCrumbsToEnd();
  await loadFolder(target.id, target.path);
  if (folderErrors.value[target.id]) {
    pathStack.value = [{ ...root }];
    scrollCrumbsToEnd();
  }
}

function applyParsedGuangya(res: GuangyaShareParseResult, input: string) {
  provider.value = "guangya";
  activeQuarkFavorite.value = null;
  quarkSourceItems.value = [];
  quarkSourceError.value = "";
  if (res.saved_link_id > 0) activeFavoriteKey.value = `g:${res.saved_link_id}`;
  result.value = res;
  parsedURL.value = input;
  if (!res.need_code) password.value = "";
  folderGeneration += 1;
  folderCache.value = { "": res.items };
  loadingFolders.value = {};
  folderErrors.value = {};
  pathStack.value = [{ id: "", name: "全部文件", path: "" }];
  scrollCrumbsToEnd();
  if (!res.need_password) {
    void restoreBrowsePath(res.last_browse_path);
  }
  selectedIds.value = new Set();
  selectedMap.value = new Map();
  anchorIndex.value = -1;
  resetRenameRules();
  keyword.value = "";
  page.value = 1;
  sortKey.value = "name";
  sortDir.value = "asc";
  resetTmdbMatch();
  if (!res.need_password) {
    applyRenameReplace(res.rename_replace);
    applyDestinationForShare(res);
    passwordPrompt.value = false;
  }
}

async function parseShare(refreshSaved = true, savedID?: number) {
  if (interactionBusy.value) {
    toast.info("正在入库，请稍候");
    return;
  }
  const input = url.value.trim();
  if (!input) {
    toast.info("请先粘贴光鸭分享链接");
    return;
  }
  parseAbort?.abort();
  if (input !== parsedURL.value) {
    result.value = null;
    selectedIds.value = new Set();
    selectedMap.value = new Map();
    anchorIndex.value = -1;
  }
  const controller = new AbortController();
  parseAbort = controller;
  parsing.value = true;
  parseError.value = "";
  try {
    const res = await bueiGuangyaShareApi.parse(
      { url: input, password: password.value.trim(), saved_id: savedID },
      controller.signal,
    );
    if (controller.signal.aborted) return;
    applyParsedGuangya(res, input);
    if (refreshSaved && activeFavoriteKey.value === null) void loadAllFavorites();
  } catch (error) {
    if (controller.signal.aborted) return;
    const message = errorText(error, "解析分享失败");
    parseError.value = message;
    if (message.includes("提取码")) passwordPrompt.value = true;
  } finally {
    if (parseAbort === controller) {
      parsing.value = false;
      parseAbort = null;
    }
  }
}

function resetParse() {
  parseAbort?.abort();
  parseAbort = null;
  parsing.value = false;
  parseError.value = "";
  result.value = null;
  parsedURL.value = "";
  password.value = "";
  passwordPrompt.value = false;
  activeFavoriteKey.value = null;
  folderGeneration += 1;
  folderCache.value = {};
  loadingFolders.value = {};
  folderErrors.value = {};
  pathStack.value = [{ id: "", name: "全部文件", path: "" }];
  selectedIds.value = new Set();
  selectedMap.value = new Map();
  anchorIndex.value = -1;
  resetRenameRules();
  keyword.value = "";
  page.value = 1;
  folderTitle.value = "";
  isSeries.value = true;
  season.value = 1;
  autoStrm.value = true;
  autoScrape.value = true;
  scrapeTaskId.value = null;
  embyRefresh.value = true;
  embyMode.value = "global";
  embyLibraryId.value = "";
  embyLibraries.value = [];
  sortKey.value = "name";
  sortDir.value = "asc";
  resetTmdbMatch();
}

async function loadFolder(parentID: string, parentPath: string) {
  if (!result.value || folderCache.value[parentID] || loadingFolders.value[parentID]) return;
  const generation = folderGeneration;
  setFolderLoading(parentID, true);
  setFolderError(parentID, "");
  try {
    const res = await bueiGuangyaShareApi.list({
      url: parsedURL.value || url.value.trim(),
      share_id: result.value.share_id,
      password: password.value.trim(),
      parent_id: parentID,
      parent_path: parentPath,
    });
    if (generation !== folderGeneration) return;
    folderCache.value = { ...folderCache.value, [parentID]: res.items };
  } catch (error) {
    if (generation !== folderGeneration) return;
    setFolderError(parentID, getApiErrorMessage(error, "加载文件夹失败"));
  } finally {
    if (generation === folderGeneration) setFolderLoading(parentID, false);
  }
}

async function enterFolder(item: GuangyaShareItem) {
  if (!item.is_dir) return;
  pathStack.value = [...pathStack.value, { id: item.id, name: item.name, path: item.path }];
  scrollCrumbsToEnd();
  keyword.value = "";
  page.value = 1;
  setFolderError(item.id, "");
  if (!folderCache.value[item.id]) {
    await loadFolder(item.id, item.path);
  }
}

async function navigateToCrumb(index: number) {
  if (index < 0 || index >= pathStack.value.length - 1) return;
  pathStack.value = pathStack.value.slice(0, index + 1);
  scrollCrumbsToEnd();
  keyword.value = "";
  page.value = 1;
  const crumb = pathStack.value[index];
  setFolderError(crumb.id, "");
  if (!folderCache.value[crumb.id]) {
    await loadFolder(crumb.id, crumb.path);
  }
}

function goUp() {
  void navigateToCrumb(pathStack.value.length - 2);
}

function onRowClick(item: GuangyaShareItem, event: MouseEvent) {
  if (provider.value === "quark") {
    if (!item.is_dir) toggleSelect(item, event);
    return;
  }
  if (item.is_dir) {
    void enterFolder(item);
    return;
  }
  if (isProcessed(item)) return;
  toggleSelect(item, event);
}

async function refreshCurrentFolder() {
  if (provider.value === "quark") {
    anchorIndex.value = -1;
    await refreshQuarkFavoriteFiles();
    return;
  }
  anchorIndex.value = -1;
  const id = currentFolderID.value;
  const nextCache = { ...folderCache.value };
  delete nextCache[id];
  folderCache.value = nextCache;
  setFolderError(id, "");
  await loadFolder(id, currentFolderPath.value);
}

function toggleSort(key: SortKey) {
  if (sortKey.value !== key) {
    sortKey.value = key;
    sortDir.value = "asc";
    return;
  }
  // 同一列循环：升序 → 降序 → 取消排序。
  if (sortDir.value === "asc") {
    sortDir.value = "desc";
    return;
  }
  sortKey.value = null;
}

async function resolveDirectoryCrumbs(
  accountId: number,
  basePath: string,
  baseDirs: string[],
  segments: string[],
) {
  const crumbs: { id: string; name: string }[] = [{ id: "", name: "根目录" }];
  const baseParts = baseDirs.length
    ? [...baseDirs]
    : basePath
        .split("/")
        .map((part) => part.trim())
        .filter(Boolean);
  const parts = [...baseParts, ...segments];
  const forceFrom = Math.max(0, baseParts.length - 1);
  let parentId = "";
  for (let index = 0; index < parts.length; index++) {
    const name = parts[index];
    // 基础目录前面的层级优先用缓存；最后一级基础目录和新建的标题/季数目录强制刷新。
    const res = await filesApi.list(accountId, parentId, { forceRefresh: index >= forceFrom });
    const folder = res.items.find((item) => item.is_dir && item.name === name);
    if (!folder) throw new Error(`未找到目录：${name}`);
    parentId = folder.id;
    crumbs.push({ id: folder.id, name: folder.name });
  }
  return crumbs;
}

async function openTransferLocation(base: DestinationBase, segments: string[]) {
  if (!base.accountId) return;
  let crumbs: { id: string; name: string }[];
  let fallback = false;
  try {
    crumbs = await resolveDirectoryCrumbs(base.accountId, base.path, base.dirs, segments);
  } catch {
    try {
      crumbs = await resolveDirectoryCrumbs(base.accountId, base.path, base.dirs, []);
      fallback = true;
    } catch {
      toast.error("无法定位保存位置，请到前台文件页手动打开目标目录");
      return;
    }
  }
  if (fallback) {
    toast.info("目标文件夹暂未出现，已打开基础目录");
  }
  try {
    const failure = await router.push({
      path: "/",
      query: buildBrowserLocationQuery({ accountId: base.accountId, crumbs }),
    });
    if (failure) {
      toast.info("已取消跳转，可稍后手动打开前台页面");
    }
  } catch {
    toast.error("打开前台页面失败");
  }
}

async function flattenQuarkFavorite() {
  const favorite = activeQuarkFavorite.value;
  if (!favorite || interactionBusy.value) return;
  const ok = await confirm({
    title: "平铺目录",
    message:
      "将递归移动收藏文件夹内所有子文件夹中的文件到当前目录，并删除所有子文件夹。文件名冲突时会自动追加 (1)、(2)…。是否继续？",
    confirmText: "开始平铺",
    danger: true,
  }).catch(() => false);
  if (!ok) return;
  quarkFlattening.value = true;
  try {
    const res = await bueiQuarkIngestApi.flatten(favorite.account_id, favorite.id);
    clearSelection();
    const extra = res.renamed ? `，重命名 ${res.renamed} 个` : "";
    toast.success(`已移动 ${res.moved} 个文件，删除 ${res.deleted_folders} 个文件夹${extra}`);
    await refreshQuarkFavoriteFiles();
  } catch (error) {
    toast.error(getApiErrorMessage(error, "平铺目录失败"));
  } finally {
    quarkFlattening.value = false;
  }
}

async function deleteQuarkSelectedFiles() {
  const favorite = activeQuarkFavorite.value;
  const ids = selectedFiles.value.map((item) => item.id).filter(Boolean);
  if (!favorite || !ids.length || interactionBusy.value) return;
  const ok = await confirm({
    title: "确认删除",
    message: `确定要将选中的 ${ids.length} 个文件移入回收站吗？`,
    confirmText: "删除",
    danger: true,
  }).catch(() => false);
  if (!ok) return;
  quarkDeleting.value = true;
  try {
    await filesApi.deleteFiles({
      account_id: favorite.account_id,
      file_ids: ids,
      parent_id: favorite.folder_id,
    });
    clearSelection();
    toast.success(`已删除到回收站 ${ids.length} 个文件`);
    await refreshQuarkFavoriteFiles();
  } catch (error) {
    toast.error(getApiErrorMessage(error, "删除文件失败"));
  } finally {
    quarkDeleting.value = false;
  }
}

async function discardSelected() {
  if (!result.value || discarding.value || transferring.value) return;
  const files = selectedFiles.value;
  if (!files.length) {
    toast.info("请先勾选要丢弃的文件");
    return;
  }
  const ok = await confirm({
    title: "标记为已丢弃？",
    message: `将 ${files.length} 个文件标记为已丢弃。行会置灰并跳过批量选择，但仍可通过复选框单独选中后入库。`,
    confirmText: "标记丢弃",
    cancelText: "取消",
    danger: false,
  }).catch(() => false);
  if (!ok) return;

  discarding.value = true;
  try {
    if (!result.value.saved_link_id) {
      toast.error("收藏记录不存在，请重新打开收藏");
      return;
    }
    const res = await bueiGuangyaShareApi.discard(
      result.value.saved_link_id,
      files.map((file) => ({ file_id: file.id, original_name: file.name })),
    );
    if (result.value) {
      const now = new Date().toISOString();
      const merged = new Map((result.value.transferred_files ?? []).map((file) => [file.file_id, file]));
      for (const file of files) {
        merged.set(file.id, {
          file_id: file.id,
          original_name: file.name,
          new_name: file.name,
          account_id: 0,
          target_parent_id: "",
          status: "discarded",
          updated_at: now,
        });
      }
      result.value = { ...result.value, transferred_files: [...merged.values()] };
    }
    const nextIds = new Set(selectedIds.value);
    const nextMap = new Map(selectedMap.value);
    for (const file of files) {
      nextIds.delete(file.id);
      nextMap.delete(file.id);
    }
    selectedIds.value = nextIds;
    selectedMap.value = nextMap;
    toast.success(`已丢弃 ${res.discarded} 个文件`);
  } catch (error) {
    toast.error(getApiErrorMessage(error, "标记丢弃失败"));
  } finally {
    discarding.value = false;
  }
}

function submitTransfer() {
  if (interactionBusy.value) return;
  if (provider.value === "quark") {
    submitQuarkTransfer();
    return;
  }
  if (!result.value) return;
  if (!selectedItems.value.length) {
    toast.info("请先勾选要入库的文件");
    return;
  }
  if (!destination.value.accountId) {
    toast.info("请先选择入库账号和目录");
    return;
  }
  if (!sanitizedFolderTitle.value) {
    toast.info("请填写目标文件夹名");
    return;
  }
  if (renameReplaceError.value) {
    toast.error(`替换规则的正则表达式无效：${renameReplaceError.value}`);
    return;
  }
  const base = { ...destination.value };
  const previews = renamePreviewRows.value;
  const changedRows = previews.filter((row) => row.newName !== row.oldName);
  const files = orderedSelectedFiles.value.map((item) => ({
    fileId: item.id,
    originalName: item.name,
    newName: renamedNameFor(item),
    isDir: false,
  }));
  const browsePath = pathStack.value.map((crumb) => ({ id: crumb.id, name: crumb.name, path: crumb.path }));
  const renameRules = renameReplaceRules.value
    .filter((rule) => !!rule.find)
    .map((rule) => ({ find: rule.find, replaceWith: rule.replaceWith }));
  const renameReplace = {
    enabled: renameReplaceOn.value && renameRules.length > 0 && !renameReplaceError.value,
    find: renameRules[0]?.find ?? "",
    replaceWith: renameRules[0]?.replaceWith ?? "",
    rules: renameRules,
  };
  pendingTransfer.value = {
    provider: "guangya",
    target: `${base.accountName || "未选择账号"} · ${targetPath.value}`,
    count: selectedItems.value.length,
    renames: changedRows,
    files,
    browsePath,
    renameReplace,
    autoStrm: autoStrm.value,
    // 保存用户偏好；真正是否刮削由后端结合 STRM 开关判断。
    autoScrape: autoScrape.value && scrapeAvailable.value,
    scrapeTaskId: scrapeTaskId.value ? Number(scrapeTaskId.value) : 0,
    refreshEmby: embyEffective.value,
    embyConfigId: embyConfigId.value,
    embyMode: embyMode.value,
    embyLibraryId: embyLibraryId.value,
    unchangedCount: previews.length - changedRows.length,
    segments: [...targetSegments.value],
    base,
    title: sanitizedFolderTitle.value,
    season: seasonNumber.value,
    series: isSeries.value,
  };
  transferDone.value = false;
  transferConfirmOpen.value = true;
}

function submitQuarkTransfer() {
  const favorite = activeQuarkFavorite.value;
  if (!favorite) {
    toast.info("请先从左侧选择夸克收藏");
    return;
  }
  if (!selectedFiles.value.length) {
    toast.info("请先勾选要入库的文件");
    return;
  }
  if (destination.value.accountId !== favorite.account_id) {
    toast.info("请选择当前夸克账号内的保存位置");
    return;
  }
  if (!sanitizedFolderTitle.value) {
    toast.info("请填写目标文件夹名称");
    return;
  }
  if (renameReplaceError.value) {
    toast.error(`替换规则的正则表达式无效：${renameReplaceError.value}`);
    return;
  }
  const base = { ...destination.value, dirs: [...destination.value.dirs] };
  const previews = renamePreviewRows.value;
  const changedRows = previews.filter((row) => row.newName !== row.oldName);
  const files = orderedSelectedFiles.value.map((item) => ({
    fileId: item.id,
    originalName: item.name,
    newName: renamedNameFor(item),
    isDir: false,
  }));
  const renameRules = renameReplaceRules.value
    .filter((rule) => !!rule.find)
    .map((rule) => ({ find: rule.find, replaceWith: rule.replaceWith }));
  const renameReplace = {
    enabled: renameReplaceOn.value && renameRules.length > 0 && !renameReplaceError.value,
    find: renameRules[0]?.find ?? "",
    replaceWith: renameRules[0]?.replaceWith ?? "",
    rules: renameRules,
  };
  pendingTransfer.value = {
    provider: "quark",
    target: `${base.accountName || favorite.accountName} · ${targetPath.value}`,
    count: files.length,
    renames: changedRows,
    files,
    browsePath: [],
    renameReplace,
    autoStrm: autoStrm.value,
    autoScrape: autoScrape.value && scrapeAvailable.value,
    scrapeTaskId: scrapeTaskId.value ? Number(scrapeTaskId.value) : 0,
    refreshEmby: embyEffective.value,
    embyConfigId: embyConfigId.value,
    embyMode: embyMode.value,
    embyLibraryId: embyLibraryId.value,
    unchangedCount: Math.max(0, files.length - changedRows.length),
    segments: [...targetSegments.value],
    base,
    title: sanitizedFolderTitle.value,
    season: seasonNumber.value,
    series: isSeries.value,
    quarkFavoriteId: favorite.id,
    quarkAccountId: favorite.account_id,
    quarkSourceParentId: favorite.folder_id,
    quarkSourceFolderParentId: favorite.parent_folder_id,
    quarkSourceFolderName: favorite.folder_name,
  };
  transferDone.value = false;
  transferConfirmOpen.value = true;
}

function resetTransferDialog() {
  transferConfirmOpen.value = false;
  transferDone.value = false;
  pendingTransfer.value = null;
  // 进度只在这一个弹窗里展示，关闭后停止轮询。
  stopScrapePolling();
  stopProgressPolling();
  progressIdRef = "";
  transferPhase.value = "";
  scrapeWatchId = 0;
}

function cancelTransferConfirm() {
  if (transferring.value) return;
  resetTransferDialog();
}

function stayAfterTransfer() {
  targetFilesRefreshKey.value += 1;
  resetTransferDialog();
  toast.success("入库完成");
}

async function openAfterTransfer() {
  const pending = pendingTransfer.value;
  resetTransferDialog();
  if (pending) await openTransferLocation(pending.base, pending.segments);
}

const transferPhaseLabel = computed(() => {
  switch (transferPhase.value) {
    case "rename":
      return "重命名中…";
    case "strm":
      return "生成 STRM 中…";
    case "scrape":
      return "启动刮削中…";
    case "emby":
      return "刷库中…";
    case "done":
      return "即将完成…";
    case "failed":
      return "处理失败";
    default:
      return "入库中…";
  }
});

function newProgressId() {
  const cryptoApi = globalThis.crypto;
  if (cryptoApi && typeof cryptoApi.randomUUID === "function") return cryptoApi.randomUUID();
  return `gys-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

function stopProgressPolling() {
  if (progressTimer !== undefined) {
    window.clearTimeout(progressTimer);
    progressTimer = undefined;
  }
}

function isProgressFinished(phase: string) {
  return phase === "done" || phase === "failed";
}

// 轮询入库阶段；请求返回后如果刮削/刷库还在后台跑，继续轮询到真正结束。
async function pollTransferProgress(id: string, source: Provider) {
  stopProgressPolling();
  try {
    const progress =
      source === "quark"
        ? await bueiQuarkIngestApi.progress(id)
        : await bueiGuangyaShareApi.progress(id);
    if (progressIdRef !== id) return;
    if (progress.found && progress.phase) {
      transferPhase.value = progress.phase;
      if (progress.error) transferResult.value.embyError = progress.error;
    }
  } catch {
    // 忽略单次失败，下一轮继续。
  } finally {
    if (progressIdRef === id && !isProgressFinished(transferPhase.value)) {
      progressTimer = window.setTimeout(() => void pollTransferProgress(id, source), 800);
    }
  }
}

async function confirmTransfer() {
  const pending = pendingTransfer.value;
  if (!pending || transferring.value || interactionBusy.value) return;
  if (pending.provider === "quark") {
    await confirmQuarkTransfer(pending);
    return;
  }
  if (!result.value) return;
  transferring.value = true;
  transferPhase.value = "transfer";
  const progressId = newProgressId();
  progressIdRef = progressId;
  void pollTransferProgress(progressId, "guangya");
  let completed = false;
  let renamed = 0;
  let renameSkipped = 0;
  let renameScheduled = false;
  let strmGenerated = 0;
  let strmMatched = false;
  let strmScheduled = false;
  let strmError = "";
  try {
    const res = await bueiGuangyaShareApi.transfer({
      url: parsedURL.value || url.value.trim(),
      share_id: result.value.share_id,
      password: password.value.trim(),
      saved_link_id: result.value.saved_link_id,
      file_ids: selectedItems.value.map((item) => item.id),
      account_id: pending.base.accountId,
      parent_id: pending.base.parentId,
      target_path: pending.segments,
      base_path: pending.base.path || "/",
      base_dirs: pending.base.dirs,
      title: pending.title,
      season: pending.season,
      is_series: pending.series,
      files: pending.files.map((file) => ({
        file_id: file.fileId,
        original_name: file.originalName,
        new_name: file.newName,
        is_dir: file.isDir,
      })),
      browse_path: pending.browsePath,
      rename_replace: {
        enabled: pending.renameReplace.enabled,
        find: pending.renameReplace.find,
        replace_with: pending.renameReplace.replaceWith,
        rules: pending.renameReplace.rules.map((rule) => ({
          find: rule.find,
          replace_with: rule.replaceWith,
        })),
      },
      auto_strm: pending.autoStrm,
      auto_scrape: pending.autoScrape,
      auto_scrape_task_id: pending.scrapeTaskId,
      refresh_emby: pending.refreshEmby,
      emby_config_id: pending.embyConfigId,
      emby_mode: pending.embyMode,
      emby_library_id: pending.embyLibraryId,
      progress_id: progressId,
    });
    completed = res.completed;
    renamed = res.renamed;
    renameSkipped = res.rename_skipped;
    renameScheduled = res.rename_scheduled;
    strmGenerated = res.strm_generated || 0;
    strmMatched = !!res.strm_matched;
    strmScheduled = !!res.strm_scheduled;
    strmError = res.strm_error || "";
    transferResult.value = {
      renamed,
      renameSkipped,
      strmGenerated,
      strmMatched,
      strmError,
      scrapeStarted: !!res.scrape_started,
      scrapeError: res.scrape_error || "",
      embyRefreshed: !!res.emby_refreshed,
      embyScheduled: !!res.emby_scheduled,
      embyError: res.emby_error || "",
    };
    if (res.scrape_started && pending.scrapeTaskId) {
      scrapeProgress.value = null;
      watchScrapeProgress(Number(pending.scrapeTaskId));
    }
    if (completed && result.value && pending.files.length) {
      const now = new Date().toISOString();
      const merged = new Map((result.value.transferred_files ?? []).map((file) => [file.file_id, file]));
      for (const file of pending.files) {
        merged.set(file.fileId, {
          file_id: file.fileId,
          original_name: file.originalName,
          new_name: file.newName,
          account_id: pending.base.accountId,
          target_parent_id: pending.base.parentId,
          status: "transferred",
          updated_at: now,
        });
      }
      result.value = { ...result.value, transferred_files: [...merged.values()] };
    }
    clearSelection();
    if (!completed) {
      const followUps = [
        renameScheduled ? "重命名" : "",
        strmScheduled ? "STRM 生成" : "",
        strmScheduled && pending.autoScrape ? "刮削" : "",
        pending.refreshEmby ? "Emby 刷库" : "",
      ].filter(Boolean);
      toast.info(
        followUps.length
          ? `入库任务已提交，完成后会自动执行${followUps.join("与")}，请稍后查看`
          : "入库任务已提交，内容较多时可能需要稍等片刻",
      );
    }
  } catch (error) {
    toast.error(getApiErrorMessage(error, "入库失败"));
    return;
  } finally {
    transferring.value = false;
    if (completed) {
      transferDone.value = true;
      targetFilesRefreshKey.value += 1;
      // 刮削/刷库仍在后台时保留轮询，弹窗里能看到它们的真实状态。
      const asyncPending = !!(transferResult.value.scrapeStarted || transferResult.value.embyScheduled);
      if (!asyncPending) {
        stopProgressPolling();
        progressIdRef = "";
        transferPhase.value = "";
      }
    } else {
      resetTransferDialog();
    }
  }
}

async function refreshQuarkAfterIngest() {
  await loadQuarkFavorites();
  const favorite = activeQuarkFavorite.value;
  if (!favorite) return;
  const current = quarkFavorites.value.find((item) => item.id === favorite.id);
  if (current) {
    activeQuarkFavorite.value = current;
    await refreshQuarkFavoriteFiles();
  }
}

async function confirmQuarkTransfer(pending: PendingTransfer) {
  if (!pending.quarkFavoriteId || !pending.quarkAccountId || !pending.quarkSourceParentId) return;
  transferring.value = true;
  transferPhase.value = "transfer";
  const progressId = newProgressId();
  progressIdRef = progressId;
  void pollTransferProgress(progressId, "quark");
  try {
    const payload: QuarkIngestPayload = {
      account_id: pending.base.accountId,
      favorite_id: pending.quarkFavoriteId,
      source_parent_id: pending.quarkSourceParentId,
      source_folder_parent_id: pending.quarkSourceFolderParentId || "",
      source_folder_name: pending.quarkSourceFolderName || "",
      file_ids: pending.files.map((file) => file.fileId),
      parent_id: pending.base.parentId,
      target_path: pending.segments,
      base_path: pending.base.path || "/",
      base_dirs: pending.base.dirs,
      title: pending.title,
      season: pending.season,
      is_series: pending.series,
      files: pending.files.map((file) => ({
        file_id: file.fileId,
        original_name: file.originalName,
        new_name: file.newName,
        is_dir: file.isDir,
      })),
      rename_replace: {
        enabled: pending.renameReplace.enabled,
        find: pending.renameReplace.find,
        replace_with: pending.renameReplace.replaceWith,
        rules: pending.renameReplace.rules.map((rule) => ({
          find: rule.find,
          replace_with: rule.replaceWith,
        })),
      },
      auto_strm: pending.autoStrm,
      auto_scrape: pending.autoScrape,
      auto_scrape_task_id: pending.scrapeTaskId,
      refresh_emby: pending.refreshEmby,
      emby_config_id: pending.embyConfigId,
      emby_mode: pending.embyMode,
      emby_library_id: pending.embyLibraryId,
      progress_id: progressId,
    };
    const res = await bueiQuarkIngestApi.transfer(payload);
    transferResult.value = {
      renamed: res.renamed || 0,
      renameSkipped: res.rename_skipped || 0,
      strmGenerated: res.strm_generated || 0,
      strmMatched: !!res.strm_matched,
      strmError: res.strm_error || "",
      scrapeStarted: !!res.scrape_started,
      scrapeError: res.scrape_error || "",
      embyRefreshed: !!res.emby_refreshed,
      embyScheduled: !!res.emby_scheduled,
      embyError: res.emby_error || "",
    };
    if (res.scrape_started && pending.scrapeTaskId) {
      scrapeProgress.value = null;
      watchScrapeProgress(Number(pending.scrapeTaskId));
    }
    clearSelection();
    transferDone.value = true;
    targetFilesRefreshKey.value += 1;
    await refreshQuarkAfterIngest();
    const asyncPending = !!(transferResult.value.scrapeStarted || transferResult.value.embyScheduled);
    if (!asyncPending) {
      stopProgressPolling();
      progressIdRef = "";
      transferPhase.value = "";
    }
  } catch (error) {
    toast.error(getApiErrorMessage(error, "入库失败"));
    stopProgressPolling();
    progressIdRef = "";
    transferPhase.value = "failed";
  } finally {
    transferring.value = false;
  }
}

function goPage(delta: number) {
  page.value = Math.min(totalPages.value, Math.max(1, page.value + delta));
}

// 「辅助工具」tab 顶部按钮通过 ref 读取这些状态。
const transferHint = computed(() => {
  if (!hasSource.value) return "先从左侧收藏选择要入库的内容";
  if (!selectedItems.value.length) return "先在文件列表勾选要入库的内容";
  const parts: string[] = [];
  if (provider.value === "guangya" && selectedFolderCount.value) {
    parts.push(`含 ${selectedFolderCount.value} 个文件夹`);
  }
  parts.push(`文件合计 ${formatSize(selectedFileSize.value)}`);
  if (provider.value === "guangya") parts.push("文件夹按实际内容入库");
  return parts.join(" · ");
});

defineExpose({
  selectedCount: computed(() => selectedItems.value.length),
  canTransfer,
  transferring,
  transferPhaseLabel,
  transferHint,
  submitTransfer,
});

watch(guangyaAccounts, () => {
  ensureDestination();
});

onMounted(async () => {
  // 进入 tab 即初始化工作区；收藏列表仍在左侧，等待手动选择。
  if (!accountsStore.accounts.length) {
    await accountsStore.loadAccounts().catch(() => undefined);
  }
  resetParse();
  url.value = "";
  activeFavoriteKey.value = null;
  parseError.value = "";
  await loadAllFavorites();
  void loadScrapeTasks();
  void loadEmbyConfigs();
  ensureDestination();
});

watch(quarkAccounts, () => {
  void loadQuarkFavorites();
});

onBeforeUnmount(() => {
  parseAbort?.abort();
  quarkSourceSeq += 1;
  stopProgressPolling();
  stopScrapePolling();
});
</script>

<template>
  <!-- 单一根节点：父级 tab 用 v-show 控制显隐，多根节点会让指令属性无法透传。 -->
  <div class="gys-page">
    <div class="gys">
      <div class="gys__body">
        <div class="gys__left">
          <section class="gys__pair gys__pair--source">
          <aside class="gys__savedcol">
            <section class="gys__panel gys__panel--saved">
              <div class="gys__panel-head">
                <h4>收藏</h4>
                <span class="gys__panel-count">{{ unifiedFavorites.length }}</span>
                <button
                  type="button"
                  class="gys__icon-btn gys__icon-btn--add"
                  title="添加收藏"
                  :disabled="interactionBusy"
                  @click="openAddFavorite"
                >
                  <SvgIcon name="plus" :size="14" />
                </button>
                <button
                  type="button"
                  class="gys__icon-btn"
                  title="刷新收藏"
                  :disabled="favoritesLoading"
                  @click="loadAllFavorites"
                >
                  <SvgIcon name="rotate" :size="13" />
                </button>
              </div>
              <div class="gys__saved-body">
                <div v-if="favoritesLoading" class="gys__saved-state">
                  <BusySpinner variant="notch" :size="16" />
                  加载中…
                </div>
                <div v-else-if="favoritesError && !unifiedFavorites.length" class="gys__saved-state is-error">
                  <span>{{ favoritesError }}</span>
                  <button type="button" @click="loadAllFavorites">重试</button>
                </div>
                <div v-else-if="!unifiedFavorites.length" class="gys__saved-state">
                  点击右上角 + 添加分享链接
                </div>
                <div v-else class="gys__saved-list">
                  <div v-if="favoritesError" class="gys__saved-warning">
                    {{ favoritesError }}
                    <button type="button" @click="loadAllFavorites">重试</button>
                  </div>
                  <div
                    v-for="favorite in unifiedFavorites"
                    :key="favorite.key"
                    class="gys__saved-row"
                    :class="{ 'is-active': activeFavoriteKey === favorite.key }"
                  >
                    <button
                      type="button"
                      class="gys__saved-main"
                      :title="favorite.link"
                      :disabled="interactionBusy"
                      @click="selectFavorite(favorite)"
                    >
                      <SvgIcon name="hand-star-solid" :size="14" class="gys__saved-star" />
                      <span class="gys__saved-meta">
                        <span class="gys__saved-title">{{ favorite.title }}</span>
                        <span class="gys__saved-sub">
                          <span class="gys__provider" :class="`is-${favorite.provider}`">
                            {{ favorite.provider === "quark" ? "夸克" : "光鸭" }}
                          </span>
                          {{ favorite.subtitle }}
                        </span>
                      </span>
                    </button>
                    <div class="gys__saved-actions">
                      <button
                        type="button"
                        class="gys__saved-edit"
                        title="修改收藏"
                        :disabled="interactionBusy"
                        @click.stop="openEditFavorite(favorite)"
                      >
                        <SvgIcon name="hand-edit" :size="12" />
                      </button>
                      <button
                        type="button"
                        class="gys__saved-del"
                        :title="favorite.provider === 'quark' ? '删除收藏及文件夹' : '取消收藏'"
                        :disabled="interactionBusy"
                        @click.stop="removeFavorite(favorite)"
                      >
                        <SvgIcon name="trash" :size="13" />
                      </button>
                    </div>
                  </div>
                </div>
              </div>
            </section>
          </aside>
        <div class="gys__main">
          <div v-if="hasSource" class="gys__sharehead">
            <span class="gys__sharetitle" :title="sourceTitle">{{ sourceTitle }}</span>
            <span class="gys__sharesub">{{ sourceSubtitle }}</span>
            <a
              v-if="activeFavoriteLink"
              class="gys__sharejump"
              :href="activeFavoriteLink"
              target="_blank"
              rel="noopener noreferrer"
              title="在新窗口打开收藏链接"
              aria-label="在新窗口打开收藏链接"
            >
              <SvgIcon name="arrow-up-right-from-square" :size="13" />
            </a>
          </div>

          <div v-if="hasSource && provider === 'guangya'" class="gys__crumbs">
            <button
              type="button"
              class="gys__up"
              title="返回上级"
              aria-label="返回上级"
              :disabled="pathStack.length <= 1"
              @click="goUp"
            >
              <SvgIcon name="chevron-left" :size="15" />
            </button>
            <div ref="crumbListRef" class="gys__crumb-list">
              <template v-for="(crumb, index) in pathStack" :key="crumb.id || 'root'">
                <span v-if="index > 0" class="gys__crumb-sep">/</span>
                <button
                  type="button"
                  class="gys__crumb"
                  :class="{ 'is-current': index === pathStack.length - 1 }"
                  :disabled="index === pathStack.length - 1"
                  :title="crumb.name"
                  @click="navigateToCrumb(index)"
                >
                  {{ crumb.name }}
                </button>
              </template>
            </div>
          </div>

          <div v-if="hasSource" class="gys__quickbar">
            <span class="gys__quick-label" :class="{ 'is-hint': ancestorSelected }">
              {{ ancestorSelected ? "当前目录已包含在所选文件夹" : "快速勾选" }}
            </span>
            <div class="gys__quick">
              <button
                v-for="kind in quickKinds"
                :key="kind.key"
                type="button"
                class="gys__quick-btn"
                :title="`勾选当前目录的${kind.label}`"
                :disabled="ancestorSelected || !kindCounts[kind.key]"
                @click="quickSelect(kind.key)"
              >
                <SvgIcon :name="kind.icon" :size="14" />
                {{ kind.label }}
                <span class="gys__quick-count">{{ kindCounts[kind.key] ?? 0 }}</span>
              </button>
            </div>
          </div>

          <div v-if="hasSource" class="gys__toolbar">
            <div class="gys__search">
              <SvgIcon name="magnifying-glass" :size="14" />
              <input v-model="keyword" type="text" placeholder="搜索当前目录" />
            </div>
            <span class="gys__folder-count">
              {{ sourceLoading ? "加载中…" : `${sourceItems.length} 项` }}
            </span>
            <button
              type="button"
              class="gys__refresh"
              title="刷新当前目录"
              :disabled="sourceLoading || interactionBusy"
              @click="refreshCurrentFolder"
            >
              <SvgIcon name="rotate" :size="13" />
              刷新
            </button>
            <template v-if="provider === 'quark'">
              <button
                type="button"
                class="gys__discard"
                title="递归移动子文件夹文件到当前目录并删除子文件夹"
                :disabled="sourceLoading || interactionBusy"
                @click="flattenQuarkFavorite"
              >
                <SvgIcon name="folder-tree" :size="13" />
                {{ quarkFlattening ? "平铺中…" : "平铺目录" }}
              </button>
              <button
                type="button"
                class="gys__discard"
                title="将选中的文件移到回收站"
                :disabled="!selectedFileCount || interactionBusy"
                @click="deleteQuarkSelectedFiles"
              >
                <SvgIcon name="trash" :size="13" />
                {{ quarkDeleting ? "删除中…" : "删除" }}
              </button>
            </template>
            <button
              v-else
              type="button"
              class="gys__discard"
              title="将选中的文件标记为已丢弃"
              :disabled="!canDiscard"
              @click="discardSelected"
            >
              <SvgIcon name="trash" :size="13" />
              {{ discarding ? "丢弃中…" : "丢弃" }}
            </button>
          </div>

          <p v-if="parseError && provider === 'guangya'" class="gys__error">{{ parseError }}</p>

          <div
            v-if="provider === 'guangya' && (result?.need_password || passwordPrompt)"
            class="gys__password-inline"
          >
            <SvgIcon name="hand-lock" :size="18" />
            <span>该分享需要提取码</span>
            <input
              v-model="password"
              class="gys__password-input"
              type="text"
              maxlength="12"
              placeholder="提取码"
              :disabled="parsing"
              @keydown.enter.prevent="parseShare(false, result?.saved_link_id || undefined)"
            />
            <AppButton size="sm" variant="primary" :disabled="parsing" @click="parseShare(false, result?.saved_link_id || undefined)">
              {{ parsing ? "提取中…" : "提取文件" }}
            </AppButton>
          </div>

          <div v-else-if="parsing" class="gys__empty">
            <BusySpinner variant="notch" :size="34" />
            <p>正在解析分享内容…</p>
            <small>目录较多时可能需要几十秒，请稍候</small>
          </div>

          <div v-else-if="!hasSource" class="gys__empty">
            <SvgIcon name="hand-cloud" :size="44" />
            <p>从左侧收藏选择，或点击 + 添加分享链接</p>
            <small>支持光鸭与夸克分享，添加时自动识别网盘类型</small>
          </div>

          <div v-else-if="sourceLoading" class="gys__empty">
            <BusySpinner variant="notch" :size="30" />
            <p>正在加载文件…</p>
          </div>

          <div v-else-if="sourceError" class="gys__empty">
            <SvgIcon name="triangle-exclamation" :size="36" />
            <p>{{ sourceError }}</p>
            <AppButton size="sm" variant="secondary" @click="refreshCurrentFolder">重试</AppButton>
          </div>

          <div v-else class="gys__tablewrap">
            <table class="gys__table">
              <thead>
                <tr>
                  <th class="gys__col-check">
                    <button
                      type="button"
                      class="gys__check"
                      :class="{ 'is-checked': allCurrentSelected, 'is-partial': someCurrentSelected && !allCurrentSelected }"
                      role="checkbox"
                      :aria-checked="allCurrentSelected ? 'true' : someCurrentSelected ? 'mixed' : 'false'"
                      :disabled="!selectableCurrentItems.length"
                      :title="allCurrentSelected ? '取消全选当前目录' : '全选当前目录'"
                      @click="selectAllCurrent"
                    >
                      <SvgIcon v-if="allCurrentSelected" name="check" :size="10" />
                      <span v-else-if="someCurrentSelected" class="gys__check-dash" />
                    </button>
                  </th>
                  <th class="gys__th-sort" :class="{ 'is-sorted': sortKey === 'name' }" @click="toggleSort('name')">
                    名称
                    <SvgIcon v-if="sortKey === 'name'" :name="sortDir === 'asc' ? 'chevron-up' : 'chevron-down'" :size="11" />
                  </th>
                  <th class="gys__col-kind gys__th-sort" :class="{ 'is-sorted': sortKey === 'kind' }" @click="toggleSort('kind')">
                    类型
                    <SvgIcon v-if="sortKey === 'kind'" :name="sortDir === 'asc' ? 'chevron-up' : 'chevron-down'" :size="11" />
                  </th>
                  <th class="gys__col-size gys__th-sort" :class="{ 'is-sorted': sortKey === 'size' }" @click="toggleSort('size')">
                    大小
                    <SvgIcon v-if="sortKey === 'size'" :name="sortDir === 'asc' ? 'chevron-up' : 'chevron-down'" :size="11" />
                  </th>
                  <th class="gys__col-time gys__th-sort" :class="{ 'is-sorted': sortKey === 'time' }" @click="toggleSort('time')">
                    修改时间
                    <SvgIcon v-if="sortKey === 'time'" :name="sortDir === 'asc' ? 'chevron-up' : 'chevron-down'" :size="11" />
                  </th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="item in pageItems"
                  :key="item.id"
                  class="gys__row"
                  :class="{
                    'is-selected': selectedIds.has(item.id),
                    'is-included': ancestorSelected,
                    'is-folder': item.is_dir,
                    'is-static': provider === 'quark' && item.is_dir,
                    'is-processed': isProcessed(item),
                  }"
                  @click="onRowClick(item, $event)"
                >
                  <td class="gys__col-check" @click.stop>
                    <button
                      type="button"
                      class="gys__check"
                      :class="{ 'is-checked': selectedIds.has(item.id) || ancestorSelected }"
                      role="checkbox"
                      :aria-checked="selectedIds.has(item.id) || ancestorSelected"
                      :disabled="ancestorSelected || (provider === 'quark' && item.is_dir)"
                      @click="toggleSelect(item, $event, true)"
                    >
                      <SvgIcon
                        v-if="selectedIds.has(item.id) || ancestorSelected"
                        name="check"
                        :size="10"
                      />
                    </button>
                  </td>
                  <td class="gys__col-name">
                    <div class="gys__namecell">
                      <span class="gys__fileicon" :class="`is-${item.kind}`">
                        <SvgIcon :name="item.is_dir ? 'folder' : kindIcons[item.kind]" :size="16" />
                      </span>
                      <span class="gys__namebody">
                        <span
                          class="gys__name"
                          :title="item.is_dir && provider === 'guangya' ? `进入 ${item.name}` : item.name"
                        >
                          <span class="gys__name-stem">{{ displayNameParts(item.name, item.is_dir).stem }}</span>
                          <span class="gys__name-ext">{{ displayNameParts(item.name, item.is_dir).ext }}</span>
                        </span>
                        <span v-if="itemNewName(item) || isProcessed(item)" class="gys__name-sub">
                          <span v-if="isProcessed(item)" class="gys__transferred-tag">
                            {{ processedStatus(item) === "discarded" ? "已丢弃" : "已入库" }}
                          </span>
                          <span
                            v-if="itemNewName(item)"
                            class="gys__name-new"
                            :class="{ 'is-done': isProcessed(item) && !selectedIds.has(item.id) }"
                            :title="itemNewName(item)"
                          >
                            <span class="gys__name-stem">{{ splitFileName(itemNewName(item)).stem }}</span>
                            <span class="gys__name-ext">{{ splitFileName(itemNewName(item)).ext }}</span>
                          </span>
                        </span>
                      </span>
                      <span v-if="item.is_dir && provider === 'guangya'" class="gys__enter">
                        <SvgIcon name="chevron-right" :size="13" />
                      </span>
                      <span v-if="ancestorSelected" class="gys__included-tag">已包含</span>
                    </div>
                  </td>
                  <td class="gys__col-kind">
                    <span class="gys__kind">{{ kindLabels[item.kind] }}</span>
                  </td>
                  <td class="gys__col-size">{{ item.is_dir ? "-" : formatSize(item.size) }}</td>
                  <td class="gys__col-time">{{ formatTime(item.mod_time) }}</td>
                </tr>
                <tr v-if="!pageItems.length">
                  <td colspan="5" class="gys__noresult">
                    {{ keyword ? "没有匹配的文件" : provider === "quark" ? "收藏文件夹为空" : "此文件夹为空" }}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>

          <div v-if="hasSource && sortedItems.length > pageSize" class="gys__pager">
            <span>共 {{ sortedItems.length }} 项</span>
            <div class="gys__pager-btns">
              <button type="button" :disabled="page <= 1" @click="goPage(-1)">
                <SvgIcon name="chevron-left" :size="13" />
              </button>
              <span>{{ page }} / {{ totalPages }}</span>
              <button type="button" :disabled="page >= totalPages" @click="goPage(1)">
                <SvgIcon name="chevron-right" :size="13" />
              </button>
            </div>
          </div>

        </div>
          </section>
          <section class="gys__pair gys__pair--dest">
            <aside class="gys__destcol">
            <section
              v-if="hasSource"
              class="gys__panel gys__panel--dest"
              :class="{ 'is-busy': transferring }"
            >
              <div class="gys__panel-head">
                <h4>保存位置</h4>
                <span class="gys__panel-hint">不存在时自动创建</span>
              </div>
              <AccountFolderField
                class="gys__basefield"
                :display="destinationDisplay"
                :placeholder="provider === 'quark' ? '选择当前夸克账号内的目录' : '选择入库账号和目录'"
                browse-label="选择"
                :title="destinationDisplay || '选择入库账号和目录'"
                @browse="openPicker"
              />
              <label class="gys__field">
                <span class="gys__field-label">目标文件夹</span>
                <input
                  :value="folderTitle"
                  class="gys__field-input"
                  type="text"
                  maxlength="120"
                  :placeholder="provider === 'quark' ? '默认取收藏文件夹名' : '默认为分享标题，可修改'"
                  :disabled="transferring"
                  @input="onFolderTitleInput"
                  @blur="normalizeFolderTitle"
                />
              </label>
              <p v-if="provider === 'quark'" class="gys__field-tip">
                入库时会同步重命名「来自：分享」中的收藏文件夹。
              </p>
              <div class="gys__field-row">
                <div class="gys__type-toggle" role="group" aria-label="内容类型">
                  <button
                    type="button"
                    class="gys__type-btn"
                    :class="{ 'is-active': isSeries }"
                    :disabled="transferring"
                    :aria-pressed="isSeries"
                    @click="isSeries = true"
                  >
                    剧集
                  </button>
                  <button
                    type="button"
                    class="gys__type-btn"
                    :class="{ 'is-active': !isSeries }"
                    :disabled="transferring"
                    :aria-pressed="!isSeries"
                    @click="isSeries = false"
                  >
                    电影
                  </button>
                </div>
                <div v-if="isSeries" class="gys__season-box">
                  <span>Season</span>
                  <input
                    v-model.number="season"
                    class="gys__season-input"
                    type="number"
                    min="0"
                    max="99"
                    step="1"
                    :disabled="transferring"
                    @blur="normalizeSeasonInput"
                  />
                </div>
              </div>
              <div class="gys__preview" :title="targetPathDisplay">
                <span class="gys__preview-label">预览</span>
                <span class="gys__preview-path">{{ targetPathDisplay }}</span>
              </div>
            </section>
              <div v-if="!hasSource" class="gys__empty gys__empty--small">
                <SvgIcon name="hand-folder" :size="34" />
                <p>选择收藏后设置保存位置</p>
              </div>
            </aside>
            <div class="gys__targetcol">
              <BueiIngestTargetFiles
                v-if="hasSource && targetSegments.length"
                show-missing
                title="目标文件夹浏览"
                :account-id="destination.accountId"
                :path="targetPath"
                :refresh-key="targetFilesRefreshKey"
              />
              <div v-else class="gys__empty gys__empty--small">
                <SvgIcon name="folder" :size="34" />
                <p>{{ hasSource ? "填写目标文件夹后显示" : "选择收藏后显示目标文件夹" }}</p>
              </div>
            </div>
          </section>
        </div>
        <aside class="gys__sidecol">
        <section class="gys__side gys__side--tmdb">
          <div class="gys__side-scroll">
            <div v-if="!hasSource" class="gys__empty gys__empty--small">
              <SvgIcon name="film" :size="36" />
              <p>选择收藏后匹配 TMDB</p>
            </div>
            <section
              v-if="hasSource"
              class="gys__panel gys__panel--tmdb"
              :class="{ 'is-busy': transferring }"
            >
              <div class="gys__panel-head">
                <h4>TMDB</h4>
                <span class="gys__panel-hint">
                  {{ tmdbLoading ? "匹配中…" : tmdbSelectedID ? "已应用" : "可选" }}
                </span>
              </div>

              <div class="gys__tmdb-queryrow">
                <span class="gys__tmdb-query" :title="tmdbSearchTitle || '请先填写目标文件夹'">
                  <span class="gys__tmdb-query-type">{{ isSeries ? "剧集" : "电影" }}</span>
                  <span class="gys__tmdb-query-name">{{ tmdbSearchTitle || "请先填写目标文件夹" }}</span>
                </span>
                <AppButton
                  class="gys__tmdb-match"
                  size="sm"
                  variant="secondary"
                  :disabled="!tmdbSearchTitle || transferring || tmdbLoading"
                  @click="searchTMDB"
                >
                  {{ tmdbLoading ? "匹配中…" : "匹配" }}
                </AppButton>
              </div>

              <p v-if="tmdbError" class="gys__rule-error gys__tmdb-error">{{ tmdbError }}</p>
              <p v-else-if="!tmdbSearchTitle" class="gys__tmdb-empty">填写目标文件夹后可匹配 TMDB。</p>

              <div v-if="tmdbResults.length" class="gys__tmdb-results">
                <div
                  v-for="item in sortedTmdbResults"
                  :key="`${tmdbResultType(item)}:${item.id}`"
                  class="gys__tmdb-result"
                  :class="{ 'is-selected': isTmdbSelected(item) }"
                >
                  <a
                    class="gys__tmdb-result-link"
                    :href="tmdbWebURL(item)"
                    target="_blank"
                    rel="noopener noreferrer"
                    :title="`在 TMDB 打开 ${tmdbResultTitle(item)}`"
                  >
                    <span class="gys__tmdb-result-body">
                      <span class="gys__tmdb-result-title">
                        {{ tmdbResultTitle(item) }}
                        <span v-if="tmdbResultYear(item)" class="gys__tmdb-year">({{ tmdbResultYear(item) }})</span>
                      </span>
                      <span class="gys__tmdb-result-sub">
                        {{ tmdbResultTypeLabel(item) }}
                        <template v-if="tmdbResultOriginal(item)"> · {{ tmdbResultOriginal(item) }}</template>
                      </span>
                    </span>
                    <SvgIcon name="arrow-up-right-from-square" :size="11" />
                  </a>
                  <button
                    type="button"
                    class="gys__tmdb-pick"
                    :class="{ 'is-selected': isTmdbSelected(item) }"
                    :disabled="transferring"
                    @click="selectTMDBResult(item)"
                  >
                    {{ isTmdbSelected(item) ? "已选" : "选择" }}
                  </button>
                </div>
              </div>
              <p v-else-if="tmdbSearched && !tmdbLoading" class="gys__tmdb-empty">没有找到匹配项。</p>
            </section>
          </div>
        </section>
        <section class="gys__side gys__side--rename">
          <div class="gys__side-scroll">
            <div v-if="!hasSource" class="gys__empty gys__empty--small">
              <SvgIcon name="wand-magic-sparkles" :size="36" />
              <p>选择收藏后设置重命名</p>
            </div>
            <section
              v-if="hasSource"
              class="gys__panel gys__panel--rename"
              :class="{ 'is-busy': transferring }"
            >
              <div class="gys__panel-head">
                <h4>重命名</h4>
                <span class="gys__panel-hint">{{ renameActive ? "入库后自动应用" : "可选" }}</span>
              </div>

              <label class="gys__rule" :class="{ 'is-disabled': !renamePrefixBase }">
                <input
                  v-model="renamePrefixOn"
                  type="checkbox"
                  :disabled="transferring || !renamePrefixBase"
                  @change="renamePrefixTouched = true"
                />
                <span class="gys__rule-body">
                  <span class="gys__rule-title">
                    添加前缀
                    <code>{{ renamePrefixBase || "U" }} + 连接符</code>
                  </span>
                  <span class="gys__rule-desc">
                    {{ isSeries ? "剧集去年份与首尾空格" : "电影保留年份" }}；均去掉 {tmdb-ID}；连接符沿用原名中的 . - _ 空格，否则用 -
                  </span>
                </span>
              </label>

              <label class="gys__rule" :class="{ 'is-disabled': !isSeries }">
                <input
                  v-model="renameEpisodeOn"
                  type="checkbox"
                  :disabled="transferring || !isSeries"
                  @change="renameEpisodeTouched = true"
                />
                <span class="gys__rule-body">
                  <span class="gys__rule-title">
                    补季数集数
                    <code>{{ `S${pad2(seasonNumber)}Enn` }}</code>
                  </span>
                  <span class="gys__rule-desc">{{ isSeries ? "原名没有 SxxEnn 时补在名称前" : "仅剧集可用" }}</span>
                </span>
              </label>

              <div v-if="renameEpisodeActive" class="gys__rule-extra">
                <span class="gys__rule-label">起始集数</span>
                <input
                  v-model.number="renameStartEpisode"
                  class="gys__ep-input"
                  type="number"
                  min="1"
                  max="99"
                  step="1"
                  :disabled="transferring"
                />
                <span class="gys__rule-desc">按表格顺序递增</span>
              </div>

              <label class="gys__rule">
                <input
                  v-model="renameReplaceOn"
                  type="checkbox"
                  :disabled="transferring"
                  @change="onRenameReplaceToggle"
                />
                <span class="gys__rule-body">
                  <span class="gys__rule-title">替换文本</span>
                  <span class="gys__rule-desc">支持正则、可多组按顺序替换，替换内容可留空</span>
                </span>
              </label>

              <div v-if="renameReplaceOn" class="gys__replace-list">
                <div
                  v-for="(rule, index) in renameReplaceRules"
                  :key="index"
                  class="gys__replace"
                >
                  <div class="gys__replace-head">
                    <span class="gys__replace-no">{{ index + 1 }}</span>
                    <span class="gys__replace-title">第 {{ index + 1 }} 组</span>
                    <button
                      type="button"
                      class="gys__replace-del"
                      :title="renameReplaceRules.length > 1 ? '删除这组' : '清空这组'"
                      :disabled="transferring"
                      @click="removeRenameReplaceRule(index)"
                    >
                      <SvgIcon name="trash" :size="12" />
                    </button>
                  </div>
                  <div class="gys__replace-row">
                    <input
                      v-model="rule.find"
                      class="gys__replace-input"
                      type="text"
                      placeholder="查找（正则）"
                      :disabled="transferring"
                    />
                    <input
                      v-model="rule.replaceWith"
                      class="gys__replace-input"
                      type="text"
                      placeholder="替换为（可留空）"
                      :disabled="transferring"
                    />
                  </div>
                  <p v-if="renameRuleErrors[index]" class="gys__rule-error">{{ renameRuleErrors[index] }}</p>
                </div>
                <button
                  type="button"
                  class="gys__replace-add"
                  :disabled="transferring"
                  @click="addRenameReplaceRule"
                >
                  <SvgIcon name="plus" :size="12" />
                  增加一组替换
                </button>
              </div>
            </section>

          </div>
        </section>
        <section class="gys__side gys__side--post">
          <div class="gys__side-scroll">
            <div v-if="!hasSource" class="gys__empty gys__empty--small">
              <SvgIcon name="bolt" :size="36" />
              <p>选择收藏后设置联动动作</p>
            </div>
            <section
              v-if="hasSource"
              class="gys__panel gys__panel--post"
              :class="{ 'is-busy': transferring }"
            >
              <div class="gys__panel-head">
                <h4>联动动作</h4>
                <span class="gys__panel-hint">按顺序执行</span>
              </div>
              <div class="gys__switch-row">
                <span class="gys__switch-text">
                  <span class="gys__switch-title">
                    <span class="gys__action-no">1</span>生成 STRM
                  </span>
                  <span class="gys__switch-desc">按目标目录命中的 STRM 任务生成/更新 STRM 文件</span>
                </span>
                <button
                  type="button"
                  class="gys__switch"
                  :class="{ 'is-on': autoStrm }"
                  role="switch"
                  :aria-checked="autoStrm"
                  aria-label="生成 STRM"
                  :disabled="transferring"
                  @click="autoStrm = !autoStrm"
                >
                  <span class="gys__switch-knob" />
                </button>
              </div>

              <div class="gys__switch-row" :class="{ 'is-disabled': !autoStrm || !scrapeAvailable }">
                <span class="gys__switch-text">
                  <span class="gys__switch-title">
                    <span class="gys__action-no">2</span>刮削元数据
                  </span>
                  <span class="gys__switch-desc">
                    <template v-if="!autoStrm">需先开启「生成 STRM」</template>
                    <template v-else-if="!scrapeAvailable">暂无 STRM 任务可刮削</template>
                    <template v-else>入库后重建刮削索引，只刮本次入库的作品</template>
                  </span>
                </span>
                <button
                  type="button"
                  class="gys__switch"
                  :class="{ 'is-on': autoScrape && autoStrm && scrapeAvailable }"
                  role="switch"
                  :aria-checked="autoScrape && autoStrm && scrapeAvailable"
                  aria-label="刮削元数据"
                  :disabled="transferring || !autoStrm || !scrapeAvailable"
                  @click="autoScrape = !autoScrape"
                >
                  <span class="gys__switch-knob" />
                </button>
              </div>

              <div v-if="autoStrm && autoScrape && scrapeAvailable" class="gys__scrape">
                <span class="gys__field-label">
                  刮削任务
                  <span class="gys__field-hint">（对应「STRM 刮削 - 海报墙」中的任务）</span>
                </span>
                <AppSelect
                  v-model="scrapeTaskValue"
                  class="gys__scrape-select"
                  :options="scrapeTaskOptions"
                  :placeholder="scrapeTasksLoading ? '加载中…' : '选择刮削任务'"
                  :disabled="transferring || scrapeTasksLoading"
                />
                <p v-if="scrapeTasksError" class="gys__rule-error">{{ scrapeTasksError }}</p>
              </div>

              <div class="gys__switch-row" :class="{ 'is-disabled': !embyAvailable }">
                <span class="gys__switch-text">
                  <span class="gys__switch-title">
                    <span class="gys__action-no">3</span>Emby 刷库
                  </span>
                  <span class="gys__switch-desc">
                    <template v-if="embyConfigsLoading">正在加载 Emby/Jellyfin 配置…</template>
                    <template v-else-if="!embyAvailable">系统内还没有配置 Emby/Jellyfin</template>
                    <template v-else-if="autoStrm && autoScrape && scrapeAvailable">
                      等刮削完成后触发，避免刷到未刮削的内容
                    </template>
                    <template v-else>入库后触发一次媒体库扫描</template>
                  </span>
                </span>
                <button
                  type="button"
                  class="gys__switch"
                  :class="{ 'is-on': embyRefresh && embyAvailable }"
                  role="switch"
                  :aria-checked="embyRefresh && embyAvailable"
                  aria-label="Emby 刷库"
                  :disabled="transferring || !embyAvailable || embyConfigsLoading"
                  @click="embyRefresh = !embyRefresh"
                >
                  <span class="gys__switch-knob" />
                </button>
              </div>

              <div v-if="embyRefresh && embyAvailable" class="gys__scrape">
                <span class="gys__field-label">Emby 配置</span>
                <AppSelect
                  v-model="embyConfigId"
                  class="gys__scrape-select"
                  :options="embyConfigOptions"
                  :placeholder="embyConfigsLoading ? '加载中…' : '选择 Emby/Jellyfin 配置'"
                  :disabled="transferring || embyConfigsLoading"
                />
                <span class="gys__field-label">执行范围</span>
                <AppSelect
                  v-model="embyMode"
                  class="gys__scrape-select"
                  :options="embyModeOptions"
                  :disabled="transferring"
                />
                <template v-if="embyMode === 'library'">
                  <span class="gys__field-label">媒体库</span>
                  <AppSelect
                    v-model="embyLibraryId"
                    class="gys__scrape-select"
                    :options="embyLibraryOptions"
                    :placeholder="embyLibrariesLoading ? '加载中…' : '选择媒体库'"
                    :disabled="transferring || embyLibrariesLoading || !embyConfigId"
                  />
                </template>
              </div>
              <p v-if="embyError" class="gys__rule-error">{{ embyError }}</p>
            </section>
          </div>
        </section>
        </aside>
      </div>
  </div>

  <BueiGuangyaShareConfirm
    :open="transferConfirmOpen"
    :target="pendingTransfer?.target || ''"
    :count="pendingTransfer?.count || 0"
    :renames="pendingTransfer?.renames || []"
    :unchanged-count="pendingTransfer?.unchangedCount || 0"
    :loading="transferring"
    :phase="transferPhase"
    :auto-strm="pendingTransfer?.autoStrm !== false"
    :done="transferDone"
    :renamed="transferResult.renamed"
    :rename-skipped="transferResult.renameSkipped"
    :strm-generated="transferResult.strmGenerated"
    :strm-matched="transferResult.strmMatched"
    :strm-error="transferResult.strmError"
    :auto-scrape="pendingTransfer?.autoScrape !== false"
    :scrape-task-id="pendingTransfer?.scrapeTaskId || 0"
    :scrape-started="transferResult.scrapeStarted"
    :scrape-error="transferResult.scrapeError"
    :scrape-progress="scrapeStatus"
    :refresh-emby="pendingTransfer?.refreshEmby !== false"
    :emby-refreshed="transferResult.embyRefreshed"
    :emby-scheduled="transferResult.embyScheduled"
    :emby-error="transferResult.embyError"
    :hint="
      pendingTransfer?.provider === 'quark'
        ? '仅移动勾选的文件，收藏文件夹会同步改名。'
        : '勾选的文件夹会连同其中的文件一起入库。'
    "
    @confirm="confirmTransfer"
    @cancel="cancelTransferConfirm"
    @stay="stayAfterTransfer"
    @open="openAfterTransfer"
  />

  <AppModal
    :open="favoriteDialogOpen"
    :title="favoriteEditing ? '修改收藏' : '添加收藏'"
    size="sm"
    @close="closeFavoriteDialog"
  >
    <div class="gys__fav-editor">
      <label class="gys__editor-field">
        <span>分享链接</span>
        <input
          v-model="favoriteLink"
          type="text"
          placeholder="粘贴光鸭或其他云盘分享链接"
          :disabled="favoriteSaving"
          @input="onFavoriteLinkInput"
        />
      </label>

      <label v-if="showFavoriteProviderSelect" class="gys__editor-field">
        <span>云盘</span>
        <AppSelect
          :model-value="favoriteProviderManual"
          :options="FAVORITE_PROVIDER_OPTIONS"
          :disabled="favoriteSaving"
          @update:model-value="onFavoriteProviderChange"
        />
        <small>不校验域名，可填写任意链接。</small>
      </label>

      <div v-if="favoriteProvider === 'guangya'" class="gys__editor-provider">
        <span class="gys__provider">光鸭</span>
        <span>{{ favoriteEditing ? "保存后自动重新解析" : "自动解析并收藏，提取码可选" }}</span>
      </div>

      <label v-if="favoriteProvider === 'guangya'" class="gys__editor-field">
        <span>提取码</span>
        <input
          v-model="favoritePassword"
          type="text"
          maxlength="12"
          placeholder="没有则留空"
          :disabled="favoriteSaving"
          @keydown.enter.prevent="submitFavorite"
        />
      </label>

      <template v-if="favoriteProvider === 'quark'">
        <label class="gys__editor-field">
          <span>夸克账号</span>
          <AppSelect
            :model-value="favoriteQuarkAccountId"
            :options="favoriteQuarkAccountOptions"
            placeholder="选择夸克账号"
            :disabled="favoriteSaving || !quarkAccounts.length"
            @update:model-value="onFavoriteQuarkAccountChange"
          />
        </label>
        <div class="gys__editor-field">
          <span>收藏文件夹</span>
          <button
            type="button"
            class="gys__editor-folder"
            :disabled="favoriteSaving || !favoriteQuarkInbox"
            @click="openFavoriteFolderPicker"
          >
            <SvgIcon name="hand-folder" :size="14" />
            <span :class="{ 'is-placeholder': !favoriteQuarkFolderName }">
              {{ favoriteQuarkFolderName || "选择「来自：分享」下的文件夹" }}
            </span>
            <span class="gys__editor-folder-action">选择</span>
          </button>
          <small v-if="favoriteQuarkInboxLoading">正在读取「来自：分享」…</small>
          <small v-else-if="favoriteQuarkInboxError" class="is-error">{{ favoriteQuarkInboxError }}</small>
          <small v-else>链接仅用于记录，不会解析夸克分享内容。</small>
        </div>
      </template>

      <p v-if="favoriteError" class="gys__editor-error">{{ favoriteError }}</p>
    </div>
    <template #footer>
      <AppButton variant="ghost" :disabled="favoriteSaving" @click="closeFavoriteDialog">取消</AppButton>
      <AppButton
        variant="primary"
        :loading="favoriteSaving"
        :disabled="favoriteProvider === 'quark' && !favoriteQuarkFolderId"
        @click="submitFavorite"
      >
        {{
          favoriteProvider === "guangya"
            ? favoriteEditing
              ? "保存并解析"
              : "解析并收藏"
            : "保存收藏"
        }}
      </AppButton>
    </template>
  </AppModal>

  <FolderPickerModal
    :open="favoriteFolderPickerOpen"
    nested
    :account-id="favoriteQuarkAccountId"
    :accounts="[]"
    :root-anchor="favoriteQuarkInbox ? { parentId: favoriteQuarkInbox.id, path: `/${favoriteQuarkInbox.name}`, label: favoriteQuarkInbox.name } : undefined"
    allow-create-folder
    title="选择收藏文件夹"
    confirm-text="使用当前文件夹"
    @close="favoriteFolderPickerOpen = false"
    @resolve="onFavoriteFolderResolved"
  />

  <FolderPickerModal
    :open="pickerOpen"
    nested
    selectable-account
    :accounts="pickerAccounts"
    :account-id="pickerAccountId"
    :initial-path="pickerInitialPath"
    allow-create-folder
    :title="pickerTitle"
    confirm-text="入库到此目录"
    @close="pickerOpen = false"
    @resolve="onFolderResolved"
  />
  </div>
</template>

<style scoped>
.gys {
  display: flex;
  flex-direction: column;
  width: 100%;
  background: var(--surface);
  border-radius: var(--radius-lg);
  overflow: hidden;
}

.gys__linkbar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 0 0 auto;
  padding: 12px 18px 8px;
}

.gys__input-wrap,
.gys__password {
  display: flex;
  align-items: center;
  gap: 7px;
  min-width: 0;
  flex: 1;
  height: 34px;
  padding: 0 10px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  transition: border-color 0.15s;
}

.gys__password {
  flex: 0 0 170px;
}

.gys__input-wrap:focus-within,
.gys__password:focus-within {
  border-color: var(--brand);
}

.gys__input-icon {
  color: var(--text-muted);
  flex-shrink: 0;
}

.gys__input {
  flex: 1;
  min-width: 0;
  height: 100%;
  border: none;
  outline: none;
  background: transparent;
  color: var(--text);
  font-size: 13px;
}

.gys__input-clear {
  display: grid;
  place-items: center;
  width: 20px;
  height: 20px;
  border: none;
  border-radius: var(--radius-xs);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
}

.gys__input-clear:hover {
  background: var(--surface-sunken);
  color: var(--text);
}

.gys__error {
  flex: 0 0 auto;
  margin: 0 18px 6px;
  padding: 7px 10px;
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--danger) 8%, var(--surface));
  color: var(--danger);
  font-size: 12px;
}

/* 三栏布局：左收藏、中文件浏览、右保存位置与重命名 */
.gys__body {
  display: grid;
  grid-template-columns: 232px minmax(0, 1fr) 304px;
}

.gys__savedcol {
  display: flex;
  flex-direction: column;
  padding: 2px 12px 12px 16px;
}

.gys__savedcol .gys__panel--saved {
  flex: 0 0 auto;
  max-height: none;
}

.gys__main {
  display: flex;
  flex-direction: column;
  min-width: 0;
  padding: 0 14px 10px;
  user-select: none;
}

.gys__main input,
.gys__main textarea {
  user-select: text;
}

.gys__side {
  display: flex;
  flex-direction: column;
  padding: 0 16px 12px;
}

.gys__side-scroll {
  display: flex;
  flex: 0 1 auto;
  flex-direction: column;
  padding-right: 2px;
}

.gys__sharehead {
  display: flex;
  align-items: baseline;
  gap: 8px;
  flex: 0 0 auto;
  min-width: 0;
  padding: 0 0 6px;
}

.gys__sharetitle {
  flex: 0 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 14px;
  font-weight: 600;
  color: var(--text);
}

.gys__sharesub {
  flex: 0 0 auto;
  font-size: 11px;
  color: var(--text-muted);
}

.gys__sharejump {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  align-self: center;
  width: 22px;
  height: 22px;
  margin-left: auto;
  flex: 0 0 auto;
  border-radius: var(--radius-xs);
  color: var(--text-muted);
  text-decoration: none;
}

.gys__sharejump:hover {
  background: var(--surface-hover);
  color: var(--brand);
}

.gys__crumbs {
  display: flex;
  align-items: center;
  gap: 4px;
  flex: 0 0 auto;
  min-width: 0;
  padding: 0 0 4px;
}

.gys__up {
  display: grid;
  place-items: center;
  width: 24px;
  height: 24px;
  flex: 0 0 auto;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  transition: color 0.15s ease, background 0.15s ease;
}

.gys__up:hover:not(:disabled) {
  background: var(--surface-hover);
  color: var(--brand);
}

.gys__up:disabled {
  opacity: 0.3;
  cursor: default;
}

.gys__crumb-list {
  display: flex;
  align-items: center;
  gap: 1px;
  flex: 1;
  min-width: 0;
  overflow-x: auto;
  scrollbar-width: none;
}

.gys__crumb-list::-webkit-scrollbar {
  display: none;
}

.gys__crumb {
  flex: 0 0 auto;
  max-width: min(48vw, 420px);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  padding: 3px 6px;
  border: none;
  border-radius: var(--radius-xs);
  background: transparent;
  color: var(--text-muted);
  font-size: 12px;
  cursor: pointer;
}

.gys__crumb:hover:not(:disabled) {
  background: var(--surface-hover);
  color: var(--brand);
}

.gys__crumb.is-current {
  color: var(--text);
  font-weight: 600;
  cursor: default;
}

.gys__crumb-sep {
  color: var(--text-muted);
  font-size: 11px;
}

.gys__quickbar {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  flex: 0 0 auto;
  min-width: 0;
  padding: 0 0 4px;
}

.gys__quick-label {
  flex: 0 0 auto;
  padding-top: 4px;
  color: var(--text-muted);
  font-size: 11px;
  white-space: nowrap;
}

.gys__quick-label.is-hint {
  color: color-mix(in srgb, var(--brand) 75%, var(--text-muted));
}

.gys__quick {
  display: flex;
  align-items: center;
  gap: 5px;
  flex: 1;
  min-width: 0;
  flex-wrap: wrap;
  justify-content: flex-start;
}

.gys__quick-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 25px;
  padding: 0 8px;
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-pill);
  background: var(--surface);
  color: var(--text-regular);
  font-size: 12px;
  cursor: pointer;
  white-space: nowrap;
}

.gys__quick-btn:hover:not(:disabled) {
  border-color: color-mix(in srgb, var(--brand) 45%, var(--border-soft));
  color: var(--brand);
}

.gys__quick-btn:disabled {
  opacity: 0.45;
  cursor: default;
}

.gys__quick-count {
  color: var(--text-muted);
  font-size: 11px;
}

.gys__toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 0 0 auto;
  padding: 0 0 6px;
}

.gys__search {
  display: flex;
  align-items: center;
  gap: 5px;
  flex: 1;
  min-width: 0;
  height: 28px;
  padding: 0 9px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  color: var(--text-muted);
}

.gys__search input {
  flex: 1;
  min-width: 0;
  border: none;
  outline: none;
  background: transparent;
  color: var(--text);
  font-size: 12px;
}

.gys__folder-count {
  flex: 0 0 auto;
  font-size: 11px;
  color: var(--text-muted);
  white-space: nowrap;
}

.gys__refresh {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  flex: 0 0 auto;
  height: 28px;
  padding: 0 8px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  color: var(--text-regular);
  font-size: 12px;
  cursor: pointer;
}

.gys__refresh:hover:not(:disabled) {
  border-color: color-mix(in srgb, var(--brand) 45%, var(--border));
  color: var(--brand);
}

.gys__refresh:disabled {
  opacity: 0.45;
  cursor: default;
}

.gys__discard {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  flex: 0 0 auto;
  height: 28px;
  padding: 0 8px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  color: var(--text-regular);
  font-size: 12px;
  cursor: pointer;
}

.gys__discard:hover:not(:disabled) {
  border-color: color-mix(in srgb, var(--danger) 45%, var(--border));
  color: var(--danger);
}

.gys__discard:disabled {
  opacity: 0.45;
  cursor: default;
}

.gys__targetfiles {
  flex: 0 0 auto;
  margin-top: 10px;
}

.gys__tablewrap {
  flex: 0 0 auto;
  /* 文件浏览表格限高，超出在表格内部滚动（表头已吸顶） */
  max-height: 600px;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  overflow: auto;
  -webkit-overflow-scrolling: touch;
  overscroll-behavior-x: contain;
}

.gys__table {
  width: 100%;
  min-width: 640px;
  border-collapse: collapse;
  font-size: 12px;
  table-layout: fixed;
}

.gys__table thead {
  position: sticky;
  top: 0;
  z-index: 1;
  background: var(--surface-sunken);
}

.gys__table th {
  padding: 7px 10px;
  text-align: left;
  font-size: 11px;
  font-weight: 600;
  color: var(--text-muted);
  border-bottom: 1px solid var(--border);
  white-space: nowrap;
}

.gys__table td {
  padding: 6px 10px;
  border-bottom: 1px solid var(--border-soft);
  vertical-align: middle;
}

.gys__th-sort {
  cursor: pointer;
  user-select: none;
}

.gys__th-sort:hover {
  color: var(--text);
}

.gys__th-sort.is-sorted {
  color: var(--brand);
}

.gys__th-sort .lp-svg-icon {
  vertical-align: -2px;
}

.gys__row {
  cursor: pointer;
}

.gys__row.is-static {
  cursor: default;
}

.gys__row:hover {
  background: var(--surface-hover);
}

.gys__row.is-selected {
  background: color-mix(in srgb, var(--brand) 6%, var(--surface));
}

.gys__row.is-included {
  background: color-mix(in srgb, var(--brand) 3%, var(--surface));
}

.gys__row.is-processed {
  background: var(--surface-sunken);
  cursor: default;
}

.gys__row.is-processed .gys__check {
  cursor: pointer;
}

.gys__row.is-processed:hover {
  background: var(--surface-sunken);
}

.gys__row.is-processed.is-selected {
  background: color-mix(in srgb, var(--brand) 6%, var(--surface));
}

.gys__row.is-processed .gys__name,
.gys__row.is-processed .gys__kind,
.gys__row.is-processed .gys__col-size,
.gys__row.is-processed .gys__col-time {
  color: var(--text-muted);
}

.gys__row.is-folder:hover .gys__name {
  color: var(--brand);
}

.gys__col-check {
  width: 30px;
  padding-right: 0 !important;
}

.gys__col-kind {
  width: 60px;
}

.gys__col-size {
  width: 80px;
  text-align: right !important;
  color: var(--text-muted);
}

.gys__col-time {
  width: 132px;
  color: var(--text-muted);
}

.gys__check {
  display: grid;
  place-items: center;
  width: 15px;
  height: 15px;
  padding: 0;
  border: 1px solid var(--border-strong, var(--border));
  border-radius: 3px;
  background: var(--surface);
  color: transparent;
  cursor: pointer;
  transition: background 0.12s ease, border-color 0.12s ease, color 0.12s ease;
}

.gys__check:hover:not(:disabled) {
  border-color: color-mix(in srgb, var(--brand) 55%, var(--border));
}

.gys__check.is-checked,
.gys__check.is-partial {
  border-color: var(--brand);
  background: var(--brand);
  color: #fff;
}

.gys__check-dash {
  width: 7px;
  height: 2px;
  border-radius: 1px;
  background: #fff;
}

.gys__check:focus-visible {
  outline: 2px solid color-mix(in srgb, var(--brand) 45%, transparent);
  outline-offset: 1px;
}

.gys__check:disabled {
  opacity: 0.45;
  cursor: default;
}

.gys__col-name {
  min-width: 0;
}

.gys__namecell {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.gys__fileicon {
  display: grid;
  place-items: center;
  width: 24px;
  height: 24px;
  flex-shrink: 0;
  border-radius: var(--radius-xs);
  background: var(--surface-sunken);
  color: var(--text-muted);
}

.gys__fileicon.is-folder {
  color: #d97706;
  background: color-mix(in srgb, #f59e0b 12%, var(--surface));
}

.gys__fileicon.is-video {
  color: #7c3aed;
  background: color-mix(in srgb, #8b5cf6 10%, var(--surface));
}

.gys__fileicon.is-audio {
  color: #0891b2;
  background: color-mix(in srgb, #06b6d4 10%, var(--surface));
}

.gys__fileicon.is-image {
  color: #db2777;
  background: color-mix(in srgb, #ec4899 10%, var(--surface));
}

.gys__fileicon.is-subtitle {
  color: #0f766e;
  background: color-mix(in srgb, #14b8a6 12%, var(--surface));
}

.gys__fileicon.is-document {
  color: #2563eb;
  background: color-mix(in srgb, #3b82f6 10%, var(--surface));
}

.gys__fileicon.is-archive {
  color: #b45309;
  background: color-mix(in srgb, #f59e0b 10%, var(--surface));
}

.gys__namebody {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-width: 0;
}

.gys__name,
.gys__name-new {
  display: flex;
  align-items: baseline;
  min-width: 0;
  white-space: nowrap;
}

.gys__name {
  color: var(--text);
}

.gys__name-stem {
  flex: 0 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}

.gys__name-ext {
  flex: 0 0 auto;
}

.gys__name-sub {
  display: flex;
  align-items: center;
  gap: 5px;
  min-width: 0;
  margin-top: 1px;
}

.gys__name-sub .gys__transferred-tag {
  flex: 0 0 auto;
  padding: 0 5px;
  line-height: 14px;
  font-size: 9px;
}

.gys__name-new {
  flex: 1 1 auto;
  min-width: 0;
  color: var(--brand);
  font-size: 10px;
  line-height: 1.4;
}

.gys__name-new.is-done {
  color: var(--text-muted);
}

.gys__transferred-tag {
  flex-shrink: 0;
  padding: 1px 6px;
  border-radius: var(--radius-pill);
  background: var(--surface-sunken);
  color: var(--text-muted);
  font-size: 10px;
}

.gys__enter {
  flex-shrink: 0;
  color: var(--text-muted);
}

.gys__included-tag {
  flex-shrink: 0;
  font-size: 10px;
  color: var(--text-muted);
  background: var(--surface-sunken);
  border-radius: var(--radius-pill);
  padding: 1px 6px;
}

.gys__kind {
  color: var(--text-regular);
}

.gys__noresult {
  padding: 36px 0 !important;
  text-align: center;
  color: var(--text-muted);
}

.gys__pager {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex: 0 0 auto;
  padding: 7px 0 0;
  font-size: 12px;
  color: var(--text-muted);
}

.gys__pager-btns {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.gys__pager-btns button {
  display: grid;
  place-items: center;
  width: 24px;
  height: 24px;
  border: 1px solid var(--border);
  border-radius: var(--radius-xs);
  background: var(--surface);
  color: var(--text-regular);
  cursor: pointer;
}

.gys__pager-btns button:disabled {
  opacity: 0.4;
  cursor: default;
}

/* 右栏面板 */
.gys__panel {
  display: flex;
  flex-direction: column;
  min-height: 0;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--surface);
}

.gys__panel + .gys__panel {
  margin-top: 10px;
}

.gys__panel--dest {
  flex: 0 0 auto;
  margin-top: 2px;
}

.gys__panel--dest.is-busy {
  opacity: 0.65;
  pointer-events: none;
}

.gys__panel--saved {
  flex: 0 0 auto;
  min-height: 120px;
}

.gys__panel-head {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 0 0 auto;
  min-width: 0;
  margin-bottom: 8px;
}

.gys__panel-head h4 {
  margin: 0;
  font-size: 12px;
  font-weight: 600;
  color: var(--text);
  white-space: nowrap;
}

.gys__panel-hint {
  margin-left: auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 11px;
  color: var(--text-muted);
}

.gys__panel-count {
  min-width: 17px;
  padding: 0 5px;
  border-radius: var(--radius-pill);
  background: var(--surface-sunken);
  color: var(--text-muted);
  font-size: 10px;
  line-height: 16px;
  text-align: center;
}

.gys__icon-btn {
  display: grid;
  place-items: center;
  width: 22px;
  height: 22px;
  margin-left: auto;
  flex: 0 0 auto;
  border: none;
  border-radius: var(--radius-xs);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
}

.gys__icon-btn:hover:not(:disabled) {
  background: var(--surface-hover);
  color: var(--text);
}

.gys__icon-btn:disabled {
  opacity: 0.45;
  cursor: default;
}

.gys__basefield :deep(.account-folder-field__main) {
  padding: 6px 10px;
  font-size: 12px;
}

.gys__basefield :deep(.account-folder-field__browse) {
  min-width: 54px;
  font-size: 12px;
}

.gys__field {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  margin-top: 8px;
}

.gys__field-label {
  flex: 0 0 52px;
  font-size: 11px;
  color: var(--text-muted);
  white-space: nowrap;
}

.gys__field-input {
  flex: 1;
  min-width: 0;
  height: 32px;
  padding: 0 9px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  color: var(--text);
  font-size: 12px;
  outline: none;
}

.gys__field-input:focus {
  border-color: var(--brand);
}

.gys__field-row {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  margin-top: 8px;
}

.gys__field-tip {
  margin: 6px 0 0;
  color: var(--text-muted);
  font-size: 10px;
  line-height: 1.5;
}

.gys__type-toggle {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  height: 32px;
  padding: 2px;
  flex: 1;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
}

.gys__type-btn {
  flex: 1;
  height: 26px;
  padding: 0 6px;
  border: none;
  border-radius: calc(var(--radius-sm) - 1px);
  background: transparent;
  color: var(--text-muted);
  font-size: 12px;
  cursor: pointer;
}

.gys__type-btn:hover:not(:disabled) {
  color: var(--text);
  background: var(--surface-hover);
}

.gys__type-btn.is-active {
  color: var(--brand);
  background: color-mix(in srgb, var(--brand) 10%, var(--surface));
  font-weight: 600;
}

.gys__type-btn:disabled {
  opacity: 0.55;
  cursor: default;
}

.gys__season-box {
  display: flex;
  align-items: center;
  gap: 6px;
  height: 32px;
  padding: 0 9px;
  flex: 0 0 auto;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  color: var(--text-muted);
  font-size: 12px;
}

.gys__season-input {
  width: 46px;
  border: none;
  outline: none;
  background: transparent;
  color: var(--text);
  font-size: 12px;
  font-weight: 600;
  text-align: center;
}

.gys__preview {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  min-width: 0;
  margin-top: 8px;
  padding: 5px 8px;
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-sm);
  background: var(--surface-sunken);
  font-size: 11px;
}

.gys__preview-label {
  flex: 0 0 auto;
  color: var(--text-muted);
}

.gys__preview-path {
  min-width: 0;
  color: var(--text-regular);
  line-height: 1.5;
  overflow-wrap: anywhere;
  word-break: break-word;
  white-space: normal;
}

.gys__panel--rename {
  flex: 0 0 auto;
}

.gys__panel--post {
  flex: 0 0 auto;
}

.gys__panel--post.is-busy {
  opacity: 0.65;
  pointer-events: none;
}

/* 联动动作开关行 */
.gys__switch-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 4px 0 2px;
}

.gys__switch-text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  flex: 1 1 auto;
  min-width: 0;
}

.gys__switch-title {
  color: var(--text);
  font-size: 12px;
}

.gys__switch-desc {
  color: var(--text-muted);
  font-size: 10px;
  line-height: 1.4;
}

.gys__switch {
  position: relative;
  flex: 0 0 auto;
  width: 38px;
  height: 22px;
  padding: 0;
  border: none;
  border-radius: var(--radius-pill);
  background: var(--border);
  cursor: pointer;
  transition: background 0.15s ease;
}

.gys__switch.is-on {
  background: var(--brand);
}

.gys__switch:disabled {
  cursor: default;
  opacity: 0.6;
}

.gys__switch-row.is-disabled {
  opacity: 0.55;
}

/* 联动动作的序号标记 */
.gys__action-no {
  display: inline-grid;
  place-items: center;
  width: 15px;
  height: 15px;
  margin-right: 5px;
  border-radius: 50%;
  background: color-mix(in srgb, var(--brand) 12%, var(--surface));
  color: var(--brand);
  font-size: 10px;
  font-weight: 600;
  vertical-align: 1px;
}

.gys__scrape {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 4px 0 0;
}

/* 覆盖横向字段布局的固定宽度，避免纵向排布时撑出空白 */
.gys__scrape .gys__field-label {
  flex: 0 0 auto;
  white-space: normal;
  line-height: 1.4;
}

.gys__field-hint {
  color: var(--text-muted);
  opacity: 0.85;
}

.gys__scrape-select :deep(.select__trigger) {
  padding: 6px 10px;
  font-size: 12px;
  line-height: 1.4;
}

.gys__switch-knob {
  position: absolute;
  top: 3px;
  left: 3px;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: #fff;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.2);
  transition: transform 0.15s ease;
}

.gys__switch.is-on .gys__switch-knob {
  transform: translateX(16px);
}

.gys__rule {
  display: flex;
  align-items: flex-start;
  gap: 7px;
  padding: 4px 0;
  cursor: pointer;
}

.gys__rule.is-disabled {
  opacity: 0.5;
  cursor: default;
}

.gys__rule input[type="checkbox"] {
  width: 14px;
  height: 14px;
  margin: 1px 0 0;
  flex: 0 0 auto;
  accent-color: var(--brand);
  cursor: inherit;
}

.gys__rule-body {
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 0;
}

.gys__rule-title {
  display: flex;
  align-items: center;
  gap: 5px;
  min-width: 0;
  font-size: 12px;
  color: var(--text);
  white-space: nowrap;
}

.gys__rule-title code {
  padding: 0 5px;
  border-radius: var(--radius-xs);
  background: var(--surface-sunken);
  color: var(--text-regular);
  font-size: 10px;
  line-height: 16px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 140px;
}

.gys__rule-desc {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 10px;
  color: var(--text-muted);
}

.gys__rule-extra {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 2px 0 4px 21px;
  min-width: 0;
}

.gys__rule-label {
  font-size: 11px;
  color: var(--text-muted);
  white-space: nowrap;
}

.gys__ep-input {
  width: 48px;
  height: 26px;
  padding: 0 6px;
  border: 1px solid var(--border);
  border-radius: var(--radius-xs);
  background: var(--surface);
  color: var(--text);
  font-size: 12px;
  text-align: center;
  outline: none;
}

.gys__ep-input:focus {
  border-color: var(--brand);
}

.gys__replace-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin: 2px 0 4px 21px;
}

/* 每组替换独立成块，用序号和边框区分 */
.gys__replace {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 7px 8px 8px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--surface) 60%, var(--surface-hover));
}

.gys__replace-head {
  display: flex;
  align-items: center;
  gap: 6px;
}

.gys__replace-no {
  display: grid;
  place-items: center;
  width: 15px;
  height: 15px;
  border-radius: 50%;
  background: color-mix(in srgb, var(--brand) 14%, transparent);
  color: var(--brand);
  font-size: 9px;
  font-weight: 600;
  line-height: 1;
}

.gys__replace-title {
  color: var(--text-muted);
  font-size: 11px;
}

.gys__replace-del {
  display: grid;
  place-items: center;
  width: 20px;
  height: 20px;
  margin-left: auto;
  padding: 0;
  border: none;
  border-radius: var(--radius-xs);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
}

.gys__replace-del:hover:not(:disabled) {
  background: color-mix(in srgb, var(--danger) 12%, transparent);
  color: var(--danger);
}

.gys__replace-del:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.gys__replace-row {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.gys__replace-add {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  height: 26px;
  border: 1px dashed var(--border);
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted);
  font-size: 11px;
  cursor: pointer;
}

.gys__replace-add:hover:not(:disabled) {
  border-color: var(--brand);
  color: var(--brand);
}

.gys__replace-add:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.gys__replace-input {
  width: 100%;
  height: 28px;
  padding: 0 8px;
  border: 1px solid var(--border);
  border-radius: var(--radius-xs);
  background: var(--surface);
  color: var(--text);
  font-size: 12px;
  outline: none;
}

.gys__replace-input:focus {
  border-color: var(--brand);
}

.gys__rule-error {
  margin: 0;
  color: var(--danger);
  font-size: 10px;
  word-break: break-all;
}

.gys__saved-body {
  flex: 0 1 auto;
  margin: 0 -4px -2px;
  padding: 0 4px 2px;
}

.gys__saved-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 16px 10px;
  color: var(--text-muted);
  font-size: 11px;
  text-align: center;
}

.gys__saved-state.is-error {
  color: var(--danger);
}

.gys__saved-state button {
  border: none;
  background: transparent;
  color: var(--brand);
  font-size: 11px;
  cursor: pointer;
  padding: 2px 4px;
}

.gys__saved-list {
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.gys__saved-row {
  display: flex;
  align-items: center;
  gap: 2px;
  border-radius: var(--radius-sm);
}

.gys__saved-row:hover {
  background: var(--surface-hover);
}

.gys__saved-row.is-active {
  background: color-mix(in srgb, var(--brand) 12%, var(--surface));
  box-shadow: inset 2px 0 0 var(--brand);
}

.gys__saved-row.is-active .gys__saved-title {
  color: var(--brand);
  font-weight: 600;
}

.gys__saved-main {
  display: flex;
  align-items: center;
  gap: 7px;
  flex: 1;
  min-width: 0;
  padding: 5px 6px;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  text-align: left;
  cursor: pointer;
}

.gys__saved-star {
  color: #d97706;
  flex-shrink: 0;
}

.gys__saved-meta {
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 0;
}

.gys__saved-title {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text);
  font-size: 12px;
}

.gys__saved-sub {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-muted);
  font-size: 10px;
}

.gys__saved-del {
  display: grid;
  place-items: center;
  width: 24px;
  height: 24px;
  flex: 0 0 auto;
  border: none;
  border-radius: var(--radius-xs);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
}

.gys__saved-del:hover {
  background: color-mix(in srgb, var(--danger) 10%, var(--surface));
  color: var(--danger);
}

.gys__empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  min-height: 0;
  color: var(--text-muted);
  padding: 30px 20px;
}

.gys__empty p {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  color: var(--text-regular);
}

.gys__empty small {
  font-size: 12px;
}

@media (max-width: 1120px) and (min-width: 981px) {
  .gys__body {
    grid-template-columns: 200px minmax(0, 1fr) 280px;
  }
}

/* 窄屏：收藏 → 保存位置 → TMDB → 重命名 → 联动动作 依次纵向排列，滚动交给页面。 */
@media (max-width: 980px) {
  .gys__body {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 10px 12px 0;
  }

  .gys__savedcol {
    order: 1;
    flex: 0 0 auto;
    padding: 0;
  }

  .gys__main {
    order: 2;
    flex: 0 0 auto;
    padding: 0;
  }

  /* 让保存位置与重命名参与同一列排布 */
  .gys__side {
    display: contents;
  }

  .gys__side-scroll {
    order: 3;
    flex: 0 0 auto;
    padding: 0;
  }

  .gys__panel--saved {
    min-height: 0;
  }

  .gys__panel--dest,
  .gys__panel--rename {
    flex: 0 0 auto;
  }
}

@media (max-width: 600px) {
  .gys {
    border-radius: var(--radius-md);
  }

  .gys__body {
    padding: 8px 10px 0;
  }

  .gys__table {
    min-width: 540px;
  }
}

/* 影视入库：左侧收藏+文件、保存位置+目标浏览两张大卡片；右侧 TMDB / 重命名 / 联动动作自适应高度。 */
.gys__body {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 330px;
  gap: 10px;
  padding: 12px 16px 14px;
  align-items: start;
}

.gys__left,
.gys__sidecol {
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-width: 0;
}

.gys__sidecol {
  align-self: start;
}

.gys__pair {
  display: grid;
  grid-template-columns: 232px minmax(0, 1fr);
  min-width: 0;
  min-height: 0;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--surface);
  overflow: hidden;
}

.gys__pair--source {
  height: clamp(380px, 52vh, 520px);
}

.gys__pair--dest {
  height: clamp(260px, 38vh, 360px);
}

.gys__savedcol {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  height: 100%;
  padding: 0;
  border-right: 1px solid var(--border-soft);
}

.gys__savedcol .gys__panel--saved {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  min-height: 0;
  padding: 14px 12px 10px;
  border: 0;
  border-radius: 0;
  background: transparent;
}

.gys__saved-body {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
}

.gys__saved-warning {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 6px;
  padding: 5px 7px;
  border-radius: var(--radius-xs);
  background: color-mix(in srgb, var(--danger) 8%, var(--surface));
  color: var(--danger);
  font-size: 10px;
  line-height: 1.4;
}

.gys__saved-warning button {
  margin-left: auto;
  border: none;
  background: transparent;
  color: inherit;
  font-size: 10px;
  cursor: pointer;
  text-decoration: underline;
}

.gys__saved-actions {
  display: inline-flex;
  align-items: center;
  gap: 1px;
  flex: 0 0 auto;
}

.gys__saved-edit {
  display: grid;
  place-items: center;
  width: 24px;
  height: 24px;
  border: none;
  border-radius: var(--radius-xs);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
}

.gys__saved-edit:hover:not(:disabled) {
  background: var(--surface-hover);
  color: var(--brand);
}

.gys__saved-edit:disabled {
  opacity: 0.4;
  cursor: default;
}

.gys__provider {
  display: inline-flex;
  align-items: center;
  height: 15px;
  padding: 0 5px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--brand) 10%, var(--surface-sunken));
  color: var(--brand);
  font-size: 9px;
  font-style: normal;
  line-height: 1;
}

.gys__provider.is-quark {
  background: color-mix(in srgb, var(--success, #1a9f5b) 12%, var(--surface-sunken));
  color: var(--success, #1a9f5b);
}

.gys__panel-head .gys__icon-btn {
  margin-left: 0;
}

.gys__icon-btn--add {
  margin-left: auto;
  color: var(--brand);
}

.gys__pair--source .gys__main {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  padding: 12px 14px 10px;
  overflow: hidden;
}

.gys__pair--source .gys__tablewrap {
  flex: 1 1 auto;
  min-height: 0;
  max-height: none;
  overflow: auto;
  overscroll-behavior: contain;
}

.gys__pair--source .gys__table {
  min-width: 580px;
}

.gys__pair--source .gys__empty {
  flex: 1 1 auto;
}

.gys__pair--source .gys__toolbar,
.gys__pair--source .gys__quickbar {
  flex-wrap: wrap;
}

.gys__destcol {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  padding: 10px 12px;
  overflow-y: auto;
  border-right: 1px solid var(--border-soft);
}

.gys__destcol .gys__empty {
  flex: 1 1 auto;
}

.gys__destcol .gys__panel--dest {
  padding: 0;
  border: 0;
  border-radius: 0;
  background: transparent;
}

.gys__targetcol {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  padding: 10px 12px;
  overflow: auto;
}

.gys__targetcol :deep(.btf) {
  flex: 1 1 auto;
  height: 100%;
}

.gys__targetcol :deep(.btf__tablewrap) {
  flex: 1 1 auto;
  min-height: 0;
  max-height: none;
}

.gys__side,
.gys__side--rename,
.gys__side--post {
  display: flex;
  flex-direction: column;
  min-width: 0;
  height: auto;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--surface);
  overflow: visible;
}

.gys__side-scroll {
  display: block;
  flex: 0 0 auto;
  min-height: 0;
  padding-right: 0;
  overflow: visible;
}

.gys__side .gys__panel {
  padding: 0;
  border: 0;
  border-radius: 0;
  background: transparent;
}

.gys__side--tmdb {
  max-height: 300px;
  overflow: hidden;
}

.gys__side--tmdb .gys__side-scroll {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  min-height: 0;
  max-height: 300px;
  overflow: hidden;
}

.gys__side--tmdb .gys__panel--tmdb {
  min-height: 0;
}

.gys__tmdb-queryrow {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.gys__tmdb-query {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 1;
  min-width: 0;
}

.gys__tmdb-query-type {
  flex: 0 0 auto;
  padding: 1px 5px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--brand) 10%, var(--surface-sunken));
  color: var(--brand);
  font-size: 9px;
  line-height: 15px;
}

.gys__tmdb-query-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text);
  font-size: 11px;
}

.gys__tmdb-match {
  flex: 0 0 auto;
}

.gys__tmdb-error {
  margin-top: 6px;
}

.gys__tmdb-empty {
  margin: 6px 0 0;
  color: var(--text-muted);
  font-size: 10px;
}

.gys__tmdb-results {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  gap: 5px;
  min-height: 0;
  margin-top: 8px;
  padding-right: 2px;
  overflow-y: auto;
}

.gys__tmdb-result {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 7px;
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-sm);
  background: var(--surface-sunken);
}

.gys__tmdb-result.is-selected {
  border-color: color-mix(in srgb, var(--brand) 45%, var(--border));
  background: color-mix(in srgb, var(--brand) 7%, var(--surface-sunken));
}

.gys__tmdb-result-link {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 1;
  min-width: 0;
  color: inherit;
  text-decoration: none;
}

.gys__tmdb-result-link:hover .gys__tmdb-result-title {
  color: var(--brand);
}

.gys__tmdb-result-body {
  display: flex;
  flex-direction: column;
  gap: 2px;
  flex: 1;
  min-width: 0;
}

.gys__tmdb-result-title {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text);
  font-size: 11px;
}

.gys__tmdb-year {
  color: var(--text-muted);
  font-weight: 400;
}

.gys__tmdb-result-sub {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-muted);
  font-size: 9px;
}

.gys__tmdb-pick {
  flex: 0 0 auto;
  height: 23px;
  padding: 0 7px;
  border: 1px solid var(--border);
  border-radius: var(--radius-xs);
  background: var(--surface);
  color: var(--text-muted);
  font-size: 10px;
  cursor: pointer;
}

.gys__tmdb-pick:hover:not(:disabled) {
  border-color: var(--brand);
  color: var(--brand);
}

.gys__tmdb-pick.is-selected {
  border-color: var(--brand);
  color: var(--brand);
  background: color-mix(in srgb, var(--brand) 7%, var(--surface));
}

.gys__tmdb-pick:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.gys__side .gys__empty {
  flex: 1 1 auto;
}

.gys__password-inline {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 0 0 auto;
  margin: 4px 0 10px;
  padding: 9px 10px;
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-sm);
  background: var(--surface-sunken);
  color: var(--text-muted);
  font-size: 12px;
}

.gys__password-input {
  flex: 1;
  min-width: 0;
  height: 30px;
  padding: 0 8px;
  border: 1px solid var(--border);
  border-radius: var(--radius-xs);
  background: var(--surface);
  color: var(--text);
  font-size: 12px;
  outline: none;
}

.gys__password-input:focus {
  border-color: var(--brand);
}

.gys__error {
  flex: 0 0 auto;
}

.gys__fav-editor {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.gys__editor-field {
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 12px;
  color: var(--text-muted);
}

.gys__editor-field > input,
.gys__editor-folder {
  width: 100%;
  height: 34px;
  padding: 0 10px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  color: var(--text);
  font-size: 12px;
  outline: none;
}

.gys__editor-field > input:focus {
  border-color: var(--brand);
}

.gys__editor-provider {
  display: flex;
  align-items: center;
  gap: 7px;
  margin-top: -4px;
  font-size: 11px;
  color: var(--text-muted);
}

.gys__editor-hint,
.gys__editor-error {
  margin: -4px 0 0;
  font-size: 11px;
  color: var(--danger);
}

.gys__editor-folder {
  display: flex;
  align-items: center;
  gap: 7px;
  text-align: left;
  cursor: pointer;
}

.gys__editor-folder:disabled {
  opacity: 0.55;
  cursor: default;
}

.gys__editor-folder > span:nth-child(2) {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.gys__editor-folder .is-placeholder {
  color: var(--text-muted);
}

.gys__editor-folder-action {
  margin-left: auto;
  flex: 0 0 auto;
  color: var(--brand);
  font-size: 11px;
}

.gys__editor-field small {
  font-size: 11px;
  color: var(--text-muted);
}

.gys__editor-field small.is-error {
  color: var(--danger);
}

.gys__icon-btn--add {
  margin-left: auto;
}

.gys__saved-body .gys__saved-row {
  min-height: 0;
}

.gys__empty--small {
  flex: 1 1 auto;
  min-height: 160px;
  padding: 20px 14px;
}

.gys__empty--small p {
  font-size: 12px;
  text-align: center;
}

@media (max-width: 1400px) {
  .gys__body {
    grid-template-columns: minmax(0, 1fr) 300px;
  }

  .gys__pair {
    grid-template-columns: 205px minmax(0, 1fr);
  }

  .gys__pair--source .gys__table {
    min-width: 460px;
  }

  .gys__pair--source .gys__col-kind {
    width: 52px;
  }

  .gys__pair--source .gys__col-size {
    width: 72px;
  }

  .gys__pair--source .gys__col-time {
    width: 112px;
  }

  .gys__targetcol :deep(.btf__table) {
    min-width: 420px;
  }

  .gys__targetcol :deep(.btf__col-time) {
    width: 132px;
  }
}

@media (max-width: 1180px) {
  .gys__body {
    grid-template-columns: minmax(0, 1fr) 300px;
  }

  .gys__pair {
    grid-template-columns: 210px minmax(0, 1fr);
  }
}

@media (max-width: 980px) {
  .gys__body {
    grid-template-columns: minmax(0, 1fr);
    padding: 10px 12px 12px;
  }

  .gys__pair {
    grid-template-columns: minmax(0, 1fr);
    grid-template-rows: 180px minmax(0, 1fr);
  }

  .gys__pair--source {
    height: 580px;
  }

  .gys__pair--dest {
    height: auto;
    grid-template-rows: auto 340px;
  }

  .gys__savedcol,
  .gys__destcol {
    border-right: 0;
    border-bottom: 1px solid var(--border-soft);
  }

  .gys__destcol {
    overflow: visible;
  }

  .gys__side {
    height: auto;
  }
}

@media (max-width: 640px) {
  .gys {
    border-radius: var(--radius-md);
  }

  .gys__body {
    padding: 8px 10px 10px;
  }

  .gys__table {
    min-width: 500px;
  }

}
</style>
