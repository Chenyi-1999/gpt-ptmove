<template>
  <section class="home-card home-panel task-progress" aria-labelledby="home-task-progress-title">
    <div class="home-panel__heading">
      <div>
        <h2 id="home-task-progress-title">任务</h2>
        <p>
          {{ runningCount }} 项进行中<span v-if="recentCount">
            · 最近 {{ recentCount }} 项结果</span
          >
        </p>
      </div>
      <button type="button" class="task-progress__link" @click="openMonitor">
        查看全部 <ArrowRight aria-hidden="true" />
      </button>
    </div>

    <p
      v-if="serverState === 'unavailable' && visibleTasks.length"
      class="task-progress__warning"
      role="status"
    >
      暂时无法同步，显示上次读取的状态。
    </p>

    <ul v-if="visibleTasks.length" class="task-progress__list" aria-label="进行中任务与最近结果">
      <li v-for="task in visibleTasks" :key="task.key" class="task-progress__item">
        <div class="task-progress__main">
          <strong class="task-progress__title" :title="task.title">{{ task.title }}</strong>
          <span class="task-progress__state" :class="`task-progress__state--${task.display.tone}`">
            {{ task.display.label }}
          </span>
        </div>
        <p v-if="task.message" class="task-progress__message">{{ task.message }}</p>
        <p
          v-if="task.progressText && task.progressText !== task.message"
          class="task-progress__detail"
        >
          {{ task.progressText }}
        </p>
        <p v-if="task.error && task.error !== task.message" class="task-progress__warning">
          {{ task.error }}
        </p>
      </li>
    </ul>

    <div v-else class="task-progress__empty" role="status">
      <p v-if="serverState === 'loading'">正在读取任务…</p>
      <p v-else-if="serverState === 'unavailable'">暂时无法读取任务</p>
      <p v-else>暂无进行中任务</p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { storeToRefs } from 'pinia'
import { ArrowRight } from '@element-plus/icons-vue'
import { useTaskMonitorStore } from '@/stores/taskMonitor'
import { getTaskDisplayStatus } from '@/utils/publishPresentation'

const { taskList, runningCount, serverState } = storeToRefs(useTaskMonitorStore())
const recent = computed(() =>
  taskList.value
    .filter((task) => task.status !== 'running' && Date.now() - task.updatedAt < 30 * 60000)
    .slice(0, 6),
)
const recentCount = computed(() => recent.value.length)
const visibleTasks = computed(() =>
  [...taskList.value.filter((task) => task.status === 'running'), ...recent.value].map((task) => ({
    ...task,
    display: getTaskDisplayStatus(task),
  })),
)

function openMonitor() {
  window.dispatchEvent(new Event('starpt-open-task-monitor'))
}
</script>

<style scoped>
.task-progress__link {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  flex: none;
  padding: 4px;
  border: 0;
  background: transparent;
  color: var(--star-data-muted);
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
}

.task-progress__link:hover {
  color: var(--star-data-ink);
}
.task-progress__link svg {
  width: 14px;
  height: 14px;
}

.task-progress__list {
  display: grid;
  gap: 8px;
  max-height: 356px;
  overflow: auto;
  margin: 14px 0 0;
  padding: 0;
  list-style: none;
  scrollbar-width: thin;
}

.task-progress__item {
  min-width: 0;
  padding: 11px 12px;
  border: 1px solid var(--star-data-border);
  border-radius: 10px;
  background: var(--star-data-panel-raised);
}

.task-progress__main {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 8px;
}

.task-progress__title {
  min-width: 0;
  overflow-wrap: anywhere;
  color: var(--star-data-ink);
  font-size: 13px;
  line-height: 1.4;
}

.task-progress__state {
  flex: none;
  padding: 3px 7px;
  border: 1px solid currentColor;
  border-radius: 999px;
  color: var(--star-data-muted);
  font-size: 10px;
  font-weight: 650;
}

.task-progress__state--running {
  color: var(--star-data-accent);
}
.task-progress__state--success {
  color: var(--el-color-success);
}
.task-progress__state--failed {
  color: var(--el-color-danger);
}

.task-progress__message,
.task-progress__detail,
.task-progress__warning {
  margin: 5px 0 0;
  overflow-wrap: anywhere;
  font-size: 12px;
  line-height: 1.4;
}

.task-progress__message {
  color: var(--star-data-ink);
}
.task-progress__detail {
  color: var(--star-data-muted);
  font-variant-numeric: tabular-nums;
}
.task-progress__warning {
  color: var(--el-color-warning);
}

.task-progress__empty {
  display: grid;
  min-height: 88px;
  align-content: center;
  color: var(--star-data-muted);
  font-size: 13px;
}
</style>
