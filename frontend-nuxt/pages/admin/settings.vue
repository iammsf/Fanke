<script setup lang="ts">
// 站点设置各分区独立保存，插件配置通过贡献入口进入详情页。
// 主题管理:占位(后续独立功能,不在本期开发)。
definePageMeta({ layout: 'admin', middleware: 'auth' })
import type { MediaItem, NavItem, SiteSettings } from '~/types'

const { api } = useAuth()
const { contributions, contributionsLoaded, refreshContributions } = useExtensions()
const pluginSettings = computed(() => contributions.value?.settings || [])
if (import.meta.client && !contributionsLoaded.value) void refreshContributions()
const { push } = useToast()
const { run } = useBusy() // 与 DB/存储交互期间的全屏加载遮罩(兼防连点)

const loading = ref(true)
const error = ref<any>(null)

// ---- site ----
const site = reactive({ title: '', subtitle: '', author: '', footer: '', copyright: '' })
// ---- comment_policy ----
const commentMode = ref('moderate')
// ---- home_sort ----
const homeSort = reactive({ field: 'date', order: 'desc' })
// ---- default_covers ----
const defaultCovers = reactive({ standard: '', moment: '' })

const coverPickerFor = ref<'' | 'standard' | 'moment'>('')
const coverPickerOpen = computed({
  get: () => !!coverPickerFor.value,
  set: (v) => {
    if (!v) coverPickerFor.value = ''
  },
})
// ---- nav ----
const nav = ref<NavItem[]>([])
const busy = ref<Record<string, boolean>>({})

const put = async (key: string, value: unknown, label: string) => {
  busy.value[key] = true
  try {
    await run(`保存「${label}」中…`, () => api(`/admin/settings?key=${encodeURIComponent(key)}`, { method: 'PUT', body: JSON.stringify(value) }))
    push(`已保存「${label}」`, true)
  } catch (e: any) {
    push(e.message || '保存失败', false)
  } finally {
    busy.value[key] = false
  }
}

const load = async () => {
  loading.value = true
  error.value = null
  let s: SiteSettings
  try {
    s = await api<SiteSettings>('/admin/settings')
  } catch (e: any) {
    error.value = e
    loading.value = false
    return
  }
  site.title = s.site?.title || ''
  site.subtitle = s.site?.subtitle || ''
  site.author = s.site?.author || ''
  site.footer = s.site?.footer || ''
  site.copyright = s.site?.copyright || ''
  const mode = s.comment_policy?.mode || 'moderate'
  commentMode.value = ['moderate', 'open', 'close'].includes(mode) ? mode : 'moderate'
  const hs = (s as any).home_sort
  if (hs && typeof hs === 'object') {
    if (['date', 'views', 'words', 'type'].includes(hs.field)) homeSort.field = hs.field
    if (hs.order === 'asc') homeSort.order = 'asc'
  }
  const dc = (s as any).default_covers
  if (dc && typeof dc === 'object') {
    defaultCovers.standard = dc.standard || ''
    defaultCovers.moment = dc.moment || ''
  }
  nav.value = Array.isArray(s.nav) && s.nav.length ? [...s.nav] : [{ label: '首页', href: '/' }]

  loading.value = false
}
onMounted(load)

const saveSite = () =>
  put(
    'site',
    {
      title: site.title.trim(),
      subtitle: site.subtitle.trim(),
      author: site.author.trim(),
      footer: site.footer.trim(),
      copyright: site.copyright.trim(),
    },
    '站点信息',
  )
const saveCommentPolicy = () => put('comment_policy', { mode: commentMode.value }, '评论策略')
const saveHomeSort = () => put('home_sort', { field: homeSort.field, order: homeSort.order }, '首页文章排序')
const pickDefaultCover = (m: MediaItem) => {
  if (!m.mime.startsWith('image/')) {
    push('请选择图片文件', false)
    return
  }
  if (coverPickerFor.value) defaultCovers[coverPickerFor.value] = m.url
  coverPickerFor.value = ''
  push('已选择,点击保存生效', true)
}
const saveDefaultCovers = () =>
  put('default_covers', { standard: defaultCovers.standard.trim(), moment: defaultCovers.moment.trim() }, '默认封面图')
const saveNav = () => put('nav', nav.value.map((r) => ({ label: r.label.trim(), href: r.href.trim() })).filter((r) => r.label && r.href), '导航')
const addNavRow = () => nav.value.push({ label: '', href: '' })
const delNavRow = (i: number) => nav.value.splice(i, 1)

// 默认封面缩略图 → Element Plus 图片查看器
const coverViewerOpen = ref(false)
const coverViewerUrl = ref('')
const openCoverViewer = (url: string) => {
  if (!url) return
  coverViewerUrl.value = url
  coverViewerOpen.value = true
}

useHead({ title: '设置 · fanke 后台' })
</script>

<template>
  <div>
    <el-skeleton v-if="loading" :rows="8" animated />
    <ErrorBox v-else-if="error" :error="error" retry @retry="load" />
    <template v-else>
      <!-- 站点信息 -->
      <div class="panel card">
        <h2>站点信息(site)</h2>
        <el-form label-position="top">
          <el-form-item label="标题"><el-input v-model="site.title" /></el-form-item>
          <el-form-item label="副标题"><el-input v-model="site.subtitle" /></el-form-item>
          <el-form-item label="作者"><el-input v-model="site.author" /></el-form-item>
          <el-form-item label="页脚内容">
            <el-input v-model="site.footer" type="textarea" :rows="4" placeholder="纯文本或 HTML/JS 代码" />
            <div class="hint" style="font-size:12px;color:var(--muted);margin-top:4px">支持 HTML 与 &lt;script&gt;(如备案链接、统计代码);内容原样输出,请确保代码可信</div>
          </el-form-item>
          <el-form-item label="版权信息">
            <el-input v-model="site.copyright" :placeholder="`© ${new Date().getFullYear()} ${site.title || '网站标题'}`" />
            <div class="hint" style="font-size:12px;color:var(--muted);margin-top:4px">支持变量:$y = 当前年份,$admin = 管理员昵称;留空则显示默认</div>
          </el-form-item>
        </el-form>
        <div style="margin-top: 12px">
          <el-button type="primary" :disabled="busy.site" @click="saveSite">保存站点信息</el-button>
        </div>
      </div>

      <!-- 主题(占位) -->
      <div class="panel card">
        <h2>主题(theme)</h2>
        <p class="muted">主题上传/切换功能开发中,当前使用内置 Element Plus 主题。占位,敬请期待。</p>
      </div>

      <!-- 评论策略 -->
      <div class="panel card">
        <h2>评论策略(comment_policy)</h2>
        <el-form label-position="top">
          <el-form-item label="模式">
            <el-select v-model="commentMode">
              <el-option value="moderate" label="先审后发(moderate)" />
              <el-option value="open" label="直接发布(open)" />
              <el-option value="close" label="全站关闭(close)" />
            </el-select>
            <div class="hint" style="font-size:12px;color:var(--muted);margin-top:4px">moderate:新评论进待审队列;open:直接公开;close:全站关闭。</div>
          </el-form-item>
        </el-form>
        <div style="margin-top: 12px">
          <el-button type="primary" :disabled="busy.comment_policy" @click="saveCommentPolicy">保存评论策略</el-button>
        </div>
      </div>

      <!-- 首页文章排序 -->
      <div class="panel card">
        <h2>首页文章排序(home_sort)</h2>
        <p class="muted" style="margin-top: 0">置顶文章始终排在最前;以下设置决定其余文章的顺序。</p>
        <el-form label-position="top">
          <div class="ed-cols2">
            <el-form-item label="排序字段">
              <el-select v-model="homeSort.field">
                <el-option value="date" label="按日期" />
                <el-option value="views" label="按浏览量(待接入统计,暂按日期处理)" disabled />
                <el-option value="words" label="按字数" />
                <el-option value="type" label="按类型" />
              </el-select>
            </el-form-item>
            <el-form-item label="方向">
              <el-select v-model="homeSort.order">
                <el-option value="desc" label="降序(新→旧 / 多→少)" />
                <el-option value="asc" label="升序(旧→新 / 少→多)" />
              </el-select>
            </el-form-item>
          </div>
        </el-form>
        <div style="margin-top: 12px">
          <el-button type="primary" :disabled="busy.home_sort" @click="saveHomeSort">保存排序</el-button>
        </div>
      </div>

      <!-- 默认封面图 -->
      <div class="panel card">
        <h2>默认封面图(default_covers)</h2>
        <p class="muted" style="margin-top: 0">未手动设置封面的文章按其类型使用对应默认封面;随记未配置时回退到标准。已设封面的文章不受影响。</p>
        <el-form label-position="top">
          <div class="ed-cols2">
            <el-form-item label="标准(standard)">
              <div style="display: flex; gap: 8px; align-items: center; flex-wrap: wrap">
                <el-button @click="coverPickerFor = 'standard'">从媒体库选择</el-button>
                <el-button size="small" :disabled="!defaultCovers.standard" @click="defaultCovers.standard = ''">清除</el-button>
                <img
                  v-if="defaultCovers.standard"
                  :src="defaultCovers.standard"
                  class="cover-thumb"
                  alt="标准默认封面预览"
                  title="点击预览"
                  style="cursor: zoom-in"
                  @click="openCoverViewer(defaultCovers.standard)"
                />
                <span v-else class="muted">未设置</span>
              </div>
            </el-form-item>
            <el-form-item label="随记(moment)">
              <div style="display: flex; gap: 8px; align-items: center; flex-wrap: wrap">
                <el-button @click="coverPickerFor = 'moment'">从媒体库选择</el-button>
                <el-button size="small" :disabled="!defaultCovers.moment" @click="defaultCovers.moment = ''">清除</el-button>
                <img
                  v-if="defaultCovers.moment"
                  :src="defaultCovers.moment"
                  class="cover-thumb"
                  alt="随记默认封面预览"
                  title="点击预览"
                  style="cursor: zoom-in"
                  @click="openCoverViewer(defaultCovers.moment)"
                />
                <span v-else class="muted">未设置</span>
              </div>
            </el-form-item>
          </div>
        </el-form>
        <div style="margin-top: 12px">
          <el-button type="primary" :disabled="busy.default_covers" @click="saveDefaultCovers">保存默认封面</el-button>
        </div>
      </div>

      <!-- 导航 -->
      <div class="panel card">
        <h2>导航(nav)</h2>
        <div class="ed-wrap">
          <div
            v-for="(r, i) in nav"
            :key="i"
            class="ed-cols2"
            style="align-items: center; grid-template-columns: 1fr 2fr auto; gap: 8px"
          >
            <el-input v-model="r.label" placeholder="名称" />
            <el-input v-model="r.href" placeholder="链接,如 /categories" />
            <el-button size="small" type="danger" @click="delNavRow(i)">移除</el-button>
          </div>
        </div>
        <div style="display: flex; gap: 10px; margin-top: 12px">
          <el-button @click="addNavRow">+ 添加一项</el-button>
          <el-button type="primary" :disabled="busy.nav" @click="saveNav">保存导航</el-button>
        </div>
      </div>

      <div v-for="item in pluginSettings" :key="`${item.plugin_slug}:${item.id}`" class="panel card">
        <h2>{{ item.title }}</h2>
        <p class="muted">配置由插件维护，请在插件详情页调整并保存。</p>
        <NuxtLink :to="`/admin/plugins/${encodeURIComponent(item.plugin_slug)}`">打开插件配置 →</NuxtLink>
      </div>

      <el-dialog v-model="coverPickerOpen" title="选择默认封面" width="min(70vw, calc(100vw - 32px))" top="10vh" class="post-media-dialog">
        <MediaLibrary mode="pick" :page-size="21" @pick="pickDefaultCover" />
      </el-dialog>
    </template>
  </div>

  <!-- Element Plus 图片查看器(默认封面缩略图) -->
  <el-image-viewer
    v-if="coverViewerOpen"
    :url-list="[coverViewerUrl]"
    teleported
    hide-on-click-modal
    @close="coverViewerOpen = false"
  />
</template>

<style scoped>
</style>
