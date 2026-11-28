// src/router/index.ts
import { createRouter, createWebHistory } from 'vue-router'

const whiteList: string[] = ['/login']

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: '/',
      name: 'home',
      component: () => import('../views/HomeView.vue'),
    },
    {
      path: '/info',
      redirect: { name: 'home' },
    },
    {
      path: '/torrents',
      name: 'torrents',
      component: () => import('../views/TorrentsView.vue'),
    },
    {
      path: '/data',
      name: 'data',
      component: () => import('../views/CrossSeedDataView.vue'),
    },
    {
      path: '/seed-maintenance',
      name: 'seed-maintenance',
      component: () => import('../views/SeedMaintenanceView.vue'),
    },
    {
      path: '/plugins',
      name: 'plugins',
      component: () => import('../views/PluginsView.vue'),
    },
    {
      path: '/publish-logs',
      name: 'publish-logs',
      component: () => import('../views/PublishLogsView.vue'),
    },
    {
      path: '/sites',
      name: 'sites',
      component: () => import('../views/SitesView.vue'),
    },
    {
      path: '/settings',
      name: 'settings',
      component: () => import('../views/SettingsView.vue'),
      redirect: '/settings/general',
      children: [
        {
          path: 'general',
          name: 'settings-general',
          component: () => import('../components/settings/GeneralSettings.vue'),
        },
        {
          path: 'layout',
          name: 'settings-layout',
          component: () => import('../components/settings/LayoutSettings.vue'),
        },
        {
          path: 'downloader',
          name: 'settings-downloader',
          component: () => import('../components/settings/DownloaderSettings.vue'),
        },
        {
          path: 'cookie',
          name: 'settings-cookie',
          component: () => import('../components/settings/SitesSettings.vue'),
        },
        {
          path: 'transfer',
          name: 'settings-transfer',
          alias: 'automation',
          component: () => import('../components/settings/GeneralSettings.vue'),
          props: { transferOnly: true },
        },
        {
          path: 'site-profiles',
          name: 'settings-site-profiles',
          component: () => import('../components/settings/SiteProfilesSettings.vue'),
        },
        {
          path: 'api',
          name: 'settings-api',
          component: () => import('../components/settings/AISettings.vue'),
        },
        {
          path: 'ai',
          redirect: { name: 'settings-api' },
        },
      ],
    },
    {
      path: '/login',
      name: 'login',
      component: () => import('../views/LoginView.vue'),
    },
  ],
})

// 简单路由守卫：当开启后端认证时，未携带 token 的请求会被 401 拦截
router.beforeEach(async (to, _from, next) => {
  const token = localStorage.getItem('token')
  if (whiteList.includes(to.path)) return next()
  if (!token) {
    // 未登录：直接去 login
    return next({ path: '/login', query: { redirect: to.fullPath } })
  }
  return next()
})

export default router
