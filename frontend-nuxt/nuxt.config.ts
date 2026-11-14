// fanke 前端(Nuxt 3 + Vue 3,纯静态 SPA)
// 产物:.output/public → 直接由 1Panel/OpenResty 作为静态站点托管。
// SEO 让位于「纯静态部署」;所有数据浏览器端同域 fetch /api/v1(反代到 Go 后端)。
export default defineNuxtConfig({
  ssr: false,
  devtools: { enabled: false },
  compatibilityDate: '2025-01-01',

  modules: ['@element-plus/nuxt'],

  css: [
    '~/assets/css/tokens.css', // 内置 Element Plus 主题令牌(上传主题运行时注入覆盖)
    'element-plus/theme-chalk/dark/css-vars.css', // Element Plus 暗色变量(挂 html.dark)
    '@fontsource/noto-color-emoji/400.css',
    '~/assets/css/global.css',
  ],

  // 组件库扁平引用:<PostEditor> / <MediaLibrary> 等,与目录层级无关
  components: [
    { path: '~/components/admin', pathPrefix: false },
    { path: '~/components' },
  ],

  app: {
    head: {
      htmlAttrs: { lang: 'zh-CN' },
      title: 'fanke',
      meta: [
        { charset: 'utf-8' },
        { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      ],
      link: [{ rel: 'icon', href: '/favicon.ico' }],
      script: [
        // 首屏前恢复暗色偏好,避免闪烁
        {
          children:
            "(()=>{try{const t=localStorage.getItem('fanke-theme');if(t==='dark'||(!t&&matchMedia('(prefers-color-scheme: dark)').matches)){document.documentElement.dataset.theme='dark';document.documentElement.classList.add('dark')}}catch(e){}})();",
        },
      ],
    },
  },

  runtimeConfig: {
    public: {
      // 生产同域;本地开发可用 NUXT_PUBLIC_API_BASE 覆盖
      apiBase: process.env.NUXT_PUBLIC_API_BASE || '/api/v1',
    },
  },

  nitro: {
    // 仅开发模式生效:把 API/本地上传目录/缩图接口代理到后端
    devProxy: {
      '/api': { target: process.env.API_PROXY_TARGET || 'http://127.0.0.1:8081', changeOrigin: true },
      '/uploads': { target: process.env.API_PROXY_TARGET || 'http://127.0.0.1:8081', changeOrigin: true },
      '/img': { target: process.env.API_PROXY_TARGET || 'http://127.0.0.1:8081', changeOrigin: true },
    },
  },
})
