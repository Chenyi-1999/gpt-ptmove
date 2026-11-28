<template>
  <el-tabs v-model="selectedView" class="layout-settings-tabs" aria-label="页面布局">
    <el-tab-pane name="home" label="首页" lazy>
      <LayoutPanelEditor
        layout-id="home-layout"
        title="首页布局"
        description="手机端固定单列。任务卡占一整行。勾选要显示的卡片，再调整顺序。"
        :labels="HOME_PANEL_LABELS"
        :store="homeLayout"
      />
    </el-tab-pane>
    <el-tab-pane name="sites" label="做种检索" lazy>
      <LayoutPanelEditor
        layout-id="sites-layout"
        title="做种检索布局"
        description="手机端固定单列。双列按从左到右、从上到下排列。勾选要显示的模块，再调整顺序。"
        :labels="SITES_PANEL_LABELS"
        :store="sitesLayout"
      />
    </el-tab-pane>
  </el-tabs>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import LayoutPanelEditor from './LayoutPanelEditor.vue'
import { HOME_PANEL_LABELS, useHomeLayoutStore } from '@/stores/homeLayout'
import { SITES_PANEL_LABELS, useSitesLayoutStore } from '@/stores/sitesLayout'

const homeLayout = useHomeLayoutStore()
const sitesLayout = useSitesLayoutStore()
const route = useRoute()
const router = useRouter()

const selectedView = computed({
  get: () => (route.query.view === 'sites' ? 'sites' : 'home'),
  set: (view: string) => {
    void router.replace({ query: { ...route.query, view: view === 'sites' ? 'sites' : undefined } })
  },
})
</script>

<style scoped>
.layout-settings-tabs {
  min-width: 0;
}

.layout-settings-tabs :deep(.el-tabs__item) {
  min-height: 44px;
}
</style>
