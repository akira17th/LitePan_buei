<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { containsQuery } from "@/utils/format";
import { useAccountsStore } from "@/stores/accounts";
import AppButton from "@/components/base/AppButton.vue";
import ToolCard from "@/components/admin/ToolCard.vue";
import BueiGuangyaShareModal from "@/components/buei/BueiGuangyaShareModal.vue";

// buei 定制：光鸭分享收藏工具卡片（挂在 后台 / 辅助工具 / 增强工具）。
const props = withDefaults(defineProps<{ searchQuery?: string }>(), { searchQuery: "" });

const accountsStore = useAccountsStore();
const open = ref(false);

const guangyaAccounts = computed(() =>
  accountsStore.accounts.filter((account) => account.driver_type === "guangya" && account.is_active),
);
const visible = computed(() => containsQuery("光鸭分享收藏", props.searchQuery));

onMounted(() => {
  if (!accountsStore.accounts.length) {
    void accountsStore.loadAccounts().catch(() => undefined);
  }
});
</script>

<template>
  <div v-show="visible">
    <ToolCard
      :enabled="true"
      name="光鸭分享收藏"
      driver="光鸭云盘 · 分享收藏与转存"
      logo-src="/logos/guangya.png"
      logo-alt="光鸭云盘"
      :stat-value="guangyaAccounts.length"
      stat-label="个光鸭账号可转存"
    >
      收藏光鸭分享链接，随时选择解析、逐层浏览，转存到指定账号目录，并可自动生成 STRM。
      <template #actions>
        <AppButton size="sm" variant="secondary" @click="open = true">打开工具</AppButton>
      </template>
    </ToolCard>

    <BueiGuangyaShareModal :open="open" @close="open = false" />
  </div>
</template>
