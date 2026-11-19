<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import type { FileItem } from "@/api/types";
import { filesApi } from "@/api/files";
import { getApiErrorMessage } from "@/api/client";
import {
  bueiQuarkIngestApi,
  type QuarkIngestFavorite,
} from "@/api/bueiQuarkIngest";
import { isQuarkShareInboxName } from "@/utils/bueiQuarkShare";
import { naturalSort } from "@/utils/naturalSort";
import { formatSize, formatTime } from "@/utils/format";
import { useAccountsStore } from "@/stores/accounts";
import { confirm } from "@/composables/useConfirm";
import { toast } from "@/composables/useToast";
import AppButton from "@/components/base/AppButton.vue";
import AppModal from "@/components/base/AppModal.vue";
import AccountSelector from "@/components/file/AccountSelector.vue";
import BusySpinner from "@/components/base/BusySpinner.vue";
import FileIcon from "@/components/file/FileIcon.vue";
import FolderPickerModal from "@/components/file/FolderPickerModal.vue";
import SvgIcon from "@/components/icons/SvgIcon.vue";
import BueiQuarkIngestSettings from "@/components/buei/BueiQuarkIngestSettings.vue";

// buei 定制：后台「辅助工具 / 夸克入库」独立 tab 的工作区。
// 左侧收藏分享链接与「来自：分享」下的文件夹，打开文件夹后勾选文件入库。

type SortKey = "name" | "time" | "size";
type PickedFolder = {
  accountId: number;
  accountName: string;
  parentId: string;
  parentDirId?: string;
  path: string;
  dirs?: string[];
};

type QuarkIngestSettingsApi = {
  selectedCount?: number;
  canIngest?: boolean;
  submitting?: boolean;
  dialogOpen?: boolean;
  transferPhaseLabel?: string;
  transferHint?: string;
  renamePreviewMap?: Record<string, string>;
  submitIngest?: () => void;
};

const accountsStore = useAccountsStore();
const quarkAccounts = computed(() =>
  accountsStore.accounts.filter((account) => account.driver_type === "quark" && account.is_active),
);
const accountsReady = ref(false);
const accountId = ref<number | null>(null);
const accountName = computed(
  () => quarkAccounts.value.find((account) => account.id === accountId.value)?.name ?? "",
);

const inbox = ref<FileItem | null>(null);
const favorites = ref<QuarkIngestFavorite[]>([]);
const favoritesLoading = ref(false);
const contextLoading = ref(false);
const contextError = ref("");
const selectedFavoriteId = ref<number | null>(null);
const selectedFavorite = computed(
  () => favorites.value.find((favorite) => favorite.id === selectedFavoriteId.value) ?? null,
);

const items = ref<FileItem[]>([]);
const filesLoading = ref(false);
const filesRefreshing = ref(false);
const filesError = ref("");
const keyword = ref("");
const sortKey = ref<SortKey | null>(null);
const sortDir = ref<"asc" | "desc">("asc");
const selectedIds = ref<string[]>([]);
const settingsRef = ref<QuarkIngestSettingsApi | null>(null);
let contextSeq = 0;
let filesSeq = 0;

const selectedSet = computed(() => new Set(selectedIds.value));
const selectableItems = computed(() => items.value.filter((item) => !item.is_dir));
const selectedItems = computed(() =>
  selectableItems.value.filter((item) => selectedSet.value.has(fileKey(item))),
);
const selectedCount = computed(() => selectedItems.value.length);

const visibleItems = computed(() => {
  const keywordText = keyword.value.trim().toLowerCase();
  const filtered = keywordText
    ? items.value.filter((item) => item.name.toLowerCase().includes(keywordText))
    : [...items.value];
  return filtered.sort(compareItems);
});

const visibleSelectableItems = computed(() => visibleItems.value.filter((item) => !item.is_dir));
const allVisibleSelected = computed(
  () =>
    visibleSelectableItems.value.length > 0 &&
    visibleSelectableItems.value.every((item) => selectedSet.value.has(fileKey(item))),
);
const someVisibleSelected = computed(
  () =>
    !allVisibleSelected.value &&
    visibleSelectableItems.value.some((item) => selectedSet.value.has(fileKey(item))),
);

// 设置面板按表格当前顺序处理重命名编号；搜索隐藏的已选项追加在末尾。
const orderedSelectedItems = computed(() => {
  const ordered = visibleItems.value.filter(
    (item) => !item.is_dir && selectedSet.value.has(fileKey(item)),
  );
  const seen = new Set(ordered.map(fileKey));
  for (const item of selectedItems.value) {
    const key = fileKey(item);
    if (!seen.has(key)) ordered.push(item);
  }
  return ordered;
});

const renamePreviewMap = computed(() => settingsRef.value?.renamePreviewMap ?? {});

function renamePreviewFor(item: FileItem) {
  if (!selectedSet.value.has(fileKey(item))) return "";
  return renamePreviewMap.value[item.id] ?? "";
}

function renamePreviewChanged(item: FileItem) {
  const preview = renamePreviewFor(item);
  return !!preview && preview !== item.name;
}

const settingsBusy = computed(
  () => !!(settingsRef.value?.submitting || settingsRef.value?.dialogOpen),
);
const favoriteEditorOpen = ref(false);
const editingFavorite = ref<QuarkIngestFavorite | null>(null);
const favoriteSaving = ref(false);
const favoriteError = ref("");
const draftLink = ref("");
const draftFolderId = ref("");
const draftParentFolderId = ref("");
const draftFolderName = ref("");
const favoriteFolderPickerOpen = ref(false);
const deleting = ref(false);
const flattening = ref(false);
const interactionBusy = computed(
  () => settingsBusy.value || favoriteSaving.value || deleting.value || flattening.value,
);

const selectedSizeText = computed(() => {
  const size = selectedItems.value.reduce((sum, item) => sum + (item.size || 0), 0);
  return size ? formatSize(size) : "";
});

function fileKey(item: FileItem) {
  return String(item.id || item.name);
}

function compareItems(a: FileItem, b: FileItem) {
  if (!sortKey.value) return 0;
  if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1;
  let result = 0;
  if (sortKey.value === "name") {
    result = naturalSort(a.name || "", b.name || "");
  } else if (sortKey.value === "time") {
    result = new Date(a.mod_time || 0).getTime() - new Date(b.mod_time || 0).getTime();
  } else {
    result = (a.size || 0) - (b.size || 0);
  }
  if (result === 0) result = naturalSort(a.name || "", b.name || "");
  return sortDir.value === "asc" ? result : -result;
}

function toggleSort(key: SortKey) {
  if (sortKey.value !== key) {
    sortKey.value = key;
    sortDir.value = "asc";
    return;
  }
  if (sortDir.value === "asc") {
    sortDir.value = "desc";
    return;
  }
  // 第三次点击取消排序，恢复当前目录原始顺序。
  sortKey.value = null;
  sortDir.value = "asc";
}

function sortIcon(key: SortKey) {
  if (sortKey.value !== key) return "";
  return sortDir.value === "asc" ? "chevron-up" : "chevron-down";
}

function toggleSelect(item: FileItem) {
  if (item.is_dir) return;
  const key = fileKey(item);
  selectedIds.value = selectedSet.value.has(key)
    ? selectedIds.value.filter((id) => id !== key)
    : [...selectedIds.value, key];
}

function toggleCheckbox(item: FileItem, checked: boolean) {
  if (item.is_dir) return;
  const key = fileKey(item);
  if (checked === selectedSet.value.has(key)) return;
  toggleSelect(item);
}

function toggleSelectAll(checked: boolean) {
  const next = new Set(selectedIds.value);
  for (const item of visibleSelectableItems.value) {
    const key = fileKey(item);
    if (checked) next.add(key);
    else next.delete(key);
  }
  selectedIds.value = [...next];
}

function applyFiles(itemsNext: FileItem[]) {
  items.value = itemsNext;
  const validFiles = new Set(
    itemsNext.filter((item) => !item.is_dir).map((item) => fileKey(item)),
  );
  selectedIds.value = selectedIds.value.filter((key) => validFiles.has(key));
}

async function loadFavoriteFiles(favorite: QuarkIngestFavorite, showRefreshing = false) {
  const id = accountId.value;
  if (!id) return;
  const seq = ++filesSeq;
  filesLoading.value = true;
  filesRefreshing.value = showRefreshing;
  filesError.value = "";
  try {
    const res = await filesApi.list(id, favorite.folder_id, { forceRefresh: true });
    if (seq !== filesSeq || selectedFavoriteId.value !== favorite.id) return;
    applyFiles(res.items);
  } catch (err) {
    if (seq !== filesSeq || selectedFavoriteId.value !== favorite.id) return;
    items.value = [];
    selectedIds.value = [];
    filesError.value = getApiErrorMessage(err, "读取收藏文件夹失败，可能已被移动或删除");
  } finally {
    if (seq === filesSeq) {
      filesLoading.value = false;
      filesRefreshing.value = false;
    }
  }
}

async function selectFavorite(favorite: QuarkIngestFavorite, force = false) {
  if (interactionBusy.value) return;
  if (!inbox.value) {
    toast.error("未找到「来自：分享」文件夹，暂时无法打开收藏");
    return;
  }
  if (!force && selectedFavoriteId.value === favorite.id && !filesError.value) return;
  selectedFavoriteId.value = favorite.id;
  selectedIds.value = [];
  keyword.value = "";
  await loadFavoriteFiles(favorite);
}

function refreshCurrent() {
  const favorite = selectedFavorite.value;
  if (!favorite || interactionBusy.value) return;
  void loadFavoriteFiles(favorite, true);
}

async function deleteSelectedFiles() {
  const favorite = selectedFavorite.value;
  const id = accountId.value;
  const ids = selectedItems.value.map((item) => String(item.id)).filter(Boolean);
  if (!favorite || !id || !ids.length || interactionBusy.value) return;
  try {
    const accepted = await confirm({
      title: "确认删除",
      message: `确定要将选中的 ${ids.length} 个文件移入回收站吗？`,
      confirmText: "删除",
      danger: true,
    });
    if (!accepted) return;
  } catch {
    return;
  }
  deleting.value = true;
  try {
    await filesApi.deleteFiles({
      account_id: id,
      file_ids: ids,
      parent_id: favorite.folder_id,
    });
    selectedIds.value = [];
    toast.success(`已删除到回收站 ${ids.length} 个文件`);
    await loadFavoriteFiles(favorite);
  } catch (error) {
    toast.error(getApiErrorMessage(error, "删除文件失败"));
  } finally {
    deleting.value = false;
  }
}

async function flattenCurrent() {
  const favorite = selectedFavorite.value;
  const id = accountId.value;
  if (!favorite || !id || filesLoading.value || interactionBusy.value) return;
  try {
    const accepted = await confirm({
      title: "平铺目录",
      message: "将递归移动当前收藏文件夹内所有子文件夹中的文件到当前目录，并删除所有子文件夹。文件名冲突时会自动追加 (1)、(2)…。是否继续？",
      confirmText: "开始平铺",
      danger: true,
    });
    if (!accepted) return;
  } catch {
    return;
  }
  flattening.value = true;
  try {
    const result = await bueiQuarkIngestApi.flatten(id, favorite.id);
    selectedIds.value = [];
    const extra = result.renamed ? `，重命名 ${result.renamed} 个` : "";
    toast.success(`已移动 ${result.moved} 个文件，删除 ${result.deleted_folders} 个文件夹${extra}`);
  } catch (error) {
    toast.error(getApiErrorMessage(error, "平铺目录失败"));
  } finally {
    try {
      await loadFavoriteFiles(favorite);
    } finally {
      flattening.value = false;
    }
  }
}

async function loadFavorites() {
  const id = accountId.value;
  if (!id) {
    favorites.value = [];
    return;
  }
  favoritesLoading.value = true;
  try {
    const res = await bueiQuarkIngestApi.favorites(id);
    if (id !== accountId.value) return;
    favorites.value = res.items;
  } catch (error) {
    if (id === accountId.value) toast.error(getApiErrorMessage(error, "刷新收藏列表失败"));
  } finally {
    if (id === accountId.value) favoritesLoading.value = false;
  }
}

async function loadAccountContext(seq: number) {
  const id = accountId.value;
  if (!id) return;
  contextLoading.value = true;
  contextError.value = "";
  filesLoading.value = false;
  filesRefreshing.value = false;
  favorites.value = [];
  favoritesLoading.value = true;
  selectedFavoriteId.value = null;
  items.value = [];
  selectedIds.value = [];
  filesError.value = "";
  inbox.value = null;
  try {
    const [favoriteRes, root] = await Promise.all([
      bueiQuarkIngestApi.favorites(id),
      filesApi.list(id, "", { forceRefresh: true }),
    ]);
    if (seq !== contextSeq) return;
    favorites.value = favoriteRes.items;
    const found = root.items.find((item) => item.is_dir && isQuarkShareInboxName(item.name));
    if (!found) {
      contextError.value = "未找到「来自：分享」文件夹，请先在夸克 App 中手动转存分享内容";
      return;
    }
    inbox.value = found;
  } catch (err) {
    if (seq !== contextSeq) return;
    contextError.value = getApiErrorMessage(err, "读取夸克账号数据失败");
  } finally {
    if (seq === contextSeq) {
      contextLoading.value = false;
      favoritesLoading.value = false;
    }
  }
}

function refreshAccountContext() {
  if (!accountId.value) return;
  const seq = ++contextSeq;
  void loadAccountContext(seq);
}

function afterFavoriteFolderRenamed() {
  const current = selectedFavoriteId.value;
  const accountAtStart = accountId.value;
  void (async () => {
    await loadFavorites();
    if (accountId.value !== accountAtStart || selectedFavoriteId.value !== current) return;
    const favorite = favorites.value.find((item) => item.id === current);
    if (favorite) {
      selectedFavoriteId.value = favorite.id;
      await loadFavoriteFiles(favorite);
      return;
    }
    const first = favorites.value[0];
    if (first) {
      await selectFavorite(first);
    } else {
      filesSeq += 1;
      selectedFavoriteId.value = null;
      items.value = [];
      selectedIds.value = [];
      filesLoading.value = false;
      filesRefreshing.value = false;
    }
  })();
}

function handleIngestDone() {
  selectedIds.value = [];
  afterFavoriteFolderRenamed();
}

function resetFavoriteDraft() {
  draftLink.value = "";
  draftFolderId.value = "";
  draftParentFolderId.value = "";
  draftFolderName.value = "";
  favoriteError.value = "";
}

function openAddFavorite() {
  if (!inbox.value) {
    toast.error("未找到「来自：分享」文件夹，暂时无法添加收藏");
    return;
  }
  editingFavorite.value = null;
  resetFavoriteDraft();
  favoriteEditorOpen.value = true;
}

function openEditFavorite(favorite: QuarkIngestFavorite) {
  if (interactionBusy.value) return;
  editingFavorite.value = favorite;
  draftLink.value = favorite.link;
  draftFolderId.value = favorite.folder_id;
  draftParentFolderId.value = favorite.parent_folder_id || "";
  draftFolderName.value = favorite.folder_name;
  favoriteError.value = "";
  favoriteEditorOpen.value = true;
}

function closeFavoriteEditor() {
  if (favoriteSaving.value) return;
  favoriteEditorOpen.value = false;
}

function openFavoriteFolderPicker() {
  if (!inbox.value) {
    toast.error("未找到「来自：分享」文件夹，暂时无法选择收藏文件夹");
    return;
  }
  if (favoriteSaving.value) return;
  favoriteFolderPickerOpen.value = true;
}

function onFavoriteFolderResolved(folder: PickedFolder) {
  favoriteFolderPickerOpen.value = false;
  if (!inbox.value) return;
  if (folder.accountId !== accountId.value) return;
  const dirs = folder.dirs || [];
  const rootPath = `/${inbox.value.name.replace(/\/+$/, "")}`;
  const selectedPath = (folder.path || "").replace(/\/+$/, "");
  const underInbox = selectedPath === rootPath || selectedPath.startsWith(`${rootPath}/`);
  if (!dirs.length || folder.parentId === inbox.value.id || !underInbox) {
    toast.error("请选择「来自：分享」下的子文件夹");
    return;
  }
  const name = dirs[dirs.length - 1] || folder.path.split("/").filter(Boolean).pop() || "";
  if (!name) {
    toast.error("无法识别所选文件夹名称");
    return;
  }
  draftFolderId.value = folder.parentId;
  draftParentFolderId.value = folder.parentDirId || inbox.value.id;
  draftFolderName.value = name;
  favoriteError.value = "";
}

async function saveFavorite() {
  if (!accountId.value) return;
  const link = draftLink.value.trim();
  if (!link) {
    favoriteError.value = "请填写收藏链接";
    return;
  }
  if (!draftFolderId.value || !draftFolderName.value) {
    favoriteError.value = "请选择「来自：分享」下的收藏文件夹";
    return;
  }
  favoriteSaving.value = true;
  favoriteError.value = "";
  try {
    const payload = {
      account_id: accountId.value,
      link,
      folder_id: draftFolderId.value,
      parent_folder_id: draftParentFolderId.value,
      folder_name: draftFolderName.value,
    };
    let saved: QuarkIngestFavorite;
    if (editingFavorite.value) {
      saved = await bueiQuarkIngestApi.updateFavorite(editingFavorite.value.id, payload);
    } else {
      saved = await bueiQuarkIngestApi.createFavorite(payload);
    }
    favoriteEditorOpen.value = false;
    await loadFavorites();
    const target = favorites.value.find((item) => item.id === saved.id) || saved;
    favoriteSaving.value = false;
    await selectFavorite(target, true);
    toast.success(editingFavorite.value ? "收藏已更新" : "收藏已添加");
  } catch (error) {
    favoriteError.value = getApiErrorMessage(error, "保存收藏失败");
  } finally {
    favoriteSaving.value = false;
  }
}

async function deleteFavorite(favorite: QuarkIngestFavorite) {
  const accountAtStart = accountId.value;
  if (!accountAtStart || interactionBusy.value) return;
  try {
    const accepted = await confirm({
      title: "删除收藏和文件夹",
      message: `确定删除收藏「${favorite.link}」吗？会同时删除「来自：分享」中的文件夹「${favorite.folder_name}」及其内容。`,
      confirmText: "删除",
      danger: true,
    });
    if (!accepted) return;
  } catch {
    return;
  }
  try {
    await bueiQuarkIngestApi.deleteFavorite(favorite.id, accountAtStart);
    if (accountId.value !== accountAtStart) return;
    toast.success("收藏及文件夹已删除");
    const wasSelected = selectedFavoriteId.value === favorite.id;
    await loadFavorites();
    if (accountId.value !== accountAtStart) return;
    if (wasSelected) {
      filesSeq += 1;
      selectedFavoriteId.value = null;
      items.value = [];
      selectedIds.value = [];
      filesError.value = "";
      const first = favorites.value[0];
      if (first) await selectFavorite(first);
    }
  } catch (error) {
    toast.error(getApiErrorMessage(error, "删除收藏失败"));
  }
}

watch(quarkAccounts, (accounts) => {
  if (accountId.value && accounts.some((account) => account.id === accountId.value)) return;
  accountId.value = accounts[0]?.id ?? null;
}, { immediate: true });

watch(accountId, (id, previous) => {
  if (id === previous) return;
  selectedIds.value = [];
  selectedFavoriteId.value = null;
  favorites.value = [];
  inbox.value = null;
  items.value = [];
  keyword.value = "";
  filesError.value = "";
  contextError.value = "";
  filesLoading.value = false;
  filesRefreshing.value = false;
  favoriteEditorOpen.value = false;
  filesSeq += 1;
  contextSeq += 1;
  if (!id) {
    contextLoading.value = false;
    filesLoading.value = false;
    filesRefreshing.value = false;
    favoritesLoading.value = false;
    return;
  }
  const seq = contextSeq;
  void loadAccountContext(seq);
}, { immediate: true });

onMounted(async () => {
  try {
    if (!accountsStore.accounts.length) await accountsStore.loadAccounts();
  } catch {
    // 账号加载失败时保留空状态，用户可刷新后台页面重试。
  } finally {
    accountsReady.value = true;
  }
});

onBeforeUnmount(() => {
  contextSeq += 1;
  filesSeq += 1;
});

defineExpose({
  selectedCount,
  canIngest: computed(() => !interactionBusy.value && (settingsRef.value?.canIngest ?? false)),
  submitting: computed(() => settingsRef.value?.submitting ?? false),
  transferPhaseLabel: computed(() => settingsRef.value?.transferPhaseLabel ?? ""),
  transferHint: computed(() => {
    if (!selectedFavorite.value) return "先从左侧选择一条收藏";
    return settingsRef.value?.transferHint || "先勾选要入库的文件";
  }),
  submitIngest: () => {
    if (interactionBusy.value) return;
    settingsRef.value?.submitIngest?.();
  },
});
</script>

<template>
  <div class="qip-page">
    <div class="qip">
      <div class="qip__topbar">
        <div
          class="qip__account"
          :class="{ 'qip__account--busy': settingsBusy || favoriteSaving || favoriteEditorOpen || deleting || flattening }"
        >
          <span class="qip__label">夸克账号</span>
          <AccountSelector
            :accounts="quarkAccounts"
            :model-value="accountId"
            @update:model-value="accountId = $event"
          />
        </div>
        <div class="qip__inbox">
          <SvgIcon name="hand-folder" :size="15" />
          <span v-if="contextLoading">正在读取「来自：分享」…</span>
          <span v-else-if="contextError">{{ contextError }}</span>
          <span v-else-if="inbox">「{{ inbox.name }}」 · 收藏 {{ favorites.length }} 条</span>
          <span v-else>请选择夸克账号</span>
        </div>
        <span class="qip__selected">
          {{ selectedCount ? `已选 ${selectedCount} 个文件` : "未选择文件" }}
          <template v-if="selectedSizeText"> · {{ selectedSizeText }}</template>
        </span>
      </div>

      <div v-if="!accountsReady" class="qip__empty">
        <BusySpinner variant="notch" :size="34" />
        <p>正在加载夸克账号…</p>
      </div>

      <div v-else-if="!quarkAccounts.length" class="qip__empty">
        <SvgIcon name="hand-folder" :size="42" />
        <p>还没有可用的夸克账号</p>
        <small>请先到「存储管理」添加并启用夸克网盘账号。</small>
      </div>

      <div v-else class="qip__body">
        <aside class="qip__favorites">
          <section class="qip__fav-panel">
          <header class="qip__fav-head">
            <div class="qip__fav-title">
              <h4>收藏</h4>
              <span>{{ favorites.length }} 条</span>
            </div>
            <button
              type="button"
              class="qip__fav-add"
              title="添加收藏"
              :disabled="!inbox || contextLoading || interactionBusy"
              @click="openAddFavorite"
            >
              <SvgIcon name="plus" :size="14" />
            </button>
          </header>

          <div v-if="favoritesLoading" class="qip__fav-loading">
            <BusySpinner variant="notch" :size="20" />
            <span>加载收藏…</span>
          </div>
          <div v-else-if="!favorites.length" class="qip__fav-empty">
            <SvgIcon name="hand-folder" :size="28" />
            <p>还没有收藏</p>
            <small>点击右上角 + 添加分享链接和对应文件夹</small>
          </div>
          <div v-else class="qip__fav-list">
            <div
              v-for="favorite in favorites"
              :key="favorite.id"
              class="qip__fav"
              :class="{ 'is-active': favorite.id === selectedFavoriteId }"
            >
              <button
                type="button"
                class="qip__fav-main"
                :title="`${favorite.folder_name}\n${favorite.link}`"
                :disabled="interactionBusy"
                @click="selectFavorite(favorite)"
              >
                <SvgIcon name="hand-star-solid" :size="14" class="qip__fav-star" />
                <span class="qip__fav-meta">
                  <span class="qip__fav-name" :title="favorite.folder_name">{{ favorite.folder_name }}</span>
                  <span class="qip__fav-link" :title="favorite.link">{{ favorite.link }}</span>
                </span>
              </button>
              <div class="qip__fav-actions">
                <button
                  type="button"
                  class="qip__fav-action"
                  title="修改收藏"
                  :disabled="interactionBusy"
                  @click.stop="openEditFavorite(favorite)"
                >
                  <SvgIcon name="hand-edit" :size="13" />
                </button>
                <button
                  type="button"
                  class="qip__fav-action qip__fav-action--danger"
                  title="删除收藏及文件夹"
                  :disabled="interactionBusy"
                  @click.stop="deleteFavorite(favorite)"
                >
                  <SvgIcon name="trash" :size="13" />
                </button>
              </div>
            </div>
          </div>
          </section>
        </aside>

        <section class="qip__main">
          <div v-if="contextLoading" class="qip__empty qip__empty--main">
            <BusySpinner variant="notch" :size="32" />
            <p>正在读取账号与收藏…</p>
          </div>

          <div v-else-if="contextError" class="qip__empty qip__empty--main">
            <SvgIcon name="hand-exclamation-triangle" :size="38" />
            <p>{{ contextError }}</p>
            <AppButton variant="secondary" size="sm" @click="refreshAccountContext">重试</AppButton>
          </div>

          <div v-else-if="!selectedFavorite" class="qip__empty qip__empty--main">
            <SvgIcon name="hand-folder" :size="40" />
            <p>{{ favorites.length ? "从左侧选择一条收藏" : "暂无收藏" }}</p>
            <small>
              {{ favorites.length ? "点击收藏后会打开对应的「来自：分享」文件夹" : "点击左侧 + 添加分享链接和收藏文件夹" }}
            </small>
          </div>

          <template v-else>
            <header class="qip__main-head">
              <div class="qip__main-title">
                <h4>{{ selectedFavorite.folder_name }}</h4>
                <a
                  v-if="selectedFavorite.link"
                  class="qip__main-link"
                  :href="selectedFavorite.link"
                  target="_blank"
                  rel="noopener noreferrer"
                  :title="selectedFavorite.link"
                >
                  {{ selectedFavorite.link }}
                </a>
                <span v-else class="qip__main-link is-empty">未填写收藏链接</span>
              </div>
            </header>

            <div class="qip__controls">
              <div class="qip__search">
                <SvgIcon name="magnifying-glass" :size="14" />
                <input v-model="keyword" type="text" placeholder="搜索当前文件夹" />
              </div>
              <AppButton
                variant="secondary"
                size="sm"
                :disabled="filesLoading || interactionBusy"
                @click="refreshCurrent"
              >
                <span class="qip__refresh-icon" :class="{ spin: filesRefreshing }">
                  <SvgIcon name="badge-refresh" :size="14" />
                </span>
                <span>刷新</span>
              </AppButton>
              <AppButton
                variant="secondary"
                size="sm"
                title="递归移动所有子文件夹中的文件到当前目录，并删除子文件夹"
                :disabled="filesLoading || interactionBusy"
                @click="flattenCurrent"
              >
                <SvgIcon name="folder-tree" :size="14" />
                <span>{{ flattening ? "平铺中…" : "平铺目录" }}</span>
              </AppButton>
              <AppButton
                variant="secondary"
                size="sm"
                class="qip__delete-btn"
                :disabled="!selectedCount || filesLoading || interactionBusy"
                @click="deleteSelectedFiles"
              >
                <SvgIcon name="trash" :size="14" />
                <span>{{ deleting ? "删除中…" : "删除" }}</span>
              </AppButton>
            </div>

            <div v-if="filesLoading && !items.length" class="qip__empty qip__empty--main">
              <BusySpinner variant="notch" :size="32" />
              <p>正在读取「{{ selectedFavorite.folder_name }}」…</p>
            </div>

            <div v-else-if="filesError" class="qip__empty qip__empty--main">
              <SvgIcon name="hand-exclamation-triangle" :size="36" />
              <p>{{ filesError }}</p>
              <AppButton variant="secondary" size="sm" @click="refreshCurrent">重试</AppButton>
            </div>

            <template v-else>
              <div class="qip__toolbar">
                <span>{{ visibleItems.length }} / {{ items.length }} 项</span>
                <span class="qip__hint">仅勾选文件可入库；文件夹保留在「来自：分享」中</span>
              </div>

              <div class="qip__tablewrap">
                <table class="qip__table">
                  <thead>
                    <tr>
                      <th class="qip__col-check" title="全选 / 反选">
                        <input
                          type="checkbox"
                          :checked="allVisibleSelected"
                          :indeterminate="someVisibleSelected"
                          :disabled="!visibleSelectableItems.length"
                          aria-label="全选或反选当前文件夹中的文件"
                          @change="toggleSelectAll(($event.target as HTMLInputElement).checked)"
                        />
                      </th>
                      <th
                        class="qip__th-sort"
                        :class="{ 'is-sorted': sortKey === 'name' }"
                        title="点击切换升序、降序或取消排序"
                        @click="toggleSort('name')"
                      >
                        名称
                        <SvgIcon v-if="sortKey === 'name'" :name="sortIcon('name')" :size="11" />
                      </th>
                      <th
                        class="qip__col-size qip__th-sort"
                        :class="{ 'is-sorted': sortKey === 'size' }"
                        title="点击切换升序、降序或取消排序"
                        @click="toggleSort('size')"
                      >
                        大小
                        <SvgIcon v-if="sortKey === 'size'" :name="sortIcon('size')" :size="11" />
                      </th>
                      <th
                        class="qip__col-time qip__th-sort"
                        :class="{ 'is-sorted': sortKey === 'time' }"
                        title="点击切换升序、降序或取消排序"
                        @click="toggleSort('time')"
                      >
                        修改时间
                        <SvgIcon v-if="sortKey === 'time'" :name="sortIcon('time')" :size="11" />
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr
                      v-for="item in visibleItems"
                      :key="fileKey(item)"
                      class="qip__row"
                      :class="{
                        'is-selected': selectedSet.has(fileKey(item)),
                        'is-folder': item.is_dir,
                      }"
                      @click="toggleSelect(item)"
                    >
                      <td class="qip__col-check" @click.stop>
                        <input
                          v-if="!item.is_dir"
                          type="checkbox"
                          :checked="selectedSet.has(fileKey(item))"
                          @change="toggleCheckbox(item, ($event.target as HTMLInputElement).checked)"
                        />
                        <span v-else class="qip__no-check">—</span>
                      </td>
                      <td class="qip__col-name">
                        <div class="qip__namecell">
                          <span class="qip__fileicon"><FileIcon :file="item" :size="18" /></span>
                          <span class="qip__namebody">
                            <span class="qip__nameline">
                              <span class="qip__name" :title="item.name">{{ item.name }}</span>
                              <span v-if="item.is_dir" class="qip__folder-tag">文件夹</span>
                            </span>
                            <span
                              v-if="renamePreviewFor(item)"
                              class="qip__rename-preview"
                              :class="{ 'is-changed': renamePreviewChanged(item) }"
                              :title="renamePreviewFor(item)"
                            >
                              → {{ renamePreviewFor(item) }}
                            </span>
                          </span>
                        </div>
                      </td>
                      <td class="qip__col-size">{{ item.is_dir ? "-" : formatSize(item.size) }}</td>
                      <td class="qip__col-time">{{ formatTime(item.mod_time) }}</td>
                    </tr>
                    <tr v-if="!visibleItems.length">
                      <td colspan="4" class="qip__noresult">
                        {{ keyword ? "没有匹配的内容" : "收藏文件夹为空" }}
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </template>
          </template>
        </section>

        <aside class="qip__side">
          <BueiQuarkIngestSettings
            v-if="selectedFavorite && inbox && accountId"
            :key="`${selectedFavorite.id}:${selectedFavorite.folder_id}`"
            ref="settingsRef"
            :account-id="accountId"
            :account-name="accountName"
            :favorite-id="selectedFavorite.id"
            :source-parent-id="selectedFavorite.folder_id"
            :source-folder-parent-id="selectedFavorite.parent_folder_id || inbox.id"
            :source-folder-name="selectedFavorite.folder_name"
            :files="orderedSelectedItems"
            @done="handleIngestDone"
          />
          <div v-else class="qip__side-placeholder">
            选择收藏后，即可设置保存位置、重命名规则与联动动作。
          </div>
        </aside>
      </div>
    </div>

    <AppModal
      :open="favoriteEditorOpen"
      :title="editingFavorite ? '修改收藏' : '添加收藏'"
      size="sm"
      @close="closeFavoriteEditor"
    >
      <div class="qip__editor">
        <label class="qip__editor-field">
          <span>收藏链接</span>
          <input
            v-model="draftLink"
            type="text"
            placeholder="粘贴分享链接或备注，无需解析"
            :disabled="favoriteSaving"
          />
        </label>
        <div class="qip__editor-field">
          <span>收藏文件夹</span>
          <button
            type="button"
            class="qip__editor-folder"
            :disabled="favoriteSaving || !inbox"
            @click="openFavoriteFolderPicker"
          >
            <SvgIcon name="hand-folder" :size="14" />
            <span :class="{ 'is-placeholder': !draftFolderName }">
              {{ draftFolderName || "选择「来自：分享」下的文件夹" }}
            </span>
            <span class="qip__editor-folder-action">选择</span>
          </button>
          <small>只能选择「来自：分享」下的子文件夹；该文件夹中已手动转存该分享。</small>
        </div>
        <p v-if="favoriteError" class="qip__editor-error">{{ favoriteError }}</p>
      </div>
      <template #footer>
        <AppButton variant="ghost" :disabled="favoriteSaving" @click="closeFavoriteEditor">取消</AppButton>
        <AppButton variant="primary" :loading="favoriteSaving" @click="saveFavorite">
          {{ editingFavorite ? "保存修改" : "添加收藏" }}
        </AppButton>
      </template>
    </AppModal>

    <FolderPickerModal
      :open="favoriteFolderPickerOpen"
      nested
      :account-id="accountId"
      :accounts="[]"
      :root-anchor="inbox ? { parentId: inbox.id, path: `/${inbox.name}`, label: inbox.name } : undefined"
      allow-create-folder
      title="选择收藏文件夹"
      confirm-text="使用当前文件夹"
      @close="favoriteFolderPickerOpen = false"
      @resolve="onFavoriteFolderResolved"
    />
  </div>
</template>

<style scoped>
.qip-page {
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.qip {
  display: flex;
  flex-direction: column;
  width: 100%;
  min-height: min(70vh, 760px);
  background: var(--surface);
  border-radius: var(--radius-lg);
  overflow: hidden;
}

.qip__topbar {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
  flex: 0 0 auto;
  padding: 12px 16px 8px;
}

.qip__account {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.qip__label {
  color: var(--text-muted);
  font-size: 12px;
  white-space: nowrap;
}

.qip__account--busy {
  pointer-events: none;
  opacity: 0.6;
}

.qip__inbox {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  flex: 1 1 220px;
  color: var(--text-muted);
  font-size: 12px;
}

.qip__inbox span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.qip__refresh-icon {
  display: inline-flex;
  line-height: 0;
}

.qip__refresh-icon.spin {
  animation: qip-spin 0.9s linear infinite;
}

@keyframes qip-spin {
  to {
    transform: rotate(360deg);
  }
}

.qip__selected {
  margin-left: auto;
  padding: 4px 10px;
  border-radius: var(--radius-pill);
  background: var(--surface-sunken);
  color: var(--text-regular);
  font-size: 11px;
  white-space: nowrap;
}

.qip__body {
  display: grid;
  grid-template-columns: 232px minmax(0, 1fr) 330px;
  gap: 12px;
  flex: 1 1 auto;
  min-height: 0;
  padding: 2px 16px 14px;
}

.qip__favorites {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
}

.qip__fav-panel {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--surface);
}

.qip__fav-head {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 0 0 auto;
  padding: 10px 12px;
}

.qip__fav-title {
  display: flex;
  align-items: baseline;
  gap: 6px;
  min-width: 0;
  flex: 1;
}

.qip__fav-title h4 {
  margin: 0;
  color: var(--text);
  font-size: 13px;
  font-weight: 600;
}

.qip__fav-title span {
  color: var(--text-muted);
  font-size: 11px;
}

.qip__fav-add {
  display: inline-grid;
  place-items: center;
  width: 26px;
  height: 26px;
  padding: 0;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  color: var(--text-regular);
  cursor: pointer;
}

.qip__fav-add:hover:not(:disabled) {
  border-color: var(--brand);
  color: var(--brand);
}

.qip__fav-add:disabled {
  opacity: 0.45;
  cursor: default;
}

.qip__fav-loading,
.qip__fav-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  min-height: 160px;
  padding: 18px 14px;
  color: var(--text-muted);
  font-size: 12px;
  text-align: center;
}

.qip__fav-loading {
  flex-direction: row;
  min-height: 80px;
}

.qip__fav-empty p {
  margin: 0;
  color: var(--text-regular);
  font-size: 13px;
}

.qip__fav-empty small {
  line-height: 1.6;
}

.qip__fav-list {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 0 6px 8px;
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.qip__fav {
  display: flex;
  align-items: center;
  gap: 2px;
  border-radius: var(--radius-sm);
  background: transparent;
}

.qip__fav:hover {
  background: var(--surface-hover);
}

.qip__fav.is-active {
  background: color-mix(in srgb, var(--brand) 12%, var(--surface));
  box-shadow: inset 2px 0 0 var(--brand);
}

.qip__fav-main {
  display: flex;
  align-items: center;
  gap: 7px;
  flex: 1;
  min-width: 0;
  padding: 6px 7px;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text);
  text-align: left;
  cursor: pointer;
}

.qip__fav-main:disabled {
  cursor: default;
}

.qip__fav-star {
  flex: 0 0 auto;
  color: #d97706;
}

.qip__fav-meta {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
  flex: 1;
}

.qip__fav-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text);
  font-size: 12px;
  font-weight: 600;
}

.qip__fav.is-active .qip__fav-name {
  color: var(--brand);
}

.qip__fav-link {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-muted);
  font-size: 11px;
}

.qip__fav-actions {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 1px;
  padding: 2px;
  opacity: 0.55;
  transition: opacity 0.15s ease;
}

.qip__fav:hover .qip__fav-actions,
.qip__fav.is-active .qip__fav-actions {
  opacity: 1;
}

.qip__fav-action {
  display: inline-grid;
  place-items: center;
  width: 22px;
  height: 22px;
  padding: 0;
  border: none;
  border-radius: var(--radius-xs);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
}

.qip__fav-action:hover:not(:disabled) {
  background: var(--surface-hover);
  color: var(--brand);
}

.qip__fav-action--danger:hover:not(:disabled) {
  color: var(--danger);
}

.qip__fav-action:disabled {
  opacity: 0.4;
  cursor: default;
}

.qip__main {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--surface);
}

.qip__main-head {
  display: flex;
  align-items: center;
  gap: 10px;
  flex: 0 0 auto;
  padding: 11px 14px 8px;
}

.qip__main-title {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.qip__main-title h4 {
  margin: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text);
  font-size: 14px;
  font-weight: 600;
}

.qip__main-link {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 100%;
  color: var(--text-muted);
  font-size: 11px;
  text-decoration: none;
}

.qip__main-link:hover {
  color: var(--brand);
  text-decoration: underline;
}

.qip__main-link.is-empty {
  color: var(--text-muted);
  opacity: 0.8;
}

.qip__controls {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 0 0 auto;
  padding: 0 14px 8px;
}

.qip__delete-btn.btn--secondary:not(:disabled):hover {
  border-color: var(--danger);
  background: var(--surface);
  color: var(--danger);
}

.qip__search {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 1 1 auto;
  min-width: 120px;
  height: 32px;
  padding: 0 9px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  color: var(--text-muted);
}

.qip__search input {
  flex: 1;
  min-width: 0;
  border: none;
  outline: none;
  background: transparent;
  color: var(--text);
  font-size: 12px;
}

.qip__toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  flex: 0 0 auto;
  padding: 0 14px 8px;
  color: var(--text-muted);
  font-size: 11px;
}

.qip__hint {
  margin-left: auto;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.qip__tablewrap {
  flex: 1 1 auto;
  min-height: 300px;
  overflow: auto;
  border-top: 1px solid var(--border-soft);
}

.qip__table {
  width: 100%;
  border-collapse: collapse;
  table-layout: fixed;
  font-size: 12px;
}

.qip__table thead {
  position: sticky;
  top: 0;
  z-index: 2;
  background: var(--surface-sunken);
}

.qip__table th {
  padding: 8px 10px;
  border-bottom: 1px solid var(--border-soft);
  color: var(--text-muted);
  font-weight: 500;
  text-align: left;
  white-space: nowrap;
}

.qip__th-sort {
  cursor: pointer;
  user-select: none;
}

.qip__th-sort svg {
  vertical-align: -1px;
}

.qip__th-sort.is-sorted {
  color: var(--brand);
}

.qip__table td {
  padding: 8px 10px;
  border-bottom: 1px solid var(--border-soft);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.qip__col-check {
  width: 38px;
  text-align: center;
}

.qip__col-check input {
  accent-color: var(--brand);
}

.qip__no-check {
  color: var(--text-muted);
  font-size: 10px;
}

.qip__col-size {
  width: 92px;
  text-align: right;
}

.qip__col-time {
  width: 138px;
  text-align: right;
}

.qip__row {
  cursor: pointer;
  transition: background 0.15s ease;
}

.qip__row:hover {
  background: var(--surface-sunken);
}

.qip__row.is-folder {
  cursor: default;
}

.qip__row.is-selected {
  background: var(--info-soft);
}

.qip__namecell {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.qip__fileicon {
  display: inline-flex;
  flex: 0 0 auto;
  line-height: 0;
}

.qip__namebody {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
  flex: 1;
}

.qip__nameline {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.qip__name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.qip__rename-preview {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-muted);
  font-size: 11px;
  line-height: 1.4;
}

.qip__rename-preview.is-changed {
  color: var(--brand);
}

.qip__folder-tag {
  flex: 0 0 auto;
  padding: 0 5px;
  border-radius: var(--radius-xs);
  background: var(--surface-sunken);
  color: var(--text-muted);
  font-size: 10px;
  line-height: 16px;
}

.qip__noresult {
  padding: 42px 12px !important;
  color: var(--text-muted);
  text-align: center;
}

.qip__side {
  min-width: 0;
  min-height: 0;
  overflow-y: auto;
}

.qip__side-placeholder {
  padding: 24px 16px;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--surface);
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.7;
  text-align: center;
}

.qip__empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  min-height: 360px;
  margin: 2px 16px 14px;
  padding: 32px 20px;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--surface);
  color: var(--text-muted);
  font-size: 13px;
  text-align: center;
}

.qip__empty--main {
  flex: 1;
  min-height: 360px;
  margin: 0;
  border: none;
  border-radius: 0;
  background: transparent;
}

.qip__empty p {
  margin: 0;
  color: var(--text-regular);
}

.qip__empty small {
  line-height: 1.6;
}

.qip__editor {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.qip__editor-field {
  display: flex;
  flex-direction: column;
  gap: 7px;
  color: var(--text-regular);
  font-size: 12px;
}

.qip__editor-field > span:first-child {
  color: var(--text-muted);
  font-size: 11px;
}

.qip__editor-field input {
  width: 100%;
  height: 36px;
  padding: 0 10px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  color: var(--text);
  font-size: 13px;
  box-sizing: border-box;
}

.qip__editor-field small {
  color: var(--text-muted);
  font-size: 11px;
  line-height: 1.5;
}

.qip__editor-folder {
  display: flex;
  align-items: center;
  gap: 7px;
  width: 100%;
  min-height: 36px;
  padding: 0 10px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  color: var(--text);
  font-size: 13px;
  text-align: left;
  cursor: pointer;
}

.qip__editor-folder > span:nth-child(2) {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.qip__editor-folder .is-placeholder {
  color: var(--text-muted);
}

.qip__editor-folder:disabled {
  opacity: 0.55;
  cursor: default;
}

.qip__editor-folder-action {
  color: var(--brand);
  font-size: 11px;
}

.qip__editor-error {
  margin: 0;
  color: var(--danger);
  font-size: 12px;
}

@media (max-width: 1280px) {
  .qip__body {
    grid-template-columns: 210px minmax(0, 1fr) 310px;
  }
}

@media (max-width: 1080px) {
  .qip__body {
    grid-template-columns: 210px minmax(0, 1fr);
  }

  .qip__side {
    grid-column: 1 / -1;
    overflow: visible;
  }
}

@media (max-width: 720px) {
  .qip__topbar {
    padding: 10px 10px 8px;
  }

  .qip__body {
    grid-template-columns: minmax(0, 1fr);
    padding: 2px 10px 10px;
  }

  .qip__empty {
    margin: 2px 10px 10px;
  }

  .qip__empty--main {
    margin: 0;
  }

  .qip__favorites {
    max-height: 230px;
  }

  .qip__main-head {
    align-items: flex-start;
  }

  .qip__controls {
    flex-wrap: wrap;
  }

  .qip__search {
    flex: 1 0 100%;
  }

  .qip__hint {
    display: none;
  }

  .qip__side {
    grid-column: auto;
  }
}
</style>
