// 与后端 API 对应的共享类型(公开 + 后台)
export interface Term {
  id: number
  name: string
  slug: string
  type: 'category' | 'tag'
}

export interface TermWithCount extends Term {
  count: number
}

export interface PostSummary {
  id: number
  slug: string
  title: string
  summary: string
  status: string
  author_id: number
  author_name: string
  cover_media_id: number | null
  cover_url?: string
  cover_width?: number
  cover_height?: number
  allow_comments: boolean
  is_public: boolean
  views: number
  type: 'standard' | 'moment' | string
  sticky: number
  published_at: string | null
  created_at: string
  updated_at: string
  terms: Term[]
}

export interface Post extends PostSummary {
  content_html: string
  cover_url?: string
}

export interface CommentItem {
  id: number
  post_id: number
  parent_id: number | null
  author_name: string
  content: string
  created_at: string
}

export interface Page<T> {
  items: T[]
  total: number
  page: number
  per: number
}

export interface NavItem {
  label: string
  href: string
}

export interface SiteSettings {
  site?: { title?: string; subtitle?: string; author?: string; footer?: string; copyright?: string; admin_name?: string }
  theme?: { name?: string }
  plugins?: Record<string, boolean>
  nav?: NavItem[]
  comment_policy?: { mode?: string }
  media_image_upload?: { enabled: boolean }
  [k: string]: unknown
}

export interface UserPlugin {
  id: number
  name: string
  css: string
  js: string
  enabled: boolean
  sort: number
}

// ---------- 后台 ----------
export interface AdminUser {
  id: number
  email: string
  username: string
  role: string
  display_name: string
  avatar_url: string
  bio: string
  created_at: string
}

export interface AdminTerm {
  id: number
  name: string
  slug: string
  type: string
  count?: number
}

export interface TermRef {
  name: string
  slug: string
}

export interface AdminPost {
  id: number
  slug: string
  title: string
  summary: string
  content_md?: string
  content_html?: string
  status: 'draft' | 'published' | string
  type: 'standard' | 'moment' | string
  sticky: number
  author_id: number
  author_name?: string
  cover_media_id: number | null
  cover_url?: string
  allow_comments: boolean
  is_public: boolean
  views: number
  published_at: string | null
  created_at: string
  updated_at: string
  terms?: AdminTerm[]
}

export interface MediaItem {
  id: number
  filename: string
  storage: string
  key: string
  url: string
  mime: string
  size: number
  width: number
  height: number
  sha256: string
  created_at: string
}

export interface ExtensionSettingContribution {
  id: string
  plugin_slug: string
  kind: string
  title: string
}

export interface UploadMenuContribution {
  id: string
  plugin_slug: string
  label: string
  storage: string
  is_default?: boolean
}

export interface MediaContributions {
  settings: ExtensionSettingContribution[]
  upload_menu: UploadMenuContribution[]
  image_processing: boolean
  s3_storage: boolean
}

export interface AdminComment {
  id: number
  post_id: number
  parent_id: number | null
  author_name: string
  author_email?: string
  user_id: number | null
  content: string
  status: string
  created_at: string
}

export interface SetupStatus {
  state?: string
  needs_setup?: boolean
}

// ---------- 插件(扩展) ----------
export interface Extension {
  id: number
  slug: string
  name: string
  author: string
  version: string
  date: string
  description: string
  url: string
  update_url: string
  favico: string
  hooks: string[]
  enabled: boolean
  initialized: boolean
  installed_at: string
  updated_at: string
}

export interface ExtensionUpdateInfo {
  version: string
  download_url: string
  page_url?: string
  notes?: string
  date: string
  has_update: boolean
}

// 博客本体更新检查结果
export interface CoreUpdateInfo {
  current: string
  latest: string
  date?: string
  notes?: string
  page_url?: string
  download_url?: string
  has_update: boolean
  manifest_url?: string
}

// 图片焦点数据(与后端 image_focus hooks 对应)
export interface FocusRect {
  x: number
  y: number
  w: number
  h: number
}

export interface FocusData {
  cx: number
  cy: number
  square: FocusRect
  wide: FocusRect
  tall: FocusRect
}
