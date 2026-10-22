# 快速开始

[English](getting-started.md) · [中文](getting-started.zh-CN.md)

本指南带你从全新检出的代码走到可运行的 LitStream feed。最快的路径是用 Docker Compose 跑数据层与后端，前端则使用 Vite 开发服务器。

> `searcher/` 是参考用的 arXiv 生产者，下文用它来为演示目录灌入数据。它**不是必需的**：后端只读取 MySQL 与 Milvus，你可以用任何工具载入自己的物品。数据契约见[数据管道](data-pipeline.zh-CN.md)。

## 1. 前置条件

| 要求 | 说明 |
| --- | --- |
| Docker Desktop / Docker Engine + Compose v2 | `docker compose version` 应能正常执行 |
| 至少 4 GB 空闲内存，推荐 8 GB | Milvus standalone、MySQL、Redis、etcd 和 MinIO 是内存占用的大头 |
| Node.js 18+ 与 Yarn 1.x（或 npm） | 用于 Vue 前端 |
| 一个 DeepSeek API 密钥 | 后端用于生成画像与精排 |
| 一个 DashScope（阿里云百炼）API 密钥 | 用于 embedding；参考 searcher 还用它调用 Qwen 生成摘要 |
| 一个阿里云 DirectMail 发信地址 + AccessKey | 注册时的邮箱验证码需要它 |
| 可选：阿里云 MT 或 Qwen-MT 密钥 | 仅用于参考 searcher 的中文翻译 |

后端仅用 CPU。无需 GPU。

## 2. 各部分运行在哪里

| 进程 | 快速路径中的运行方式 | 地址 |
| --- | --- | --- |
| MySQL、Redis、etcd、MinIO、Milvus | Docker Compose | 内部；Milvus/MinIO 对外发布端口 |
| 后端 + asynq worker | Docker Compose | `http://127.0.0.1:8080` |
| 前端开发服务器 | 宿主机上的 Vite | `http://127.0.0.1:5173` |
| 目录入库 | 一次性 `searcher` 容器（参考）或你自己的载入器 | `tools` profile，或任意外部任务 |

前端不在 `docker-compose.yml` 里。该文件中的 Nginx 配置块已被注释掉，且它引用的文件不在仓库中。开发时请使用 Vite，或用你自己的反向代理托管 `frontend/dist`。

后端不依赖 searcher。如果你已有目录管道，可以跳过 searcher，直接写入相同的物品契约：一行 MySQL `items` 记录，加上一个 ID 相同的 Milvus `(id, vec)`，其中 `vec` 是物品语义文本的 embedding。

## 3. 使用 Docker Compose 的快速路径

### 3.1 创建并填写 `.env`

在仓库根目录执行：

```bash
# macOS / Linux
cp .env.example .env

# Windows PowerShell
Copy-Item .env.example .env
```

然后编辑 `.env`。至少设置：

| 变量 | 原因 |
| --- | --- |
| `ACCESS_TOKEN_SECRET` | 长的随机字符串；用于签发 access JWT |
| `REFRESH_TOKEN_SECRET` | 长的随机字符串；用于签发 refresh JWT |
| `MODEL_API_KEY` | 用于生成画像与精排的 DeepSeek 密钥 |
| `MODEL_NAME` | 你的 DeepSeek 密钥可访问的模型 |
| `EMBEDDING_MODEL_API_KEY` | 用于 embedding 的 DashScope 密钥 |
| `EMBEDDING_MODEL_NAME` | 必须与 `VEC_DIM` 及 collection 匹配 |
| `EMBEDDING_MODEL_BASE_URL_WITH_OPENAI` | DashScope 的 OpenAI 兼容 base URL |
| `ALIYUN_ACCESS_KEY_ID` / `ALIYUN_ACCESS_KEY_SECRET` | 用于注册邮件的 DirectMail 凭据 |
| `MAIL_ACCOUNT_NAME` | 已验证的 DirectMail 发信地址 |
| `SUMMARY_MODEL_API_KEY` | searcher 使用的 Qwen MaaS 端点密钥 |

`MYSQL_DSN`、`REDIS_ADDRESS` 和 `MILVUS_ADDRESS` 请保持 Compose 的默认值，除非你也同时修改了 Compose 的服务名或密码。

> **Compose 中的 MySQL 密码是开发默认值。** 它在 `docker-compose.yml` 和示例 DSN 中被硬编码为 `123456`。任何非本地部署都应同时修改这两处。

完整变量参考见[配置](configuration.zh-CN.md)。

### 3.2 启动整套服务

```bash
docker compose up -d --build
docker compose ps
docker compose logs -f backend
```

等到 MySQL、Redis 和 Milvus 都报告健康。Milvus 首次启动可能需要 60–90 秒。

检查 API：

```bash
curl http://127.0.0.1:8080/hello
# {"message":"Hello, World!"}
```

后端以只读方式把根目录的 `.env` 挂载到 `/app/.env`；你可以修改它并重启后端，而无需重新构建镜像：

```bash
docker compose restart backend
```

### 3.3 灌入目录数据（必需）

空的 Milvus collection 不会产生友好的空 feed；召回会失败，API 返回 500。你需要 MySQL 中至少有一条物品记录，以及 Milvus 中与之匹配的向量。

下面的命令使用参考的 arXiv searcher。如果你已有目录管道，请改为载入自己的物品；后端只要求满足物品契约（MySQL 与 Milvus 中同一 ID、语义文本、匹配的 embedding 模型与维度）。

```bash
docker compose --profile tools run --rm searcher \
  -config /app/.env \
  -date 2025-01-15 \
  -max 200
```

- `-date` 使用 arXiv 的 `submittedDate`，按**美国东部时间**解释。
- `-max 200` 让首次运行保持较小规模；省略它则入库一整天。
- searcher 镜像是单次任务（`restart: "no"`），执行完即退出。
- 如果只想看会抓取什么，先加 `-dry-run`。

检查入库结果：

```bash
docker compose --profile tools run --rm searcher \
  -config /app/.env -op stats
```

### 3.4 启动前端

在另一个终端中：

```bash
cd frontend
yarn install
yarn dev
```

打开 `http://127.0.0.1:5173`。

Vite 开发服务器把 `/api` 代理到 `http://127.0.0.1:8080`（见 `frontend/vite.config.js`）。如果你使用不同的后端端口，请在 `frontend/.env.development` 中设置 `VITE_API_PROXY_TARGET`。

### 3.5 首次运行检查清单

1. 用邮箱注册；请求 6 位验证码，然后完成注册。
   - 密码规则：8–20 位字母/数字。用户名：2–15 个非空格字符。
   - 验证码有效期 5 分钟；同一邮箱 60 秒内不能再次请求验证码。
2. 填写年龄、领域、组织，以及至少一个兴趣。提交。
3. 等待画像生成任务。UI 会轮询 `/api/users/info`，直到 `summary` 出现。
4. 把 feed 滚动到底部。
   - 第一个窗口通常是 `source=coarse`。
   - 异步精排完成后，下一个窗口通常是 `source=llm`。
5. 打开一张卡片、给物品点赞，并滚动几个窗口。这些交互会供给增量画像更新。

## 4. 不带后端容器的本地开发

### 4.1 数据服务

你需要能在 `localhost` 上访问 MySQL 8、Redis 7 和 Milvus。有两种选择：

**方案 A —— 宿主机安装的服务。** 本地安装 MySQL 与 Redis；用 Milvus standalone 自带的 Compose 文件或二进制运行 Milvus。

**方案 B —— 复用 Compose 数据层并发布端口。** 创建一个临时的 `docker-compose.override.yml`：

```yaml
services:
  mysql:
    ports:
      - "127.0.0.1:3306:3306"
  redis:
    ports:
      - "127.0.0.1:6379:6379"
```

然后：

```bash
docker compose up -d mysql redis etcd minio milvus
docker compose stop backend   # free port 8080 for the local Go process
```

### 4.2 后端

创建 `backend/.env`，填写本地地址。不要提交它。

```dotenv
SERVER_ADDRESS=:8080
MYSQL_DSN=new_user:123456@tcp(127.0.0.1:3306)/litstream?charset=utf8mb4&parseTime=True&loc=Asia%2FShanghai&interpolateParams=true
REDIS_ADDRESS=127.0.0.1:6379
MILVUS_ADDRESS=127.0.0.1:19530
# ... plus the same model, embedding, and mail keys as the root .env
```

后端启动前数据库必须已存在：

```sql
CREATE DATABASE litstream CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
```

运行 API 与 worker（同一进程）：

```bash
cd backend
go run ./cmd
```

### 4.3 前端

```bash
cd frontend
yarn install
yarn dev
```

### 4.4 Searcher

searcher 默认读取 `../backend/.env`，这在本地开发时很方便：

```bash
cd searcher
go run . -date 2025-01-15 -max 200
```

如果你只维护一份根目录的 `.env`，请显式传入：

```bash
go run . -config ../.env -date 2025-01-15 -max 200
```

注意根目录的 `.env` 使用的是 Compose 主机名（`mysql`、`redis`、`milvus`），这些主机名在宿主机上无法解析。宿主机上运行的进程请使用 localhost 值。

## 5. 生产构建

```bash
cd frontend
yarn install
yarn build          # outputs frontend/dist
```

把 `dist` 作为静态 SPA 提供服务，并在同一源上把 `/api` 代理到后端。Nginx 配置示例：

```nginx
server {
    listen 80;
    server_name example.com;

    root /srv/yustream/dist;
    index index.html;

    location / {
        try_files $uri $uri/ /index.html;
    }

    location /api/ {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
```

由于后端在 Compose 中绑定 `127.0.0.1:8080`，反向代理应运行在同一台主机上，或通过隧道访问它。请添加 TLS（Nginx/Caddy），并且不要把 MySQL、Redis 或 MinIO 暴露到公网。

## 6. 参考 searcher 命令参考

这些命令专用于 arXiv 参考生产者。如果你载入自己的目录数据，请忽略本节。

在本地从 `searcher/` 目录运行它们，或在 Docker 中给命令加上前缀 `docker compose --profile tools run --rm searcher -config /app/.env`。

| 任务 | 命令 |
| --- | --- |
| 入库一天 | `go run . -date 2025-01-15` |
| 入库一个日期区间（含首尾） | `go run . -from 2025-01-01 -to 2025-01-31` |
| 限制单次运行数量 | `go run . -date 2025-01-15 -max 200` |
| 只检查而不调用 LLM/不写入 | `go run . -date 2025-01-15 -dry-run` |
| 统计记录数 | `go run . -op stats` |
| 列出已收录的日期 | `go run . -op dates` |
| 删除某一天 | `go run . -op delete-date -date 2025-01-15 -yes` |
| 清空所有数据 | `go run . -op purge -yes` |
| 重新生成翻译 | `go run . -op regen-translate -date 2025-01-15 -yes` |
| 重新生成摘要 | `go run . -op regen-summary -date 2025-01-15 -yes` |
| 重新生成向量 | `go run . -op regen-embedding -date 2025-01-15 -yes` |

破坏性/重生成操作必须加 `-yes`；不加时命令只报告影响范围。

重生成顺序很重要：向量是根据摘要计算的，因此更换摘要模型后应先运行 `regen-summary`，再运行 `regen-embedding`。

## 7. 故障排查

| 现象 | 可能原因 | 处理 |
| --- | --- | --- |
| `docker compose ps` 显示 MySQL 不健康 | 密码/DSN 不匹配，或首次初始化仍在进行 | 查看 `docker compose logs mysql`；保持 `MYSQL_DSN` 与 Compose 凭据一致 |
| 后端退出并报 `Can't find the file .env` | 进程工作目录下缺少 `.env` | Compose 会挂载根目录的 `.env`；本地后端需要 `backend/.env` |
| 连接 Milvus 报 `connect: connection refused` | Milvus 尚未就绪，或地址错误 | 等待其健康；检查 `MILVUS_ADDRESS` |
| `collection not loaded` | Milvus 已启动，但 collection 加载还未完成 | 等 Milvus 健康后重启后端；该服务的加载是幂等的 |
| `embedding 维度不符` | `VEC_DIM` 与模型/collection 不匹配 | 设置正确的维度，重建 collection，重新入库 |
| 首次运行时 feed 为空或返回 500 | 尚未入库任何目录数据 | 运行 searcher 或你自己的载入器；核对 MySQL 行数与 Milvus 向量数 |
| `LLM API 返回 401` | 后端 LLM 密钥错误，或用了其他服务商的密钥 | 检查 `MODEL_API_KEY`；后端端点仅支持 DeepSeek |
| 注册验证码始终收不到 | DirectMail 发信地址未验证、缺少权限，或触发限流 | 核对 `MAIL_ACCOUNT_NAME`，检查 RAM 策略，等待 60 秒，查看垃圾邮件 |
| 翻译缺失 | 未配置翻译服务商/密钥 | 设置 `TRANSLATE_PROVIDER` 及对应凭据；见[配置](configuration.zh-CN.md) |
| 生产环境中刷新页面后前端请求 404 | SPA history 模式未被重写 | 添加 `try_files $uri $uri/ /index.html;` |
| 浏览器报 CORS | 前端与 API 不同源 | 开发时使用 Vite 代理，生产时使用 Nginx 的 `/api` 代理 |
| 找不到 `yarn` | 未安装 Yarn 1.x | `npm install -g yarn`，或改用 `npm install` / `npm run dev` |
| 在 `backend/test` 下 `go build ./...` 失败 | `test/` 中是临时程序，不是常规包 | 用 `go build ./cmd` 构建/运行应用；只运行针对性测试 |

## 8. 生产注意事项

- 修改默认的 MySQL 密码与所有密钥。永不提交 `.env`。
- 在 API 前放置 TLS 与反向代理；让 MySQL、Redis、Milvus 和 MinIO 远离公网。
- 备份 MySQL（用户/物品元数据）和 Milvus（向量）。Redis 大部分可重建，但丢失它会丢掉排队中的任务、交互缓冲和缓存。
- 有意识地设置 Redis 的 `maxmemory`/淘汰策略。淘汰队列或 stream key 与淘汰缓存并不等价。
- 关注 LLM token 用量：每次 LLM 响应都是一次精排调用，每第五次响应还会附加一次画像更新。入库则为每条物品增加一次摘要（以及可选的翻译）。
- `SEARCH_TOP_K`、`LIMIT_TOP_K`、摘要长度和更新频率是主要的成本/延迟调节点。
- 后端没有内置的指标端点。大规模部署前请先接入 Prometheus/OpenTelemetry。
