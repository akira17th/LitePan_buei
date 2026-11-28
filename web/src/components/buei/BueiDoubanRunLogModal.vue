<script setup lang="ts">
import { computed, ref, watch } from "vue";
import type { BueiDoubanRunLog, BueiDoubanRunLogTask } from "@/api/bueiDoubanScore";
import AppButton from "@/components/base/AppButton.vue";
import AppModal from "@/components/base/AppModal.vue";
import SvgIcon from "@/components/icons/SvgIcon.vue";

// buei:run-log —— 每日自动查询日志弹窗：
// 左侧是有实际查询内容的 STRM 任务（一个项目都没查的任务不出现），
// 右侧是该任务本轮查过的媒体项目：名称 + 成功/失败，失败附具体原因。

const props = defineProps<{
  runLog: BueiDoubanRunLog | null;
}>();

const emit = defineEmits<{
  (e: "close"): void;
}>();

const activeTaskId = ref<number | null>(null);

const tasks = computed<BueiDoubanRunLogTask[]>(() =>
  (props.runLog?.tasks ?? []).filter((task) => (task.items?.length ?? 0) > 0 || task.pending > 0),
);

watch(
  tasks,
  (list) => {
    if (!list.length) {
      activeTaskId.value = null;
      return;
    }
    if (activeTaskId.value != null && list.some((t) => t.task_id === activeTaskId.value)) return;
    activeTaskId.value = list[0].task_id;
  },
  { immediate: true },
);

const activeTask = computed(() => tasks.value.find((t) => t.task_id === activeTaskId.value) ?? null);
const activeItems = computed(() => activeTask.value?.items ?? []);

function formatDuration(ms: number) {
  if (!ms || ms < 0) return "—";
  const totalSec = Math.round(ms / 1000);
  if (totalSec < 60) return `${totalSec} 秒`;
  const m = Math.floor(totalSec / 60);
  const s = totalSec % 60;
  return s ? `${m} 分 ${s} 秒` : `${m} 分钟`;
}

function formatDateTime(raw?: string) {
  if (!raw) return "—";
  const d = new Date(raw);
  if (Number.isNaN(d.getTime())) return "—";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}
</script>

<template>
  <AppModal :open="true" size="lg" title="自动查询日志" @close="emit('close')">
    <div v-if="runLog" class="rlog">
      <div class="rlog__meta">
        <span>{{ formatDateTime(runLog.finished_at) }} 结束</span>
        <span class="rlog__sep">·</span>
        <span>耗时 {{ formatDuration(runLog.duration_ms) }}</span>
        <span class="rlog__sep">·</span>
        <span class="rlog__pill">待查 {{ runLog.total }}</span>
        <span class="rlog__pill rlog__pill--ok">成功 {{ runLog.success }}</span>
        <span class="rlog__pill" :class="{ 'rlog__pill--fail': runLog.failed > 0 }">失败 {{ runLog.failed }}</span>
      </div>

      <div v-if="!tasks.length" class="rlog__empty">这次没有需要查询的作品。</div>
      <div v-else class="rlog__body">
        <aside class="rlog__rail" aria-label="执行过的 STRM 任务">
          <button
            v-for="task in tasks"
            :key="task.task_id"
            type="button"
            class="rlog__task"
            :class="{ 'rlog__task--active': task.task_id === activeTaskId }"
            @click="activeTaskId = task.task_id"
          >
            <span class="rlog__task-name" :title="task.task_name">{{ task.task_name }}</span>
            <span class="rlog__task-counts">
              <span class="rlog__count rlog__count--ok">{{ task.success }}</span>
              <span v-if="task.failed" class="rlog__count rlog__count--fail">{{ task.failed }}</span>
            </span>
          </button>
        </aside>

        <section class="rlog__detail">
          <header v-if="activeTask" class="rlog__detail-head">
            <span class="rlog__detail-name" :title="activeTask.task_name">{{ activeTask.task_name }}</span>
            <span class="rlog__detail-stats">
              待查 {{ activeTask.pending }} · 成功 {{ activeTask.success }}<template v-if="activeTask.failed"> · 失败 {{ activeTask.failed }}</template>
            </span>
          </header>
          <div v-if="!activeItems.length" class="rlog__empty">这个任务没有项目明细。</div>
          <ul v-else class="rlog__items">
            <li
              v-for="(item, index) in activeItems"
              :key="`${item.title}:${index}`"
              class="rlog__item"
              :class="`rlog__item--${item.state}`"
            >
              <SvgIcon
                :name="item.state === 'failed' ? 'circle-exclamation' : 'circle-check'"
                :size="13"
                class-name="rlog__item-icon"
              />
              <span class="rlog__item-title" :title="item.title">{{ item.title }}</span>
              <span class="rlog__item-detail" :title="item.detail || ''">{{ item.detail || "—" }}</span>
            </li>
          </ul>
        </section>
      </div>
    </div>
    <div v-else class="rlog rlog--empty">还没有自动查询记录。</div>
    <template #footer>
      <AppButton type="button" variant="ghost" @click="emit('close')">关闭</AppButton>
    </template>
  </AppModal>
</template>

<style scoped>
.rlog {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: 180px;
}

.rlog--empty {
  align-items: center;
  justify-content: center;
  color: var(--text-muted);
  font-size: 12px;
}

.rlog__meta {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
  color: var(--text-muted);
  font-size: 12px;
}

.rlog__sep {
  opacity: 0.5;
}

.rlog__pill {
  padding: 2px 10px;
  border-radius: var(--radius-pill);
  background: var(--surface-sunken);
  color: var(--text-muted);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}

.rlog__pill--ok {
  color: var(--success);
  background: color-mix(in srgb, var(--success) 10%, transparent);
}

.rlog__pill--fail {
  color: var(--danger);
  background: color-mix(in srgb, var(--danger) 10%, transparent);
}

.rlog__empty {
  padding: 18px 0;
  text-align: center;
  color: var(--text-muted);
  font-size: 12px;
}

.rlog__body {
  display: grid;
  grid-template-columns: 190px minmax(0, 1fr);
  gap: 12px;
  min-height: 0;
  max-height: 56vh;
}

.rlog__rail {
  display: flex;
  flex-direction: column;
  gap: 2px;
  overflow-y: auto;
  padding: 8px 6px;
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: var(--surface);
}

.rlog__task {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 7px 9px;
  border: 0;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-regular);
  font-size: 13px;
  text-align: left;
  cursor: pointer;
  transition: background var(--transition), color var(--transition);
}

.rlog__task:hover {
  background: var(--surface-hover);
}

.rlog__task--active {
  background: var(--tab-active-bg);
  color: var(--accent-text);
  font-weight: 600;
}

.rlog__task-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.rlog__task-counts {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
}

.rlog__count {
  min-width: 18px;
  padding: 0 5px;
  border-radius: var(--radius-pill);
  font-size: 11px;
  font-weight: 600;
  line-height: 17px;
  text-align: center;
  font-variant-numeric: tabular-nums;
}

.rlog__count--ok {
  background: color-mix(in srgb, var(--success) 12%, transparent);
  color: var(--success);
}

.rlog__count--fail {
  background: color-mix(in srgb, var(--danger) 12%, transparent);
  color: var(--danger);
}

.rlog__detail {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
  min-height: 0;
  overflow-y: auto;
}

.rlog__detail-head {
  display: flex;
  align-items: center;
  gap: 10px;
  padding-bottom: 6px;
  border-bottom: 1px solid var(--border);
}

.rlog__detail-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text);
  font-size: 13px;
  font-weight: 600;
}

.rlog__detail-stats {
  flex-shrink: 0;
  color: var(--text-muted);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}

.rlog__items {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.rlog__item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  border-radius: var(--radius-sm);
  background: var(--surface);
  font-size: 12px;
}

.rlog__item-icon {
  flex: none;
  color: var(--success);
}

.rlog__item--failed .rlog__item-icon {
  color: var(--danger);
}

.rlog__item-title {
  flex: 1 1 40%;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text);
  font-weight: 600;
}

.rlog__item-detail {
  flex: 1 1 60%;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-muted);
  text-align: right;
}

.rlog__item--failed .rlog__item-detail {
  color: var(--danger);
}

@media (max-width: 720px) {
  .rlog__body {
    grid-template-columns: 1fr;
  }

  .rlog__rail {
    flex-direction: row;
    overflow-x: auto;
    overflow-y: hidden;
  }

  .rlog__task {
    width: auto;
    white-space: nowrap;
  }

  .rlog__task-name {
    max-width: 120px;
  }
}
</style>
