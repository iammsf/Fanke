// 动态链接协议白名单校验:
// 默认仅允许 http: / https: 绝对地址;allowRelative=true 时额外允许单斜杠开头的站内相对路径(排除 // 与 /\ 协议相对地址)。
// 严禁 javascript: / data: / vbscript: 等危险协议,不合法时统一返回空串。
export function safeHref(raw?: string | null, allowRelative = false): string {
  const s = String(raw || '').trim()
  if (!s) return ''
  // 拒绝控制字符(防止换行/制表符绕过协议识别)
  if (/[\u0000-\u001f\u007f]/.test(s)) return ''
  if (allowRelative && s.startsWith('/') && !s.startsWith('//') && !s.startsWith('/\\')) {
    return s
  }
  try {
    const u = new URL(s)
    if (u.protocol === 'http:' || u.protocol === 'https:') {
      return u.href
    }
  } catch {
    // 非法 URL
  }
  return ''
}
