<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import type { FileItem } from "@/api/types";
import { filesApi } from "@/api/files";
import { getApiErrorMessage } from "@/api/client";
import type { GuangyaShareKind } from "@/api/bueiGuangyaShare";
import { formatSize, formatTime } from "@/utils/format";
import {
  INGEST_KIND_ICONS,
  INGEST_KIND_LABELS,
  ingestKindFromFile,
} from "@/utils/bueiIngestKinds";
import SvgIcon from "@/components/icons/SvgIcon.vue";

// buei 定制：入库页展示目标文件夹现有内容，只读、无勾选，支持表头排序。
const props = withDefaults(
  defineProps<{
    accountId: number;
    path: string;
    refreshKey?: number;
    title?: string;
    showMissing?: boolean;
  }>(),
  { refreshKey: 0, title: "目标文件夹内容", showMissing: false },
);

type State = "idle" | "loading" | "ready" | "missing" | "error";
type SortKey = "name" | "kind" | "size" | "time";

const state = ref<State>("idle");
const items = ref<FileItem[]>([]);
const error = ref("");
const sortKey = ref<SortKey>("name");
const sortDir = ref<"asc" | "desc">("desc");
let generation = 0;
let timer: number | undefined;

const pathParts = computed(() =>
  String(props.path ?? "")
    .split("/")
    .map((part) => part.trim())
    .filter(Boolean),
);

const showShell = computed(
  () => props.showMissing || state.value === "ready" || state.value === "error",
);

function kindOf(item: FileItem): GuangyaShareKind {
  return ingestKindFromFile(item);
}

function kindLabel(item: FileItem) {
  return INGEST_KIND_LABELS[kindOf(item)];
}

function kindIcon(item: FileItem) {
  return item.is_dir ? "folder" : INGEST_KIND_ICONS[kindOf(item)];
}

const sortedItems = computed(() => {
  const direction = sortDir.value === "asc" ? 1 : -1;
  return [...items.value].sort((a, b) => {
    let result = 0;
    if (sortKey.value === "name") {
      result = a.name.localeCompare(b.name, "zh-CN", { numeric: true, sensitivity: "base" });
    } else if (sortKey.value === "kind") {
      result = INGEST_KIND_LABELS[kindOf(a)].localeCompare(
        INGEST_KIND_LABELS[kindOf(b)],
        "zh-CN",
      );
    } else if (sortKey.value === "size") {
      result = a.size - b.size;
    } else {
      result = String(a.mod_time ?? "").localeCompare(String(b.mod_time ?? ""));
    }
    if (result === 0) {
      result = a.name.localeCompare(b.name, "zh-CN", { numeric: true, sensitivity: "base" });
    }
    return result * direction;
  });
});

function stopTimer() {
  if (timer !== undefined) {
    window.clearTimeout(timer);
    timer = undefined;
  }
}

function toggleSort(key: SortKey) {
  if (sortKey.value === key) {
    sortDir.value = sortDir.value === "asc" ? "desc" : "asc";
    return;
  }
  sortKey.value = key;
  sortDir.value = "asc";
}

function sortIcon(key: SortKey) {
  if (sortKey.value !== key) return "";
  return sortDir.value === "asc" ? "chevron-up" : "chevron-down";
}

function ariaSort(key: SortKey) {
  if (sortKey.value !== key) return "none";
  return sortDir.value === "asc" ? "ascending" : "descending";
}

async function load() {
  const seq = ++generation;
  const accountId = props.accountId;
  const parts = pathParts.value;
  if (!accountId || !parts.length) {
    state.value = "idle";
    items.value = [];
    error.value = "";
    return;
  }
  state.value = "loading";
  error.value = "";
  try {
    let parentId = "";
    for (let index = 0; index < parts.length; index += 1) {
      const res = await filesApi.list(accountId, parentId, {
        // 目标目录可能是刚创建的，最后两级强制刷新，前面层级继续走缓存。
        forceRefresh: index >= parts.length - 2,
      });
      if (seq !== generation) return;
      const folder = res.items.find((item) => item.is_dir && item.name === parts[index]);
      if (!folder) {
        state.value = "missing";
        items.value = [];
        return;
      }
      parentId = folder.id;
    }
    const res = await filesApi.list(accountId, parentId, { forceRefresh: true });
    if (seq !== generation) return;
    items.value = res.items;
    state.value = "ready";
  } catch (err) {
    if (seq !== generation) return;
    items.value = [];
    state.value = "error";
    error.value = getApiErrorMessage(err, "读取目标文件夹失败");
  }
}

function schedule() {
  stopTimer();
  generation += 1;
  if (!props.accountId || !pathParts.value.length) {
    state.value = "idle";
    items.value = [];
    error.value = "";
    return;
  }
  state.value = "loading";
  timer = window.setTimeout(() => {
    timer = undefined;
    void load();
  }, 260);
}

watch(
  () => `${props.accountId}:${props.path}:${props.refreshKey}`,
  () => schedule(),
  { immediate: true },
);

onBeforeUnmount(() => {
  generation += 1;
  stopTimer();
});
</script>

<template>
  <section v-if="showShell" class="btf">
    <header class="btf__head">
      <div class="btf__heading">
        <SvgIcon name="folder" :size="15" />
        <h4>{{ title }}</h4>
        <span class="btf__count">{{ state === "ready" ? items.length : 0 }} 项</span>
      </div>
      <span class="btf__path" :title="path">{{ path }}</span>
    </header>

    <div v-if="state === 'loading'" class="btf__state">正在读取目标文件夹…</div>

    <div v-else-if="state === 'missing'" class="btf__state">目标文件夹尚未创建</div>

    <div v-else-if="state === 'idle'" class="btf__state">选择保存位置后显示目标文件夹内容</div>

    <div v-else-if="state === 'error'" class="btf__state is-error">
      <span>{{ error }}</span>
      <button type="button" class="btf__retry" @click="schedule">重试</button>
    </div>

    <div v-else class="btf__tablewrap">
      <table class="btf__table">
        <thead>
          <tr>
            <th
              class="btf__th btf__col-name"
              :class="{ 'is-sorted': sortKey === 'name' }"
              :aria-sort="ariaSort('name')"
              title="点击切换升序或降序"
              @click="toggleSort('name')"
            >
              名称
              <SvgIcon v-if="sortKey === 'name'" :name="sortIcon('name')" :size="11" />
            </th>
            <th
              class="btf__th btf__col-kind"
              :class="{ 'is-sorted': sortKey === 'kind' }"
              :aria-sort="ariaSort('kind')"
              title="点击切换升序或降序"
              @click="toggleSort('kind')"
            >
              类型
              <SvgIcon v-if="sortKey === 'kind'" :name="sortIcon('kind')" :size="11" />
            </th>
            <th
              class="btf__th btf__col-size"
              :class="{ 'is-sorted': sortKey === 'size' }"
              :aria-sort="ariaSort('size')"
              title="点击切换升序或降序"
              @click="toggleSort('size')"
            >
              大小
              <SvgIcon v-if="sortKey === 'size'" :name="sortIcon('size')" :size="11" />
            </th>
            <th
              class="btf__th btf__col-time"
              :class="{ 'is-sorted': sortKey === 'time' }"
              :aria-sort="ariaSort('time')"
              title="点击切换升序或降序"
              @click="toggleSort('time')"
            >
              修改时间
              <SvgIcon v-if="sortKey === 'time'" :name="sortIcon('time')" :size="11" />
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in sortedItems" :key="item.id" class="btf__row">
            <td class="btf__col-name">
              <div class="btf__namecell">
                <span class="btf__fileicon" :class="`is-${kindOf(item)}`">
                  <SvgIcon :name="kindIcon(item)" :size="16" />
                </span>
                <span class="btf__name" :title="item.name">{{ item.name }}</span>
                <span v-if="item.is_dir" class="btf__folder-tag">文件夹</span>
              </div>
            </td>
            <td class="btf__col-kind"><span class="btf__kind">{{ kindLabel(item) }}</span></td>
            <td class="btf__col-size">{{ item.is_dir ? "-" : formatSize(item.size) }}</td>
            <td class="btf__col-time">{{ formatTime(item.mod_time) }}</td>
          </tr>
          <tr v-if="!sortedItems.length">
            <td colspan="4" class="btf__state">目标文件夹为空</td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>

<style scoped>
.btf {
  display: flex;
  flex-direction: column;
  min-width: 0;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--surface);
  overflow: hidden;
}

.btf__head {
  display: flex;
  align-items: center;
  gap: 10px;
  flex: 0 0 auto;
  min-width: 0;
  padding: 8px 11px;
  border-bottom: 1px solid var(--border-soft);
  background: var(--surface-sunken);
}

.btf__heading {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 0 0 auto;
  color: var(--text-muted);
}

.btf__heading h4 {
  margin: 0;
  color: var(--text);
  font-size: 12px;
  font-weight: 600;
}

.btf__count {
  font-size: 11px;
}

.btf__path {
  min-width: 0;
  margin-left: auto;
  overflow: hidden;
  color: var(--text-muted);
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.btf__tablewrap {
  max-height: 260px;
  overflow: auto;
  -webkit-overflow-scrolling: touch;
}

.btf__table {
  width: 100%;
  min-width: 480px;
  border-collapse: collapse;
  table-layout: fixed;
  font-size: 12px;
}

.btf__table thead {
  position: sticky;
  top: 0;
  z-index: 1;
  background: var(--surface-sunken);
}

.btf__th {
  padding: 7px 10px;
  border-bottom: 1px solid var(--border-soft);
  color: var(--text-muted);
  font-size: 11px;
  font-weight: 500;
  text-align: left;
  white-space: nowrap;
  cursor: pointer;
  user-select: none;
}

.btf__th:hover,
.btf__th.is-sorted {
  color: var(--brand);
}

.btf__th svg {
  vertical-align: -2px;
}

.btf__row:hover {
  background: var(--surface-hover);
}

.btf__table td {
  padding: 7px 10px;
  border-bottom: 1px solid var(--border-soft);
  color: var(--text);
  vertical-align: middle;
}

.btf__table tbody tr:last-child td {
  border-bottom: none;
}

.btf__col-kind {
  width: 58px;
}

.btf__col-size {
  width: 82px;
}

.btf__col-time {
  width: 146px;
}

.btf__fileicon {
  display: grid;
  place-items: center;
  width: 24px;
  height: 24px;
  flex: 0 0 auto;
  border-radius: var(--radius-xs);
  background: var(--surface-sunken);
  color: var(--text-muted);
}

.btf__fileicon.is-folder {
  color: #d97706;
  background: color-mix(in srgb, #f59e0b 12%, var(--surface));
}

.btf__fileicon.is-video {
  color: #7c3aed;
  background: color-mix(in srgb, #8b5cf6 10%, var(--surface));
}

.btf__fileicon.is-audio {
  color: #0891b2;
  background: color-mix(in srgb, #06b6d4 10%, var(--surface));
}

.btf__fileicon.is-image {
  color: #db2777;
  background: color-mix(in srgb, #ec4899 10%, var(--surface));
}

.btf__fileicon.is-subtitle {
  color: #0f766e;
  background: color-mix(in srgb, #14b8a6 12%, var(--surface));
}

.btf__fileicon.is-document {
  color: #2563eb;
  background: color-mix(in srgb, #3b82f6 10%, var(--surface));
}

.btf__fileicon.is-archive {
  color: #b45309;
  background: color-mix(in srgb, #f59e0b 10%, var(--surface));
}

.btf__kind {
  color: var(--text-muted);
  font-size: 11px;
  white-space: nowrap;
}

.btf__namecell {
  display: flex;
  align-items: center;
  gap: 7px;
  min-width: 0;
}

.btf__name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.btf__folder-tag {
  flex: 0 0 auto;
  padding: 1px 5px;
  border-radius: 999px;
  background: var(--surface-sunken);
  color: var(--text-muted);
  font-size: 10px;
}

.btf__state,
.btf__table .btf__state {
  padding: 18px 12px;
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.6;
  text-align: center;
  word-break: break-word;
}

.btf__state.is-error {
  display: flex;
  align-items: center;
  justify-content: center;
  flex-wrap: wrap;
  gap: 10px;
}

.btf__retry {
  border: none;
  background: transparent;
  color: var(--brand);
  font-size: 12px;
  cursor: pointer;
}

.btf__retry:hover {
  text-decoration: underline;
}
</style>
