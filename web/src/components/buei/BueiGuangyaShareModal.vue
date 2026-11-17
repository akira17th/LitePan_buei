<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRouter } from "vue-router";
import {
  bueiGuangyaShareApi,
  type GuangyaShareItem,
  type GuangyaShareKind,
  type GuangyaShareParseResult,
  type GuangyaShareBrowseCrumb,
  type GuangyaShareRenameReplace,
  type GuangyaShareSavedLink,
  type GuangyaShareTransferredFile,
} from "@/api/bueiGuangyaShare";
import { ApiError, getApiErrorMessage } from "@/api/client";
import { filesApi } from "@/api/files";
import { buildBrowserLocationQuery } from "@/utils/bueiBrowserLocation";
import { toast } from "@/composables/useToast";
import { confirm, showConfirm } from "@/composables/useConfirm";
import { formatSize, formatTime, formatTimeShort } from "@/utils/format";
import { useAccountsStore } from "@/stores/accounts";
import AppButton from "@/components/base/AppButton.vue";
import AppModal from "@/components/base/AppModal.vue";
import BusySpinner from "@/components/base/BusySpinner.vue";
import AccountFolderField from "@/components/admin/AccountFolderField.vue";
import FolderPickerModal from "@/components/file/FolderPickerModal.vue";
import SvgIcon from "@/components/icons/SvgIcon.vue";
import BueiGuangyaShareConfirm, {
  type RenamePreviewRow,
} from "@/components/buei/BueiGuangyaShareConfirm.vue";

// buei 定制：后台「辅助工具 / 增强工具 / 光鸭分享收藏」工作区。

const props = defineProps<{ open: boolean }>();
const emit = defineEmits<{ close: [] }>();

const router = useRouter();
const accountsStore = useAccountsStore();
const guangyaAccounts = computed(() =>
  accountsStore.accounts.filter((a) => a.driver_type === "guangya" && a.is_active),
);

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

const url = ref("");
const urlInputRef = ref<HTMLInputElement | null>(null);
const crumbListRef = ref<HTMLElement | null>(null);
const password = ref("");
const parsing = ref(false);
const parseError = ref("");
const passwordPrompt = ref(false);
const result = ref<GuangyaShareParseResult | null>(null);
const parsedURL = ref("");
let parseAbort: AbortController | null = null;

const savedLinks = ref<GuangyaShareSavedLink[]>([]);
const savedLoading = ref(false);
const savedError = ref("");
const activeSavedLinkId = ref<number | null>(null);

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

const selectedIds = ref<Set<string>>(new Set());
const selectedMap = ref<Map<string, GuangyaShareItem>>(new Map());
// Shift 连选的锚点：记录上一次点击在当前页中的位置。
const anchorIndex = ref(-1);
const keyword = ref("");
const sortKey = ref<SortKey | null>("time");
const sortDir = ref<"asc" | "desc">("desc");
const page = ref(1);
const pageSize = 100;

const destination = ref<DestinationBase>({ accountId: 0, accountName: "", parentId: "", path: "", dirs: [] });
const folderTitle = ref("");
const isSeries = ref(true);
const season = ref(1);
const pickerOpen = ref(false);
const transferring = ref(false);
const discarding = ref(false);

// 自动重命名规则：prefix=加 U- 前缀，episode=补 SxxEnn，replace=正则替换。
const SEASON_EPISODE_RE = /S\d{1,2}E\d{1,3}/i;
const renamePrefixOn = ref(false);
const renamePrefixTouched = ref(false);
const renameEpisodeOn = ref(false);
const renameEpisodeTouched = ref(false);
const renameReplaceOn = ref(false);
const renameFind = ref("");
const renameReplaceWith = ref("");
const renameStartEpisode = ref(1);
const transferConfirmOpen = ref(false);
const pendingTransfer = ref<PendingTransfer | null>(null);

interface PendingTransfer {
  target: string;
  count: number;
  renames: RenamePreviewRow[];
  files: { fileId: string; originalName: string; newName: string; isDir: boolean }[];
  browsePath: GuangyaShareBrowseCrumb[];
  renameReplace: { enabled: boolean; find: string; replaceWith: string };
  unchangedCount: number;
  segments: string[];
  base: DestinationBase;
  title: string;
  season: number;
  series: boolean;
}

const kindLabels: Record<GuangyaShareKind, string> = {
  folder: "文件夹",
  video: "视频",
  audio: "音频",
  image: "图片",
  subtitle: "字幕",
  document: "文档",
  archive: "压缩包",
  other: "其他",
};

const quickKinds: { key: GuangyaShareKind; label: string; icon: string }[] = [
  { key: "video", label: "视频", icon: "film" },
  { key: "image", label: "图片", icon: "image" },
  { key: "subtitle", label: "字幕", icon: "closed-captioning-regular" },
  { key: "document", label: "文档", icon: "file-lines" },
  { key: "other", label: "其他", icon: "file" },
];

const kindIcons: Record<GuangyaShareKind, string> = {
  folder: "folder",
  video: "film",
  audio: "music",
  image: "image",
  subtitle: "closed-captioning-regular",
  document: "file-lines",
  archive: "file-zipper",
  other: "file",
};

function isQuickKind(item: GuangyaShareItem, kind: GuangyaShareKind) {
  if (item.is_dir) return false;
  if (kind === "other") return !QUICK_FILE_KINDS.has(item.kind);
  return item.kind === kind;
}

// 当前目录是否已被上层选中的文件夹覆盖；覆盖后子项默认“已包含”，不允许再单独勾选。
const ancestorSelected = computed(() =>
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
  const selectable = list.filter((item) => !isProcessed(item));
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
    if (isProcessed(list[index])) continue;
    addSelection(list[index], nextIds, nextMap);
  }
  selectedIds.value = nextIds;
  selectedMap.value = nextMap;
  return true;
}

function toggleSelect(item: GuangyaShareItem, event?: MouseEvent, fromCheckbox = false) {
  if (ancestorSelected.value) return;
  // 已转存文件只允许通过复选框重新选中，点击整行不触发。
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
  for (const item of currentItems.value) {
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
  const candidates = currentItems.value.filter((item) => !isProcessed(item) && isQuickKind(item, kind));
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
  const list = currentItems.value;
  if (!list.length) return;
  const remove = isAllSelected(list);
  const nextIds = new Set(selectedIds.value);
  const nextMap = new Map(selectedMap.value);
  for (const item of list) {
    if (isProcessed(item)) continue;
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
  if (!query) return currentItems.value;
  return currentItems.value.filter((item) => item.name.toLowerCase().includes(query));
});

const sortedItems = computed(() => {
  // 取消排序后保留接口返回的原始顺序。
  if (!sortKey.value) return filteredItems.value;
  const direction = sortDir.value === "asc" ? 1 : -1;
  return [...filteredItems.value].sort((a, b) => {
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

// 表头复选框：只统计当前目录里可批量选择的项（已转存文件需单独勾选）。
const selectableCurrentItems = computed(() => currentItems.value.filter((item) => !isProcessed(item)));
const allCurrentSelected = computed(
  () =>
    selectableCurrentItems.value.length > 0 &&
    selectableCurrentItems.value.every((item) => selectedIds.value.has(item.id)),
);
const someCurrentSelected = computed(() =>
  selectableCurrentItems.value.some((item) => selectedIds.value.has(item.id)),
);

// 已转存文件：按 file_id 置灰，并显示持久化的新名称。
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

// #region buei:guangya-rename
// 前缀基名 U：文件夹名去掉“(年份)”与空格。剧集目录名里通常带年份，逐个文件再补前缀会很啰嗦。
const renamePrefixBase = computed(() =>
  sanitizedFolderTitle.value
    .replace(/\s*[（(]\s*\d{4}\s*[）)]\s*/g, " ")
    .trim(),
);

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

const renameReplaceError = computed(() => {
  if (!renameReplaceOn.value || !renameFind.value) return "";
  try {
    new RegExp(renameFind.value, "g");
    return "";
  } catch (error) {
    return error instanceof Error ? error.message : "正则表达式无效";
  }
});

const renameEpisodeActive = computed(() => renameEpisodeOn.value && isSeries.value);

const renameActive = computed(
  () =>
    (renamePrefixOn.value && !!renamePrefixBase.value) ||
    renameEpisodeActive.value ||
    (renameReplaceOn.value && !!renameFind.value && !renameReplaceError.value),
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
  if (renameReplaceOn.value && renameFind.value && !renameReplaceError.value) {
    try {
      base = base.replace(new RegExp(renameFind.value, "g"), renameReplaceWith.value);
    } catch {
      // 正则已在上方校验，这里只兜底。
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
  renameReplaceOn.value = !!rule.enabled && !!rule.find;
  renameFind.value = rule.find || "";
  renameReplaceWith.value = rule.replace_with || "";
}

function resetRenameRules() {
  renamePrefixOn.value = false;
  renamePrefixTouched.value = false;
  renameEpisodeOn.value = false;
  renameEpisodeTouched.value = false;
  renameReplaceOn.value = false;
  renameFind.value = "";
  renameReplaceWith.value = "";
  renameStartEpisode.value = 1;
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
  const account = guangyaAccounts.value.find((item) => item.id === base.accountId);
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
}

function normalizeFolderTitle() {
  folderTitle.value = sanitizedFolderTitle.value;
}

function normalizeSeasonInput() {
  season.value = seasonNumber.value;
}

const canTransfer = computed(
  () =>
    !!result.value &&
    !result.value.need_password &&
    selectedItems.value.length > 0 &&
    destination.value.accountId > 0 &&
    !!sanitizedFolderTitle.value &&
    !transferring.value &&
    !discarding.value,
);

const canDiscard = computed(
  () =>
    !!result.value &&
    !result.value.need_password &&
    selectedFileCount.value > 0 &&
    !transferring.value &&
    !discarding.value,
);

function selectDestination(accountID: number, accountName: string) {
  destination.value = { accountId: accountID, accountName, parentId: "", path: "/", dirs: [] };
}

function ensureDestination() {
  if (destination.value.accountId) return;
  const first = guangyaAccounts.value[0];
  if (first) selectDestination(first.id, first.name);
}

function openPicker() {
  if (transferring.value) return;
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
  destination.value = {
    accountId: payload.accountId,
    accountName: payload.accountName,
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
    savedError.value = getApiErrorMessage(error, "加载收藏失败");
  } finally {
    savedLoading.value = false;
  }
}

async function selectSaved(link: GuangyaShareSavedLink) {
  if (transferring.value || discarding.value) return;
  activeSavedLinkId.value = link.id;
  url.value = (link.url || link.share_id).trim();
  password.value = link.password || "";
  await parseShare(false);
}

async function removeSavedLink(link: GuangyaShareSavedLink) {
  const ok = await confirm({
    title: "取消收藏？",
    message: `将移除「${link.title || link.share_id}」的收藏记录，不影响网盘文件和分享本身。`,
    confirmText: "取消收藏",
    cancelText: "保留",
    danger: true,
  }).catch(() => false);
  if (!ok) return;
  try {
    await bueiGuangyaShareApi.deleteSaved(link.id);
    savedLinks.value = savedLinks.value.filter((item) => item.id !== link.id);
    if (activeSavedLinkId.value === link.id) activeSavedLinkId.value = null;
    toast.success("已取消收藏");
  } catch (error) {
    toast.error(getApiErrorMessage(error, "取消收藏失败"));
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

async function parseShare(refreshSaved = true) {
  if (transferring.value) {
    toast.info("正在转存，请稍候");
    return;
  }
  if (discarding.value) return;
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
      { url: input, password: password.value.trim() },
      controller.signal,
    );
    if (controller.signal.aborted) return;
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
    sortKey.value = "time";
    sortDir.value = "desc";
    if (!res.need_password) {
      applyRenameReplace(res.rename_replace);
      applyDestinationForShare(res);
      passwordPrompt.value = false;
      // 选择已有收藏时不需要刷新固定顺序列表；手动解析新链接才刷新。
      if (refreshSaved && activeSavedLinkId.value === null) void loadSavedLinks();
    }
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
  activeSavedLinkId.value = null;
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
  sortKey.value = "time";
  sortDir.value = "desc";
}

function clearInput() {
  resetParse();
  url.value = "";
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
  if (item.is_dir) {
    void enterFolder(item);
    return;
  }
  if (isProcessed(item)) return;
  toggleSelect(item, event);
}

async function refreshCurrentFolder() {
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

async function discardSelected() {
  if (!result.value || discarding.value || transferring.value) return;
  const files = selectedFiles.value;
  if (!files.length) {
    toast.info("请先勾选要丢弃的文件");
    return;
  }
  const ok = await confirm({
    title: "标记为已丢弃？",
    message: `将 ${files.length} 个文件标记为已丢弃。行会置灰并跳过批量选择，但仍可通过复选框单独选中后转存。`,
    confirmText: "标记丢弃",
    cancelText: "取消",
    danger: false,
  }).catch(() => false);
  if (!ok) return;

  discarding.value = true;
  try {
    const res = await bueiGuangyaShareApi.discard(
      result.value.share_id,
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
  if (!result.value || transferring.value || discarding.value) return;
  if (!selectedItems.value.length) {
    toast.info("请先勾选要转存的文件");
    return;
  }
  if (!destination.value.accountId) {
    toast.info("请先选择转存账号和目录");
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
  const renameReplace = {
    enabled: renameReplaceOn.value && !!renameFind.value && !renameReplaceError.value,
    find: renameFind.value,
    replaceWith: renameReplaceWith.value,
  };
  pendingTransfer.value = {
    target: `${base.accountName || "未选择账号"} · ${targetPath.value}`,
    count: selectedItems.value.length,
    renames: changedRows,
    files,
    browsePath,
    renameReplace,
    unchangedCount: previews.length - changedRows.length,
    segments: [...targetSegments.value],
    base,
    title: sanitizedFolderTitle.value,
    season: seasonNumber.value,
    series: isSeries.value,
  };
  transferConfirmOpen.value = true;
}

function cancelTransferConfirm() {
  if (transferring.value) return;
  transferConfirmOpen.value = false;
  pendingTransfer.value = null;
}

async function confirmTransfer() {
  const pending = pendingTransfer.value;
  if (!pending || !result.value || transferring.value || discarding.value) return;
  transferConfirmOpen.value = false;
  transferring.value = true;
  let completed = false;
  let renamed = 0;
  let renameSkipped = 0;
  let renameScheduled = false;
  try {
    const res = await bueiGuangyaShareApi.transfer({
      url: parsedURL.value || url.value.trim(),
      share_id: result.value.share_id,
      password: password.value.trim(),
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
      },
    });
    completed = res.completed;
    renamed = res.renamed;
    renameSkipped = res.rename_skipped;
    renameScheduled = res.rename_scheduled;
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
      toast.info(
        renameScheduled
          ? "转存任务已提交，完成后会自动重命名，请稍后查看"
          : "转存任务已提交，内容较多时可能需要稍等片刻",
      );
    }
  } catch (error) {
    toast.error(getApiErrorMessage(error, "转存失败"));
    return;
  } finally {
    transferring.value = false;
    pendingTransfer.value = null;
  }
  if (!completed) return;

  const renameNote = renamed
    ? ` 已重命名 ${renamed} 项。`
    : "";
  const skippedNote = renameSkipped
    ? ` 有 ${renameSkipped} 项未能重命名，请到目标目录检查。`
    : "";
  const choice = await showConfirm({
    title: "转存完成",
    message: `已将 ${pending.count} 项内容保存到「${pending.target}」。${renameNote}${skippedNote}`,
    hint: "可以留在这里继续操作，或到前台文件页查看转存结果。",
    icon: "info",
    size: "sm",
    actions: [
      { id: "stay", label: "留在这里", variant: "cancel" },
      { id: "open", label: "去前台查看", variant: "primary" },
    ],
    showCancel: false,
  }).catch(() => null);
  if (!choice) {
    toast.success(`转存完成，已保存到 ${pending.target}`);
    return;
  }
  if (choice.action !== "open") return;
  await openTransferLocation(pending.base, pending.segments);
}

function close() {
  parseAbort?.abort();
  parseAbort = null;
  parsing.value = false;
  pickerOpen.value = false;
  transferConfirmOpen.value = false;
  pendingTransfer.value = null;
  emit("close");
}

function goPage(delta: number) {
  page.value = Math.min(totalPages.value, Math.max(1, page.value + delta));
}

watch(
  () => props.open,
  async (open) => {
    if (!open) return;
    // 每次打开都回到空白工作区；收藏列表仍在左侧，等待手动选择。
    if (!transferring.value) {
      resetParse();
      url.value = "";
      activeSavedLinkId.value = null;
    }
    parseError.value = "";
    if (!accountsStore.accounts.length) {
      void accountsStore.loadAccounts().catch(() => undefined);
    }
    void loadSavedLinks();
    ensureDestination();
    await nextTick();
    urlInputRef.value?.focus();
  },
);

watch(guangyaAccounts, (accounts) => {
  if (!destination.value.accountId && accounts.length) {
    selectDestination(accounts[0].id, accounts[0].name);
  }
});

onMounted(() => {
  if (!accountsStore.accounts.length) {
    void accountsStore.loadAccounts().catch(() => undefined);
  }
  ensureDestination();
});

onBeforeUnmount(() => {
  parseAbort?.abort();
});
</script>

<template>
  <AppModal :open="open" bare wide @close="close">
    <div class="gys">
      <header class="gys__head">
        <img class="gys__logo" src="/logos/guangya.png" alt="光鸭云盘" />
        <div class="gys__headings">
          <h3>光鸭分享收藏</h3>
          <p>收藏分享链接，逐层浏览并转存到指定光鸭账号</p>
        </div>
        <button class="gys__close" type="button" aria-label="关闭" @click="close">
          <SvgIcon name="xmark" :size="15" />
        </button>
      </header>

      <div class="gys__linkbar">
        <div class="gys__input-wrap">
          <SvgIcon name="hand-cloud" :size="16" class="gys__input-icon" />
          <input
            ref="urlInputRef"
            v-model="url"
            class="gys__input"
            type="text"
            placeholder="粘贴光鸭分享链接或分享口令，如 https://www.guangyapan.com/s/..."
            :disabled="parsing || transferring"
            @input="activeSavedLinkId = null"
            @keydown.enter.prevent="parseShare()"
          />
          <button
            v-if="url"
            type="button"
            class="gys__input-clear"
            aria-label="清空"
            :disabled="parsing || transferring"
            @click="clearInput"
          >
            <SvgIcon name="xmark" :size="13" />
          </button>
        </div>
        <div v-if="result?.need_password || passwordPrompt" class="gys__password">
          <SvgIcon name="hand-lock" :size="16" class="gys__input-icon" />
          <input
            v-model="password"
            class="gys__input"
            type="text"
            maxlength="12"
            placeholder="提取码"
            :disabled="parsing || transferring"
            @keydown.enter.prevent="parseShare()"
          />
        </div>
        <AppButton variant="primary" :disabled="parsing || transferring || discarding" @click="parseShare()">
          {{ parsing ? "解析中…" : result?.need_password || passwordPrompt ? "提取文件" : "解析分享" }}
        </AppButton>
      </div>

      <p v-if="parseError" class="gys__error">{{ parseError }}</p>

      <div class="gys__body">
        <aside class="gys__savedcol">
          <section class="gys__panel gys__panel--saved">
            <div class="gys__panel-head">
              <h4>收藏</h4>
              <span class="gys__panel-count">{{ savedLinks.length }}</span>
              <button
                type="button"
                class="gys__icon-btn"
                title="刷新收藏"
                :disabled="savedLoading"
                @click="loadSavedLinks"
              >
                <SvgIcon name="rotate" :size="13" />
              </button>
            </div>
            <div class="gys__saved-body">
              <div v-if="savedLoading" class="gys__saved-state">
                <BusySpinner variant="notch" :size="16" />
                加载中…
              </div>
              <div v-else-if="savedError" class="gys__saved-state is-error">
                <span>{{ savedError }}</span>
                <button type="button" @click="loadSavedLinks">重试</button>
              </div>
              <div v-else-if="!savedLinks.length" class="gys__saved-state">
                解析成功后会自动收藏到这里
              </div>
              <div v-else class="gys__saved-list">
                <div
                  v-for="link in savedLinks"
                  :key="link.id"
                  class="gys__saved-row"
                  :class="{ 'is-active': activeSavedLinkId === link.id }"
                >
                  <button
                    type="button"
                    class="gys__saved-main"
                    :title="link.url || link.share_id"
                    @click="selectSaved(link)"
                  >
                    <SvgIcon name="hand-star-solid" :size="14" class="gys__saved-star" />
                    <span class="gys__saved-meta">
                      <span class="gys__saved-title">{{ link.title || link.share_id || "未命名分享" }}</span>
                      <span class="gys__saved-sub">
                        {{ link.owner || "光鸭用户" }}
                        <template v-if="link.password"> · 有提取码</template>
                        · {{ formatTimeShort(link.last_used_at) || "刚刚" }}
                      </span>
                    </span>
                  </button>
                  <button type="button" class="gys__saved-del" title="取消收藏" @click.stop="removeSavedLink(link)">
                    <SvgIcon name="trash" :size="13" />
                  </button>
                </div>
              </div>
            </div>
          </section>
        </aside>

        <div class="gys__main">
          <div v-if="result && !result.need_password" class="gys__sharehead">
            <span class="gys__sharetitle" :title="result.title">{{ result.title || "未命名分享" }}</span>
            <span class="gys__sharesub">{{ result.owner || "光鸭用户" }}</span>
          </div>

          <div v-if="result && !result.need_password" class="gys__crumbs">
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

          <div v-if="result && !result.need_password" class="gys__quickbar">
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

          <div v-if="result && !result.need_password" class="gys__toolbar">
            <div class="gys__search">
              <SvgIcon name="magnifying-glass" :size="14" />
              <input v-model="keyword" type="text" placeholder="搜索当前目录" />
            </div>
            <span class="gys__folder-count">
              {{ currentFolderLoading ? "加载中…" : `${currentItems.length} 项` }}
            </span>
            <button
              type="button"
              class="gys__refresh"
              title="刷新当前目录"
              :disabled="currentFolderLoading"
              @click="refreshCurrentFolder"
            >
              <SvgIcon name="rotate" :size="13" />
              刷新
            </button>
            <button
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

          <div v-if="result && result.need_password" class="gys__empty">
            <SvgIcon name="hand-lock" :size="40" />
            <p>该分享需要提取码</p>
            <small>请在上方输入提取码后点击「提取文件」</small>
          </div>

          <div v-else-if="!result && parsing" class="gys__empty">
            <BusySpinner variant="notch" :size="34" />
            <p>正在解析分享内容…</p>
            <small>目录较多时可能需要几十秒，请稍候</small>
          </div>

          <div v-else-if="!result" class="gys__empty">
            <SvgIcon name="hand-cloud" :size="44" />
            <p>粘贴光鸭分享链接开始解析</p>
            <small>支持带提取码的分享口令，解析成功后自动收藏，之后可直接从右侧选择</small>
          </div>

          <div v-else-if="currentFolderLoading" class="gys__empty">
            <BusySpinner variant="notch" :size="30" />
            <p>正在加载文件夹…</p>
          </div>

          <div v-else-if="currentFolderError" class="gys__empty">
            <SvgIcon name="triangle-exclamation" :size="36" />
            <p>{{ currentFolderError }}</p>
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
                    文件
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
                      :disabled="ancestorSelected"
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
                        <span class="gys__name" :title="item.is_dir ? `进入 ${item.name}` : item.name">
                          <span class="gys__name-stem">{{ displayNameParts(item.name, item.is_dir).stem }}</span>
                          <span class="gys__name-ext">{{ displayNameParts(item.name, item.is_dir).ext }}</span>
                        </span>
                        <span v-if="itemNewName(item) || isProcessed(item)" class="gys__name-sub">
                          <span v-if="isProcessed(item)" class="gys__transferred-tag">
                            {{ processedStatus(item) === "discarded" ? "已丢弃" : "已转存" }}
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
                      <span v-if="item.is_dir" class="gys__enter">
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
                    {{ keyword ? "没有匹配的文件" : "此文件夹为空" }}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>

          <div v-if="result && !result.need_password && sortedItems.length > pageSize" class="gys__pager">
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

        <aside class="gys__side">
          <div class="gys__side-scroll">
            <section
              v-if="result && !result.need_password"
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
                placeholder="选择转存账号和目录"
                browse-label="选择"
                :title="destinationDisplay || '选择转存账号和目录'"
                @browse="openPicker"
              />
              <label class="gys__field">
                <span class="gys__field-label">文件夹名</span>
                <input
                  v-model="folderTitle"
                  class="gys__field-input"
                  type="text"
                  maxlength="120"
                  placeholder="默认为分享标题，可修改"
                  :disabled="transferring"
                  @blur="normalizeFolderTitle"
                />
              </label>
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

            <section
              v-if="result && !result.need_password"
              class="gys__panel gys__panel--rename"
              :class="{ 'is-busy': transferring }"
            >
              <div class="gys__panel-head">
                <h4>自动重命名</h4>
                <span class="gys__panel-hint">{{ renameActive ? "转存后自动应用" : "可选" }}</span>
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
                  <span class="gys__rule-desc">去年份与首尾空格；连接符沿用原名中的 . - _ 空格，否则用 -</span>
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
                <input v-model="renameReplaceOn" type="checkbox" :disabled="transferring" />
                <span class="gys__rule-body">
                  <span class="gys__rule-title">替换文本</span>
                  <span class="gys__rule-desc">支持正则，替换内容可留空</span>
                </span>
              </label>

              <div v-if="renameReplaceOn" class="gys__replace">
                <input
                  v-model="renameFind"
                  class="gys__replace-input"
                  type="text"
                  placeholder="查找（正则）"
                  :disabled="transferring"
                />
                <input
                  v-model="renameReplaceWith"
                  class="gys__replace-input"
                  type="text"
                  placeholder="替换为（可留空）"
                  :disabled="transferring"
                />
                <p v-if="renameReplaceError" class="gys__rule-error">{{ renameReplaceError }}</p>
              </div>
            </section>
          </div>

          <div class="gys__sidefoot">
            <div class="gys__selection">
              <span class="gys__selection-main">
                已选 <b>{{ selectedItems.length }}</b> 项
                <template v-if="selectedFolderCount">（{{ selectedFolderCount }} 个文件夹）</template>
              </span>
              <small v-if="selectedItems.length">文件合计 {{ formatSize(selectedFileSize) }} · 文件夹按实际内容转存</small>
              <small v-else>可在不同目录中分别勾选</small>
            </div>
            <AppButton class="gys__transfer" variant="primary" :disabled="!canTransfer" @click="submitTransfer">
              {{ transferring ? "转存中…" : "开始转存" }}
            </AppButton>
          </div>
        </aside>
      </div>
    </div>
  </AppModal>

  <BueiGuangyaShareConfirm
    :open="transferConfirmOpen"
    :target="pendingTransfer?.target || ''"
    :count="pendingTransfer?.count || 0"
    :renames="pendingTransfer?.renames || []"
    :unchanged-count="pendingTransfer?.unchangedCount || 0"
    :loading="transferring"
    @confirm="confirmTransfer"
    @cancel="cancelTransferConfirm"
  />

  <FolderPickerModal
    v-if="open"
    :open="pickerOpen"
    nested
    selectable-account
    :accounts="guangyaAccounts"
    :account-id="pickerAccountId"
    :initial-path="pickerInitialPath"
    allow-create-folder
    title="选择光鸭转存目录"
    confirm-text="转存到此目录"
    @close="pickerOpen = false"
    @resolve="onFolderResolved"
  />
</template>

<style scoped>
.gys {
  display: flex;
  flex-direction: column;
  width: min(94vw, 1240px);
  height: min(90vh, 880px);
  min-height: 0;
  background: var(--surface);
  border-radius: var(--radius-lg);
  overflow: hidden;
}

.gys__head {
  display: flex;
  align-items: center;
  gap: 12px;
  flex: 0 0 auto;
  padding: 14px 18px;
  border-bottom: 1px solid var(--border);
  background: var(--panel-head-bg);
}

.gys__logo {
  width: 34px;
  height: 34px;
  border-radius: var(--radius-md);
  object-fit: cover;
  flex-shrink: 0;
}

.gys__headings {
  flex: 1;
  min-width: 0;
}

.gys__headings h3 {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
  color: var(--text);
}

.gys__headings p {
  margin: 2px 0 0;
  font-size: 12px;
  color: var(--text-muted);
}

.gys__close {
  width: 30px;
  height: 30px;
  display: grid;
  place-items: center;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
}

.gys__close:hover {
  background: var(--surface-hover);
  color: var(--text);
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
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

.gys__savedcol {
  display: flex;
  flex-direction: column;
  min-height: 0;
  padding: 2px 12px 12px 16px;
}

.gys__savedcol .gys__panel--saved {
  flex: 1 1 auto;
  min-height: 0;
  max-height: none;
}

.gys__main {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
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
  min-height: 0;
  padding: 0 16px 12px;
  overflow: hidden;
}

.gys__side-scroll {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  min-height: 0;
  overflow-y: auto;
  padding-right: 2px;
}

.gys__sharehead {
  display: flex;
  align-items: baseline;
  gap: 8px;
  flex: 0 0 auto;
  min-width: 0;
  padding: 2px 0 4px;
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

.gys__tablewrap {
  flex: 1;
  min-height: 0;
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
  flex: 1 1 auto;
  min-height: 120px;
  max-height: 300px;
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
  word-break: break-all;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.gys__panel--rename {
  flex: 0 0 auto;
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

.gys__replace {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin: 2px 0 2px 21px;
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
  flex: 1;
  min-height: 0;
  overflow-y: auto;
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

.gys__sidefoot {
  display: flex;
  flex-direction: column;
  gap: 8px;
  flex: 0 0 auto;
  margin-top: 10px;
  padding-top: 10px;
  border-top: 1px dashed var(--border);
}

.gys__selection {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
  font-size: 12px;
  color: var(--text-regular);
}

.gys__selection-main b {
  color: var(--brand);
}

.gys__selection small {
  color: var(--text-muted);
  font-size: 11px;
}

.gys__discard {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  flex: 0 0 auto;
  height: 28px;
  padding: 0 8px;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted);
  font-size: 12px;
  cursor: pointer;
}

.gys__discard:hover:not(:disabled) {
  background: var(--surface-hover);
  color: var(--danger);
}

.gys__discard:disabled {
  opacity: 0.45;
  cursor: default;
}

.gys__transfer {
  width: 100%;
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

@media (max-width: 980px) {
  .gys {
    width: 94vw;
    height: min(92vh, 900px);
  }
  .gys__body {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    overflow-y: auto;
  }
  .gys__main {
    order: 1;
    min-height: 340px;
    padding: 0 14px 10px;
  }
  .gys__savedcol {
    order: 2;
    max-height: 260px;
    padding: 12px 14px;
  }
  .gys__side {
    order: 3;
    padding: 12px 14px;
  }
  .gys__side-scroll {
    overflow: visible;
  }
  .gys__savedcol .gys__panel--saved {
    max-height: 240px;
  }
  .gys__panel--rename {
    flex: 0 0 auto;
  }
  .gys__saved-body {
    overflow-y: auto;
  }
}
</style>
