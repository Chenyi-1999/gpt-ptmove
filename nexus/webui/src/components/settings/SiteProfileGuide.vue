<template>
  <el-dialog
    v-model="open"
    title="站点配置编写说明"
    width="680px"
    class="settings-dialog"
    align-center
    destroy-on-close
  >
    <section class="guide">
      <p class="guide-intro">
        适用于 NexusPHP 站点。每个配置文件为一个 UTF-8 JSON 对象，最大 64 KiB。
      </p>
      <ol class="guide-steps">
        <li>
          <strong>准备资料</strong>
          <p>
            下载本页的 Schema 和模板，准备站点域名、用途、Tracker 主机名，以及脱敏后的列表和详情
            HTML。目标站还需提供有结果和无结果的搜索页。
          </p>
        </li>
        <li>
          <strong>生成配置</strong>
          <p>
            把下方提示词、Schema、模板和资料交给 AI。详情和下载路径各使用一个
            <code>{id}</code>，搜索路径使用一个
            <code>{query}</code>。转载限制和查重选择器依据页面的明确标志填写。
          </p>
        </li>
        <li>
          <strong>校验并导入</strong>
          <p>
            将生成的文件上传至「导入配置」，先点「校验并预览」。导入后通过「凭据」单独填写 Cookie 和
            RSS，并用只读预览确认页面解析。
          </p>
        </li>
      </ol>
      <div class="prompt-heading">
        <h3>给 AI 的提示词</h3>
        <el-button type="primary" @click="copyPrompt">复制提示词</el-button>
      </div>
      <textarea
        ref="promptField"
        class="prompt-field"
        aria-label="给 AI 的提示词"
        :value="prompt"
        rows="5"
        readonly
      />
    </section>
    <template #footer>
      <a
        class="el-button is-plain example-link"
        :href="exampleUrl"
        download="site-profile.example.json"
      >
        下载示例 JSON
      </a>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import guidePrompt from '@/assets/site-profile-ai-prompt.txt?raw'

const open = defineModel<boolean>({ required: true })
const prompt = guidePrompt.replace(/\r\n/g, '\n').trim()
const promptField = ref<HTMLTextAreaElement | null>(null)
const exampleUrl = `${import.meta.env.BASE_URL}site-profile.example.json`

async function copyPrompt() {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(prompt)
      ElMessage.success('提示词已复制')
      return
    }
  } catch {
    // 浏览器拒绝访问剪贴板时，继续尝试从文本框复制。
  }

  const field = promptField.value
  if (!field) return
  field.focus({ preventScroll: true })
  field.select()

  try {
    if (document.execCommand('copy')) {
      ElMessage.success('提示词已复制')
      return
    }
  } catch {
    // 保留选中的文字，方便用户手动复制。
  }
  ElMessage.info('已选中提示词，请手动复制')
}
</script>

<style scoped>
.guide {
  display: grid;
  gap: 16px;
  line-height: 1.55;
}

.guide p,
.prompt-heading h3 {
  margin: 0;
}

.guide-intro,
.guide-steps p {
  color: var(--el-text-color-secondary);
}

.guide-steps {
  display: grid;
  gap: 10px;
  margin: 0;
  padding-inline-start: 1.4em;
}

.guide-steps strong,
.prompt-heading h3 {
  color: var(--el-text-color-primary);
}

.guide-steps strong {
  display: block;
  margin-bottom: 2px;
}

.prompt-heading {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 12px;
  align-items: center;
  justify-content: space-between;
}

.prompt-heading h3 {
  font-size: 0.9375rem;
}

.prompt-field {
  box-sizing: border-box;
  display: block;
  width: 100%;
  min-width: 0;
  padding: 10px 12px;
  color: var(--el-text-color-regular);
  font: inherit;
  background: var(--el-fill-color-light);
  border: 1px solid var(--el-border-color);
  border-radius: var(--el-border-radius-base);
  resize: vertical;
}

.prompt-field:focus-visible {
  outline: 2px solid var(--el-color-primary);
  outline-offset: 2px;
}

.example-link {
  text-decoration: none;
}
</style>
