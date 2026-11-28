<script setup lang="ts">
import { computed, ref } from "vue";
import { getApiErrorMessage } from "@/api/client";
import { saveBueiDoubanOverride, type BueiDoubanWallItem } from "@/api/bueiDoubanScore";
import AppButton from "@/components/base/AppButton.vue";
import AppModal from "@/components/base/AppModal.vue";

// buei:douban-override —— 海报墙卡片「齿轮」弹窗：
// 手动填写豆瓣 ID（之后查评分直接查该条目）、手动豆瓣评分（0–10，最多一位小数）、不再自动查询。

const props = defineProps<{
  item: BueiDoubanWallItem;
}>();

const emit = defineEmits<{
  (e: "close"): void;
  (e: "saved"): void;
}>();

// 豆瓣 ID 与名称匹配得到的是同一个字段：有就回填，方便确认或直接清空改为按名搜索。
const doubanID = ref(props.item.douban?.douban_id ?? "");
const ratingText = ref(
  props.item.douban?.manual_rating != null ? props.item.douban.manual_rating.toFixed(1) : "",
);
const noAutoQuery = ref(Boolean(props.item.douban?.no_auto_query));
const saving = ref(false);
const error = ref("");

const ratingError = computed(() => {
  const raw = ratingText.value.trim();
  if (!raw) return "";
  if (!/^\d{1,2}(\.\d{0,1})?$/.test(raw)) return "请输入 0 - 10 的数字，最多一位小数";
  const value = Number(raw);
  if (!Number.isFinite(value) || value < 0 || value > 10) return "评分需要在 0 到 10 之间";
  return "";
});

const idError = computed(() => {
  const raw = doubanID.value.trim();
  if (!raw) return "";
  return /^\d+$/.test(raw) ? "" : "豆瓣 ID 只能填写条目数字 ID";
});

const canSave = computed(() => !saving.value && !ratingError.value && !idError.value);

async function save() {
  if (!canSave.value) return;
  saving.value = true;
  error.value = "";
  try {
    const raw = ratingText.value.trim();
    await saveBueiDoubanOverride({
      strm_task_id: props.item.strm_task_id,
      item_id: props.item.id,
      douban_id: doubanID.value.trim(),
      rating: raw ? Number(raw) : null,
      no_auto_query: noAutoQuery.value,
    });
    emit("saved");
    emit("close");
  } catch (e) {
    error.value = getApiErrorMessage(e, "保存失败");
  } finally {
    saving.value = false;
  }
}

function onRatingInput(raw: string) {
  // 只允许数字和一个小数点，避免粘进来奇怪字符。
  const cleaned = raw.replace(/[^\d.]/g, "").replace(/(\..*)\./g, "$1");
  ratingText.value = cleaned;
  if (error.value) error.value = "";
}

function onDoubanIDInput(raw: string) {
  doubanID.value = raw.replace(/[^\d]/g, "");
  if (error.value) error.value = "";
}
</script>

<template>
  <AppModal :open="true" size="sm" :title="`编辑 · ${item.title}`" @close="emit('close')">
    <div class="override">
      <label class="override__field">
        <span class="override__label">豆瓣 ID</span>
        <input
          class="override__input"
          type="text"
          inputmode="numeric"
          placeholder="留空则按片名搜索，如 1292052"
          :value="doubanID"
          @input="onDoubanIDInput(($event.target as HTMLInputElement).value)"
        />
        <span class="override__hint" :class="{ 'override__hint--error': idError }">
          {{ idError || "名称匹配到的或手动填写的豆瓣条目 ID；非空时直接按此 ID 查评分，清空则回到按片名搜索。" }}
        </span>
      </label>

      <label class="override__field">
        <span class="override__label">豆瓣评分</span>
        <input
          class="override__input"
          type="text"
          inputmode="decimal"
          placeholder="0 - 10，可留空"
          :value="ratingText"
          @input="onRatingInput(($event.target as HTMLInputElement).value)"
        />
        <span class="override__hint" :class="{ 'override__hint--error': ratingError }">
          {{ ratingError || "填写后以手动评分为准，并写进作品 NFO。" }}
        </span>
      </label>

      <div class="override__switch-row">
        <div class="override__switch-copy">
          <span class="override__label">不再查询</span>
          <span class="override__hint">开启后每日自动查询会跳过此项目，手动「查询 / 重查」仍然有效。</span>
        </div>
        <button
          type="button"
          class="override-switch"
          :class="{ 'is-on': noAutoQuery }"
          role="switch"
          :aria-checked="noAutoQuery"
          :aria-label="noAutoQuery ? '关闭不再查询' : '开启不再查询'"
          @click="noAutoQuery = !noAutoQuery"
        >
          <span class="override-switch__knob" />
        </button>
      </div>

      <p v-if="error" class="override__error">{{ error }}</p>
    </div>

    <template #footer>
      <AppButton type="button" variant="ghost" :disabled="saving" @click="emit('close')">取消</AppButton>
      <AppButton type="button" variant="primary" :disabled="!canSave" @click="save">
        {{ saving ? "保存中…" : "保存" }}
      </AppButton>
    </template>
  </AppModal>
</template>

<style scoped>
.override {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.override__field {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.override__label {
  color: var(--text);
  font-size: 13px;
  font-weight: 600;
}

.override__input {
  width: 100%;
  height: 34px;
  padding: 0 10px;
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: var(--surface);
  color: var(--text);
  font-size: 13px;
  font-variant-numeric: tabular-nums;
}

.override__input:focus {
  outline: none;
  border-color: var(--brand);
}

.override__hint {
  color: var(--text-muted);
  font-size: 11px;
  line-height: 1.5;
}

.override__hint--error {
  color: var(--danger);
}

.override__switch-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.override__switch-copy {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}

/* 开关沿用「自动归档」配置弹窗的样式 */
.override-switch {
  position: relative;
  flex: none;
  width: 40px;
  height: 22px;
  margin-top: 2px;
  padding: 0;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--surface-sunken);
  cursor: pointer;
  transition: background 0.18s ease, border-color 0.18s ease;
}

.override-switch.is-on {
  border-color: var(--brand);
  background: var(--brand);
}

.override-switch__knob {
  position: absolute;
  top: 2px;
  left: 2px;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: var(--surface);
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.28);
  transition: transform 0.18s ease;
}

.override-switch.is-on .override-switch__knob {
  transform: translateX(18px);
}

.override-switch:focus-visible {
  outline: 2px solid var(--brand);
  outline-offset: 2px;
}

.override__error {
  margin: 0;
  color: var(--danger);
  font-size: 12px;
}
</style>
