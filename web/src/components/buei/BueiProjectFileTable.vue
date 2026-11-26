<script setup lang="ts">
import { computed, ref, watch } from "vue";
import type { BueiEmbyLibraryProjectEntry } from "@/api/bueiEmbyLibrary";
import BusySpinner from "@/components/base/BusySpinner.vue";
import SvgIcon from "@/components/icons/SvgIcon.vue";
import BueiTailText from "@/components/buei/BueiTailText.vue";
import {
  PROJECT_ENTRY_KIND_LABELS,
  projectEntryKind,
  projectEntryKindIcon,
  projectEntryKindLabel,
  sortProjectEntries,
  type ProjectEntryKind,
  type ProjectSortDir,
  type ProjectSortField,
} from "@/components/buei/bueiProjectSort";
import { useDismissOnOutside } from "@/composables/useDismissOnOutside";
import { formatSize, formatTime } from "@/utils/format";

const props = withDefaults(
  defineProps<{
    title: string;
    icon: string;
    path?: string;
    entries: BueiEmbyLibraryProjectEntry[];
    loading?: boolean;
    error?: string;
    sortField: ProjectSortField;
    sortDir: ProjectSortDir;
    selected: Set<string>;
    deleting?: boolean;
  }>(),
  { loading: false, error: "", deleting: false, path: "" },
);

const emit = defineEmits<{
  sort: [field: ProjectSortField];
  "open-dir": [name: string];
  "update:selected": [value: Set<string>];
  delete: [names: string[]];
}>();

type TypeFilter = "all" | ProjectEntryKind;

type Column = { field: ProjectSortField; label: string; numeric?: boolean; filter?: boolean };

const columns: Column[] = [
  { field: "name", label: "名称" },
  { field: "type", label: "类型", filter: true },
  { field: "size", label: "大小", numeric: true },
  { field: "time", label: "添加时间" },
];

const typeFilter = ref<TypeFilter>("all");
const filterOpen = ref(false);
const filterRef = ref<HTMLElement | null>(null);
const filterRefs = computed(() => [filterRef.value]);

// 下拉里只列出当前目录实际存在的类型，避免出现永远筛不出结果的空选项。
const kindCounts = computed(() => {
  const counts = new Map<ProjectEntryKind, number>();
  for (const entry of props.entries) {
    const kind = projectEntryKind(entry);
    counts.set(kind, (counts.get(kind) || 0) + 1);
  }
  return counts;
});

const presentKinds = computed(() => (Object.keys(PROJECT_ENTRY_KIND_LABELS) as ProjectEntryKind[]).filter((kind) => kindCounts.value.has(kind)));

const typeFilterOptions = computed(() => [
  { value: "all" as TypeFilter, label: "全部类型", count: props.entries.length },
  ...presentKinds.value.map((kind) => ({
    value: kind as TypeFilter,
    label: PROJECT_ENTRY_KIND_LABELS[kind],
    count: kindCounts.value.get(kind) || 0,
  })),
]);

// 切换目录后原来的筛选项可能已不存在，自动回落到“全部类型”。
watch(presentKinds, (kinds) => {
  if (typeFilter.value !== "all" && !kinds.includes(typeFilter.value)) typeFilter.value = "all";
});

useDismissOnOutside(filterOpen, filterRefs, () => {
  filterOpen.value = false;
});

const sorted = computed(() => sortProjectEntries(props.entries, props.sortField, props.sortDir));
const visible = computed(() => {
  if (typeFilter.value === "all") return sorted.value;
  return sorted.value.filter((entry) => projectEntryKind(entry) === typeFilter.value);
});

const allSelected = computed(() => visible.value.length > 0 && visible.value.every((entry) => props.selected.has(entry.name)));
const someSelected = computed(() => !allSelected.value && visible.value.some((entry) => props.selected.has(entry.name)));

function sortMark(field: ProjectSortField): "asc" | "desc" | "" {
  if (props.sortField !== field || !props.sortDir) return "";
  return props.sortDir;
}

function toggleAll() {
  const next = new Set(props.selected);
  if (allSelected.value) {
    for (const entry of visible.value) next.delete(entry.name);
  } else {
    for (const entry of visible.value) next.add(entry.name);
  }
  emit("update:selected", next);
}

function toggleRow(name: string) {
  const next = new Set(props.selected);
  if (next.has(name)) next.delete(name);
  else next.add(name);
  emit("update:selected", next);
}

function openDir(entry: BueiEmbyLibraryProjectEntry) {
  if (entry.is_dir) emit("open-dir", entry.name);
}

function selectTypeFilter(value: TypeFilter) {
  typeFilter.value = value;
  filterOpen.value = false;
}

const rows = computed(() =>
  visible.value.map((entry) => {
    const [date, clock] = (entry.modified_at ? formatTime(entry.modified_at) : "-").split(" ");
    const kind = projectEntryKind(entry);
    return { entry, date, clock: clock || "", kindLabel: projectEntryKindLabel(kind), kindIcon: projectEntryKindIcon(kind) };
  }),
);
</script>

<template>
  <section class="project-pane">
    <header class="project-pane__head">
      <span class="project-pane__title"><span class="project-pane__title-icon"><SvgIcon :name="icon" :size="12" /></span>{{ title }}</span>
      <BueiTailText class="project-pane__path" :text="path || ''" placeholder="根目录" :title="path || '根目录'" />
      <button
        type="button"
        class="project-pane__delete"
        :disabled="!selected.size || deleting"
        :aria-busy="deleting"
        @click="emit('delete', [...selected])"
      >
        <BusySpinner v-if="deleting" variant="notch" :size="11" />
        <SvgIcon v-else name="trash" :size="11" />{{ deleting ? "删除中…" : "删除" }}
      </button>
    </header>

    <div class="project-pane__body">
      <div v-if="loading" class="project-pane__state"><BusySpinner variant="notch" :size="18" />正在读取…</div>
      <div v-else-if="error" class="project-pane__state project-pane__state--error">{{ error }}</div>
      <table v-else class="project-table">
        <colgroup>
          <col class="project-col--check" />
          <!-- 名称列不设宽度，自动吃掉剩余空间；表格 min-width 保证它至少有约 174px -->
          <col />
          <col class="project-col--type" />
          <col class="project-col--size" />
          <col class="project-col--time" />
        </colgroup>
        <thead>
          <tr>
            <th class="project-table__check">
              <input
                type="checkbox"
                :checked="allSelected"
                :indeterminate.prop="someSelected"
                :disabled="!visible.length"
                aria-label="全选"
                @change="toggleAll"
              />
            </th>
            <th
              v-for="column in columns"
              :key="column.field"
              :class="{ 'is-numeric': column.numeric, 'is-active': sortMark(column.field), 'has-filter': column.filter }"
            >
              <div class="project-table__head">
                <button type="button" class="project-table__sort" @click="emit('sort', column.field)">
                  <span>{{ column.label }}</span>
                  <span class="project-table__sort-mark" :class="`is-${sortMark(column.field) || 'none'}`">
                    <SvgIcon v-if="sortMark(column.field) === 'asc'" name="chevron-up" :size="9" />
                    <SvgIcon v-else-if="sortMark(column.field) === 'desc'" name="chevron-down" :size="9" />
                  </span>
                </button>
                <div v-if="column.filter" ref="filterRef" class="project-table__filter-wrap">
                  <button
                    type="button"
                    class="project-table__filter"
                    :class="{ 'is-active': typeFilter !== 'all' }"
                    :aria-expanded="filterOpen"
                    aria-haspopup="menu"
                    aria-label="筛选文件类型"
                    title="筛选文件类型"
                    @click.stop="filterOpen = !filterOpen"
                  >
                    <svg viewBox="0 0 16 16" aria-hidden="true">
                      <path d="M2 3.5h12l-4.6 5.2v4.1l-2.8 1.7V8.7z" />
                    </svg>
                  </button>
                  <div v-if="filterOpen" class="project-table__filter-menu" role="menu">
                    <button
                      v-for="option in typeFilterOptions"
                      :key="option.value"
                      type="button"
                      class="project-table__filter-item"
                      :class="{ 'is-active': typeFilter === option.value }"
                      role="menuitemradio"
                      :aria-checked="typeFilter === option.value"
                      @click.stop="selectTypeFilter(option.value)"
                    >
                      <span class="project-table__filter-check"><SvgIcon v-if="typeFilter === option.value" name="check" :size="9" /></span>
                      <span class="project-table__filter-label">{{ option.label }}</span>
                      <span class="project-table__filter-count">{{ option.count }}</span>
                    </button>
                  </div>
                </div>
              </div>
            </th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="{ entry, date, clock, kindLabel, kindIcon } in rows"
            :key="entry.name"
            :class="{ 'is-selected': selected.has(entry.name), 'is-dir': entry.is_dir }"
            @click="toggleRow(entry.name)"
            @dblclick="openDir(entry)"
          >
            <td class="project-table__check" @click.stop>
              <input
                type="checkbox"
                :checked="selected.has(entry.name)"
                :aria-label="`选择 ${entry.name}`"
                @change="toggleRow(entry.name)"
              />
            </td>
            <td class="project-table__name">
              <button
                v-if="entry.is_dir"
                type="button"
                class="project-table__name-inner is-link"
                :title="`进入「${entry.name}」`"
                @click.stop="openDir(entry)"
              >
                <SvgIcon :name="kindIcon" :size="12" :class="`is-kind-${projectEntryKind(entry)}`" />
                <span class="project-table__name-text">{{ entry.name }}</span>
              </button>
              <span v-else class="project-table__name-inner" :title="entry.name">
                <SvgIcon :name="kindIcon" :size="12" :class="`is-kind-${projectEntryKind(entry)}`" />
                <span class="project-table__name-text">{{ entry.name }}</span>
              </span>
            </td>
            <td class="project-table__type">{{ kindLabel }}</td>
            <td class="project-table__size">{{ entry.is_dir ? "-" : formatSize(entry.size) }}</td>
            <td class="project-table__time">
              <span class="project-table__date">{{ date }}</span>
              <span class="project-table__clock">{{ clock }}</span>
            </td>
          </tr>
          <tr v-if="!visible.length">
            <td colspan="5" class="project-table__empty">{{ entries.length ? "没有符合筛选条件的项目" : "当前目录为空" }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <footer class="project-pane__foot">
      <span>共 <b>{{ entries.length }}</b> 项<template v-if="typeFilter !== 'all'"> · 显示 <b>{{ visible.length }}</b> 项</template></span>
      <span v-if="selected.size">已选 <b>{{ selected.size }}</b> 项</span>
    </footer>
  </section>
</template>

<style scoped>
.project-pane {
  display: flex;
  flex-direction: column;
  min-width: 0;
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  background: var(--surface);
  overflow: hidden;
}

.project-pane__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  min-height: 40px;
  padding: 0 10px 0 12px;
  border-bottom: 1px solid var(--border);
  background: var(--surface-sunken);
}
.project-pane__title { display: inline-flex; align-items: center; gap: 7px; min-width: 0; flex-shrink: 0; font-size: 12.5px; font-weight: 600; color: var(--text); }
.project-pane__title-icon { display: inline-grid; place-items: center; width: 20px; height: 20px; border-radius: 6px; color: var(--brand); background: color-mix(in srgb, var(--brand) 12%, transparent); }
.project-pane__path {
  flex: 1 1 auto;
  min-width: 0;
  margin-left: 8px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-muted);
  font-size: 11px;
}
.project-pane__delete {
  margin-left: auto;
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 24px;
  padding: 0 9px;
  border: 1px solid color-mix(in srgb, var(--danger) 42%, var(--border));
  border-radius: 6px;
  color: var(--danger);
  background: var(--surface);
  font-size: 11px;
  font-weight: 500;
  cursor: pointer;
  transition: background .15s ease, border-color .15s ease, opacity .15s ease;
}
.project-pane__delete:hover:not(:disabled) { border-color: var(--danger); background: color-mix(in srgb, var(--danger) 10%, var(--surface)); }
.project-pane__delete:disabled { opacity: .45; cursor: not-allowed; }

.project-pane__body { flex: 1; min-height: 240px; max-height: min(52vh, 520px); overflow: auto; overscroll-behavior-x: contain; -webkit-overflow-scrolling: touch; }
.project-pane__state { display: grid; place-items: center; gap: 8px; min-height: 240px; padding: 18px; color: var(--text-muted); font-size: 12px; text-align: center; }
.project-pane__state--error { color: var(--danger); }

.project-pane__foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  min-height: 32px;
  padding: 0 12px;
  border-top: 1px solid var(--border);
  background: var(--surface-sunken);
  font-size: 11px;
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
}
.project-pane__foot b { color: var(--text); font-weight: 600; }

/* 列宽固定，屏幕过窄时整张表保持最小宽度并横向滚动，避免列被挤压到看不清。 */
.project-table { width: 100%; min-width: 460px; border-collapse: collapse; font-size: 12px; table-layout: fixed; }
.project-col--check { width: 34px; }
.project-col--type { width: 76px; }
.project-col--size { width: 78px; }
.project-col--time { width: 98px; }

.project-table thead th {
  position: sticky;
  top: 0;
  z-index: 1;
  padding: 0;
  border-bottom: 1px solid var(--border);
  background: var(--surface-sunken);
  text-align: left;
  font-weight: 500;
  white-space: nowrap;
}
.project-table__head { display: flex; align-items: center; min-width: 0; }
.project-table__sort {
  display: flex;
  align-items: center;
  gap: 4px;
  flex: 1;
  min-width: 0;
  height: 30px;
  padding: 0 4px 0 10px;
  border: none;
  background: transparent;
  color: var(--text-muted);
  font-size: 11px;
  cursor: pointer;
  transition: color .15s ease;
}
.project-table th.is-numeric .project-table__sort { justify-content: flex-end; padding: 0 2px 0 6px; }
.project-table__sort:hover { color: var(--text); }
.project-table th.is-active .project-table__sort { color: var(--brand); }
.project-table__sort-mark { display: inline-grid; place-items: center; width: 10px; color: var(--brand); }
.project-table__sort-mark.is-none { visibility: hidden; }
.project-table__filter-wrap { position: relative; display: flex; align-items: center; }
.project-table__filter {
  display: grid;
  place-items: center;
  width: 18px;
  height: 18px;
  margin-right: 5px;
  padding: 0;
  border: none;
  border-radius: 4px;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
}
.project-table__filter svg { width: 11px; height: 11px; fill: currentColor; }
.project-table__filter:hover { color: var(--text); background: color-mix(in srgb, var(--text) 8%, transparent); }
.project-table__filter.is-active { color: var(--brand); }
.project-table__filter-menu {
  position: absolute;
  top: calc(100% + 2px);
  right: -4px;
  z-index: 6;
  min-width: 108px;
  max-height: min(52vh, 260px);
  overflow: auto;
  padding: 4px;
  border: 1px solid var(--border);
  border-radius: var(--radius-md, 8px);
  background: var(--surface);
  box-shadow: 0 12px 28px rgba(2, 6, 23, .22);
}
.project-table__filter-item {
  display: flex;
  align-items: center;
  gap: 4px;
  width: 100%;
  height: 26px;
  padding: 0 8px 0 4px;
  border: none;
  border-radius: 5px;
  background: transparent;
  color: var(--text-regular);
  font-size: 12px;
  text-align: left;
  white-space: nowrap;
  cursor: pointer;
}
.project-table__filter-item:hover { background: var(--surface-hover); color: var(--text); }
.project-table__filter-item.is-active { color: var(--brand); }
.project-table__filter-check { display: inline-grid; place-items: center; width: 12px; color: var(--brand); }
.project-table__filter-label { flex: 1; }
.project-table__filter-count { color: var(--text-muted); font-size: 10.5px; font-variant-numeric: tabular-nums; }

.project-table tbody td {
  padding: 0 10px;
  height: 34px;
  border-bottom: 1px solid color-mix(in srgb, var(--border) 45%, transparent);
  color: var(--text-regular);
  vertical-align: middle;
}
.project-table tbody tr { cursor: pointer; user-select: none; }
.project-table tbody tr:hover { background: var(--surface-hover); }
.project-table tbody tr.is-selected { background: color-mix(in srgb, var(--brand) 9%, transparent); }
.project-table tbody tr.is-selected:hover { background: color-mix(in srgb, var(--brand) 13%, transparent); }
.project-table tbody tr:last-child td { border-bottom: none; }

.project-table__check { padding: 0 0 0 11px !important; }
.project-table__check input { width: 13px; height: 13px; accent-color: var(--brand); cursor: pointer; vertical-align: middle; }

.project-table__name-inner {
  display: flex;
  align-items: center;
  gap: 7px;
  min-width: 0;
  width: 100%;
  margin: 0;
  padding: 7px 0;
  border: none;
  background: transparent;
  font-size: 12px;
  text-align: left;
}
.project-table__name-inner.is-link { cursor: pointer; }
.project-table__name-inner :deep(.lp-svg-icon) { flex-shrink: 0; align-self: center; }
.project-table__name-inner :deep(.is-kind-dir) { color: color-mix(in srgb, var(--brand) 78%, var(--text-muted)); }
.project-table__name-inner :deep(.is-kind-strm) { color: color-mix(in srgb, var(--brand) 62%, var(--text-muted)); }
.project-table__name-inner :deep(.is-kind-video) { color: color-mix(in srgb, var(--success) 62%, var(--text-muted)); }
.project-table__name-inner :deep(.is-kind-audio) { color: color-mix(in srgb, var(--warning) 62%, var(--text-muted)); }
.project-table__name-inner :deep(.is-kind-subtitle) { color: color-mix(in srgb, var(--text-muted) 92%, var(--text)); }
.project-table__name-inner :deep(.is-kind-image) { color: color-mix(in srgb, var(--brand) 45%, var(--text-muted)); }
.project-table__name-inner :deep(.is-kind-nfo),
.project-table__name-inner :deep(.is-kind-json),
.project-table__name-inner :deep(.is-kind-other) { color: var(--text-muted); }
.project-table__name-text { min-width: 0; color: var(--text); line-height: 1.45; white-space: normal; overflow-wrap: anywhere; }
.project-table__name-inner.is-link:hover .project-table__name-text { color: var(--brand); }

.project-table__type { color: var(--text-muted); white-space: nowrap; }
.project-table__size { color: var(--text-muted); text-align: right; white-space: nowrap; font-variant-numeric: tabular-nums; }
.project-table__time { color: var(--text-muted); line-height: 1.3; white-space: nowrap; }
.project-table__date,
.project-table__clock { display: block; }
.project-table__clock { font-size: 10.5px; opacity: .78; }
.project-table__empty { padding: 40px 12px !important; text-align: center; color: var(--text-muted); }
</style>
