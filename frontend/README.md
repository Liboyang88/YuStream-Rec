# 流式推荐系统 · 前端

基于 **Vue 3 + Vite + Yarn** 的流式推荐系统前端。对接本仓库 `backend/`（Gin）提供的 HTTP API。

---

## 快速开始

```bash
cd frontend

# 安装依赖（Yarn 1.x）
yarn install

# 启动开发服务器（默认 http://127.0.0.1:5173）
yarn dev

# 生产构建
yarn build

# 预览构建产物
yarn preview
```

后端需先启动（默认 `:8080`）：

```bash
cd ../backend && go run ./cmd
```

### 接口地址配置

后端 **未开启 CORS**，因此开发环境统一由 Vite 代理转发，前端代码里只写相对路径 `/api`。

代理目标在 `.env.development` 中配置（参考 `.env.example`），两个变量名都支持：

```
# 优先读取 VITE_API_PROXY_TARGET（沿用项目既有约定），其次 VITE_API_TARGET
VITE_API_PROXY_TARGET=http://localhost:8080
VITE_PORT=5173
```

生产部署时，需要由 Nginx 等反向代理把 `/api` 转发到后端（保持同源），或自行在 `src/api/client.js`
中把 `baseURL` 改为后端绝对地址，并让后端开启 CORS。

---

## 功能与需求对照

### 1. 左侧侧边导航栏 — `src/components/SideBar.vue`

| 需求 | 实现 |
| --- | --- |
| 三个功能：我的 / 首页 / 设置 | 侧边栏导航项，顺序与需求一致 |
| 点击「首页」返回流式推荐主页面 | 关闭所有浮窗并 `router.push('/')` |
| 「我的」「设置」弹出固定浮动窗口 | 由 `AppModal.vue`（Teleport 到 body）承载 |
| 「设置」先保持空白 | `SettingsPanel.vue` 仅放占位空状态，无具体功能 |

侧边栏底部同时承载登录态与「登录 / 注册 / 退出登录」入口。

### 2. 中心内容区域 — `src/views/HomeView.vue` + `src/components/FeedStream.vue`

| 需求 | 实现 |
| --- | --- |
| 卡片最小长度固定 | `--card-min-h: 268px`，同一行等高 |
| 宽度恒定 | `--card-w: 320px`，卡片宽度始终 320px |
| 窗口大可一行 3 个、缩小显示 1–2 个 | 由容器宽度动态计算列数：`floor((W + gap) / (cardW + gap))`，钳制在 1–3 列（`ResizeObserver`） |
| 卡片展示标题 / 描述 / 发布时间 | `ItemCard.vue`：`title` + 摘要 + `publish_date` |
| 点击卡片弹出固定浮动窗口看详情 | `ItemDetailPanel.vue`（标题、作者、领域、摘要、推荐理由） |
| 卡片之间适当间距 | CSS Grid `gap: var(--gap)`（24px） |
| 鼠标滚轮滚动卡片（小红书式） | `useSmoothWheel.js`：拦截 wheel → rAF 缓动逼近目标位置 |
| 卡片出现时「浮现」动效 | `src/directives/reveal.js`（`v-reveal`）+ `@keyframes cardReveal`，同排错峰 70ms 形成波浪感 |
| 触底请求更多 + 底部加载动画 | `IntersectionObserver` 哨兵 + scroll/wheel 触底检测 → `FeedLoader.vue` 三点弹跳动画 |
| 每次下滑到底部都会再次请求 | **不做「到底」永久封存**：每次到达底部都真实再发一次 `GET /api/items/recommend`（贴底后继续向下滚同样触发）。仅在请求进行中或 800ms 冷却内不重复发 |
| 新内容按顺序追加到底部、不打扰用户 | `stores/feed.js`：按 id 去重后 `concat` 追加，绝不重排已有卡片 |

### 3. 登录 / 注册 / 忘记密码 — `src/views/AuthView.vue` + `src/views/OnboardingView.vue`

登录/注册与信息填写是**两个独立的整屏页面**，使用 `BlankLayout`（**不含侧边栏**），
与主页面（`MainLayout`，带左侧导航）完全分离：

| 路由 | 页面 | 布局外壳 |
| --- | --- | --- |
| `/` | 首页 · 流式推荐 | `MainLayout`：左侧导航 + 中间内容区 |
| `/auth` | 登录 / 注册 / 忘记密码 | `BlankLayout`：整屏独立页，无侧边栏 |
| `/welcome` | 信息填写 + 生成画像 | `BlankLayout`：整屏独立页，无侧边栏 |

`src/router/index.js` 的前置守卫串起引导链路：

- 未登录访问 `/` → 跳转 `/auth`
- 已登录但资料仍为空 → 强制进入 `/welcome`
- 已登录再访问 `/auth` → 送回 `/`（资料为空则送到 `/welcome`）
- 在 `/welcome` 点「稍后再说」→ 进入 `/`，跳过状态持久化（刷新不再弹回引导页）

#### 账号策略：邮箱登录

后端已把登录凭据从「用户名」改为「邮箱」（`email` 唯一索引）：用户名只在注册时填写、
作为昵称展示，**不再参与登录校验**。`AuthPanel.vue` / `stores/auth.js` 据此调整：

- 登录表单只要求 邮箱 + 密码，`autocomplete` 用 `email` / `current-password`
- 登录接口只回传 token、不回传昵称与邮箱，所以邮箱在本地留档（`dcrec.email`）；
  界面身份优先展示昵称，昵称未知时回退展示邮箱（侧边栏与「我的」浮窗）
- 「我的」浮窗新增「账号邮箱」一行；身份卡在昵称存在时补一行邮箱副标题（昵称就是邮箱时不重复展示）
- 退出登录会一并清掉本地留档的邮箱

#### 三个模式 + 验证码两步流程

`AuthPanel.vue` 用 `mode` 切换 `login` / `register` / `forgot` 三种形态。页签仍只保留
「登录 / 注册」，**忘记密码**由登录表单内「密码」一栏右侧的「忘记密码？」按钮进入
（该模式下页签滑块隐藏，并提供「返回登录」）：

| 模式 | 字段 | 提交接口 |
| --- | --- | --- |
| 登录 | 邮箱、密码 | `POST /api/users/login` |
| 注册 | 用户名、邮箱、密码、确认密码、邮箱验证码 | 先 `POST /api/users/signup/email` 取码 → `POST /api/users/signup` |
| 忘记密码 | 邮箱、新密码、确认密码、邮箱验证码 | 先 `POST /api/users/forget-password/email` 取码 → `PATCH /api/users/forget-password` |

验证码交互与后端 `internal/service/user_service.go` 的语义对齐：

- 「获取验证码」在上方字段未通过校验时禁用，避免必然失败的请求
- 发送成功后进入 **60 秒倒计时**（按钮文案 `Ns 后重发` 且禁用）；验证码 **5 分钟内有效**
- 冷却期内重发会被后端拒绝（409 `已发送验证码，请 Xs 后重试`），前端按提示恢复倒计时
- 取码后修改邮箱会提示「验证码已发往原邮箱，请重新获取」
- 重置密码成功后提示并**自动回到登录表单**（保留邮箱、清空密码），用新密码登录即可

表单校验规则与后端 `pkg/binding_validator.go` 完全一致：

- 用户名：2–15 位、不含空格（仅注册）
- 邮箱：`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`；提交前统一小写，避免同一邮箱产生多个验证码 key
- 密码：8–20 位字母或数字
- 验证码：6 位数字
- 注册 / 找回密码均需二次确认密码

错误提示：后端字段级校验错误用 Go 字段名作 key（如 `{"Email":"邮箱格式错误"}`），
`src/api/client.js` 的 `FIELD_LABELS` 会把它转成中文标签后再展示。

信息填写页（`OnboardingForm.vue`）：年龄、性别、职业、学历、所在城市、所属机构、
感兴趣领域（最多 10 个，含自定义添加）。

#### 填资料与生成画像是两个接口（重要）

后端已把两者拆开，`POST /api/users/info`（FillInfo）**只写库、不投递任何画像任务**，
所以前端必须自己把画像生成补上：

| 场景 | 请求序列 |
| --- | --- |
| 注册引导首次填写（`/welcome`） | `POST /api/users/info`（只落库）→ `PUT /api/users/summary`（GenerateSummary，显式触发生成） |
| 「我的」→ 编辑资料 | `PUT /api/users/info`（UpdateInfo，后端按改动字段增量更新画像，保留互动沉淀） |

对应 `stores/auth.js` 的 `saveProfile(patch, { regenerate })`：`'client'` = 前端显式触发，
`'server'` = 后端投递任务、前端只等结果，`'none'` = 不动画像。
两条路径都通过 `pollSummary()` 轮询 `GET /api/users/info`（每 2.5s，最多 45s）直到新画像出现，
由 `summaryGenerating` / `profile.summary` 驱动页面就地展示「生成中 → 画像结果」。
画像生成**不阻塞**表单提交，界面在后台自行更新。

---

## 目录结构

```
src/
├── api/
│   ├── client.js         # axios 实例、JWT 注入、错误归一化、401 处理、登录态留档（含邮箱）
│   ├── users.js          # 邮箱登录 / 注册验证码 / 找回密码 / 填资料与改资料 / 画像
│   ├── items.js          # 推荐内容
│   └── interactions.js   # 互动上报（view/click/like）
├── layouts/
│   ├── MainLayout.vue        # 主页面外壳：左侧导航 + 内容区
│   └── BlankLayout.vue       # 引导页外壳：整屏，无侧边栏
├── views/
│   ├── HomeView.vue          # 首页（流式推荐卡片流）
│   ├── AuthView.vue          # /auth 登录 / 注册 / 忘记密码独立页
│   └── OnboardingView.vue    # /welcome 信息填写 + 生成画像独立页
├── components/
│   ├── SideBar.vue           # 左侧导航
│   ├── AppModal.vue          # 通用固定浮动窗口
│   ├── AppIcon.vue           # 内联 SVG 图标
│   ├── AuthPanel.vue         # 登录 / 注册 / 找回密码表单（邮箱验证码 + 倒计时）
│   ├── OnboardingForm.vue    # 信息填写 + 生成画像
│   ├── ProfilePanel.vue      # 「我的」子页面
│   ├── SettingsPanel.vue     # 「设置」子页面（空白占位）
│   ├── FeedStream.vue        # 卡片流：列数自适应 / 触底加载
│   ├── ItemCard.vue          # 推荐卡片
│   ├── ItemDetailPanel.vue   # 推荐详情
│   ├── FeedLoader.vue        # 底部加载动画
│   └── ToastHost.vue         # 轻提示
├── composables/
│   └── useSmoothWheel.js     # 鼠标滚轮平滑滚动
├── directives/
│   └── reveal.js             # v-reveal 浮现动画
├── stores/
│   ├── auth.js               # 登录态（邮箱/昵称）、验证码发送、重置密码、填资料、画像生成与轮询
│   ├── feed.js               # 推荐列表、去重追加、到底判定
│   └── ui.js                 # 浮窗 / 轻提示
├── styles/base.css           # 设计令牌与全局样式
├── utils/format.js           # 文案与日期格式化
├── router/index.js           # 路由表（layout / requiresAuth / guestOnly）+ 引导守卫
└── App.vue                   # 按路由 meta.layout 切换外壳 + 浮窗装配
```

---

## 对接的后端接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/users/signup/email` | 注册第一步：发送邮箱验证码（202），入参含 `username`/`email`/`password`/`repassword` |
| POST | `/api/users/signup` | 注册（需 `code`），返回 `accessToken` / `refreshToken` |
| POST | `/api/users/login` | 登录：`{ email, password }` |
| POST | `/api/users/forget-password/email` | 找回密码第一步：校验邮箱已注册并发送验证码 |
| PATCH | `/api/users/forget-password` | 找回密码：`{ email, password, repassword, code }`，成功不签发 token |
| GET | `/api/users/info` | 用户资料 + 画像 `summary` |
| POST | `/api/users/info` | 填写资料（FillInfo）：只写库，**不投递画像任务** |
| PUT | `/api/users/info` | 更新资料（UpdateInfo）：写库 + 后端按改动字段增量更新画像 |
| POST | `/api/users/summary` | 画像增量更新（依据最新互动，异步，202） |
| PUT | `/api/users/summary` | 画像重新生成（首次填写后由前端显式调用，异步，202） |
| GET | `/api/items/recommend` | 推荐内容 `{ items, source }` |
| POST | `/api/interactions` | 互动上报 `{ type, target, detail, long_ts }` |

除 `signup` / `signup/email` / `login` / `forget-password*` 外均需请求头
`Authorization: Bearer <accessToken>`。
请求体的字段语义是指针 + omitempty：只提交要变更的字段，空串会触发校验失败。

发送验证码的接口有两层限流：同 IP 每天 10 次（`429`），同一邮箱 60 秒冷却（`409`，
提示 `已发送验证码，请 Xs 后重试`）。

### 错误响应兼容

后端存在两种错误信封，`src/api/client.js` 的 `extractMessage()` 同时兼容并归一化为可读文案：

- 新版：`{ "kind": "...", "msg": "..." }`（校验错误时 `msg` 为字段映射对象）
- 旧版：`{ "error": "..." }` / `{ "error": { "Username": "..." } }`

---

## 说明与已知依赖

- **推荐接口依赖 Milvus**：`GET /api/items/recommend` 未命中 Redis 精排缓存时需访问 Milvus
  （`:19530`）。Milvus 未启动时接口会返回错误，前端会展示空状态并提供「重新加载」。
- **推荐列表无游标分页**：后端为窗口式推荐，同一次请求可能返回已看过的内容。前端按 `id` 去重后
  顺序追加，**不会因为一轮没有新内容就停止**：每次下滑到底部都会再发一次请求；
  若该轮没有新内容，底部提示「暂时没有更多新内容了 · 继续下滑会再次获取」。
  仅当登录失效（401）或服务不可达时才暂停自动请求，并给出手动「重试」。
- **触底触发时机**：`scroll` 到达底部即请求一次；已贴底时继续向下滚（此时不产生 `scroll` 事件）
  也会请求。请求进行中或 800ms 冷却内不重复发送，避免一次连续滚动里发出多个请求。
- **浏览器的互动上报为「即发即忘」**：卡片首次进入视口记 `view`，点击记 `click`，详情页收藏记 `like`，
  上报失败不影响浏览。
