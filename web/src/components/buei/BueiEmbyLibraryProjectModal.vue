<script setup lang="ts">
import { ref, watch } from "vue";
import {
  fetchBueiEmbyLibraryProjectFiles,
  type BueiEmbyLibraryItem,
  type BueiEmbyLibraryProjectEntry,
} from "@/api/bueiEmbyLibrary";
import { getApiErrorMessage } from "@/api/client";
import AppModal from "@/components/base/AppModal.vue";
import BusySpinner from "@/components/base/BusySpinner.vue";
import BueiProjectFileTable from "@/components/buei/BueiProjectFileTable.vue";
import {
  nextProjectSort,
  type ProjectSortDir,
  type ProjectSortField,
} from "@/components/buei/bueiProjectSort";
import SvgIcon from "@/components/icons/SvgIcon.vue";

const props = defineProps<{
  open: boolean;
  item: BueiEmbyLibraryItem | null;
}>();

const emit = defineEmits<{ close: [] }>();

const path = ref<string[]>([]);
const strmEntries = ref<BueiEmbyLibraryProjectEntry[]>([]);
const cloudEntries = ref<BueiEmbyLibraryProjectEntry[]>([]);
const loading = ref(false);
const error = ref("");
const strmError = ref("");
const cloudError = ref("");
const sortField = ref<ProjectSortField>("name");
const sortDir = ref<ProjectSortDir>("");
const strmSelected = ref<Set<string>>(new Set());
const cloudSelected = ref<Set<string>>(new Set());


watch(
  () => [props.open, props.item?.id] as const,
  ([open, itemId]) => {
    if (!open || !itemId) return;
    path.value = [];
    sortField.value = "name";
    sortDir.value = "";
    strmSelected.value = new Set();
    cloudSelected.value = new Set();
    void load();
  },
  { immediate: true },
);

async function load() {
  const item = props.item;
  if (!item) return;
  loading.value = true;
  error.value = "";
  strmError.value = "";
  cloudError.value = "";
  try {
    const result = await fetchBueiEmbyLibraryProjectFiles(item.id, path.value.join("/"));
    strmEntries.value = result.strm_entries || [];
    cloudEntries.value = result.cloud_entries || [];
    strmError.value = result.strm_error || "";
    cloudError.value = result.cloud_error || "";
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
  void load();
}

// 面包屑与返回按钮只在项目内前进/后退，项目根目录之上不可到达。
function goTo(index: number) {
  if (index >= path.value.length - 1) return;
  path.value = index < 0 ? [] : path.value.slice(0, index + 1);
  void load();
}

function goUp() {
  if (!path.value.length) return;
  path.value = path.value.slice(0, -1);
  void load();
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
        <button type="button" class="project-browse__refresh" :disabled="loading" @click="load">
          <BusySpinner v-if="loading" variant="notch" :size="12" />
          <SvgIcon v-else name="rotate" :size="12" />
          刷新
        </button>
      </div>

      <div v-if="error" class="project-browse__error">{{ error }}</div>
      <div v-else class="project-browse__panes">
        <BueiProjectFileTable
          title="STRM 目录"
          icon="server"
          :entries="strmEntries"
          :loading="loading"
          :error="strmError"
          :sort-field="sortField"
          :sort-dir="sortDir"
          :selected="strmSelected"
          @sort="handleSort"
          @open-dir="openDir"
          @update:selected="strmSelected = $event"
        />
        <BueiProjectFileTable
          title="网盘目录"
          icon="cloud-arrow-down"
          :entries="cloudEntries"
          :loading="loading"
          :error="cloudError"
          :sort-field="sortField"
          :sort-dir="sortDir"
          :selected="cloudSelected"
          @sort="handleSort"
          @open-dir="openDir"
          @update:selected="cloudSelected = $event"
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
