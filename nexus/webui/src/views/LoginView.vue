<template>
  <div class="star-login">
    <div class="star-login__panel star-surface">
      <div class="star-login__brand">
        <div class="star-login__emblem">
          <img src="/starpt.svg" width="64" height="64" alt="" />
        </div>
        <div>
          <strong>STARPT</strong>
          <p>种子管理与转种工作台</p>
        </div>
      </div>

      <section class="star-login__main" aria-labelledby="login-title">
        <h1 id="login-title">登录</h1>
        <form class="star-login__form" :aria-busy="loading" @submit.prevent="onSubmit">
          <label for="login-username">用户名</label>
          <input
            id="login-username"
            v-model="form.username"
            name="username"
            autocomplete="username"
            autocapitalize="none"
            spellcheck="false"
            required
          />

          <label for="login-password">密码</label>
          <div class="star-login__password">
            <input
              id="login-password"
              v-model="form.password"
              name="password"
              :type="showPassword ? 'text' : 'password'"
              autocomplete="current-password"
              required
            />
            <button
              type="button"
              class="star-icon-button"
              :aria-label="showPassword ? '隐藏密码' : '显示密码'"
              :title="showPassword ? '隐藏密码' : '显示密码'"
              aria-controls="login-password"
              @click="showPassword = !showPassword"
            >
              <component :is="showPassword ? Hide : View" aria-hidden="true" />
            </button>
          </div>

          <p v-if="loginError" class="star-login__error" role="alert">
            <Warning aria-hidden="true" />
            {{ loginError }}
          </p>

          <button type="submit" class="star-button star-button--primary" :disabled="loading">
            <Loading v-if="loading" class="star-spinner" aria-hidden="true" />
            {{ loading ? '正在登录…' : '登录' }}
          </button>
        </form>

        <details class="star-login__help">
          <summary>在哪里查看初始密码？</summary>
          <p>
            Docker 端请查看容器日志。Windows 端请查看
            <code>%APPDATA%\pt-nexus\data\logs</code> 中的日志。
          </p>
        </details>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import axios from 'axios'
import { Hide, Loading, View, Warning } from '@element-plus/icons-vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from '@/utils/uiNotify'

const route = useRoute()
const router = useRouter()
const loading = ref(false)
const loginError = ref('')
const showPassword = ref(false)
const form = ref({ username: '', password: '' })

const onSubmit = async () => {
  if (loading.value) return
  loading.value = true
  loginError.value = ''
  try {
    const { data } = await axios.post('/api/auth/login', form.value)
    if (!data?.success || !data.token) {
      loginError.value = data?.message || '登录失败，请检查用户名和密码。'
      return
    }

    localStorage.setItem('token', data.token)

    if (data.is_temp_password || data.must_change_password) {
      ElMessage.warning({
        message: '请在基础设置的账户信息中修改临时密码。',
        duration: 8000,
        showClose: true,
      })
      await router.replace('/settings')
      return
    }

    const redirect = route.query.redirect
    const destination =
      typeof redirect === 'string' && redirect.startsWith('/') && !redirect.startsWith('//')
        ? redirect
        : '/'
    await router.replace(destination)
    if (router.currentRoute.value.path === '/login') await router.replace('/')
    ElMessage.success('登录成功')
  } catch (error: unknown) {
    loginError.value = axios.isAxiosError(error)
      ? (error.response?.data as { message?: string } | undefined)?.message ||
        '暂时无法登录，请稍后重试。'
      : '暂时无法登录，请稍后重试。'
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.star-login {
  display: grid;
  flex: 1 0 auto;
  box-sizing: border-box;
  min-height: 100%;
  place-items: center;
  padding: max(20px, env(safe-area-inset-top, 0px)) max(16px, env(safe-area-inset-right, 0px))
    max(24px, env(safe-area-inset-bottom, 0px)) max(16px, env(safe-area-inset-left, 0px));
}

.star-login__panel {
  display: grid;
  grid-template-columns: minmax(240px, 0.9fr) minmax(300px, 1fr);
  width: min(100%, 758px);
  overflow: hidden;
  border-radius: 22px;
  box-shadow: 0 18px 56px rgb(24 68 95 / 11%);
}

.star-login__brand {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  justify-content: center;
  gap: 21px;
  overflow: hidden;
  padding: clamp(24px, 4vw, 50px);
  border-right: 1px solid var(--star-border);
  background:
    linear-gradient(140deg, rgb(235 246 251 / 89%), rgb(214 239 247 / 90%)), var(--star-accent-soft);
}

.star-login__brand::before,
.star-login__brand::after {
  position: absolute;
  top: -105px;
  right: -92px;
  width: 275px;
  height: 275px;
  border: 1px solid #bddfea;
  border-radius: 50%;
  content: '';
  pointer-events: none;
}

.star-login__brand::after {
  top: -83px;
  right: -70px;
  width: 231px;
  height: 231px;
}

.star-login__brand > div {
  z-index: 1;
}

.star-login__emblem {
  display: grid;
  width: 82px;
  height: 82px;
  place-items: center;
  border: 1px solid rgb(255 255 255 / 75%);
  border-radius: 22px;
  background: rgb(255 255 255 / 50%);
}

.star-login__emblem img {
  display: block;
}

.star-login__brand strong {
  display: block;
  color: var(--star-ink);
  font-size: clamp(2rem, 5vw, 3.35rem);
  font-weight: 750;
  line-height: 1.06;
  letter-spacing: -0.045em;
}

.star-login__brand p {
  margin: 9px 0 0;
  color: var(--star-muted);
  font-size: 15px;
}

.star-login__main {
  min-width: 0;
  padding: clamp(24px, 4vw, 48px);
}

.star-login__main h1 {
  margin: 0 0 25px;
  color: var(--star-ink);
  font-size: 28px;
  letter-spacing: -0.025em;
}

.star-login__form {
  display: flex;
  flex-direction: column;
  gap: 7px;
}

.star-login__form label {
  color: var(--star-ink);
  font-size: 14px;
  font-weight: 650;
}

.star-login__form input {
  box-sizing: border-box;
  width: 100%;
  min-height: 47px;
  padding: 8px 12px;
  border: 1px solid var(--star-border-strong);
  border-radius: 9px;
  color: var(--star-ink);
  background-color: var(--star-surface);
  font-size: 16px;
}

.star-login__form .star-icon-button {
  border: 0;
  border-radius: 7px;
}

.star-login__form label[for='login-password'] {
  margin-top: 9px;
}

.star-login__password {
  position: relative;
}

.star-login__password input {
  padding-right: 49px;
}

.star-login__password .star-icon-button {
  position: absolute;
  top: 1px;
  right: 1px;
  bottom: 1px;
  height: auto;
  background: transparent;
}

.star-login__password .star-icon-button:hover {
  background: var(--star-accent-soft);
}

.star-login__password .star-icon-button:focus-visible {
  outline-offset: -2px;
}

.star-login__form .star-button {
  min-height: 47px;
  margin-top: 10px;
}

.star-login__error {
  display: flex;
  align-items: flex-start;
  gap: 7px;
  margin: 4px 0 0;
  padding: 8px 10px;
  border-radius: 8px;
  color: var(--star-danger);
  background: var(--star-danger-soft);
  overflow-wrap: anywhere;
}

.star-login__error svg {
  flex: none;
  width: 17px;
  height: 17px;
  margin-top: 2px;
}

.star-login__help {
  margin-top: 15px;
  color: var(--star-muted);
  font-size: 14px;
}

.star-login__help summary {
  box-sizing: border-box;
  min-height: 44px;
  padding-block: 11px;
  cursor: pointer;
}

.star-login__help p {
  margin: 0;
  overflow-wrap: anywhere;
}

@media (max-width: 620px) {
  .star-login__panel {
    grid-template-columns: minmax(0, 1fr);
    max-width: 440px;
    border-radius: 17px;
  }

  .star-login__brand {
    flex-direction: row;
    align-items: center;
    justify-content: flex-start;
    gap: 12px;
    min-height: 70px;
    padding: 13px 20px;
    border-right: 0;
    border-bottom: 1px solid var(--star-border);
  }

  .star-login__emblem {
    flex: none;
    width: 51px;
    height: 51px;
    border-radius: 12px;
  }

  .star-login__emblem img {
    width: 40px;
    height: 40px;
  }

  .star-login__brand strong {
    font-size: 26px;
  }

  .star-login__brand p {
    margin-top: 4px;
    font-size: 14px;
  }

  .star-login__main {
    padding: 22px 20px 16px;
  }

  .star-login__main h1 {
    margin-bottom: 19px;
    font-size: 24px;
  }
}
</style>
