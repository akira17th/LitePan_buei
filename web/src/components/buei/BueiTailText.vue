<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";

const props = defineProps<{ text: string; placeholder?: string }>();
defineOptions({ inheritAttrs: false });

const root = ref<HTMLElement | null>(null);
const probe = ref<HTMLElement | null>(null);
const display = ref("");

// 从头部省略：路径太长时保留尾部（当前目录），开头用省略号代替。
function fit() {
  const container = root.value;
  const measure = probe.value;
  const text = props.text || props.placeholder || "";
  const available = container?.clientWidth ?? 0;
  if (!container || !measure) {
    display.value = text;
    return;
  }
  measure.textContent = text;
  if (!text || available <= 0 || measure.scrollWidth <= available) {
    display.value = text;
    return;
  }
  const chars = Array.from(text);
  let low = 1;
  let high = chars.length;
  let best = "";
  while (low <= high) {
    const mid = Math.floor((low + high) / 2);
    const candidate = `…${chars.slice(chars.length - mid).join("")}`;
    measure.textContent = candidate;
    if (measure.scrollWidth <= available) {
      best = candidate;
      low = mid + 1;
    } else {
      high = mid - 1;
    }
  }
  display.value = best || "…";
}

let observer: ResizeObserver | null = null;
onMounted(() => {
  void nextTick(fit);
  if (typeof ResizeObserver === "undefined" || !root.value) return;
  observer = new ResizeObserver(() => fit());
  observer.observe(root.value);
});
onBeforeUnmount(() => {
  observer?.disconnect();
  observer = null;
});
watch(() => [props.text, props.placeholder], () => void nextTick(fit));
</script>

<template>
  <span ref="root" class="buei-tail-text" v-bind="$attrs">
    <span ref="probe" class="buei-tail-text__probe" aria-hidden="true" />
    {{ display }}
  </span>
</template>

<style scoped>
.buei-tail-text {
  position: relative;
  display: block;
  overflow: hidden;
  white-space: nowrap;
}
.buei-tail-text__probe {
  position: absolute;
  top: 0;
  left: 0;
  visibility: hidden;
  white-space: nowrap;
  pointer-events: none;
}
</style>
