<script setup lang="ts">
// buei 定制：Emby 媒体库分类图标，供「Emby 媒体库」页面按媒体库类型渲染。
import { computed } from "vue";
import { bueiEmbyLibraryIcon, type BueiEmbyLibraryKind } from "./bueiEmbyLibraryIcons";

const props = withDefaults(
  defineProps<{
    kind?: BueiEmbyLibraryKind;
    size?: number | string;
  }>(),
  { kind: "mixed", size: 16 },
);

// size 支持像素数值或 1em 等 CSS 长度。
const rootStyle = computed(() => {
  const raw = props.size;
  if (typeof raw === "string" && /[a-z%]/i.test(raw.trim())) {
    const len = raw.trim();
    return { width: len, height: len };
  }
  const n = Number(raw);
  const px = Number.isFinite(n) && n > 0 ? n : 16;
  return { width: `${px}px`, height: `${px}px` };
});

const markup = computed(() => bueiEmbyLibraryIcon(props.kind));
</script>

<template>
  <span class="buei-emby-library-icon" :class="`is-${kind}`" :style="rootStyle" aria-hidden="true">
    <span class="buei-emby-library-icon__svg" v-html="markup" />
  </span>
</template>

<style scoped>
.buei-emby-library-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  line-height: 0;
}
.buei-emby-library-icon__svg {
  display: block;
  width: 100%;
  height: 100%;
}
.buei-emby-library-icon__svg :deep(svg) {
  display: block;
  width: 100%;
  height: 100%;
  overflow: visible;
}
</style>
