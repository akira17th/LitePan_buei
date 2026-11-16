<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import {
  bueiGuangyaShareApi,
  type GuangyaShareItem,
  type GuangyaShareKind,
  type GuangyaShareParseResult,
  type GuangyaShareSavedLink,
} from "@/api/bueiGuangyaShare";
import { ApiError, getApiErrorMessage } from "@/api/client";
import { toast } from "@/composables/useToast";
import { confirm } from "@/composables/useConfirm";
import { formatSize, formatTime, formatTimeShort } from "@/utils/format";
import { useAccountsStore } from "@/stores/accounts";
import AppButton from "@/components/base/AppButton.vue";
import AppModal from "@/components/base/AppModal.vue";
import BusySpinner from "@/components/base/BusySpinner.vue";
import AccountFolderField from "@/components/admin/AccountFolderField.vue";
import FolderPickerModal from "@/components/file/FolderPickerModal.vue";
import SvgIcon from "@/components/icons/SvgIcon.vue";

// buei 定制：后台「辅助工具 / 增强工具 / 光鸭分享收藏」工作区。

const props = defineProps<{ open: boolean }>();
const emit = defineEmits<{ close: [] }>();

const accountsStore = useAccountsStore();
const guangyaAccounts = computed(() =>
  accountsStore.accounts.filter((a) => a.driver_type === "guangya" && a.is_active),
);

type FolderCrumb = { id: string; name: string; path: string };
type SortKey = "name" | "time" | "size" | "kind";

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
const savedOpen = ref(false);
const savedLoading = ref(false);
const savedError = ref("");

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
const keyword = ref("");
const sortKey = ref<SortKey>("name");
const sortDir = ref<"asc" | "desc">("asc");
const page = ref(1);
const pageSize = 100;

const destination = ref({ accountId: 0, accountName: "", parentId: "", path: "" });
const pickerOpen = ref(false);
const transferring = ref(false);

const kindLabels: Record<GuangyaShareKind, string> = {
  folder: "文件夹",
  video: "视频",
  audio: "音频",
  image: "图片",
  document: "文档",
  archive: "压缩包",
  other: "其他",
};

const quickKinds: { key: GuangyaShareKind; label: string; icon: string }[] = [
  { key: "video", label: "视频", icon: "film" },
  { key: "audio", label: "音频", icon: "music" },
  { key: "image", label: "图片", icon: "image" },
  { key: "document", label: "文档", icon: "file-lines" },
  { key: "archive", label: "压缩包", icon: "file-zipper" },
  { key: "other", label: "其他", icon: "file" },
  { key: "folder", label: "文件夹", icon: "folder" },
];

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
  return list.length > 0 && list.every((item) => selectedIds.value.has(item.id));
}

function toggleSelect(item: GuangyaShareItem) {
  if (ancestorSelected.value) return;
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
}

const kindCounts = computed(() => {
  const counts: Record<string, number> = {};
  for (const item of currentItems.value) {
    counts[item.kind] = (counts[item.kind] ?? 0) + 1;
  }
  return counts;
});

function quickSelect(kind: GuangyaShareKind) {
  if (ancestorSelected.value) {
    toast.info("当前目录已包含在所选文件夹中");
    return;
  }
  const candidates = kind === "folder"
    ? currentItems.value.filter((item) => item.is_dir)
    : currentItems.value.filter((item) => !item.is_dir && item.kind === kind);
  if (!candidates.length) {
    toast.info(kind === "folder" ? "当前目录没有子文件夹" : `当前目录没有${kindLabels[kind]}文件`);
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
}

const filteredItems = computed(() => {
  const query = keyword.value.trim().toLowerCase();
  if (!query) return currentItems.value;
  return currentItems.value.filter((item) => item.name.toLowerCase().includes(query));
});

const sortedItems = computed(() => {
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
});
watch(totalPages, (total) => {
  if (page.value > total) page.value = total;
});

const selectedItems = computed(() => [...selectedMap.value.values()]);
const selectedFolderCount = computed(() => selectedItems.value.filter((item) => item.is_dir).length);
const selectedFileSize = computed(() =>
  selectedItems.value.reduce((sum, item) => sum + (item.is_dir ? 0 : item.size), 0),
);

const destinationDisplay = computed(() => {
  if (!destination.value.accountId) return "";
  const path = destination.value.path && destination.value.path !== "/" ? destination.value.path : "/";
  return `${destination.value.accountName} ${path}`;
});

const canTransfer = computed(
  () =>
    !!result.value &&
    !result.value.need_password &&
    selectedItems.value.length > 0 &&
    destination.value.accountId > 0 &&
    !transferring.value,
);

function selectDestination(accountID: number, accountName: string) {
  destination.value = { accountId: accountID, accountName, parentId: "", path: "/" };
}

function ensureDestination() {
  if (destination.value.accountId) return;
  const first = guangyaAccounts.value[0];
  if (first) selectDestination(first.id, first.name);
}

function openPicker() {
  if (!guangyaAccounts.value.length) {
    toast.info("请先在「存储管理」添加光鸭云盘账号");
    return;
  }
  pickerOpen.value = true;
}

function onFolderResolved(payload: { accountId: number; accountName: string; parentId: string; path: string }) {
  destination.value = {
    accountId: payload.accountId,
    accountName: payload.accountName,
    parentId: payload.parentId,
    path: payload.path || "/",
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

function toggleSaved() {
  savedOpen.value = !savedOpen.value;
  if (savedOpen.value && savedError.value) {
    void loadSavedLinks();
  }
}

async function selectSaved(link: GuangyaShareSavedLink) {
  url.value = (link.url || link.share_id).trim();
  password.value = link.password || "";
  savedOpen.value = false;
  await parseShare();
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
    toast.success("已取消收藏");
  } catch (error) {
    toast.error(getApiErrorMessage(error, "取消收藏失败"));
  }
}

function errorText(error: unknown, fallback: string) {
  if (error instanceof ApiError && error.errorType === "aborted") return "";
  return getApiErrorMessage(error, fallback);
}

async function parseShare() {
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
    selectedIds.value = new Set();
    selectedMap.value = new Map();
    keyword.value = "";
    page.value = 1;
    ensureDestination();
    if (!res.need_password) {
      passwordPrompt.value = false;
      toast.success(`已解析并收藏分享，共 ${res.items.length} 项，可进入文件夹继续浏览`);
      void loadSavedLinks();
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
  folderGeneration += 1;
  folderCache.value = {};
  loadingFolders.value = {};
  folderErrors.value = {};
  pathStack.value = [{ id: "", name: "全部文件", path: "" }];
  selectedIds.value = new Set();
  selectedMap.value = new Map();
  keyword.value = "";
  page.value = 1;
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

async function refreshCurrentFolder() {
  const id = currentFolderID.value;
  const nextCache = { ...folderCache.value };
  delete nextCache[id];
  folderCache.value = nextCache;
  setFolderError(id, "");
  await loadFolder(id, currentFolderPath.value);
}

function toggleSort(key: SortKey) {
  if (sortKey.value === key) {
    sortDir.value = sortDir.value === "asc" ? "desc" : "asc";
    return;
  }
  sortKey.value = key;
  sortDir.value = "asc";
}

async function submitTransfer() {
  if (!result.value) return;
  if (!selectedItems.value.length) {
    toast.info("请先勾选要转存的文件");
    return;
  }
  if (!destination.value.accountId) {
    toast.info("请先选择转存账号和目录");
    return;
  }
  const target = destinationDisplay.value;
  const ok = await confirm({
    title: "确认转存？",
    message: `将 ${selectedItems.value.length} 项内容转存到「${target}」。文件夹会连同其中的文件一起转存。`,
    confirmText: "开始转存",
    cancelText: "再检查一下",
  }).catch(() => false);
  if (!ok) return;

  transferring.value = true;
  try {
    const res = await bueiGuangyaShareApi.transfer({
      url: parsedURL.value || url.value.trim(),
      share_id: result.value.share_id,
      password: password.value.trim(),
      file_ids: selectedItems.value.map((item) => item.id),
      account_id: destination.value.accountId,
      parent_id: destination.value.parentId,
    });
    if (res.completed) {
      toast.success("转存完成，请到目标目录查看");
    } else {
      toast.info("转存任务已提交，内容较多时可能需要稍等片刻");
    }
    clearSelection();
  } catch (error) {
    toast.error(getApiErrorMessage(error, "转存失败"));
  } finally {
    transferring.value = false;
  }
}

function close() {
  parseAbort?.abort();
  parseAbort = null;
  parsing.value = false;
  emit("close");
}

function goPage(delta: number) {
  page.value = Math.min(totalPages.value, Math.max(1, page.value + delta));
}

watch(
  () => props.open,
  async (open) => {
    if (!open) return;
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
  <AppModal :open="open" bare @close="close">
    <div class="gys">
      <header class="gys__head">
        <img class="gys__logo" src="/logos/guangya.png" alt="光鸭云盘" />
        <div class="gys__headings">
          <h3>光鸭分享收藏</h3>
          <p>收藏分享链接，随时选择、浏览并转存到指定光鸭账号</p>
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
            :disabled="parsing"
            @keydown.enter.prevent="parseShare"
          />
          <button
            v-if="url"
            type="button"
            class="gys__input-clear"
            aria-label="清空"
            :disabled="parsing"
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
            :disabled="parsing"
            @keydown.enter.prevent="parseShare"
          />
        </div>
        <button
          type="button"
          class="gys__saved-toggle"
          :class="{ 'is-open': savedOpen }"
          @click="toggleSaved"
        >
          <SvgIcon :name="savedOpen ? 'hand-star-solid' : 'hand-star'" :size="15" />
          收藏
          <span class="gys__saved-count">{{ savedLinks.length }}</span>
        </button>
        <AppButton variant="primary" :disabled="parsing" @click="parseShare">
          {{ parsing ? "解析中…" : result?.need_password || passwordPrompt ? "提取文件" : "解析分享" }}
        </AppButton>
      </div>

      <div v-if="savedOpen" class="gys__saved-panel">
        <div class="gys__saved-head">
          <span class="gys__saved-head-title">已收藏分享</span>
          <span class="gys__saved-head-sub">点击可直接解析</span>
          <button type="button" class="gys__saved-close" aria-label="收起收藏" @click="savedOpen = false">
            <SvgIcon name="xmark" :size="13" />
          </button>
        </div>
        <div v-if="savedLoading" class="gys__saved-state">
          <BusySpinner variant="notch" :size="18" />
          加载中…
        </div>
        <div v-else-if="savedError" class="gys__saved-state is-error">
          <span>{{ savedError }}</span>
          <button type="button" @click="loadSavedLinks">重试</button>
        </div>
        <div v-else-if="!savedLinks.length" class="gys__saved-state">
          还没有收藏，解析成功后会自动保存到这里
        </div>
        <div v-else class="gys__saved-list">
          <div v-for="link in savedLinks" :key="link.id" class="gys__saved-row">
            <button
              type="button"
              class="gys__saved-main"
              :title="link.url || link.share_id"
              @click="selectSaved(link)"
            >
              <SvgIcon name="hand-star-solid" :size="15" class="gys__saved-star" />
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
              <SvgIcon name="trash" :size="14" />
            </button>
          </div>
        </div>
      </div>

      <p v-if="parseError" class="gys__error">{{ parseError }}</p>

      <div v-if="result && !result.need_password" class="gys__sharebar">
        <div class="gys__sharemeta">
          <span class="gys__sharetitle" :title="result.title">{{ result.title || "未命名分享" }}</span>
          <span class="gys__sharesub">{{ result.owner || "光鸭用户" }}</span>
        </div>
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
          <span class="gys__quick-sep" />
          <button
            type="button"
            class="gys__quick-btn"
            :disabled="ancestorSelected || !currentItems.length"
            @click="selectAllCurrent"
          >
            <SvgIcon name="check" :size="14" />
            {{ isAllSelected(currentItems) ? "取消全选" : "全选" }}
          </button>
          <button
            type="button"
            class="gys__quick-btn"
            :disabled="!selectedIds.size"
            @click="clearSelection"
          >
            <SvgIcon name="xmark" :size="13" />
            清空
          </button>
        </div>
      </div>

      <div v-if="result && !result.need_password" class="gys__toolbar">
        <div class="gys__search">
          <SvgIcon name="magnifying-glass" :size="14" />
          <input v-model="keyword" type="text" placeholder="搜索当前目录" />
        </div>
        <div class="gys__toolbar-right">
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
        </div>
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
        <small>支持带提取码的分享口令，解析成功后自动收藏，之后可直接选择</small>
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
              <th class="gys__col-check" />
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
              }"
              @click="item.is_dir ? enterFolder(item) : toggleSelect(item)"
            >
              <td class="gys__col-check" @click.stop>
                <input
                  type="checkbox"
                  class="gys__check"
                  :checked="selectedIds.has(item.id) || ancestorSelected"
                  :disabled="ancestorSelected"
                  @change="toggleSelect(item)"
                />
              </td>
              <td class="gys__col-name">
                <div class="gys__namecell">
                  <span class="gys__fileicon" :class="`is-${item.kind}`">
                    <SvgIcon :name="item.is_dir ? 'folder' : quickKinds.find((k) => k.key === item.kind)?.icon || 'file'" :size="16" />
                  </span>
                  <span class="gys__namebody">
                    <span class="gys__name" :title="item.is_dir ? `进入 ${item.name}` : item.name">{{ item.name }}</span>
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

      <footer class="gys__foot">
        <div class="gys__selection">
          <span class="gys__selection-main">
            已选 <b>{{ selectedItems.length }}</b> 项
            <template v-if="selectedFolderCount">（{{ selectedFolderCount }} 个文件夹）</template>
          </span>
          <small v-if="selectedItems.length">文件合计 {{ formatSize(selectedFileSize) }} · 文件夹按实际内容转存</small>
          <small v-else>可在不同目录中分别勾选</small>
        </div>
        <AccountFolderField
          class="gys__dest"
          :display="destinationDisplay"
          placeholder="选择转存账号和目录"
          browse-label="选择目录"
          :title="destinationDisplay || '选择转存账号和目录'"
          @browse="openPicker"
        />
        <AppButton variant="primary" :disabled="!canTransfer" @click="submitTransfer">
          {{ transferring ? "转存中…" : "开始转存" }}
        </AppButton>
      </footer>
    </div>
  </AppModal>

  <FolderPickerModal
    :open="pickerOpen"
    nested
    selectable-account
    :accounts="guangyaAccounts"
    :account-id="destination.accountId || null"
    :initial-path="destination.path || '/'"
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
  width: min(94vw, 940px);
  height: min(88vh, 760px);
  min-height: 0;
  background: var(--surface);
  border-radius: var(--radius-lg);
  overflow: hidden;
}

.gys__head {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 16px 20px;
  border-bottom: 1px solid var(--border);
  background: var(--panel-head-bg);
}

.gys__logo {
  width: 40px;
  height: 40px;
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
  font-size: 16px;
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
  padding: 14px 20px 10px;
}

.gys__input-wrap,
.gys__password {
  display: flex;
  align-items: center;
  gap: 7px;
  min-width: 0;
  flex: 1;
  height: 36px;
  padding: 0 10px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  transition: border-color 0.15s;
}

.gys__password {
  flex: 0 0 190px;
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
  margin: 0 20px 8px;
  padding: 8px 10px;
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--danger) 8%, var(--surface));
  color: var(--danger);
  font-size: 12px;
}

.gys__saved-toggle {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 36px;
  padding: 0 10px;
  flex: 0 0 auto;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  color: var(--text-regular);
  font-size: 13px;
  cursor: pointer;
  transition: border-color 0.15s ease, color 0.15s ease, background 0.15s ease;
}

.gys__saved-toggle:hover,
.gys__saved-toggle.is-open {
  border-color: color-mix(in srgb, var(--primary) 45%, var(--border));
  color: var(--primary);
  background: color-mix(in srgb, var(--primary) 5%, var(--surface));
}

.gys__saved-count {
  min-width: 17px;
  padding: 0 5px;
  border-radius: var(--radius-pill);
  background: var(--surface-sunken);
  color: var(--text-muted);
  font-size: 11px;
  line-height: 16px;
  text-align: center;
}

.gys__saved-toggle.is-open .gys__saved-count {
  color: var(--primary);
  background: color-mix(in srgb, var(--primary) 12%, var(--surface));
}

.gys__saved-panel {
  display: flex;
  flex-direction: column;
  max-height: 230px;
  min-height: 0;
  margin: 0 20px 10px;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--surface);
  overflow: hidden;
}

.gys__saved-head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 10px 7px 12px;
  border-bottom: 1px solid var(--border-soft);
  background: var(--surface-sunken);
  font-size: 12px;
}

.gys__saved-head-title {
  font-weight: 600;
  color: var(--text-regular);
}

.gys__saved-head-sub {
  color: var(--text-muted);
}

.gys__saved-close {
  display: grid;
  place-items: center;
  width: 22px;
  height: 22px;
  margin-left: auto;
  border: none;
  border-radius: var(--radius-xs);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
}

.gys__saved-close:hover {
  background: var(--surface-hover);
  color: var(--text);
}

.gys__saved-state {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  padding: 18px 14px;
  color: var(--text-muted);
  font-size: 12px;
}

.gys__saved-state.is-error {
  color: var(--danger);
}

.gys__saved-state button {
  border: none;
  background: transparent;
  color: var(--primary);
  font-size: 12px;
  cursor: pointer;
  padding: 2px 4px;
}

.gys__saved-list {
  min-height: 0;
  overflow-y: auto;
  padding: 4px;
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

.gys__saved-main {
  display: flex;
  align-items: center;
  gap: 9px;
  flex: 1;
  min-width: 0;
  padding: 7px 8px;
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
  font-size: 13px;
}

.gys__saved-sub {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-muted);
  font-size: 11px;
}

.gys__saved-del {
  display: grid;
  place-items: center;
  width: 28px;
  height: 28px;
  flex: 0 0 auto;
  margin-right: 4px;
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

.gys__sharebar {
  padding: 0 20px 6px;
}

.gys__sharemeta {
  display: flex;
  flex-direction: column;
  width: 100%;
  min-width: 0;
}

.gys__sharetitle {
  display: block;
  max-width: 100%;
  font-size: 15px;
  font-weight: 600;
  color: var(--text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.gys__sharesub {
  margin-top: 1px;
  font-size: 12px;
  color: var(--text-muted);
}

.gys__quickbar {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  min-width: 0;
  padding: 0 20px 8px;
}

.gys__quick-label {
  flex: 0 0 auto;
  padding-top: 5px;
  color: var(--text-muted);
  font-size: 12px;
  white-space: nowrap;
}

.gys__quick-label.is-hint {
  color: color-mix(in srgb, var(--primary) 75%, var(--text-muted));
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
  height: 26px;
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
  border-color: color-mix(in srgb, var(--primary) 45%, var(--border-soft));
  color: var(--primary);
}

.gys__quick-btn:disabled {
  opacity: 0.45;
  cursor: default;
}

.gys__quick-count {
  color: var(--text-muted);
  font-size: 11px;
}

.gys__quick-sep {
  width: 1px;
  height: 16px;
  margin: 0 2px;
  background: var(--border);
}

.gys__crumbs {
  display: flex;
  align-items: center;
  gap: 4px;
  min-width: 0;
  padding: 0 20px 8px;
}

.gys__up {
  display: grid;
  place-items: center;
  width: 26px;
  height: 26px;
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
  color: var(--primary);
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
  max-width: min(56vw, 460px);
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
  color: var(--primary);
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

.gys__folder-count {
  flex: 0 0 auto;
  font-size: 12px;
  color: var(--text-muted);
  white-space: nowrap;
}

.gys__toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 0 20px 10px;
}

.gys__search {
  display: flex;
  align-items: center;
  gap: 5px;
  height: 30px;
  padding: 0 9px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  color: var(--text-muted);
  min-width: 0;
}

.gys__search input {
  width: 200px;
  border: none;
  outline: none;
  background: transparent;
  color: var(--text);
  font-size: 12px;
}

.gys__toolbar-right {
  display: flex;
  align-items: center;
  gap: 10px;
  flex: 0 0 auto;
}

.gys__refresh {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 30px;
  padding: 0 9px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  color: var(--text-regular);
  font-size: 12px;
  cursor: pointer;
}

.gys__refresh:hover:not(:disabled) {
  border-color: color-mix(in srgb, var(--primary) 45%, var(--border));
  color: var(--primary);
}

.gys__refresh:disabled {
  opacity: 0.45;
  cursor: default;
}

.gys__tablewrap {
  flex: 1;
  min-height: 0;
  margin: 0 20px;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  overflow: auto;
}

.gys__table {
  width: 100%;
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
  padding: 8px 10px;
  text-align: left;
  font-size: 11px;
  font-weight: 600;
  color: var(--text-muted);
  border-bottom: 1px solid var(--border);
  white-space: nowrap;
}

.gys__table td {
  padding: 7px 10px;
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
  color: var(--primary);
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
  background: color-mix(in srgb, var(--primary) 6%, var(--surface));
}

.gys__row.is-included {
  background: color-mix(in srgb, var(--primary) 3%, var(--surface));
}

.gys__row.is-folder:hover .gys__name {
  color: var(--primary);
}

.gys__col-check {
  width: 34px;
  padding-right: 0 !important;
}

.gys__col-kind {
  width: 80px;
}

.gys__col-size {
  width: 90px;
  text-align: right !important;
  color: var(--text-muted);
}

.gys__col-time {
  width: 140px;
  color: var(--text-muted);
}

.gys__check {
  width: 14px;
  height: 14px;
  accent-color: var(--primary);
  cursor: pointer;
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
  width: 26px;
  height: 26px;
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

.gys__name {
  color: var(--text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
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
  padding: 8px 20px 0;
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

.gys__foot {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 20px;
  margin-top: 10px;
  border-top: 1px solid var(--border);
  background: var(--panel-head-bg);
}

.gys__selection {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 150px;
  font-size: 12px;
  color: var(--text-regular);
}

.gys__selection-main b {
  color: var(--primary);
}

.gys__selection small {
  color: var(--text-muted);
  font-size: 11px;
}

.gys__dest {
  flex: 1;
  min-width: 0;
}

.gys__empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: var(--text-muted);
  padding: 40px 20px;
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

@media (max-width: 760px) {
  .gys {
    width: 94vw;
    height: min(92vh, 780px);
  }
  .gys__linkbar {
    flex-wrap: wrap;
  }
  .gys__password {
    flex: 1 1 140px;
  }
  .gys__foot {
    flex-wrap: wrap;
    align-items: stretch;
  }
  .gys__quickbar {
    flex-wrap: wrap;
    align-items: stretch;
  }
  .gys__quick-label {
    padding-top: 0;
  }
  .gys__col-time {
    display: none;
  }
  .gys__selection {
    min-width: 100%;
  }
  .gys__search {
    flex: 1;
  }
  .gys__search input {
    width: auto;
    flex: 1;
  }
}
</style>
