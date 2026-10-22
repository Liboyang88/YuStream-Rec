# 前端结构

[English](frontend-structure.md) · [中文](frontend-structure.zh-CN.md)

前端是一个用 Vite 构建的 Vue 3 单页应用。它刻意保持框架轻量：Composition API、Pinia、Vue Router、Axios、手写 CSS 与 KaTeX。没有 TypeScript、UI 组件库、SSR 或测试运行器。

前端只消费 HTTP API。物品来自 `searcher/` 还是其他入库管道，它完全无从得知。

## 1. 目录结构

```text
frontend/
├── index.html
├── vite.config.js              # @ 别名、/api 开发代理、构建输出
├── package.json                # yarn / npm 脚本
├── src/
│   ├── main.js                 # 创建 app、Pinia、router、reveal 指令、CSS
│   ├── App.vue                 # 布局切换 + 全局弹窗 + toast
│   ├── api/
│   │   ├── client.js           # Axios 实例、令牌存储、错误归一化
│   │   ├── users.js            # 认证 + 画像端点
│   │   ├── items.js            # GET /items/recommend
│   │   └── interactions.js     # 交互类型 + 负载塑形
│   ├── components/
│   │   ├── SideBar.vue         # 品牌、导航、账户状态
│   │   ├── FeedStream.vue      # 无限卡片 feed、底部触发、滚动会话
│   │   ├── ItemCard.vue        # 卡片、原文链接、点击交互
│   │   ├── ItemDetailPanel.vue # 详情、摘要、点赞
│   │   ├── FeedLoader.vue      # 加载中 / 无新内容 / 错误页脚
│   │   ├── OnboardingForm.vue  # 画像字段 + 兴趣，引导/编辑模式
│   │   ├── ProfilePanel.vue    # 「我的」面板：画像、概述、刷新/增量
│   │   ├── SettingsPanel.vue   # 占位设置面板
│   │   ├── AuthPanel.vue       # 登录 / 注册 / 忘记密码
│   │   ├── AppModal.vue        # teleport 弹窗外壳
│   │   ├── AppIcon.vue         # 内联 SVG 图标集
│   │   ├── ToastHost.vue       # toast 渲染
│   │   ├── SettingSwitch.vue   # 开关行
│   │   └── MathText.vue        # KaTeX/回退文本渲染
│   ├── composables/
│   │   └── useSmoothWheel.js   # feed 滚动器的 rAF 滚轮缓动
│   ├── directives/
│   │   └── reveal.js           # v-reveal IntersectionObserver 动画
│   ├── layouts/
│   │   ├── MainLayout.vue      # 侧边栏 + 内容
│   │   └── BlankLayout.vue     # 全屏认证/引导外壳
│   ├── router/
│   │   └── index.js            # 路由 + 认证/引导守卫
│   ├── stores/
│   │   ├── auth.js             # 会话、画像、异步画像生成轮询
│   │   ├── feed.js             # 推荐窗口、去重、阻塞状态
│   │   ├── preferences.js      # 概述/翻译展示开关（localStorage）
│   │   └── ui.js               # 面板、详情弹窗、浏览计时、toast
│   ├── styles/
│   │   └── base.css            # 颜色、间距、排版、共享按钮
│   ├── utils/
│   │   ├── format.js           # 标题/概述/摘要的选取、日期、来源标签
│   │   └── mathText.js         # 数学 token 解析/渲染辅助
│   └── views/
│       ├── HomeView.vue        # feed 页面外壳 + 滚动器 + 翻译开关
│       ├── AuthView.vue        # /auth
│       └── OnboardingView.vue  # /welcome
└── README.md                   # 前端原始说明（中文）
```

## 2. 启动与布局

`main.js` 创建应用、安装 Pinia 与 router、注册 `v-reveal`、导入 KaTeX CSS 与 `base.css`，并挂载 `#app`。

`App.vue` 根据 `route.meta.layout` 选择布局：

| 路由 | 布局 | 访问权限 | 用途 |
| --- | --- | --- | --- |
| `/` | `MainLayout` | 需要登录 | 流式 feed |
| `/auth` | `BlankLayout` | 仅访客 | 登录 / 注册 / 忘记密码 |
| `/welcome` | `BlankLayout` | 需要登录 | 画像引导 |
| `/:pathMatch(.*)*` | — | — | 重定向到 `/` |

`MainLayout` 是侧边栏加内容区。`BlankLayout` 是无侧边栏的全屏外壳，用于认证/引导。

`App.vue` 还承载三个全局 `AppModal`（画像、设置、物品详情）以及 `ToastHost`，因此「我的」「设置」与详情都是浮层而非路由。

## 3. 路由与守卫

`router.beforeEach`：

1. 如果已登录且画像尚未加载，`await auth.loadProfile({ silent: true })`。
2. 如果路由需要认证而用户未登录，重定向到 `/auth` 并带上 `redirect` query。
3. 如果已登录、画像已加载、没有画像详情，且用户未跳过引导，重定向到 `/welcome`。
4. 如果访客在已登录状态下访问 `/auth`，将其送到 `/welcome` 或 `/`。
5. 否则继续。

`needsOnboarding` 的计算方式为：

```text
isLoggedIn && profileLoaded && !hasProfileDetail && !onboardingDismissed
```

`onboardingDismissed` 持久化在 localStorage（`dcrec.onboarding_skipped`）中，而非用户账户里。跳过引导一次后，该浏览器上的守卫会被抑制，直到该标志被清除。

## 4. 认证与引导流程

```
/auth  ->  login (email + password)
       ->  register: send email code -> submit code -> tokens
       ->  forgot: send email code -> PATCH new password

/welcome -> OnboardingForm
         -> POST /users/info        (保存字段；无画像任务)
         -> PUT  /users/summary     (入队画像生成)
         -> poll GET /users/info every 2.5 s, up to 45 s
         -> show the generated summary
```

`auth` store 负责：

- `accessToken`、`username`、`email`、`profile`、`profileLoaded`
- `register`、`login`、`logout`、`resetPassword`
- `loadProfile`
- `saveProfile(patch, { regenerate: 'client' | 'server' | 'none' })`
- `generateSummary({ mode: 'regenerate' | 'incremental' })`
- `pollSummary` / `awaitSummaryUpdate`

`saveProfile` 的行为：

| `regenerate` | API | 画像处理 |
| --- | --- | --- |
| `client` | `POST /users/info` | 前端显式调用 `PUT /users/summary`（首次引导） |
| `server` | `PUT /users/info` | 后端入队 `user_summary:new_info`，客户端轮询 |
| `none` | `POST /users/info` | 仅保存 |

登录以邮箱作为唯一凭证。用户名仅作展示昵称；后端的 `GET /users/info` 不返回邮箱/用户名，因此 SPA 在本地保存两者用于展示。

## 5. feed 机制

### `stores/feed.js`

feed 是一系列**窗口**，而不是分页：

- `items` — 累积的卡片，按顺序追加。
- `source` — `llm` / `coarse`。
- `outline` — 最新 LLM 窗口的描述，用作 `scroll` 交互的 detail。
- `loading`、`error`、`loadedOnce`、`lastRoundAdded`、`requestCount`。
- `blocked` — 仅在 `401` 或网络失败（`status === 0`）时设置；正常的「无新内容」响应**不会**阻塞。
- `seen` — 物品 key 的 `Set`，用于客户端去重。

`loadMore()` 调用 `GET /items/recommend`，过滤掉已见过的 ID，仅追加新物品，并记录新增数量。它从不精排已有卡片。不存在永久的「feed 结束」状态：每次滚动到底都会发起真实请求。

### `components/FeedStream.vue`

- 测量网格并把列数限制在 1–3；卡片宽度固定为 320 px。
- 使用 `IntersectionObserver` 哨兵，外加捕获阶段的 `scroll` 与 `wheel` 监听。
- 当用户滚动到距底部 320 px 以内时触发请求，冷却时间 800 ms。
- `MAX_CHAIN = 6` 用于防止后端持续返回新内容、速度快于视口填充时形成无限链。
- 收到新批次后，仅当 `added > 0` 且视口仍在底部时才继续链式发起下一次请求。
- 仅当 `source === 'llm'` 时记录 `scroll` 交互；detail 为窗口的 `outline`，时长为秒数，上限 5 分钟。
- 打开详情弹窗或任一面板会结束当前滚动会话。

### 交互事件发送

| 事件 | 位置 | `detail` | `long_ts` |
| --- | --- | --- | --- |
| `click`（卡片） | `FeedStream.openDetail` | `card` | 0 |
| `click`（原文链接） | `ItemCard.handleOriginal` | `link` | 0 |
| `scroll` | `FeedStream.endScrollSession` | 窗口 `outline` | 秒数，上限 300 |
| `like` | `ItemDetailPanel.toggleLike` | 空 | 0 |
| `view` | 详情弹窗关闭时的 `ui.finishDetailView` | 空 | 秒数，上限 180 |

所有交互请求都是 fire-and-forget（`.catch(() => {})`），因此分析调用失败永远不会阻塞 UI。

## 6. Store

| Store | 职责 | 持久化 |
| --- | --- | --- |
| `auth` | 会话、画像、登录/注册、画像生成轮询 | 令牌/用户名/邮箱/引导标志存于 localStorage |
| `feed` | 推荐窗口、去重、加载/阻塞状态 | 内存中；登出时重置 |
| `preferences` | `preferSummary`、`translateChinese` | localStorage |
| `ui` | 当前面板、详情物品、详情浏览计时、toast | 内存中 |

`preferences` 默认值：展示增强概述（`preferSummary=true`），隐藏中文翻译（`translateChinese=false`）。翻译只影响非中文物品；中文物品始终展示原文。

## 7. 组件

| 组件 | 说明 |
| --- | --- |
| `SideBar` | 品牌、「我的 / 首页 / 设置」、账户卡片、登录/登出 |
| `AppModal` | teleport 到 body、点击遮罩/Escape 关闭、滚动锁定、焦点处理 |
| `AuthPanel` | 三种模式：登录、注册、忘记密码；邮箱验证码两步 |
| `OnboardingForm` | 画像字段、兴趣上限 10 个、自定义兴趣输入、`onboarding`/`edit` 模式 |
| `ProfilePanel` | 身份卡、生成的概述、基本信息、兴趣、增量/重新生成、登出 |
| `FeedStream` | 网格、哨兵、底部触发、滚动会话、空/错误状态 |
| `ItemCard` | 来源/日期、标题、160 字符概述/摘要、标签、原文链接 |
| `ItemDetailPanel` | 完整标题、作者、标签、「推荐理由」概述、摘要、点赞 |
| `MathText` | 用 KaTeX 渲染 `$...$`/`$$...$$`；回退为纯文本 |
| `SettingSwitch` | 可访问的开关行 |
| `FeedLoader` | 骨架/加载中、无新内容、重试 |
| `ToastHost` | 瞬时 toast |
| `AppIcon` | 内联 SVG 图标；不依赖图标包 |

## 8. API 层

`api/client.js`：

- `baseURL: '/api'`，超时 25 s，JSON 内容类型。
- 请求拦截器添加 `Authorization: Bearer <accessToken>`。
- 响应拦截器把错误映射为带 `status` 与 `payload` 的 `ApiError`。
- `401` 清除会话并调用已注册的未授权处理器（`App.vue` 重置 auth/feed/ui）。
- 错误信息提取同时支持 `{kind,msg}` 与 `{error}` 两种封装，并把 Go 字段名映射为中文标签。

`api/users.js` 暴露 `sendSignUpCode`、`signUp`、`logIn`、`sendResetCode`、`resetPassword`、`fetchProfile`、`fillProfile`、`updateProfile`、`requestRegenerateSummary`、`requestIncrementalSummary`。

`api/items.js` 暴露 `fetchRecommendations()`；它刻意没有 cursor 参数，因为后端每次请求只提供一个窗口。

`api/interactions.js` 定义四种类型，并在发送前限制 `view`/`scroll` 时长。

## 9. 样式与体验

- `styles/base.css` 定义颜色、圆角、阴影、间距以及共享 `.btn` 样式的 CSS 变量。
- 不使用 CSS 框架；每个组件使用 scoped CSS。
- 响应式断点会折叠卡片网格与画像网格。
- 动效基于 CSS；`v-reveal` 添加错落式入场，`useSmoothWheel` 对 feed 容器中的滚轮滚动做缓动。
- 具备基础可访问性（label、`aria-*`、卡片/弹窗的键盘激活），但尚未做过完整审计。

## 10. 新增或修改页面

1. 新增 `views/YourView.vue`。
2. 在 `router/index.js` 中注册，按需设置 `meta.layout`（`main` 或 `blank`）与 `meta.requiresAuth`/`guestOnly`。
3. 在 `src/api/` 中添加相应的 API 函数（不要在组件中直接调用 Axios）。
4. 如果数据的生命周期长于组件，则在 store 中添加状态。
5. 如果它是子页面而不是路由，在 `ui` 中添加面板 key，并在 `App.vue` 中添加 `AppModal` —— 「我的」与「设置」就是这样工作的。

## 11. 构建与部署

```bash
yarn install
yarn dev       # http://127.0.0.1:5173, proxies /api to :8080
yarn build     # frontend/dist
yarn preview
```

Vite 把 `/api` 代理到 `VITE_API_PROXY_TARGET`（或 `VITE_API_TARGET`），默认为 `http://127.0.0.1:8080`。生产环境中，提供 `dist` 并把 `/api` 代理到同源的后端；后端没有 CORS。参见 [快速开始](getting-started.zh-CN.md#5-生产构建)。

## 12. 已知限制

1. **没有 TypeScript、单元测试或 E2E 测试。** 重构依赖手动验证。
2. **只有客户端去重。** 后端可能合法地返回客户端已见过的物品；UI 会隐藏它们，因此一个窗口可能添加少于 `LIMIT_TOP_K` 张卡片。
3. **没有虚拟化。** 很长的会话会把每张卡片都留在 DOM 中。
4. **`like` 仅是本地状态。** `ItemDetailPanel` 切换本地 `liked` ref 并发送交互，但状态不会被持久化或恢复。
5. **跳过引导仅限浏览器本地。** 它存储在 localStorage，而非用户记录中。
6. **设置是占位实现。** 除了 feed 的翻译开关外，`SettingsPanel.vue` 没有真实偏好设置。
7. **没有 SSR 或 SEO。** 应用是客户端渲染的 SPA；feed 需要认证。
8. **API 映射是手工维护的。** 请保持 `src/api/*.js` 与 `backend/api/route/*.go` 同步。
9. **没有离线/错误边界。** 顶层渲染错误仍会使应用白屏。
10. **构建工具对 lockfile 敏感。** `yarn.lock` 是 Yarn 1.x；npm 会生成不同的依赖树。
