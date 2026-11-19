<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRouter } from "vue-router";
import type { FileItem } from "@/api/types";
import {
  bueiQuarkIngestApi,
  type QuarkIngestFile,
  type QuarkIngestPayload,
  type QuarkIngestPhase,
  type QuarkIngestProgress,
  type QuarkIngestPreset,
  type QuarkIngestRenameReplace,
} from "@/api/bueiQuarkIngest";
import { getApiErrorMessage } from "@/api/client";
import { filesApi } from "@/api/files";
import { fetchStrmTasks, type StrmTask } from "@/api/strm";
import { fetchStrmScrapeProgress, type StrmScrapeProgress } from "@/api/strmScrape";
import {
  fetchEmbyConfigs,
  fetchEmbyLibraries,
  type EmbyConfig,
  type EmbyLibrary,
} from "@/api/emby";
import { buildBrowserLocationQuery } from "@/utils/bueiBrowserLocation";
import { toast } from "@/composables/useToast";
import { formatSize } from "@/utils/format";
import AppSelect from "@/components/base/AppSelect.vue";
import AccountFolderField from "@/components/admin/AccountFolderField.vue";
import FolderPickerModal from "@/components/file/FolderPickerModal.vue";
import SvgIcon from "@/components/icons/SvgIcon.vue";
import BueiGuangyaShareConfirm, {
  type RenamePreviewRow,
  type ScrapeProgressView,
} from "@/components/buei/BueiGuangyaShareConfirm.vue";

// buei 定制：夸克入库的保存位置 / 重命名 / 联动动作设置面板。
// 流程：保存位置 → 重命名 → 联动动作（STRM / 刮削 / Emby 刷库）。

const props = defineProps<{
  accountId: number | null;
  accountName: string;
  favoriteId: number;
  sourceParentId: string;
  sourceFolderParentId: string;
  sourceFolderName: string;
  files: FileItem[];
}>();

const emit = defineEmits<{ done: [] }>();

type DestinationBase = {
  accountId: number;
  accountName: string;
  parentId: string;
  path: string;
  dirs: string[];
};

type RenameReplaceRule = { find: string; replaceWith: string };

type PendingIngest = {
  target: string;
  count: number;
  renames: RenamePreviewRow[];
  favoriteId: number;
  sourceParentId: string;
  sourceFolderParentId: string;
  sourceFolderName: string;
  files: QuarkIngestFile[];
  renameReplace: QuarkIngestRenameReplace;
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
};

const router = useRouter();

const destination = ref<DestinationBase>({
  accountId: 0,
  accountName: "",
  parentId: "",
  path: "/",
  dirs: [],
});
const folderTitle = ref("");
const isSeries = ref(true);
const season = ref(1);

const autoStrm = ref(true);
const autoScrape = ref(true);
const scrapeTaskId = ref<number | null>(null);
const scrapeTasks = ref<StrmTask[]>([]);
const scrapeTasksLoading = ref(false);
const scrapeTasksError = ref("");
const scrapeProgress = ref<StrmScrapeProgress | null>(null);

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
let restoringEmby = false;
let destinationTouched = false;

const pickerOpen = ref(false);
const submitting = ref(false);
const confirmOpen = ref(false);
const transferDone = ref(false);
const transferPhase = ref<QuarkIngestPhase>("");
const result = ref({
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
const pendingIngest = ref<PendingIngest | null>(null);

const SEASON_EPISODE_RE = /S\d{1,2}E\d{1,3}/i;
const PREFIX_CONNECTORS = [".", "-", "_", " "] as const;

const renamePrefixOn = ref(false);
const renamePrefixTouched = ref(false);
const renameEpisodeOn = ref(false);
const renameEpisodeTouched = ref(false);
const renameReplaceOn = ref(false);
const renameReplaceRules = ref<RenameReplaceRule[]>([{ find: "", replaceWith: "" }]);
const renameStartEpisode = ref(1);

let initSeq = 0;
let scrapePollTimer: number | undefined;
let scrapeWatchId = 0;
let scrapePollDeadline = 0;
let progressTimer: number | undefined;
let progressIdRef = "";

const selectedFiles = computed(() => props.files.filter((file) => !file.is_dir));
const selectedSize = computed(() =>
  selectedFiles.value.reduce((sum, file) => sum + (file.size || 0), 0),
);

const destinationDisplay = computed(() => {
  if (!destination.value.accountId) return "";
  const path = destination.value.path && destination.value.path !== "/" ? destination.value.path : "/";
  return `${destination.value.accountName || props.accountName} ${path}`;
});

const pickerAccountId = computed(() => props.accountId);
const pickerInitialPath = computed(() => destination.value.path || "/");

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
const embyEffective = computed(
  () =>
    embyRefresh.value &&
    embyAvailable.value &&
    !!embyConfigId.value &&
    (embyMode.value !== "library" || !!embyLibraryId.value),
);

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

const scrapeStatus = computed<ScrapeProgressView | null>(() => {
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

function clampSeason(value: number) {
  if (String(value ?? "").trim() === "") return 1;
  const number = Number(value);
  if (!Number.isFinite(number)) return 1;
  return Math.max(0, Math.min(99, Math.trunc(number)));
}

function seasonFolderName(value: number) {
  return `Season ${String(clampSeason(value)).padStart(2, "0")}`;
}

function sanitizeFolderName(value: string) {
  return String(value ?? "")
    .replace(/[\\/:*?"<>|]/g, " ")
    .replace(/[\x00-\x1f\x7f]/g, "")
    .replace(/\s+/g, " ")
    .trim()
    .slice(0, 100)
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

const renamePrefixBase = computed(() => {
  let base = sanitizedFolderTitle.value;
  // 电影保留年份；剧集按原规则去掉年份，避免前缀里出现年份。
  if (isSeries.value) {
    base = base.replace(/\s*[（(]\s*\d{4}\s*[）)]\s*/g, " ").trim();
  }
  return base;
});

function splitFileName(name: string) {
  const index = name.lastIndexOf(".");
  if (index <= 0) return { stem: name, ext: "" };
  return { stem: name.slice(0, index), ext: name.slice(index) };
}

function detectPrefixConnector(stem: string) {
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

const orderedSelectedFiles = computed(() => selectedFiles.value);
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

function renamedNameFor(file: FileItem) {
  if (file.is_dir) return "";
  const { stem, ext } = splitFileName(file.name);
  let base = stem;
  const prefix = renamePrefixBase.value;
  if (renamePrefixOn.value && prefix && !base.startsWith(prefix)) {
    base = `${prefix}${detectPrefixConnector(stem)}${base}`;
  }
  if (renameEpisodeActive.value) {
    const token = `S${pad2(seasonNumber.value)}E${pad2(renameStartEpisodeNumber.value + (episodeIndexByID.value.get(file.id) ?? 0))}`;
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
  base = base.replace(/^[-\s]+|[-\s]+$/g, "");
  if (!base) return file.name;
  return `${base}${ext}`;
}

const renamePreviewRows = computed<RenamePreviewRow[]>(() =>
  selectedFiles.value.map((file) => ({
    oldName: file.name,
    newName: renamedNameFor(file),
    path: file.id,
  })),
);

const changedRows = computed(() =>
  renamePreviewRows.value.filter((row) => row.newName !== row.oldName),
);

// 表格中选中文件下方的新文件名预览。
const renamePreviewMap = computed<Record<string, string>>(() => {
  const map: Record<string, string> = {};
  for (const row of renamePreviewRows.value) {
    if (row.path) map[row.path] = row.newName;
  }
  return map;
});

const targetSegments = computed(() => {
  if (!sanitizedFolderTitle.value) return [];
  return isSeries.value
    ? [sanitizedFolderTitle.value, seasonFolderName(seasonNumber.value)]
    : [sanitizedFolderTitle.value];
});
const targetPath = computed(() => joinPath(destination.value.path, targetSegments.value));
const targetPathDisplay = computed(() => {
  const account = destination.value.accountName || props.accountName || "未选择账号";
  return `${account} · ${targetPath.value}`;
});

const canIngest = computed(
  () =>
    !submitting.value &&
    !!props.accountId &&
    !!props.favoriteId &&
    !!props.sourceParentId &&
    !!sanitizedFolderTitle.value &&
    props.files.length > 0 &&
    destination.value.accountId === props.accountId &&
    !renameReplaceError.value,
);

function firstSelectedFileName(): string {
  const item = orderedSelectedFiles.value[0];
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

function applyAutoRenameRules() {
  const files = selectedFiles.value;
  if (!files.length) return;
  const prefix = renamePrefixBase.value;
  if (!renamePrefixTouched.value) {
    renamePrefixOn.value = !!prefix && files.some((item) => !splitFileName(item.name).stem.startsWith(prefix));
  }
  if (!renameEpisodeTouched.value) {
    renameEpisodeOn.value = isSeries.value && files.some((item) => !SEASON_EPISODE_RE.test(item.name));
  }
}

watch([selectedFiles, isSeries, renamePrefixBase], applyAutoRenameRules, { flush: "post" });

function applyRenameReplace(rule?: QuarkIngestRenameReplace | null) {
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

function defaultTitle(_files: FileItem[]) {
  // 目标文件夹名默认取收藏文件夹名；入库时会同步重命名「来自：分享」中的收藏文件夹。
  return sanitizeFolderName(props.sourceFolderName);
}

function normalizeFolderTitle() {
  folderTitle.value = sanitizeFolderName(
    folderTitle.value.replace(/（/g, "(").replace(/）/g, ")"),
  );
}

function onFolderTitleInput(event: Event) {
  const input = event.target as HTMLInputElement;
  const inputEvent = event as InputEvent;
  // 中文输入法组合期间不要改写输入框，等 compositionend 后的 input 再统一清洗。
  if (inputEvent.isComposing) return;
  const raw = input.value;
  const next = raw
    .replace(/（/g, "(")
    .replace(/）/g, ")")
    .replace(/^\s+/, "")
    .replace(/\s+$/, "");
  folderTitle.value = next;
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

function resetFormState() {
  destination.value = {
    accountId: props.accountId ?? 0,
    accountName: props.accountName,
    parentId: "",
    path: "/",
    dirs: [],
  };
  folderTitle.value = defaultTitle(props.files);
  isSeries.value = true;
  season.value = 1;
  autoStrm.value = true;
  autoScrape.value = true;
  scrapeTaskId.value = null;
  scrapeProgress.value = null;
  embyRefresh.value = true;
  embyConfigId.value = "";
  embyMode.value = "global";
  embyLibraryId.value = "";
  embyLibraries.value = [];
  embyError.value = "";
  renamePrefixOn.value = false;
  renamePrefixTouched.value = false;
  renameEpisodeOn.value = false;
  renameEpisodeTouched.value = false;
  renameReplaceOn.value = false;
  renameReplaceRules.value = [{ find: "", replaceWith: "" }];
  renameStartEpisode.value = 1;
  pickerOpen.value = false;
  submitting.value = false;
  destinationTouched = false;
  resetTransferDialog();
}

async function applyPreset(preset: QuarkIngestPreset | null) {
  if (!preset || !props.accountId) return;
  if (!destinationTouched) {
    destination.value = {
      accountId: props.accountId,
      accountName: props.accountName,
      parentId: preset.parent_id || "",
      path: preset.base_path || "/",
      dirs: Array.isArray(preset.base_dirs) ? [...preset.base_dirs] : [],
    };
  }
  isSeries.value = preset.is_series !== false;
  season.value = clampSeason(preset.season);
  autoStrm.value = preset.auto_strm !== false;
  autoScrape.value = preset.auto_scrape !== false;
  scrapeTaskId.value = preset.auto_scrape_task_id > 0 ? Number(preset.auto_scrape_task_id) : null;
  restoringEmby = true;
  embyRefresh.value = preset.refresh_emby !== false;
  embyConfigId.value = preset.emby_config_id || "";
  embyMode.value = preset.emby_mode === "library" ? "library" : "global";
  embyLibraryId.value = preset.emby_library_id || "";
  if (embyConfigsLoaded.value) ensureEmbyConfig();
  if (embyMode.value === "library" && embyConfigId.value) void loadEmbyLibraries();
  applyRenameReplace(preset.rename_replace ?? null);
  await nextTick();
  restoringEmby = false;
}

async function loadPreset(seq: number) {
  if (!props.accountId) return;
  try {
    const preset = await bueiQuarkIngestApi.preset(props.accountId);
    if (seq !== initSeq) return;
    await applyPreset(preset);
  } catch (error) {
    if (seq !== initSeq) return;
    toast.error(getApiErrorMessage(error, "加载夸克入库预设失败"));
  }
}

function ensureEmbyConfig() {
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

watch(autoStrm, (on) => {
  if (on) autoScrape.value = true;
});

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

function stopScrapePolling() {
  if (scrapePollTimer !== undefined) {
    window.clearTimeout(scrapePollTimer);
    scrapePollTimer = undefined;
  }
}

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

function openPicker() {
  if (submitting.value) return;
  if (!props.accountId) {
    toast.info("请先选择一个夸克账号");
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
  if (payload.accountId !== props.accountId) {
    toast.error("入库只能在当前夸克账号内进行");
    return;
  }
  destination.value = {
    accountId: payload.accountId,
    accountName: props.accountName,
    parentId: payload.parentId,
    path: payload.path || "/",
    dirs: Array.isArray(payload.dirs) ? [...payload.dirs] : [],
  };
  destinationTouched = true;
  pickerOpen.value = false;
}

function submitIngest() {
  if (!canIngest.value) {
    if (renameReplaceError.value) toast.error(`替换规则的正则表达式无效：${renameReplaceError.value}`);
    else if (!props.files.length) toast.info("请先勾选要入库的文件");
    else if (!sanitizedFolderTitle.value) toast.info("目标文件夹名称不能为空");
    else toast.info("请先选择保存位置");
    return;
  }
  if (!targetSegments.value.length && destination.value.parentId === props.sourceParentId) {
    toast.info("保存位置与「来自：分享」相同，请选择其他目录");
    return;
  }
  const base = { ...destination.value, dirs: [...destination.value.dirs] };
  const files: QuarkIngestFile[] = props.files.map((file) => ({
    file_id: file.id,
    original_name: file.name,
    new_name: file.is_dir ? file.name : renamedNameFor(file),
    is_dir: file.is_dir,
  }));
  const rules = renameReplaceRules.value
    .filter((rule) => !!rule.find)
    .map((rule) => ({ find: rule.find, replace_with: rule.replaceWith }));
  const renameReplace: QuarkIngestRenameReplace = {
    enabled: renameReplaceOn.value && rules.length > 0 && !renameReplaceError.value,
    find: rules[0]?.find ?? "",
    replace_with: rules[0]?.replace_with ?? "",
    rules,
  };
  pendingIngest.value = {
    target: targetPathDisplay.value,
    count: props.files.length,
    renames: changedRows.value,
    favoriteId: props.favoriteId,
    sourceParentId: props.sourceParentId,
    sourceFolderParentId: props.sourceFolderParentId,
    sourceFolderName: props.sourceFolderName,
    files,
    renameReplace,
    autoStrm: autoStrm.value,
    autoScrape: autoScrape.value && scrapeAvailable.value,
    scrapeTaskId: scrapeTaskId.value ? Number(scrapeTaskId.value) : 0,
    refreshEmby: embyEffective.value,
    embyConfigId: embyConfigId.value,
    embyMode: embyMode.value,
    embyLibraryId: embyLibraryId.value,
    unchangedCount: Math.max(0, selectedFiles.value.length - changedRows.value.length),
    segments: [...targetSegments.value],
    base,
    title: sanitizedFolderTitle.value,
    season: seasonNumber.value,
    series: isSeries.value,
  };
  transferDone.value = false;
  confirmOpen.value = true;
}

function resetTransferDialog() {
  confirmOpen.value = false;
  transferDone.value = false;
  pendingIngest.value = null;
  stopScrapePolling();
  stopProgressPolling();
  progressIdRef = "";
  transferPhase.value = "";
  scrapeWatchId = 0;
  scrapeProgress.value = null;
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

async function pollTransferProgress(id: string) {
  stopProgressPolling();
  try {
    const progress: QuarkIngestProgress = await bueiQuarkIngestApi.progress(id);
    if (progressIdRef !== id) return;
    if (progress.found && progress.phase) {
      transferPhase.value = progress.phase;
      if (progress.error) result.value.embyError = progress.error;
    }
  } catch {
    // 忽略单次失败，下一轮继续。
  } finally {
    if (progressIdRef === id && !isProgressFinished(transferPhase.value)) {
      progressTimer = window.setTimeout(() => void pollTransferProgress(id), 800);
    }
  }
}

function newProgressId() {
  const cryptoApi = globalThis.crypto;
  if (cryptoApi && typeof cryptoApi.randomUUID === "function") return cryptoApi.randomUUID();
  return `qi-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

async function confirmIngest() {
  const pending = pendingIngest.value;
  if (!pending || !pending.sourceParentId || !pending.base.accountId || submitting.value) return;
  submitting.value = true;
  transferPhase.value = "transfer";
  const progressId = newProgressId();
  progressIdRef = progressId;
  void pollTransferProgress(progressId);
  try {
    const payload: QuarkIngestPayload = {
      account_id: pending.base.accountId,
      favorite_id: pending.favoriteId,
      source_parent_id: pending.sourceParentId,
      source_folder_parent_id: pending.sourceFolderParentId,
      source_folder_name: pending.sourceFolderName,
      file_ids: pending.files.map((file) => file.file_id),
      parent_id: pending.base.parentId,
      target_path: pending.segments,
      base_path: pending.base.path || "/",
      base_dirs: pending.base.dirs,
      title: pending.title,
      season: pending.season,
      is_series: pending.series,
      files: pending.files,
      rename_replace: pending.renameReplace,
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
    result.value = {
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
    transferDone.value = true;
    const asyncPending = !!(result.value.scrapeStarted || result.value.embyScheduled);
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
    submitting.value = false;
  }
}

function cancelTransferConfirm() {
  if (submitting.value) return;
  const completed = transferDone.value;
  resetTransferDialog();
  if (completed) emit("done");
}

function stayAfterIngest() {
  emit("done");
  resetTransferDialog();
  toast.success("入库完成");
}

async function openAfterIngest() {
  const pending = pendingIngest.value;
  const base = pending ? { ...pending.base, dirs: [...pending.base.dirs] } : null;
  const segments = pending ? [...pending.segments] : [];
  emit("done");
  resetTransferDialog();
  if (base) await openTargetLocation(base, segments);
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
    const res = await filesApi.list(accountId, parentId, { forceRefresh: index >= forceFrom });
    const folder = res.items.find((item) => item.is_dir && item.name === name);
    if (!folder) throw new Error(`未找到目录：${name}`);
    parentId = folder.id;
    crumbs.push({ id: folder.id, name: folder.name });
  }
  return crumbs;
}

async function openTargetLocation(base: DestinationBase, segments: string[]) {
  let crumbs: { id: string; name: string }[];
  let fallback = false;
  try {
    crumbs = await resolveDirectoryCrumbs(base.accountId, base.path, base.dirs, segments);
  } catch {
    try {
      crumbs = await resolveDirectoryCrumbs(base.accountId, base.path, base.dirs, []);
      fallback = true;
    } catch {
      toast.error("无法定位保存位置，请到文件页手动打开目标目录");
      return;
    }
  }
  if (fallback) toast.info("目标文件夹暂未出现，已打开基础目录");
  try {
    const failure = await router.push({
      path: "/",
      query: buildBrowserLocationQuery({ accountId: base.accountId, crumbs }),
    });
    if (failure) toast.info("已取消跳转，可稍后手动打开目标目录");
  } catch {
    toast.error("打开前台页面失败");
  }
}

async function initialize() {
  const seq = ++initSeq;
  resetFormState();
  if (!props.accountId) return;
  await Promise.all([loadPreset(seq), loadScrapeTasks(), loadEmbyConfigs()]);
  if (seq !== initSeq) return;
  ensureScrapeTask();
  ensureEmbyConfig();
  applyAutoRenameRules();
}

const transferPhaseLabel = computed(() => {
  switch (transferPhase.value) {
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
      return "入库中…";
  }
});

const transferHint = computed(() => {
  if (!props.files.length) return "先在左侧勾选要入库的文件";
  return `文件合计 ${formatSize(selectedSize.value)}`;
});

defineExpose({
  selectedCount: computed(() => props.files.length),
  canIngest,
  submitting,
  dialogOpen: computed(() => confirmOpen.value),
  transferPhaseLabel,
  transferHint,
  renamePreviewMap,
  submitIngest,
});

onMounted(() => {
  void initialize();
});

onBeforeUnmount(() => {
  initSeq += 1;
  stopScrapePolling();
  stopProgressPolling();
});
</script>

<template>
  <div class="qi qi--embedded">
      <div class="qi__grid">
        <div class="qi__side">
          <section class="qi__panel">
            <div class="qi__panel-head">
              <h4>保存位置</h4>
              <span class="qi__panel-hint">不存在时自动创建</span>
            </div>
            <AccountFolderField
              class="qi__basefield"
              :display="destinationDisplay"
              placeholder="选择当前夸克账号内的目录"
              browse-label="选择"
              :title="destinationDisplay || '选择保存目录'"
              @browse="openPicker"
            />
            <label class="qi__field">
              <span class="qi__field-label">目标文件夹</span>
              <input
                :value="folderTitle"
                class="qi__field-input"
                type="text"
                maxlength="100"
                placeholder="默认取收藏文件夹名"
                :disabled="submitting"
                @input="onFolderTitleInput"
                @blur="normalizeFolderTitle"
              />
            </label>
            <p class="qi__field-tip">入库时会同步重命名「来自：分享」中的收藏文件夹。</p>
            <div class="qi__field-row">
              <div class="qi__type-toggle" role="group" aria-label="内容类型">
                <button
                  type="button"
                  class="qi__type-btn"
                  :class="{ 'is-active': isSeries }"
                  :disabled="submitting"
                  :aria-pressed="isSeries"
                  @click="isSeries = true"
                >
                  剧集
                </button>
                <button
                  type="button"
                  class="qi__type-btn"
                  :class="{ 'is-active': !isSeries }"
                  :disabled="submitting"
                  :aria-pressed="!isSeries"
                  @click="isSeries = false"
                >
                  电影
                </button>
              </div>
              <div v-if="isSeries && sanitizedFolderTitle" class="qi__season-box">
                <span>Season</span>
                <input
                  v-model.number="season"
                  class="qi__season-input"
                  type="number"
                  min="0"
                  max="99"
                  step="1"
                  :disabled="submitting"
                  @blur="normalizeSeasonInput"
                />
              </div>
            </div>
            <div class="qi__preview" :title="targetPathDisplay">
              <span class="qi__preview-label">预览</span>
              <span class="qi__preview-path">{{ targetPathDisplay }}</span>
            </div>
          </section>

          <section class="qi__panel">
            <div class="qi__panel-head">
              <h4>自动重命名</h4>
              <span class="qi__panel-hint">{{ renameActive ? "入库后自动应用" : "可选" }}</span>
            </div>

            <label class="qi__rule" :class="{ 'is-disabled': !renamePrefixBase }">
              <input
                v-model="renamePrefixOn"
                type="checkbox"
                :disabled="submitting || !renamePrefixBase"
                @change="renamePrefixTouched = true"
              />
              <span class="qi__rule-body">
                <span class="qi__rule-title">
                  添加前缀
                  <code>{{ renamePrefixBase || "U" }} + 连接符</code>
                </span>
                <span class="qi__rule-desc">
                  {{ isSeries ? "剧集去年份与首尾空格" : "电影保留年份" }}；连接符沿用原名中的 . - _ 空格，否则用 -
                </span>
              </span>
            </label>

            <label class="qi__rule" :class="{ 'is-disabled': !isSeries }">
              <input
                v-model="renameEpisodeOn"
                type="checkbox"
                :disabled="submitting || !isSeries"
                @change="renameEpisodeTouched = true"
              />
              <span class="qi__rule-body">
                <span class="qi__rule-title">
                  补季数集数
                  <code>{{ `S${pad2(seasonNumber)}Enn` }}</code>
                </span>
                <span class="qi__rule-desc">{{ isSeries ? "原名没有 SxxEnn 时补在名称前" : "仅剧集可用" }}</span>
              </span>
            </label>

            <div v-if="renameEpisodeActive" class="qi__rule-extra">
              <span class="qi__rule-label">起始集数</span>
              <input
                v-model.number="renameStartEpisode"
                class="qi__ep-input"
                type="number"
                min="1"
                max="99"
                step="1"
                :disabled="submitting"
              />
              <span class="qi__rule-desc">按列表顺序递增</span>
            </div>

            <label class="qi__rule">
              <input
                v-model="renameReplaceOn"
                type="checkbox"
                :disabled="submitting"
                @change="onRenameReplaceToggle"
              />
              <span class="qi__rule-body">
                <span class="qi__rule-title">替换文本</span>
                <span class="qi__rule-desc">支持正则、可多组按顺序替换，替换内容可留空</span>
              </span>
            </label>

            <div v-if="renameReplaceOn" class="qi__replace-list">
              <div v-for="(rule, index) in renameReplaceRules" :key="index" class="qi__replace">
                <div class="qi__replace-head">
                  <span class="qi__replace-no">{{ index + 1 }}</span>
                  <span class="qi__replace-title">第 {{ index + 1 }} 组</span>
                  <button
                    type="button"
                    class="qi__replace-del"
                    :title="renameReplaceRules.length > 1 ? '删除这组' : '清空这组'"
                    :disabled="submitting"
                    @click="removeRenameReplaceRule(index)"
                  >
                    <SvgIcon name="trash" :size="12" />
                  </button>
                </div>
                <div class="qi__replace-row">
                  <input
                    v-model="rule.find"
                    class="qi__replace-input"
                    type="text"
                    placeholder="查找（正则）"
                    :disabled="submitting"
                  />
                  <input
                    v-model="rule.replaceWith"
                    class="qi__replace-input"
                    type="text"
                    placeholder="替换为（可留空）"
                    :disabled="submitting"
                  />
                </div>
                <p v-if="renameRuleErrors[index]" class="qi__rule-error">{{ renameRuleErrors[index] }}</p>
              </div>
              <button type="button" class="qi__replace-add" :disabled="submitting" @click="addRenameReplaceRule">
                <SvgIcon name="plus" :size="12" />
                增加一组替换
              </button>
            </div>
          </section>

          <section class="qi__panel">
            <div class="qi__panel-head">
              <h4>联动动作</h4>
              <span class="qi__panel-hint">按顺序执行</span>
            </div>
            <div class="qi__switch-row">
              <span class="qi__switch-text">
                <span class="qi__switch-title"><span class="qi__action-no">1</span>生成 STRM</span>
                <span class="qi__switch-desc">按目标目录命中的 STRM 任务生成/更新 STRM 文件</span>
              </span>
              <button
                type="button"
                class="qi__switch"
                :class="{ 'is-on': autoStrm }"
                role="switch"
                :aria-checked="autoStrm"
                aria-label="生成 STRM"
                :disabled="submitting"
                @click="autoStrm = !autoStrm"
              >
                <span class="qi__switch-knob" />
              </button>
            </div>

            <div class="qi__switch-row" :class="{ 'is-disabled': !autoStrm || !scrapeAvailable }">
              <span class="qi__switch-text">
                <span class="qi__switch-title"><span class="qi__action-no">2</span>刮削元数据</span>
                <span class="qi__switch-desc">
                  <template v-if="!autoStrm">需先开启「生成 STRM」</template>
                  <template v-else-if="!scrapeAvailable">暂无 STRM 任务可刮削</template>
                  <template v-else>入库后重建刮削索引，只刮本次入库的作品</template>
                </span>
              </span>
              <button
                type="button"
                class="qi__switch"
                :class="{ 'is-on': autoScrape && autoStrm && scrapeAvailable }"
                role="switch"
                :aria-checked="autoScrape && autoStrm && scrapeAvailable"
                aria-label="刮削元数据"
                :disabled="submitting || !autoStrm || !scrapeAvailable"
                @click="autoScrape = !autoScrape"
              >
                <span class="qi__switch-knob" />
              </button>
            </div>

            <div v-if="autoStrm && autoScrape && scrapeAvailable" class="qi__sub">
              <span class="qi__field-label">
                刮削任务
                <span class="qi__field-hint">（对应「STRM 刮削 - 海报墙」中的任务）</span>
              </span>
              <AppSelect
                v-model="scrapeTaskValue"
                class="qi__select"
                :options="scrapeTaskOptions"
                :placeholder="scrapeTasksLoading ? '加载中…' : '选择刮削任务'"
                :disabled="submitting || scrapeTasksLoading"
              />
              <p v-if="scrapeTasksError" class="qi__rule-error">{{ scrapeTasksError }}</p>
            </div>

            <div class="qi__switch-row" :class="{ 'is-disabled': !embyAvailable }">
              <span class="qi__switch-text">
                <span class="qi__switch-title"><span class="qi__action-no">3</span>Emby 刷库</span>
                <span class="qi__switch-desc">
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
                class="qi__switch"
                :class="{ 'is-on': embyRefresh && embyAvailable }"
                role="switch"
                :aria-checked="embyRefresh && embyAvailable"
                aria-label="Emby 刷库"
                :disabled="submitting || !embyAvailable || embyConfigsLoading"
                @click="embyRefresh = !embyRefresh"
              >
                <span class="qi__switch-knob" />
              </button>
            </div>

            <div v-if="embyRefresh && embyAvailable" class="qi__sub">
              <span class="qi__field-label">Emby 配置</span>
              <AppSelect
                v-model="embyConfigId"
                class="qi__select"
                :options="embyConfigOptions"
                :placeholder="embyConfigsLoading ? '加载中…' : '选择 Emby/Jellyfin 配置'"
                :disabled="submitting || embyConfigsLoading"
              />
              <span class="qi__field-label">执行范围</span>
              <AppSelect
                v-model="embyMode"
                class="qi__select"
                :options="embyModeOptions"
                :disabled="submitting"
              />
              <template v-if="embyMode === 'library'">
                <span class="qi__field-label">媒体库</span>
                <AppSelect
                  v-model="embyLibraryId"
                  class="qi__select"
                  :options="embyLibraryOptions"
                  :placeholder="embyLibrariesLoading ? '加载中…' : '选择媒体库'"
                  :disabled="submitting || embyLibrariesLoading || !embyConfigId"
                />
              </template>
            </div>
            <p v-if="embyError" class="qi__rule-error">{{ embyError }}</p>
          </section>
        </div>
      </div>
    </div>

    <BueiGuangyaShareConfirm
    :open="confirmOpen"
    action-label="入库"
    hint="入库只移动收藏文件夹中选中的文件；目标文件夹名称与收藏文件夹名称保持一致。"
    :target="pendingIngest?.target || ''"
    :count="pendingIngest?.count || 0"
    :renames="pendingIngest?.renames || []"
    :unchanged-count="pendingIngest?.unchangedCount || 0"
    :loading="submitting"
    :phase="transferPhase"
    :auto-strm="pendingIngest?.autoStrm !== false"
    :done="transferDone"
    :renamed="result.renamed"
    :rename-skipped="result.renameSkipped"
    :strm-generated="result.strmGenerated"
    :strm-matched="result.strmMatched"
    :strm-error="result.strmError"
    :auto-scrape="pendingIngest?.autoScrape !== false"
    :scrape-task-id="pendingIngest?.scrapeTaskId || 0"
    :scrape-started="result.scrapeStarted"
    :scrape-error="result.scrapeError"
    :scrape-progress="scrapeStatus"
    :refresh-emby="pendingIngest?.refreshEmby !== false"
    :emby-refreshed="result.embyRefreshed"
    :emby-scheduled="result.embyScheduled"
    :emby-error="result.embyError"
    @confirm="confirmIngest"
    @cancel="cancelTransferConfirm"
    @stay="stayAfterIngest"
    @open="openAfterIngest"
  />

  <FolderPickerModal
    :open="pickerOpen"
    nested
    :account-id="pickerAccountId"
    :accounts="[]"
    :initial-path="pickerInitialPath"
    allow-create-folder
    title="选择夸克入库目录"
    confirm-text="保存到此目录"
    @close="pickerOpen = false"
    @resolve="onFolderResolved"
  />
</template>

<style scoped>
.qi {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 0;
}

.qi--embedded .qi__grid {
  grid-template-columns: minmax(0, 1fr);
  gap: 0;
}

.qi--embedded .qi__side {
  max-height: none;
  overflow: visible;
  padding-right: 0;
}

.qi__grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 330px;
  gap: 12px;
  min-height: 0;
}

.qi__items,
.qi__panel {
  display: flex;
  flex-direction: column;
  min-height: 0;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--surface);
}

.qi__items {
  overflow: hidden;
}

.qi__section-head,
.qi__panel-head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  border-bottom: 1px solid var(--border-soft);
}

.qi__section-head h4,
.qi__panel-head h4 {
  margin: 0;
  color: var(--text);
  font-size: 13px;
  font-weight: 600;
  white-space: nowrap;
}

.qi__section-head > span,
.qi__panel-hint {
  margin-left: auto;
  color: var(--text-muted);
  font-size: 11px;
  white-space: nowrap;
}

.qi__list {
  flex: 1 1 auto;
  min-height: 160px;
  max-height: min(50vh, 460px);
  overflow-y: auto;
  padding: 4px;
}

.qi__row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 8px;
  border-radius: var(--radius-sm);
  font-size: 12px;
}

.qi__row:hover {
  background: var(--surface-hover);
}

.qi__names {
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 0;
  flex: 1 1 auto;
}

.qi__name,
.qi__newname {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.qi__name {
  color: var(--text);
}

.qi__newname {
  color: var(--brand);
  font-size: 11px;
}

.qi__rowmeta {
  flex: 0 0 auto;
  color: var(--text-muted);
  font-size: 11px;
}

.qi__empty {
  margin: 0;
  padding: 32px 12px;
  text-align: center;
  color: var(--text-muted);
  font-size: 12px;
}

.qi__more {
  margin: 0;
  padding: 8px 12px;
  text-align: center;
  color: var(--text-muted);
  font-size: 11px;
}

.qi__tip {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 0;
  padding: 8px 12px;
  border-top: 1px solid var(--border-soft);
  color: var(--text-muted);
  font-size: 11px;
}

.qi__side {
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-height: 0;
  max-height: min(58vh, 560px);
  overflow-y: auto;
  padding-right: 2px;
}

.qi__panel {
  flex: 0 0 auto;
  padding: 10px 12px;
}

.qi__panel-head {
  padding: 0 0 8px;
  border-bottom: none;
}

.qi__basefield :deep(.account-folder-field__main) {
  padding: 6px 10px;
  font-size: 12px;
}

.qi__basefield :deep(.account-folder-field__browse) {
  min-width: 54px;
  font-size: 12px;
}

.qi__field {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  margin-top: 8px;
}

.qi__field-label {
  flex: 0 0 auto;
  color: var(--text-muted);
  font-size: 11px;
  white-space: nowrap;
}

.qi__field-hint {
  color: var(--text-muted);
  opacity: 0.85;
}

.qi__field-input {
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

.qi__field-input:focus {
  border-color: var(--brand);
}

.qi__field-tip {
  margin: 5px 0 0;
  color: var(--text-muted);
  font-size: 11px;
  line-height: 1.5;
}

.qi__field-row {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  margin-top: 8px;
}

.qi__type-toggle {
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

.qi__type-btn {
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

.qi__type-btn:hover:not(:disabled) {
  color: var(--text);
  background: var(--surface-hover);
}

.qi__type-btn.is-active {
  color: var(--brand);
  background: color-mix(in srgb, var(--brand) 10%, var(--surface));
  font-weight: 600;
}

.qi__type-btn:disabled {
  opacity: 0.55;
  cursor: default;
}

.qi__season-box {
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

.qi__season-input {
  width: 46px;
  border: none;
  outline: none;
  background: transparent;
  color: var(--text);
  font-size: 12px;
  font-weight: 600;
  text-align: center;
}

.qi__preview {
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

.qi__preview-label {
  flex: 0 0 auto;
  color: var(--text-muted);
}

.qi__preview-path {
  min-width: 0;
  color: var(--text-regular);
  line-height: 1.5;
  word-break: break-all;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.qi__rule {
  display: flex;
  align-items: flex-start;
  gap: 7px;
  padding: 4px 0;
  cursor: pointer;
}

.qi__rule.is-disabled {
  opacity: 0.5;
  cursor: default;
}

.qi__rule input[type="checkbox"] {
  width: 14px;
  height: 14px;
  margin: 1px 0 0;
  flex: 0 0 auto;
  accent-color: var(--brand);
  cursor: inherit;
}

.qi__rule-body {
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 0;
}

.qi__rule-title {
  display: flex;
  align-items: center;
  gap: 5px;
  min-width: 0;
  color: var(--text);
  font-size: 12px;
  white-space: nowrap;
}

.qi__rule-title code {
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

.qi__rule-desc {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-muted);
  font-size: 10px;
}

.qi__rule-extra {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 2px 0 4px 21px;
  min-width: 0;
}

.qi__rule-label {
  color: var(--text-muted);
  font-size: 11px;
  white-space: nowrap;
}

.qi__ep-input {
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

.qi__ep-input:focus {
  border-color: var(--brand);
}

.qi__replace-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin: 2px 0 4px 21px;
}

.qi__replace {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 7px 8px 8px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--surface) 60%, var(--surface-hover));
}

.qi__replace-head {
  display: flex;
  align-items: center;
  gap: 6px;
}

.qi__replace-no {
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

.qi__replace-title {
  color: var(--text-muted);
  font-size: 11px;
}

.qi__replace-del {
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

.qi__replace-del:hover:not(:disabled) {
  background: color-mix(in srgb, var(--danger) 12%, transparent);
  color: var(--danger);
}

.qi__replace-del:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.qi__replace-row {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.qi__replace-add {
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

.qi__replace-add:hover:not(:disabled) {
  border-color: var(--brand);
  color: var(--brand);
}

.qi__replace-add:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.qi__replace-input {
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

.qi__replace-input:focus {
  border-color: var(--brand);
}

.qi__rule-error {
  margin: 0;
  color: var(--danger);
  font-size: 10px;
  word-break: break-all;
}

.qi__switch-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 4px 0 2px;
}

.qi__switch-text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  flex: 1 1 auto;
  min-width: 0;
}

.qi__switch-title {
  color: var(--text);
  font-size: 12px;
}

.qi__switch-desc {
  color: var(--text-muted);
  font-size: 10px;
  line-height: 1.4;
}

.qi__switch {
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

.qi__switch.is-on {
  background: var(--brand);
}

.qi__switch:disabled {
  cursor: default;
  opacity: 0.6;
}

.qi__switch-row.is-disabled {
  opacity: 0.55;
}

.qi__action-no {
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

.qi__switch-knob {
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

.qi__switch.is-on .qi__switch-knob {
  transform: translateX(16px);
}

.qi__sub {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 4px 0 0;
}

.qi__sub .qi__field-label {
  white-space: normal;
  line-height: 1.4;
}

.qi__select :deep(.select__trigger) {
  padding: 6px 10px;
  font-size: 12px;
  line-height: 1.4;
}

@media (max-width: 760px) {
  .qi {
    padding: 0;
  }

  .qi__grid {
    grid-template-columns: minmax(0, 1fr);
  }

  .qi__list {
    max-height: 34vh;
  }

  .qi__side {
    max-height: none;
    overflow: visible;
    padding-right: 0;
  }
}
</style>
