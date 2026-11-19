<script setup lang="ts">
import { computed, defineAsyncComponent, nextTick, onDeactivated, reactive, ref, watch, watchEffect } from "vue";
import { useRoute } from "vue-router";
import AppButton from "@/components/base/AppButton.vue";
import SectionTabBar from "@/components/admin/SectionTabBar.vue";
import AdminSettingsDrawer from "@/components/admin/AdminSettingsDrawer.vue";
import type StrmScrapePanelComponent from "@/components/admin/StrmScrapePanel.vue";
import { useSectionTabRoute } from "@/composables/useSectionTabRoute";
import { useSettingsPageDirty } from "@/composables/useSettingsPageDirty";
import { readPanelSaving, type SettingsPanelExpose } from "@/composables/useSettingsForm";

const StrmScrapePanel = defineAsyncComponent(() => import("@/components/admin/StrmScrapePanel.vue"));
const StrmScrapeSettings = defineAsyncComponent(() => import("@/components/admin/StrmScrapeSettings.vue"));
const CloudToolsPanel = defineAsyncComponent(() => import("@/components/admin/CloudToolsPanel.vue"));
// #region buei:guangya-share
const BueiGuangyaShareModal = defineAsyncComponent(() => import("@/components/buei/BueiGuangyaShareModal.vue"));
type GuangyaSharePanelApi = {
  selectedCount?: number;
  canTransfer?: boolean;
  transferring?: boolean;
  transferPhaseLabel?: string;
  transferHint?: string;
  submitTransfer?: () => void;
};
const guangyaShareRef = ref<GuangyaSharePanelApi | null>(null);
// #endregion buei:guangya-share
// #region buei:quark-ingest
const BueiQuarkIngestPanel = defineAsyncComponent(() => import("@/components/buei/BueiQuarkIngestPanel.vue"));
type QuarkIngestPanelApi = {
  selectedCount?: number;
  canIngest?: boolean;
  submitting?: boolean;
  transferPhaseLabel?: string;
  transferHint?: string;
  submitIngest?: () => void;
};
const quarkIngestRef = ref<QuarkIngestPanelApi | null>(null);
// #endregion buei:quark-ingest
const BackupRestorePanel = defineAsyncComponent(() => import("@/components/admin/BackupRestorePanel.vue"));

const SCRAPE_TAB = "scrape";
const PROXY_TAB = "proxy";
const ENHANCED_TAB = "enhanced";
const BACKUP_TAB = "backup";
// #region buei:guangya-share
const GUANGYA_TAB = "guangya";
// #endregion buei:guangya-share
// #region buei:quark-ingest
const QUARK_TAB = "quark";
// #endregion buei:quark-ingest
const tabs = [
  // #region buei:guangya-share
  { key: GUANGYA_TAB, label: "光鸭分享" },
  // #endregion buei:guangya-share
  // #region buei:quark-ingest
  { key: QUARK_TAB, label: "夸克入库" },
  // #endregion buei:quark-ingest
  { key: SCRAPE_TAB, label: "STRM 刮削" },
  { key: ENHANCED_TAB, label: "增强工具" },
  { key: BACKUP_TAB, label: "备份管理" },
];

const settingsDrawerOpen = ref(false);
const enhancedSearchOpen = ref(false);
const cloudToolsRef = ref<{ clearSearch: () => void } | null>(null);
const scrapeSettingsVisited = ref(false);
const scrapePanelRef = ref<InstanceType<typeof StrmScrapePanelComponent> | null>(null);
const scrapeSettingsRef = ref<SettingsPanelExpose | null>(null);
const scrapePanelDirty = ref(false);

watchEffect(() => {
  scrapePanelDirty.value = (scrapeSettingsRef.value as SettingsPanelExpose | null)?.isDirty?.() ?? false;
});

const settingsPageDirty = computed(() => settingsDrawerOpen.value && scrapePanelDirty.value);

function revertDrawerSettings() {
  scrapeSettingsRef.value?.revert?.();
}

const { confirmDiscardChanges } = useSettingsPageDirty(settingsPageDirty, revertDrawerSettings);

const route = useRoute();
// buei:guangya-share —— 无 tab 参数时默认进入光鸭分享
const initialTab = String(route.query.tab ?? "") === PROXY_TAB ? ENHANCED_TAB : GUANGYA_TAB;
const { activeTab, setActiveTab } = useSectionTabRoute(
  initialTab,
  [SCRAPE_TAB, ENHANCED_TAB, GUANGYA_TAB, QUARK_TAB, BACKUP_TAB],
  {
    beforeTabChange: async () => {
      if (!settingsDrawerOpen.value) return true;
      const ok = await confirmDiscardChanges(() => scrapePanelDirty.value);
      if (!ok) return false;
      settingsDrawerOpen.value = false;
      return true;
    },
  },
);

// 重面板首次访问时才挂载，之后只隐藏不销毁，保留已加载状态。
// 但增强工具的搜索筛选不能跟着留下来：离开这个标签页就清掉，回来看到的是全部卡片。
const tabsVisited = reactive<Record<string, boolean>>({});
watch(
  activeTab,
  (tab) => {
    tabsVisited[tab] = true;
    if (tab !== ENHANCED_TAB) {
      enhancedSearchOpen.value = false;
      cloudToolsRef.value?.clearSearch();
    }
  },
  { immediate: true },
);

const drawerSaving = computed(() => readPanelSaving(scrapeSettingsRef.value?.saving));
const isScrapeTab = computed(() => activeTab.value === SCRAPE_TAB);
const isEnhancedTab = computed(() => activeTab.value === ENHANCED_TAB);
const isBackupTab = computed(() => activeTab.value === BACKUP_TAB);
// #region buei:guangya-share
const isGuangyaTab = computed(() => activeTab.value === GUANGYA_TAB);
// #endregion buei:guangya-share
// #region buei:quark-ingest
const isQuarkTab = computed(() => activeTab.value === QUARK_TAB);
// #endregion buei:quark-ingest

async function openScrapeSettings() {
  scrapeSettingsVisited.value = true;
  settingsDrawerOpen.value = true;
  await nextTick();
  if (scrapeSettingsRef.value && !scrapeSettingsRef.value.isDirty?.()) {
    await scrapeSettingsRef.value.reload?.();
  }
}

async function closeSettingsDrawer() {
  if (!(await confirmDiscardChanges(() => scrapePanelDirty.value))) return;
  settingsDrawerOpen.value = false;
}

onDeactivated(() => {
  enhancedSearchOpen.value = false;
  cloudToolsRef.value?.clearSearch();
  if (!settingsDrawerOpen.value) return;
  if (scrapePanelDirty.value) revertDrawerSettings();
  settingsDrawerOpen.value = false;
});

async function handleDrawerSave() {
  await scrapeSettingsRef.value?.save?.();
}
</script>

<template>
  <div class="settings">
    <SectionTabBar class="aux-tabs" :model-value="activeTab" :tabs="tabs" @update:model-value="setActiveTab">
      <template #actions>
        <template v-if="isScrapeTab">
          <AppButton
            v-if="scrapePanelRef?.running"
            type="button"
            variant="secondary"
            @click="scrapePanelRef?.stopScrape()"
          >
            停止刮削
          </AppButton>
          <AppButton v-else type="button" variant="primary" @click="scrapePanelRef?.startScrape()">
            开始刮削
          </AppButton>
        </template>
        <AppButton v-else-if="isEnhancedTab" type="button" variant="secondary" @click="enhancedSearchOpen = true">
          搜索工具
        </AppButton>
        <!-- #region buei:guangya-share -->
        <AppButton
          v-else-if="isGuangyaTab"
          type="button"
          variant="primary"
          :disabled="!guangyaShareRef?.canTransfer"
          :title="guangyaShareRef?.transferHint || ''"
          @click="guangyaShareRef?.submitTransfer?.()"
        >
          {{
            guangyaShareRef?.transferring
              ? guangyaShareRef?.transferPhaseLabel
              : `转存 ${guangyaShareRef?.selectedCount ?? 0} 项`
          }}
        </AppButton>
        <!-- #endregion buei:guangya-share -->
        <!-- #region buei:quark-ingest -->
        <AppButton
          v-else-if="isQuarkTab"
          type="button"
          variant="primary"
          :disabled="!quarkIngestRef?.canIngest"
          :title="quarkIngestRef?.transferHint || ''"
          @click="quarkIngestRef?.submitIngest?.()"
        >
          {{
            quarkIngestRef?.submitting
              ? quarkIngestRef?.transferPhaseLabel
              : `入库 ${quarkIngestRef?.selectedCount ?? 0} 项`
          }}
        </AppButton>
        <!-- #endregion buei:quark-ingest -->
      </template>
    </SectionTabBar>

    <StrmScrapePanel
      v-if="tabsVisited[SCRAPE_TAB]"
      v-show="isScrapeTab"
      ref="scrapePanelRef"
      @open-settings="openScrapeSettings"
    />
    <CloudToolsPanel
      v-if="tabsVisited[ENHANCED_TAB]"
      v-show="isEnhancedTab"
      ref="cloudToolsRef"
      :search-open="enhancedSearchOpen"
      @update:search-open="enhancedSearchOpen = $event"
    />
    <!-- #region buei:guangya-share -->
    <BueiGuangyaShareModal v-if="tabsVisited[GUANGYA_TAB]" v-show="isGuangyaTab" ref="guangyaShareRef" />
    <!-- #endregion buei:guangya-share -->
    <!-- #region buei:quark-ingest -->
    <BueiQuarkIngestPanel v-if="tabsVisited[QUARK_TAB]" v-show="isQuarkTab" ref="quarkIngestRef" />
    <!-- #endregion buei:quark-ingest -->
    <BackupRestorePanel v-if="tabsVisited[BACKUP_TAB]" v-show="isBackupTab" />

    <AdminSettingsDrawer
      :open="settingsDrawerOpen"
      title="STRM 刮削设置"
      :saving="drawerSaving"
      :can-save="scrapePanelDirty"
      @close="closeSettingsDrawer"
      @cancel="closeSettingsDrawer"
      @save="handleDrawerSave"
    >
      <StrmScrapeSettings v-if="scrapeSettingsVisited" ref="scrapeSettingsRef" />
    </AdminSettingsDrawer>
  </div>
</template>

<style scoped>
/* 顶部 tab 栏吸顶：长列表滚动时仍能切换标签并使用右侧的转存/开始刮削按钮 */
.aux-tabs {
  position: sticky;
  top: 0;
  z-index: 6;
}
</style>
