<script setup lang="ts">
import { InfoFilled } from '@element-plus/icons-vue'

const tiers = [
  { size: '≤ 10', hours: 24 },
  { size: '> 10 至 20', hours: 48 },
  { size: '> 20 至 30', hours: 72 },
  { size: '> 30 至 40', hours: 96 },
  { size: '> 40 至 50', hours: 120 },
  { size: '> 50', hours: 168 },
]
</script>

<template>
  <el-popover
    :trigger="['hover', 'click']"
    placement="bottom-start"
    :width="232"
    :show-after="100"
    :popper-style="{ maxWidth: 'calc(100vw - 24px)', boxSizing: 'border-box' }"
  >
    <template #reference>
      <button type="button" class="hr-hint-trigger" aria-label="查看北洋园 H&R 做种时长">
        <InfoFilled aria-hidden="true" />
      </button>
    </template>
    <table class="hr-hint-table">
      <caption>北洋园 H&amp;R 基础做种时长</caption>
      <thead>
        <tr><th scope="col">种子体积（GiB）</th><th scope="col">时长</th></tr>
      </thead>
      <tbody>
        <tr v-for="tier in tiers" :key="tier.hours">
          <td>{{ tier.size }}</td>
          <td>{{ tier.hours }} 小时</td>
        </tr>
      </tbody>
    </table>
  </el-popover>
</template>

<style scoped>
.hr-hint-trigger {
  display: inline-grid;
  place-items: center;
  width: 26px;
  height: 26px;
  padding: 0;
  border: 0;
  border-radius: 50%;
  background: transparent;
  color: var(--star-muted);
  cursor: pointer;
}

.hr-hint-trigger:hover,
.hr-hint-trigger:focus-visible {
  color: var(--star-accent);
  background: var(--star-accent-soft);
}

.hr-hint-trigger svg {
  width: 15px;
  height: 15px;
}

.hr-hint-table {
  width: 100%;
  border-collapse: collapse;
  font-variant-numeric: tabular-nums;
  text-align: left;
}

.hr-hint-table caption {
  margin-bottom: 6px;
  color: var(--star-ink);
  font-weight: 650;
  text-align: left;
}

.hr-hint-table th,
.hr-hint-table td {
  padding: 3px 0;
  border-bottom: 1px solid var(--star-border);
}

.hr-hint-table th {
  color: var(--star-muted);
  font-size: .8rem;
}

.hr-hint-table th:last-child,
.hr-hint-table td:last-child {
  text-align: right;
}

</style>
