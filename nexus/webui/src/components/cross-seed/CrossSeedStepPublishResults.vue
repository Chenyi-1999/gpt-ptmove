<template>
  <div class="step-container results-container">
    <!-- 进度条显示 -->
    <div class="progress-section">
      <div class="progress-item" v-if="publishProgress.total > 0">
        <div class="progress-label">发布进度:</div>
        <el-progress
          :percentage="Math.round((publishProgress.current / publishProgress.total) * 100)"
          :show-text="true"
          :stroke-width="8"
        />
        <div class="progress-text">{{ publishProgress.current }} / {{ publishProgress.total }}</div>
      </div>
      <div class="progress-item" v-if="downloaderProgress.total > 0">
        <div class="progress-label">下载器添加进度:</div>
        <el-progress
          :percentage="Math.round((downloaderProgress.current / downloaderProgress.total) * 100)"
          :show-text="true"
          :stroke-width="8"
        />
        <div class="progress-text">
          {{ downloaderProgress.current }} / {{ downloaderProgress.total }}
        </div>
      </div>

      <!-- 🚫 发种限制提示 -->
      <div class="limit-alert-section" v-if="limitAlert.visible">
        <div class="limit-alert">
          <div class="limit-alert-content">
            <div class="limit-alert-title">{{ limitAlert.title }}</div>
            <div class="limit-alert-message">{{ limitAlert.message }}</div>
          </div>
        </div>
      </div>
    </div>

    <div class="results-rows-container">
      <div v-for="(row, rowIndex) in groupedResults" :key="rowIndex" class="results-row">
        <div class="row-sites">
          <div
            v-for="result in row"
            :key="result.siteName"
            class="result-card"
            :class="{
              'is-success': result.displayStatus === 'success',
              'is-warning': result.displayStatus === 'warning',
              'is-error': result.displayStatus === 'error',
              'is-waiting': result.displayStatus === 'waiting',
              'is-publishing': result.displayStatus === 'publishing',
              'is-paused':
                result.displayStatus === 'paused' || result.displayStatus === 'incomplete',
            }"
          >
            <h4 class="card-title">{{ result.siteName }}</h4>
            <div class="status-tag">
              <el-icon :class="{ 'loading-icon': result.displayStatus === 'publishing' }">
                <Loading v-if="result.displayStatus === 'publishing'" />
                <CircleClose v-else-if="result.displayStatus === 'error'" />
                <CircleCheck
                  v-else-if="
                    result.displayStatus === 'success' || result.displayStatus === 'warning'
                  "
                />
                <Clock v-else />
              </el-icon>
              <span>{{ outcomeLabel(result) }}</span>
            </div>
            <p
              v-if="
                result.source_restriction_status === 'denied' ||
                result.source_restriction_status === 'unknown'
              "
              class="result-detail"
            >
              {{ result.message }}
            </p>
            <p v-if="result.downloaderStatus?.success === false" class="result-detail">
              添加失败<template v-if="result.downloaderStatus.message">
                · {{ result.downloaderStatus.message }}</template
              >
            </p>
            <p v-else-if="result.downloaderStatus?.success" class="result-detail">下载器已添加</p>

            <!-- 操作按钮 -->
            <div class="card-extra">
              <el-button
                type="primary"
                size="small"
                @click="showSiteLog(result.siteName, result.logs)"
              >
                查看日志
              </el-button>
              <a
                v-if="result.success && result.url && filterUploadedParam(result.url)"
                :href="filterUploadedParam(result.url)"
                target="_blank"
                rel="noopener noreferrer"
                class="result-link"
              >
                查看种子
              </a>
            </div>
          </div>
        </div>
        <div class="row-action">
          <el-button
            :icon="TopRight"
            @click="openAllSitesInRow(row)"
            :disabled="!hasValidUrlsInRow(row)"
            class="open-all-button"
          >
            打开 {{ getValidUrlsCount(row) }} 个站点
          </el-button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { CircleCheck, CircleClose, Clock, Loading, TopRight } from '@element-plus/icons-vue'
import { getPublishDisplayStatus, getPublishResponseStatus } from '@/utils/publishPresentation'
import { useCrossSeedPanelContext, type PublishDisplayResult } from './crossSeedPanelContext'

const outcomeLabel = (result: PublishDisplayResult) => {
  if (result.displayStatus === 'waiting') return '等待中'
  if (result.displayStatus === 'publishing') return '发布中'
  if (result.displayStatus === 'paused' || result.displayStatus === 'incomplete') return '未完成'
  if (result.source_restriction_status === 'denied') return '源站禁转'
  if (result.source_restriction_status === 'unknown') return '待核验'
  return getPublishDisplayStatus(getPublishResponseStatus(result)).label
}

const {
  publishProgress,
  downloaderProgress,
  limitAlert,
  groupedResults,
  showSiteLog,
  filterUploadedParam,
  hasValidUrlsInRow,
  openAllSitesInRow,
  getValidUrlsCount,
} = useCrossSeedPanelContext()
</script>

<style scoped>
.results-rows-container {
  display: grid;
  gap: 18px;
}

.results-row {
  display: grid;
  gap: 10px;
}

.row-sites {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(160px, 100%), 1fr));
  align-items: stretch;
  gap: 10px;
}

.result-card {
  display: grid;
  align-content: start;
  gap: 8px;
  min-width: 0;
  padding: 14px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 12px;
  background: var(--el-bg-color-overlay);
}

.card-title {
  margin: 0;
  font-size: 15px;
  overflow-wrap: anywhere;
}

.result-card.is-success .status-tag {
  color: var(--el-color-success-dark-2);
}
.result-card.is-error .status-tag {
  color: var(--el-color-danger-dark-2);
}
.result-card.is-warning .result-detail {
  color: var(--el-color-warning-dark-2);
}
.result-card.is-publishing .status-tag {
  color: var(--el-color-primary-dark-2);
}

.loading-icon {
  animation: cross-seed-rotate 1s linear infinite;
}

.row-action {
  justify-self: end;
}

.progress-section {
  flex-wrap: wrap;
  padding: 14px;
  border-color: var(--el-border-color-light);
  background: var(--el-bg-color-overlay);
  box-shadow: none;
}

.progress-item {
  min-width: min(180px, 100%);
}
.limit-alert-section {
  width: 100%;
  margin-top: 0;
}
.limit-alert-message {
  overflow-wrap: anywhere;
}

.status-tag {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
}

.result-detail {
  margin: 0;
  color: var(--el-text-color-secondary);
  font-size: 13px;
  overflow-wrap: anywhere;
}

.result-link {
  display: inline-flex;
  align-items: center;
  padding-inline: 8px;
  color: var(--el-color-primary-dark-2);
}

.card-extra {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
}

@media (prefers-reduced-motion: reduce) {
  .loading-icon {
    animation: none;
  }
}
</style>
