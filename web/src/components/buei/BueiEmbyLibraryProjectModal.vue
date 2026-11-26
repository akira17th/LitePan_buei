<script setup lang="ts">
import { computed, ref, watch } from "vue";
import {
  cleanupBueiProjectStrm,
  deleteBueiEmbyLibraryProjectFiles,
  fetchBueiEmbyLibraryProjectFiles,
  fetchBueiProjectStrmStatus,
  generateBueiProjectStrm,
  matchBueiTMDBItem,
  rescrapeBueiProjectItem,
  searchBueiTMDB,
  type BueiEmbyLibraryItem,
  type BueiEmbyLibraryProjectEntry,
  type BueiTMDBResult,
} from "@/api/bueiEmbyLibrary";
import { getApiErrorMessage } from "@/api/client";
import { confirm } from "@/composables/useConfirm";
import { toast } from "@/composables/useToast";
import AppModal from "@/components/base/AppModal.vue";
import BusySpinner from "@/components/base/BusySpinner.vue";
import BueiProjectFileTable from "@/components/buei/BueiProjectFileTable.vue";
import {
  nextProjectSort,
  type ProjectSortDir,
  type ProjectSortField,
} from "@/components/buei/bueiProjectSort";
import SvgIcon from "@/components/icons/SvgIcon.vue";
import { projectAssocGroups } from "@/components/buei/bueiProjectAssoc";

const props = defineProps<{
  open: boolean;
  item: BueiEmbyLibraryItem | null;
  configId?: string;
  libraryId?: string;
}>();

const emit = defineEmits<{ close: []; removed: []; refreshed: [] }>();

const strmPathLabel = ref("");
const cloudPathLabel = ref("");

const path = ref<string[]>([]);
const strmEntries = ref<BueiEmbyLibraryProjectEntry[]>([]);
const cloudEntries = ref<BueiEmbyLibraryProjectEntry[]>([]);
const loading = ref(false);
const deleting = ref(false);
const error = ref("");
const strmError = ref("");
const cloudError = ref("");
const sortField = ref<ProjectSortField>("name");
const sortDir = ref<ProjectSortDir>("");
const strmSelected = ref<Set<string>>(new Set());
const cloudSelected = ref<Set<string>>(new Set());
const generating = ref(false);
const pendingStrm = ref(0);
const pendingMetadata = ref(0);
const rescraping = ref(false);
const orphanStrm = ref<string[]>([]);
const cleaning = ref(false);
// 仅忽略「当前这次」提示：离开目录后再次进入会重新检测并提示。
const strmIgnored = ref(false);

// TMDB 手动匹配
const tmdbOpen = ref(false);
const tmdbQuery = ref("");
const tmdbMediaType = ref<"tv" | "movie">("tv");
const tmdbLoading = ref(false);
const tmdbMatchingID = ref(0);
const tmdbResults = ref<BueiTMDBResult[]>([]);
const tmdbError = ref("");


watch(
  () => [props.open, props.item?.id] as const,
  ([open, itemId]) => {
    if (!open || !itemId) return;
    path.value = [];
    sortField.value = "name";
    sortDir.value = "";
    strmSelected.value = new Set();
    cloudSelected.value = new Set();
    strmIgnored.value = false;
    generating.value = false;
    closeTMDB();
    void refreshAndCheck();
  },
  { immediate: true },
);

// 两侧勾选的视频 / .strm 汇总成关联分组，两个表格共用同一组颜色。
const assocGroups = computed(() =>
  projectAssocGroups([
    { side: "cloud", names: [...cloudSelected.value] },
    { side: "strm", names: [...strmSelected.value] },
  ]),
);

// force 为 true 时绕过网盘目录缓存，确保「刷新」能拿到网盘最新文件。
async function refreshAndCheck(force = false) {
  pendingStrm.value = 0;
  pendingMetadata.value = 0;
  orphanStrm.value = [];
  strmIgnored.value = false;
  await load(force);
  void checkPendingStrm();
}

// 检测当前目录待生成的 STRM，以横幅提示，不弹窗打断浏览。
async function checkPendingStrm() {
  const item = props.item;
  if (!item || generating.value || deleting.value || tmdbMatchingID.value) return;
  const key = path.value.join("/");
  try {
    const status = await fetchBueiProjectStrmStatus(item.id, key);
    if (key !== path.value.join("/")) return;
    // 「忽略」只影响待生成横幅，多余 STRM 仍照常提示。
    pendingStrm.value = strmIgnored.value ? 0 : status.pending_strm || 0;
    pendingMetadata.value = strmIgnored.value ? 0 : status.pending_metadata || 0;
    orphanStrm.value = status.orphan_names || [];
  } catch {
    // 检测失败不打扰用户，下次进入目录再试。
    pendingStrm.value = 0;
    pendingMetadata.value = 0;
    orphanStrm.value = [];
  }
}

async function cleanupOrphanStrm() {
  const item = props.item;
  if (!item || !orphanStrm.value.length || cleaning.value) return;
  cleaning.value = true;
  const names = [...orphanStrm.value];
  try {
    const result = await cleanupBueiProjectStrm({
      itemId: item.id,
      path: path.value.join("/"),
      names,
      configId: props.configId,
      libraryId: props.libraryId,
    });
    await load(true);
    orphanStrm.value = [];
    emit("refreshed");
    toast.success(`已清理 ${result.deleted} 个文件`);
  } catch (err) {
    toast.error(getApiErrorMessage(err, "删除多余 STRM 失败"));
  } finally {
    cleaning.value = false;
  }
}

function dismissStrmBanner() {
  strmIgnored.value = true;
  pendingStrm.value = 0;
  pendingMetadata.value = 0;
}

async function generateStrm() {
  const item = props.item;
  if (!item || generating.value) return;
  const key = path.value.join("/");
  generating.value = true;
  strmIgnored.value = true;
  try {
    const result = await generateBueiProjectStrm({
      itemId: item.id,
      path: key,
      configId: props.configId,
      libraryId: props.libraryId,
    });
    await load(true);
    emit("refreshed");
    pendingStrm.value = 0;
    pendingMetadata.value = 0;
    const created = (result.created || 0) + (result.updated || 0);
    const metadata =
      (result.metadata_created || 0) + (result.metadata_uploaded || 0) + (result.metadata_deleted || 0);
    const parts: string[] = [];
    if (created > 0) parts.push(`已生成 ${created} 个 STRM`);
    if (metadata > 0) parts.push(`已同步 ${metadata} 个元数据文件`);
    if (created > 0 && result.scraped) parts.push("已自动刮削");
    toast.success(parts.length ? parts.join("，") : "没有需要处理的文件");
    if (result.scrape_error) toast.error(`自动刮削失败：${result.scrape_error}`);
    if (result.emby_error) toast.error(`刷新 Emby 媒体库失败：${result.emby_error}`);
  } catch (err) {
    toast.error(getApiErrorMessage(err, "生成 STRM 失败"));
  } finally {
    generating.value = false;
  }
}

async function load(force = false) {
  const item = props.item;
  if (!item) return;
  loading.value = true;
  error.value = "";
  strmError.value = "";
  cloudError.value = "";
  try {
    const result = await fetchBueiEmbyLibraryProjectFiles(item.id, path.value.join("/"), force);
    strmEntries.value = result.strm_entries || [];
    cloudEntries.value = result.cloud_entries || [];
    strmError.value = result.strm_error || "";
    cloudError.value = result.cloud_error || "";
    strmPathLabel.value = result.strm_path || "";
    const cloudPath = result.cloud_path || "";
    const cloudName = result.cloud_name || "";
    cloudPathLabel.value = cloudName ? `${cloudName} · ${cloudPath}` : cloudPath;
    strmSelected.value = new Set();
    cloudSelected.value = new Set();
  } catch (err) {
    error.value = getApiErrorMessage(err, "读取项目目录失败");
    strmEntries.value = [];
    cloudEntries.value = [];
  } finally {
    loading.value = false;
  }
}

function openDir(name: string) {
  path.value = [...path.value, name];
  void refreshAndCheck();
}

// 面包屑与返回按钮只在项目内前进/后退，项目根目录之上不可到达。
function goTo(index: number) {
  if (index >= path.value.length - 1) return;
  path.value = index < 0 ? [] : path.value.slice(0, index + 1);
  void refreshAndCheck();
}

function goUp() {
  if (!path.value.length) return;
  path.value = path.value.slice(0, -1);
  void refreshAndCheck();
}

async function deleteSelected(names: string[], side: "strm" | "cloud") {
  const item = props.item;
  if (!item || !names.length || deleting.value) return;
  const ok = await confirm({
    title: "删除选中的文件？",
    message:
      side === "cloud"
        ? `将删除网盘侧选中的 ${names.length} 项；勾选视频正片时会连带删除同名关联文件与对应的本地 STRM 文件，删除后不可恢复。`
        : `将删除 STRM 侧选中的 ${names.length} 项；勾选 .strm 时会连带删除同类关联文件与网盘中对应的文件，删除后不可恢复。`,
    confirmText: "确认删除",
    cancelText: "取消",
    danger: true,
  }).catch(() => false);
  if (!ok) return;
  deleting.value = true;
  try {
    const result = await deleteBueiEmbyLibraryProjectFiles({
      itemId: item.id,
      path: path.value.join("/"),
      names,
      side,
      configId: props.configId,
      libraryId: props.libraryId,
    });
    if (result.project_removed) {
      // 项目里已没有 strm，后端把整个项目文件夹删掉了，直接关闭弹窗并刷新列表。
      toast.success(`已删除项目「${item.name}」的全部文件`);
      emit("removed");
      return;
    }
    await load(true);
    strmSelected.value = new Set();
    cloudSelected.value = new Set();
    const total = Math.max(result.cloud_names?.length || 0, result.strm_names?.length || 0);
    toast.success(`已删除 ${total} 个文件，并已刷新索引与媒体库`);
  } catch (err) {
    toast.error(getApiErrorMessage(err, "删除文件失败"));
  } finally {
    deleting.value = false;
  }
}

function openTMDB() {
  const item = props.item;
  if (!item) return;
  tmdbOpen.value = true;
  tmdbQuery.value = item.name || "";
  tmdbMediaType.value = item.type === "Movie" ? "movie" : "tv";
  tmdbResults.value = [];
  tmdbError.value = "";
  void searchTMDB();
}

async function rescrape() {
  const item = props.item;
  if (!item || rescraping.value || generating.value) return;
  rescraping.value = true;
  try {
    await rescrapeBueiProjectItem({ itemId: item.id, configId: props.configId, libraryId: props.libraryId });
    await load();
    emit("refreshed");
    toast.success("已按当前 TMDB ID 重新刮削");
  } catch (err) {
    toast.error(getApiErrorMessage(err, "重新刮削失败"));
  } finally {
    rescraping.value = false;
  }
}

function closeTMDB() {
  tmdbOpen.value = false;
  tmdbResults.value = [];
  tmdbError.value = "";
}

async function searchTMDB() {
  const query = tmdbQuery.value.trim();
  if (!query || tmdbLoading.value) return;
  tmdbLoading.value = true;
  tmdbError.value = "";
  try {
    const result = await searchBueiTMDB(query, tmdbMediaType.value);
    tmdbResults.value = result.results || [];
    if (!tmdbResults.value.length) tmdbError.value = "没有搜索到结果，换个名称或类型再试试";
  } catch (err) {
    tmdbError.value = getApiErrorMessage(err, "TMDB 搜索失败");
    tmdbResults.value = [];
  } finally {
    tmdbLoading.value = false;
  }
}

async function chooseTMDB(result: BueiTMDBResult) {
  const item = props.item;
  if (!item || tmdbMatchingID.value) return;
  tmdbMatchingID.value = result.id;
  try {
    await matchBueiTMDBItem({
      itemId: item.id,
      tmdbId: String(result.id),
      mediaType: result.media_type || tmdbMediaType.value,
      title: result.title,
      year: result.year,
      configId: props.configId,
      libraryId: props.libraryId,
    });
    closeTMDB();
    await load();
    emit("refreshed");
    toast.success(`已匹配「${result.title}${result.year ? ` (${result.year})` : ""}」并重新刮削`);
  } catch (err) {
    toast.error(getApiErrorMessage(err, "TMDB 匹配刮削失败"));
  } finally {
    tmdbMatchingID.value = 0;
  }
}

function tmdbLink(result: BueiTMDBResult) {
  const kind = result.media_type === "tv" ? "tv" : "movie";
  return `https://www.themoviedb.org/${kind}/${result.id}`;
}

function handleSort(field: ProjectSortField) {
  const next = nextProjectSort(sortField.value, sortDir.value, field);
  sortField.value = next.field;
  sortDir.value = next.dir;
}
</script>

<template>
  <AppModal :open="open" size="lg" wide :title="item?.name || '项目目录'" @close="emit('close')">
    <div class="project-browse">
      <div class="project-browse__head">
        <div class="project-browse__crumbs">
          <button type="button" class="project-browse__up" :disabled="!path.length || loading" aria-label="返回上一级" title="返回上一级" @click="goUp">
            <SvgIcon name="arrow-left" :size="11" />
          </button>
          <button type="button" class="project-browse__crumb is-root" :class="{ 'is-current': !path.length }" @click="goTo(-1)">
            <SvgIcon name="folder-tree" :size="11" />根目录
          </button>
          <template v-for="(segment, index) in path" :key="segment">
            <span class="project-browse__sep" aria-hidden="true">/</span>
            <button type="button" class="project-browse__crumb" :class="{ 'is-current': index === path.length - 1 }" @click="goTo(index)">{{ segment }}</button>
          </template>
        </div>
        <div class="project-browse__actions">
          <span v-if="generating" class="project-browse__busy">
            <BusySpinner variant="notch" :size="12" />{{ pendingStrm > 0 ? "正在生成 STRM 并刮削…" : "正在同步元数据…" }}
          </span>
          <button
            type="button"
            class="project-browse__tmdb"
            :disabled="!item || generating || rescraping"
            @click="rescrape"
          >
            <BusySpinner v-if="rescraping" variant="notch" :size="11" />
            <SvgIcon v-else name="rotate-right" :size="12" />重刮
          </button>
          <button
            type="button"
            class="project-browse__tmdb"
            :class="{ 'is-active': tmdbOpen }"
            :disabled="!item || generating || rescraping"
            @click="tmdbOpen ? closeTMDB() : openTMDB()"
          >
            <SvgIcon name="wand-magic-sparkles" :size="12" />TMDB 匹配
          </button>
          <button type="button" class="project-browse__refresh" :disabled="loading || generating" @click="refreshAndCheck(true)">
            <BusySpinner v-if="loading" variant="notch" :size="12" />
            <SvgIcon v-else name="rotate" :size="12" />
            刷新
          </button>
        </div>
      </div>

      <div v-if="orphanStrm.length" class="project-strm-banner is-danger">
        <span class="project-strm-banner__text">
          <SvgIcon name="circle-exclamation" :size="12" />当前目录有 <b>{{ orphanStrm.length }}</b> 个 STRM 文件在网盘中没有对应的视频文件
        </span>
        <span class="project-strm-banner__actions">
          <button type="button" class="project-strm-banner__danger" :disabled="cleaning" @click="cleanupOrphanStrm">
            <BusySpinner v-if="cleaning" variant="notch" :size="11" />删除
          </button>
        </span>
      </div>

      <div v-if="pendingStrm > 0 || pendingMetadata > 0" class="project-strm-banner">
        <span class="project-strm-banner__text">
          <SvgIcon name="circle-info" :size="12" />当前目录
          <template v-if="pendingStrm > 0">有 <b>{{ pendingStrm }}</b> 个媒体文件还没有生成 STRM</template>
          <template v-if="pendingStrm > 0 && pendingMetadata > 0">，</template>
          <template v-if="pendingMetadata > 0">有 <b>{{ pendingMetadata }}</b> 个元数据文件待同步</template>
        </span>
        <span class="project-strm-banner__actions">
          <button type="button" class="project-strm-banner__primary" :disabled="generating" @click="generateStrm">
            <BusySpinner v-if="generating" variant="notch" :size="11" />{{ pendingStrm > 0 ? "生成并刮削" : "同步元数据" }}
          </button>
          <button type="button" class="project-strm-banner__ghost" :disabled="generating" @click="dismissStrmBanner">忽略</button>
        </span>
      </div>

      <section v-if="tmdbOpen" class="project-tmdb">
        <div class="project-tmdb__bar">
          <label class="project-tmdb__type">
            <select v-model="tmdbMediaType" aria-label="TMDB 类型" @change="searchTMDB">
              <option value="tv">剧集</option>
              <option value="movie">电影</option>
            </select>
          </label>
          <label class="project-tmdb__query">
            <SvgIcon name="magnifying-glass" :size="12" />
            <input v-model="tmdbQuery" type="search" placeholder="搜索 TMDB 名称" @keydown.enter.prevent="searchTMDB" />
          </label>
          <button type="button" class="project-tmdb__search" :disabled="tmdbLoading || !tmdbQuery.trim()" @click="searchTMDB">
            <BusySpinner v-if="tmdbLoading" variant="notch" :size="11" />
            搜索
          </button>
          <button type="button" class="project-tmdb__close" aria-label="关闭 TMDB 匹配" @click="closeTMDB">
            <SvgIcon name="xmark" :size="11" />
          </button>
        </div>
        <div v-if="tmdbResults.length" class="project-tmdb__results">
          <div
            v-for="result in tmdbResults"
            :key="result.id"
            class="project-tmdb__item"
            :class="{ 'is-disabled': !!tmdbMatchingID }"
            role="button"
            tabindex="0"
            @click="chooseTMDB(result)"
            @keydown.enter.prevent="chooseTMDB(result)"
          >
            <span class="project-tmdb__poster">
              <img v-if="result.poster_url" :src="result.poster_url" :alt="result.title" loading="lazy" />
              <SvgIcon v-else name="film" :size="16" />
            </span>
            <span class="project-tmdb__copy">
              <span class="project-tmdb__title-row">
                <span class="project-tmdb__title">{{ result.title }}<em v-if="result.year"> ({{ result.year }})</em></span>
                <a
                  class="project-tmdb__link"
                  :href="tmdbLink(result)"
                  target="_blank"
                  rel="noopener noreferrer"
                  title="在 TMDB 打开"
                  aria-label="在 TMDB 打开"
                  @click.stop
                ><SvgIcon name="arrow-up-right-from-square" :size="10" /></a>
              </span>
              <span v-if="result.overview" class="project-tmdb__overview">{{ result.overview }}</span>
            </span>
            <span class="project-tmdb__pick">
              <BusySpinner v-if="tmdbMatchingID === result.id" variant="notch" :size="12" />
              <template v-else>选择</template>
            </span>
          </div>
        </div>
        <p v-else class="project-tmdb__hint" :class="{ 'is-error': tmdbError }">{{ tmdbLoading ? "正在搜索 TMDB…" : tmdbError || "输入名称后搜索，选择结果会立即刮削该项目" }}</p>
      </section>

      <div v-if="error" class="project-browse__error">{{ error }}</div>
      <div v-else class="project-browse__panes">
        <BueiProjectFileTable
          title="STRM 目录"
          icon="server"
          :path="strmPathLabel"
          :entries="strmEntries"
          :loading="loading"
          :error="strmError"
          :sort-field="sortField"
          :sort-dir="sortDir"
          :selected="strmSelected"
          :deleting="deleting"
          :assoc-groups="assocGroups"
          @sort="handleSort"
          @open-dir="openDir"
          @update:selected="strmSelected = $event"
          @delete="deleteSelected($event, 'strm')"
        />
        <BueiProjectFileTable
          title="网盘目录"
          icon="cloud-arrow-down"
          :path="cloudPathLabel"
          :entries="cloudEntries"
          :loading="loading"
          :error="cloudError"
          :sort-field="sortField"
          :sort-dir="sortDir"
          :selected="cloudSelected"
          :deleting="deleting"
          :assoc-groups="assocGroups"
          @sort="handleSort"
          @open-dir="openDir"
          @update:selected="cloudSelected = $event"
          @delete="deleteSelected($event, 'cloud')"
        />
      </div>
    </div>
  </AppModal>
</template>

<style scoped>
.project-browse { display: flex; flex-direction: column; gap: 12px; }
.project-browse__head { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.project-browse__crumbs { display: flex; align-items: center; flex-wrap: wrap; gap: 5px; min-width: 0; font-size: 12px; color: var(--text-muted); }
.project-browse__up {
  display: inline-grid;
  place-items: center;
  width: 24px;
  height: 24px;
  padding: 0;
  border: 1px solid var(--border);
  border-radius: 6px;
  color: var(--text-muted);
  background: var(--surface);
  cursor: pointer;
  transition: border-color .15s ease, color .15s ease;
}
.project-browse__up:hover:not(:disabled) { border-color: var(--brand); color: var(--brand); }
.project-browse__up:disabled { opacity: .45; cursor: not-allowed; }
.project-browse__crumb {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  max-width: 22em;
  padding: 3px 8px;
  border: none;
  border-radius: 6px;
  background: var(--surface-sunken);
  color: var(--text-regular);
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  cursor: pointer;
  transition: background .15s ease, color .15s ease;
}
.project-browse__crumb:hover:not(.is-current) { color: var(--brand); background: color-mix(in srgb, var(--brand) 10%, var(--surface-sunken)); }
.project-browse__crumb.is-current { cursor: default; color: var(--text); font-weight: 600; }
.project-browse__crumb.is-root { color: var(--text); font-weight: 600; }
.project-browse__crumb.is-root.is-current { cursor: default; }
.project-browse__sep { color: color-mix(in srgb, var(--text-muted) 70%, transparent); }

.project-browse__actions { display: inline-flex; align-items: center; gap: 6px; }
.project-browse__busy { display: inline-flex; align-items: center; gap: 5px; color: var(--brand); font-size: 11.5px; white-space: nowrap; }
.project-browse__tmdb {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 28px;
  padding: 0 11px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  color: var(--text-muted);
  background: var(--surface);
  font-size: 11.5px;
  cursor: pointer;
  transition: border-color .15s ease, color .15s ease, background .15s ease;
}
.project-browse__tmdb:hover,
.project-browse__tmdb.is-active { border-color: var(--brand); color: var(--brand); background: color-mix(in srgb, var(--brand) 7%, var(--surface)); }

.project-strm-banner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 8px 12px;
  border: 1px solid color-mix(in srgb, var(--warning) 42%, var(--border));
  border-radius: var(--radius-lg);
  background: color-mix(in srgb, var(--warning) 10%, var(--surface));
  font-size: 12px;
  color: var(--text);
}
.project-strm-banner__text { display: inline-flex; align-items: center; gap: 6px; min-width: 0; }
.project-strm-banner__text b { color: var(--warning); font-variant-numeric: tabular-nums; }
.project-strm-banner__actions { display: inline-flex; align-items: center; gap: 6px; flex-shrink: 0; }
.project-strm-banner__primary {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 26px;
  padding: 0 11px;
  border: 1px solid color-mix(in srgb, var(--warning) 60%, var(--border));
  border-radius: 6px;
  color: #1f2937;
  background: color-mix(in srgb, var(--warning) 78%, #fff);
  font-size: 11.5px;
  font-weight: 600;
  cursor: pointer;
}
.project-strm-banner__primary:hover:not(:disabled) { background: var(--warning); }
.project-strm-banner__primary:disabled { opacity: .65; cursor: progress; }
.project-strm-banner.is-danger {
  border-color: color-mix(in srgb, var(--danger) 40%, var(--border));
  background: color-mix(in srgb, var(--danger) 8%, var(--surface));
}
.project-strm-banner.is-danger .project-strm-banner__text b { color: var(--danger); }
.project-strm-banner__danger {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 26px;
  padding: 0 11px;
  border: 1px solid color-mix(in srgb, var(--danger) 50%, var(--border));
  border-radius: 6px;
  color: var(--danger);
  background: var(--surface);
  font-size: 11.5px;
  font-weight: 600;
  cursor: pointer;
}
.project-strm-banner__danger:hover:not(:disabled) { background: color-mix(in srgb, var(--danger) 12%, var(--surface)); border-color: var(--danger); }
.project-strm-banner__danger:disabled { opacity: .65; cursor: progress; }
.project-strm-banner__ghost {
  height: 26px;
  padding: 0 9px;
  border: 1px solid var(--border);
  border-radius: 6px;
  color: var(--text-muted);
  background: var(--surface);
  font-size: 11.5px;
  cursor: pointer;
}
.project-strm-banner__ghost:hover:not(:disabled) { color: var(--text); border-color: color-mix(in srgb, var(--text-muted) 50%, var(--border)); }

.project-tmdb {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px;
  border: 1px solid color-mix(in srgb, var(--brand) 26%, var(--border));
  border-radius: var(--radius-lg);
  background: color-mix(in srgb, var(--brand) 5%, var(--surface));
}
.project-tmdb__bar { display: flex; align-items: center; gap: 8px; }
.project-tmdb__type select {
  height: 28px;
  padding: 0 8px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  color: var(--text-regular);
  font-size: 12px;
}
.project-tmdb__query {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 1;
  min-width: 0;
  height: 28px;
  padding: 0 9px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  color: var(--text-muted);
}
.project-tmdb__query input {
  flex: 1;
  min-width: 0;
  border: none;
  background: transparent;
  color: var(--text);
  font-size: 12px;
  outline: none;
}
.project-tmdb__search {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 28px;
  padding: 0 12px;
  border: 1px solid var(--brand);
  border-radius: var(--radius-sm);
  color: var(--brand);
  background: color-mix(in srgb, var(--brand) 8%, var(--surface));
  font-size: 12px;
  cursor: pointer;
}
.project-tmdb__search:disabled { opacity: .5; cursor: not-allowed; }
.project-tmdb__close {
  display: grid;
  place-items: center;
  width: 24px;
  height: 24px;
  padding: 0;
  border: none;
  border-radius: 5px;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
}
.project-tmdb__close:hover { background: var(--surface-hover); color: var(--text); }
.project-tmdb__results { display: flex; flex-direction: column; gap: 4px; max-height: 210px; overflow: auto; }
.project-tmdb__item {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  width: 100%;
  padding: 7px 8px;
  border: 1px solid transparent;
  border-radius: 8px;
  background: var(--surface);
  text-align: left;
  cursor: pointer;
  transition: border-color .15s ease, background .15s ease;
}
.project-tmdb__item:hover:not(.is-disabled) { border-color: color-mix(in srgb, var(--brand) 40%, var(--border)); background: color-mix(in srgb, var(--brand) 6%, var(--surface)); }
.project-tmdb__item:focus-visible { outline: 2px solid var(--brand); outline-offset: 1px; }
.project-tmdb__item.is-disabled { opacity: .7; cursor: default; }
.project-tmdb__title-row { display: flex; align-items: center; gap: 5px; min-width: 0; }
.project-tmdb__link {
  display: inline-grid;
  place-items: center;
  width: 15px;
  height: 15px;
  flex-shrink: 0;
  border-radius: 4px;
  color: var(--text-muted);
  background: color-mix(in srgb, var(--text-muted) 10%, transparent);
  transition: color .15s ease, background .15s ease;
}
.project-tmdb__link:hover { color: #fff; background: #01b4e4; }
.project-tmdb__poster { flex-shrink: 0; display: grid; place-items: center; width: 34px; height: 50px; overflow: hidden; border-radius: 5px; background: var(--surface-sunken); color: var(--text-muted); }
.project-tmdb__poster img { width: 100%; height: 100%; object-fit: cover; }
.project-tmdb__copy { display: flex; flex-direction: column; gap: 3px; min-width: 0; flex: 1; }
.project-tmdb__title { font-size: 12.5px; font-weight: 600; color: var(--text); }
.project-tmdb__title em { font-style: normal; font-weight: 500; color: var(--text-muted); }
.project-tmdb__overview { font-size: 11px; line-height: 1.45; color: var(--text-muted); display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
.project-tmdb__pick { flex-shrink: 0; align-self: center; padding: 3px 8px; border-radius: 5px; color: var(--brand); background: color-mix(in srgb, var(--brand) 10%, transparent); font-size: 11px; }
.project-tmdb__hint { margin: 0; padding: 10px 4px; color: var(--text-muted); font-size: 11.5px; text-align: center; }
.project-tmdb__hint.is-error { color: var(--danger); }

.project-browse__refresh {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 28px;
  padding: 0 11px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  color: var(--text-muted);
  background: var(--surface);
  font-size: 11.5px;
  cursor: pointer;
  transition: border-color .15s ease, color .15s ease;
}
.project-browse__refresh:hover:not(:disabled) { border-color: var(--brand); color: var(--brand); }
.project-browse__refresh:disabled { opacity: .65; cursor: progress; }
.project-browse__error { padding: 24px 12px; border: 1px solid var(--border); border-radius: var(--radius-lg); color: var(--danger); background: var(--surface); font-size: 12px; text-align: center; }
.project-browse__panes { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 14px; }

@media (max-width: 900px) {
  .project-browse__panes { grid-template-columns: minmax(0, 1fr); }
}
</style>
