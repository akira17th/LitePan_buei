<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { getApiErrorMessage } from "@/api/client";
import {
  fetchBueiEmbyArchiveRule,
  saveBueiEmbyArchiveRule,
  type BueiEmbyArchiveRule,
  type BueiEmbyArchiveSourceOption,
} from "@/api/bueiEmbyArchive";
import { bueiEmbyLibraryKindLabel } from "@/components/buei/bueiEmbyLibraryIcons";
import AppButton from "@/components/base/AppButton.vue";
import AppModal from "@/components/base/AppModal.vue";
import BusySpinner from "@/components/base/BusySpinner.vue";
import SvgIcon from "@/components/icons/SvgIcon.vue";
import { formatTime } from "@/utils/format";

// buei 定制：归档媒体库的自动归档配置弹窗。

const props = withDefaults(
  defineProps<{
    open: boolean;
    libraryId: string;
    libraryName: string;
    configId?: string;
  }>(),
  { configId: "" },
);

const emit = defineEmits<{ close: []; saved: [rule: BueiEmbyArchiveRule] }>();

const intervals = [
  { hours: 1, label: "每小时" },
  { hours: 6, label: "每 6 小时" },
  { hours: 24, label: "每天" },
];

const loading = ref(false);
const saving = ref(false);
const errorMessage = ref("");
const sourceLibraries = ref<BueiEmbyArchiveSourceOption[]>([]);
const selectedIDs = ref<Set<string>>(new Set());
const intervalHours = ref(1);
const enabled = ref(true);
const lastRunAt = ref("");
const nextRunAt = ref("");
const loadedLibraryId = ref("");

const availableSources = computed(() => sourceLibraries.value.filter((item) => !item.claimed_by_library_id));
const selectedCount = computed(() => selectedIDs.value.size);
const allAvailableSelected = computed(
  () => availableSources.value.length > 0 && availableSources.value.every((item) => selectedIDs.value.has(item.id)),
);

function typeLabel(option: BueiEmbyArchiveSourceOption) {
  return bueiEmbyLibraryKindLabel(option.collection_type, option.name);
}

function reset() {
  errorMessage.value = "";
  sourceLibraries.value = [];
  selectedIDs.value = new Set();
  intervalHours.value = 1;
  enabled.value = true;
  lastRunAt.value = "";
  nextRunAt.value = "";
  loadedLibraryId.value = "";
}

async function load() {
  if (!props.open || !props.libraryId) return;
  if (loadedLibraryId.value === props.libraryId) return;
  loading.value = true;
  errorMessage.value = "";
  try {
    const view = await fetchBueiEmbyArchiveRule(props.libraryId, props.configId);
    sourceLibraries.value = view.source_libraries || [];
    selectedIDs.value = new Set((view.rule?.source_library_ids || []).filter((id) => sourceLibraries.value.some((item) => item.id === id)));
    intervalHours.value = intervals.some((item) => item.hours === view.rule?.interval_hours) ? (view.rule?.interval_hours ?? 1) : 1;
    enabled.value = view.rule?.enabled !== false;
    lastRunAt.value = view.rule?.last_run_at || "";
    nextRunAt.value = view.next_run_at || view.rule?.next_run_at || "";
    loadedLibraryId.value = props.libraryId;
  } catch (error) {
    errorMessage.value = getApiErrorMessage(error, "读取自动归档配置失败");
  } finally {
    loading.value = false;
  }
}

function toggleSource(option: BueiEmbyArchiveSourceOption) {
  if (option.claimed_by_library_id) return;
  const next = new Set(selectedIDs.value);
  if (next.has(option.id)) next.delete(option.id);
  else next.add(option.id);
  selectedIDs.value = next;
}

function toggleAllAvailable() {
  if (allAvailableSelected.value) {
    selectedIDs.value = new Set();
    return;
  }
  selectedIDs.value = new Set(availableSources.value.map((item) => item.id));
}

async function submit() {
  if (saving.value) return;
  if (!selectedCount.value) {
    errorMessage.value = "请至少选择一个来源媒体库";
    return;
  }
  saving.value = true;
  errorMessage.value = "";
  try {
    const rule = await saveBueiEmbyArchiveRule({
      config_id: props.configId || undefined,
      archive_library_id: props.libraryId,
      source_library_ids: [...selectedIDs.value],
      interval_hours: intervalHours.value,
      enabled: enabled.value,
    });
    nextRunAt.value = rule.next_run_at || "";
    lastRunAt.value = rule.last_run_at || "";
    emit("saved", rule);
  } catch (error) {
    errorMessage.value = getApiErrorMessage(error, "保存自动归档配置失败");
  } finally {
    saving.value = false;
  }
}

watch(
  () => [props.open, props.libraryId, props.configId],
  ([open]) => {
    if (!open) {
      reset();
      return;
    }
    void load();
  },
);
</script>

<template>
  <AppModal :open="open" title="自动归档配置" size="md" @close="emit('close')">
    <div class="archive-rule">
      <p class="archive-rule__lead">
        定时检索来源媒体库，把符合规则的项目归档至
        <strong>{{ libraryName || "当前归档媒体库" }}</strong>。
      </p>

      <div v-if="loading" class="archive-rule__state">
        <BusySpinner variant="notch" :size="20" />正在读取配置…
      </div>

      <template v-else>
        <section class="archive-rule__section">
          <header class="archive-rule__head">
            <div>
              <h4>来源媒体库</h4>
              <p>可多选；已被其他归档媒体库使用的来源不能重复选择。</p>
            </div>
            <button v-if="availableSources.length" type="button" class="archive-rule__link" @click="toggleAllAvailable">
              {{ allAvailableSelected ? "清空" : "全选可用" }}
            </button>
          </header>
          <div v-if="sourceLibraries.length" class="archive-source-list" role="group" aria-label="来源媒体库">
            <label
              v-for="option in sourceLibraries"
              :key="option.id"
              class="archive-source"
              :class="{ 'is-claimed': option.claimed_by_library_id, 'is-selected': selectedIDs.has(option.id) }"
            >
              <input
                type="checkbox"
                :checked="selectedIDs.has(option.id)"
                :disabled="Boolean(option.claimed_by_library_id)"
                @change="toggleSource(option)"
              />
              <span class="archive-source__copy">
                <strong>{{ option.name }}</strong>
                <small>
                  {{ typeLabel(option) }}
                  <template v-if="option.claimed_by_library_name"> · 已被「{{ option.claimed_by_library_name }}」使用</template>
                </small>
              </span>
            </label>
          </div>
          <p v-else class="archive-rule__empty">没有可选的来源媒体库，请先在 Emby 中创建媒体库。</p>
        </section>

        <section class="archive-rule__section">
          <header class="archive-rule__head">
            <div>
              <h4>规则</h4>
              <p>规则固定，同时满足以下条件的项目才会归档。</p>
            </div>
          </header>
          <ul class="archive-rules">
            <li>
              <SvgIcon name="check-circle" :size="14" />
              <span>剧集已完结<small>电影不需要这一条</small></span>
            </li>
            <li>
              <SvgIcon name="check-circle" :size="14" />
              <span>任一用户已看完<small>按 Emby 观看状态判断</small></span>
            </li>
          </ul>
        </section>

        <section class="archive-rule__section">
          <header class="archive-rule__head">
            <div>
              <h4>启用自动归档</h4>
              <p>关闭后不会按频率自动执行，仍可随时“立即执行”。</p>
            </div>
            <button
              type="button"
              class="archive-switch"
              :class="{ 'is-on': enabled }"
              role="switch"
              :aria-checked="enabled"
              :aria-label="enabled ? '关闭自动执行' : '开启自动执行'"
              @click="enabled = !enabled"
            >
              <span class="archive-switch__knob" />
            </button>
          </header>
        </section>

        <section class="archive-rule__section">
          <header class="archive-rule__head">
            <div>
              <h4>执行频率</h4>
              <p>保存后按所选频率自动执行，也可以随时“立即执行”。</p>
            </div>
          </header>
          <div class="archive-interval" role="radiogroup" aria-label="执行频率">
            <button
              v-for="item in intervals"
              :key="item.hours"
              type="button"
              class="archive-interval__btn"
              :class="{ 'is-active': intervalHours === item.hours }"
              role="radio"
              :aria-checked="intervalHours === item.hours"
              @click="intervalHours = item.hours"
            >
              {{ item.label }}
            </button>
          </div>
          <p v-if="lastRunAt || nextRunAt" class="archive-rule__schedule">
            <span v-if="lastRunAt">上次执行：{{ formatTime(lastRunAt) }}</span>
            <span v-if="nextRunAt">下次执行：{{ formatTime(nextRunAt) }}</span>
          </p>
        </section>

        <p v-if="errorMessage" class="archive-rule__error" role="alert">
          <SvgIcon name="circle-exclamation" :size="14" />{{ errorMessage }}
        </p>
      </template>
    </div>

    <template #footer>
      <div class="archive-rule__footer">
        <span class="archive-rule__count">已选 {{ selectedCount }} 个来源</span>
        <div>
          <AppButton variant="secondary" :disabled="saving" @click="emit('close')">取消</AppButton>
          <AppButton :disabled="saving || loading" @click="submit">
            <BusySpinner v-if="saving" variant="notch" :size="13" />
            <SvgIcon v-else name="save" :size="13" />
            保存配置
          </AppButton>
        </div>
      </div>
    </template>
  </AppModal>
</template>

<style scoped>
.archive-rule { display: grid; gap: 18px; }
.archive-rule__lead { margin: 0; color: var(--text-muted); font-size: 12px; line-height: 1.6; }
.archive-rule__lead strong { color: var(--text); }
.archive-rule__state { display: flex; align-items: center; justify-content: center; gap: 8px; min-height: 120px; color: var(--text-muted); font-size: 12px; }
.archive-rule__section { display: grid; gap: 10px; padding: 14px; border: 1px solid var(--border); border-radius: var(--radius-card); background: var(--surface-sunken); }
.archive-switch { position: relative; flex: none; width: 40px; height: 22px; margin-top: 2px; padding: 0; border: 1px solid var(--border); border-radius: 999px; background: var(--surface-sunken); cursor: pointer; transition: background .18s ease, border-color .18s ease; }
.archive-switch.is-on { border-color: var(--brand); background: var(--brand); }
.archive-switch__knob { position: absolute; top: 2px; left: 2px; width: 16px; height: 16px; border-radius: 50%; background: var(--surface); box-shadow: 0 1px 2px rgba(15,23,42,.28); transition: transform .18s ease; }
.archive-switch.is-on .archive-switch__knob { transform: translateX(18px); }
.archive-switch:focus-visible { outline: 2px solid var(--brand); outline-offset: 2px; }
.archive-rule__head { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
.archive-rule__head h4 { margin: 0; font-size: 13px; }
.archive-rule__head p { margin: 4px 0 0; color: var(--text-muted); font-size: 11px; line-height: 1.5; }
.archive-rule__link { border: 0; padding: 0; color: var(--brand); background: transparent; font-size: 11px; font-weight: 600; cursor: pointer; white-space: nowrap; }
.archive-rule__link:hover { text-decoration: underline; }
.archive-source-list { display: grid; grid-template-columns: repeat(auto-fill, minmax(210px, 1fr)); gap: 7px; }
.archive-source { display: flex; align-items: center; gap: 9px; min-width: 0; padding: 8px 10px; border: 1px solid var(--border); border-radius: var(--radius-control); background: var(--surface); cursor: pointer; transition: border-color .15s ease, background .15s ease; }
.archive-source:hover { border-color: color-mix(in srgb, var(--brand) 45%, var(--border)); }
.archive-source.is-selected { border-color: color-mix(in srgb, var(--brand) 55%, var(--border)); background: color-mix(in srgb, var(--brand) 7%, var(--surface)); }
.archive-source.is-claimed { cursor: not-allowed; opacity: .62; }
.archive-source.is-claimed:hover { border-color: var(--border); }
.archive-source input { flex-shrink: 0; width: 14px; height: 14px; accent-color: var(--brand); }
.archive-source__copy { display: grid; gap: 2px; min-width: 0; }
.archive-source__copy strong { overflow: hidden; font-size: 12px; font-weight: 600; text-overflow: ellipsis; white-space: nowrap; }
.archive-source__copy small { overflow: hidden; color: var(--text-muted); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.archive-rules { display: grid; gap: 7px; margin: 0; padding: 0; list-style: none; }
.archive-rules li { display: flex; align-items: flex-start; gap: 7px; color: var(--text); font-size: 12px; }
.archive-rules li :deep(.lp-svg-icon) { margin-top: 1px; color: var(--success); }
.archive-rules small { display: block; margin-top: 2px; color: var(--text-muted); font-size: 10px; }
.archive-interval { display: inline-flex; padding: 3px; border: 1px solid var(--border); border-radius: var(--radius-control); background: var(--surface); }
.archive-interval__btn { min-height: 28px; padding: 0 12px; border: 0; border-radius: 7px; color: var(--text-muted); background: transparent; font-size: 11px; font-weight: 600; cursor: pointer; transition: background .15s ease, color .15s ease; }
.archive-interval__btn:hover { color: var(--text); }
.archive-interval__btn.is-active { color: var(--text-on-brand); background: var(--brand); box-shadow: var(--shadow-brand); }
.archive-rule__schedule { display: flex; flex-wrap: wrap; gap: 4px 14px; margin: 0; color: var(--text-muted); font-size: 10px; }
.archive-rule__error { display: flex; align-items: center; gap: 6px; margin: 0; padding: 8px 10px; border: 1px solid color-mix(in srgb, var(--danger) 25%, var(--border)); border-radius: var(--radius-control); color: var(--danger); background: color-mix(in srgb, var(--danger) 7%, var(--surface)); font-size: 11px; }
.archive-rule__footer { display: flex; align-items: center; justify-content: space-between; gap: 12px; width: 100%; }
.archive-rule__count { color: var(--text-muted); font-size: 11px; }
.archive-rule__footer > div { display: flex; gap: 8px; }
.archive-rule__empty { margin: 0; color: var(--text-muted); font-size: 11px; }
</style>
