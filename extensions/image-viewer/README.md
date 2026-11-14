# 更好用的 fanke-img 标签

用独立的 `<fanke-img>` 标签扩展插图能力：图注、位置、文字样式、宽度与圆角，且不占用原生 `<img>`。

## 功能

- 正文中使用自定义标签 `<fanke-img src="…" des_pos="middle bottom" des_sty="fs=13,c=#666">图注</fanke-img>`，图注既可以写在标签内部文本，也可以用 `des="…"` 属性。
- `des_pos` 由两个位置关键字组成：垂直支持 `top` / `bottom`，水平支持 `left` / `middle`（兼容 `mid`）/ `right`，缺省为 `middle bottom`；两段顺序不影响解析。
- `des_sty` 支持 `hidden`、`b`、`i`、`fs=像素`、`c=颜色`、`bg=背景色`、`bd=描边色`、`w=百分比`、`r=圆角像素`，多个 token 可用逗号、空格或分号分隔。
- 插件页可保存全局默认 `des_pos`、`des_sty` 与「替换正文中的普通 `<img>` 标签」开关；文章渲染时先套用默认值，再按 token 合并标签自身样式——标签只覆盖它写了的 token，其余沿用默认配置。
- 开启「替换普通 `<img>`」后，没有使用插件标签的图片也按同一套位置与样式渲染；没有 `src` 的图片、以及位于 `<a>` 链接内部的图片不会被替换。
- 后台编辑器注册 `fanke-img` 节点：插入菜单显示「插件图片（图注 / 位置 / 样式）」，通过媒体库选图写入 `src`；双击节点打开属性面板，按「图片 / 位置 / 图注样式 / 图片尺寸」分组编辑。
- 后端在文章创建、更新保存前对正文做规范化：仅为带 `des` / `des_pos` / `des_sty` 属性或显式写了 `</img>` 的历史图片补闭合标签，不再把默认 `des_pos` 写死进正文。
- 编辑器预览阶段，`fanke-img` 缺少 `src` 时渲染为「点击设置图片」骨架占位，避免出现碎图。

## 安装

后台「插件」页上传 `.zip` 安装包。安装包可由 `deploy/install.sh` 生成到 `/opt/fanke/dist/extensions/image-viewer.zip`，也可在 `extensions/image-viewer/` 下将目录内容打包为 zip 上传。

- 无服务端程序、无 Go 依赖、无外部命令依赖。
- 包内 `.ts` 文件（`app.ts`、`init.ts`、`editor.ts`）会在安装时自动编译成同名 `.js`。
- 上传后需在插件详情页完成初始化；`init.ts` 会建表并写入默认配置 `{des_pos:"middle bottom", des_sty:"", replace_img:false}`，之后才能启用。

## 配置

插件详情页（`index.html`）提供以下字段，保存到 `config` 键：

| 字段 | 说明 | 默认值 | 取值 |
| --- | --- | --- | --- |
| 垂直位置 | 图注在图片上方或下方，写入 `des_pos` 的垂直段 | `bottom` | `bottom` / `top` |
| 水平位置 | 图注对齐方式，写入 `des_pos` 的水平段 | `middle` | `middle`（兼容 `mid`）/ `left` / `right` |
| `hidden` | 隐藏图注 | 关闭 | 勾选后输出 `hidden` token |
| `b` / `i` | 图注加粗 / 斜体 | 关闭 | 输出 `b` / `i` token |
| `fs` | 图注字号 | 空 | 输入框限制 10–40，单位 px |
| `c` | 图注文字颜色 | `#6b7a8c` | 取色器；等于默认色时不输出该 token |
| `bg` | 图注背景色 | `#f4f6fb` | 取色器；等于默认色时不输出该 token |
| `bd` | 图注描边色 | `#e2e8f2` | 取色器；生成 `1px solid`；等于默认色时不输出 |
| `w` | 图片宽度（相对正文） | 空 | 输入框限制 10–100，单位百分比 |
| `r` | 图片圆角 | 空 | 输入框限制 0–40，单位 px |
| 原始 `des_sty` | 可直接手工编辑最终 token 字符串 | 空 | 文本输入，与上方控件双向同步 |
| 替换普通 `<img>` | 普通图片是否套用插件样式 | 关闭 | 布尔值 |

- 页面提供「保存配置」「重新读取」按钮及实时效果预览；`fs` / `w` / `r` 仅有 HTML 输入框的 `min` / `max` 约束，保存时不再做额外数值校验。
- 默认值在文章渲染阶段套用，因此修改并保存后，已发布文章中未写对应 `des_pos` / `des_sty` 的图片会立即跟着变化，无需重新保存文章。

## 声明的能力与 Hook

`info.json` 的 `request` 共 7 项；本插件不声明 `capabilities`。

| 名称 | 用途 |
| --- | --- |
| `store.ensure_table` | 初始化时创建插件 KV 表 |
| `store.save` | 保存全局默认配置（key 为 `config`） |
| `store.get` | 插件详情页读取已保存配置 |
| `store.get_batch` | 文章页与编辑器预览公开读取默认配置（公开 hook） |
| `beforePostCreate` | 文章创建保存前，规范化正文中的插件图片标签 |
| `beforePostUpdate` | 文章更新保存前，执行同样的标签规范化 |
| `editor.register` | 让后台编辑器加载 `editor.js`，注册 `fanke-img` 节点的渲染与属性面板 |

## 数据存储

- 使用宿主插件 KV 表 `ext_data_image_viewer`，只保存一条记录：key 为 `config`。
- value 结构：`{des_pos: string, des_sty: string, replace_img: boolean}`。
- 文章正文本身只保存 `<fanke-img>` 标签文本；图注、位置、样式不另存副本，渲染时实时合并默认配置。

## 使用方式

1. 在后台「插件」详情页设置默认图注位置、样式与是否替换普通 `<img>`，点击「保存配置」。
2. 在文章编辑器中点击工具栏「插入图片 / 插件节点」→「插件图片（图注 / 位置 / 样式）」，随后从媒体库选择图片，插入一个 `fanke-img` 节点。
3. 双击画布中的插件图片节点，打开属性面板：填写图片地址（支持外链 `https://…` 或站内 `/uploads/…`）、图注文字，调整位置、样式、宽度与圆角，应用后写回节点。
4. 前台文章页会把 `fanke-img` 转换为 `figure` + `figcaption`；图注位置、对齐、颜色等按「默认配置 + 标签 token」合并结果输出。
5. 手写标签时需顶格写在行首，否则 Markdown 会把它当作代码块；图注内部只支持连续纯文本，不能嵌套其他标签。

## 安全与限制

- 文章页只有在成功读取到插件公开配置后才转换标签；插件未启用、未安装或配置读取失败时，原样输出正文 HTML，不强行干预。
- 普通 `<img>` 的替换受 `replace_img` 控制；无 `src` 的图片和不位于链接内的判断之外的情况保持原样，位于 `<a>` 内的图片会被跳过，避免把 `figure` 塞进链接。
- 替换普通 `<img>` 时会剔除原有 `style` 与 `des*` 属性，其余属性（含 `srcset`、`sizes`、宽高、`loading`）原样保留；未写 `loading` 时补 `loading="lazy"`。
- 标签的 `des_pos` 整体覆盖默认位置；`des_sty` 按 token 合并而不是整段覆盖。`hidden` 以「默认或标签任一为真」为准。
- 图注文本按纯文本处理，解析正则只接受 `<fanke-img …>` 与 `</fanke-img>` 之间的连续非 `<` 文本；复杂 HTML 图注不会被识别。
- `des_sty` 是自由文本，`fs` / `w` / `r` 的数值会直接拼进 CSS；请在可信的后台环境使用，不要允许不可信用户直接提交样式字符串。
- `beforePostCreate` / `beforePostUpdate` 处理器只改写 `ContentHTML`，不写默认 `des_pos`，也不触碰其他字段；保存前仍会经过宿主 HTML 消毒。

## 关于

- 作者：電腦玩家Stan
- 作者主页：https://boke.masi.fan
- 插件主页：暂未提供；更新检查：读取仓库根目录 `updates.json` 中本插件条目。
