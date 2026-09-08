# DCPro 前端

基于 Vue 3 + Vite + Pinia + Vue Router 的学术文献推荐前端，对接 `dcpro` 后端。

## 技术栈

- Vue 3（`<script setup>` 组合式 API）
- Vite 5 + Yarn
- Pinia（auth / items 两个 store）
- Vue Router 4
- axios（JWT 拦截器）

## 页面

| 路由 | 页面 | 说明 |
|---|---|---|
| `/login` | 登录/注册 | 登录 + 注册（注册后跳转信息填写） |
| `/info` | 信息填写 | 年龄/性别/职业/学历/所在地/组织/兴趣领域 |
| `/` | 主页（推荐流） | 小红书式卡片流 + 侧边栏 |
| `/item/:id` | 详情页 | 完整信息 + 跳转原文 + 点赞 |

## 运行

```bash
# 1. 安装依赖
yarn install

# 2. 启动开发服务器（默认 5173，/api 代理到后端）
yarn dev

# 3. 构建
yarn build
```

## 后端对接说明

- 后端地址通过 `.env.development` 的 `VITE_API_PROXY_TARGET` 配置（默认 `http://localhost:8080`），vite 会代理 `/api` 请求，避免跨域。
- 登录成功后 token 存 `localStorage`，axios 请求拦截器自动带 `Authorization: Bearer <token>`。
- 交互（点击/点赞）通过 `POST /api/interactions` 上报，body 为 `{ type, target, detail, long_ts }`。

## 已知缺口（需后端配合）

1. **原文链接**：后端 `Item` 结构体没有 URL 字段，详情页「跳转原文」目前用标题构造 arXiv 检索链接兜底。建议给 `Item` 加一个 `link` 字段（arXiv 抓取脚本里可存 `http://arxiv.org/abs/<id>`）。
2. **按 id 查单条**：后端没有 `GET /items/:id` 接口，详情页靠前端内存 store 传递完整 item，直接刷新详情页会回主页。
