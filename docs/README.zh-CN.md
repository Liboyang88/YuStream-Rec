# YuStream 文档

[English](README.md) · [中文](README.zh-CN.md)

YuStream 是一个提示词优先、向量原生的框架，用于垂类产品的流式推荐。**LitStream** 是本仓库中的参考应用：一个 arXiv 论文推荐器。

代码以及部分标识符仍使用 `litstream` 这个名字（模块名、Docker 项目名、数据库名）。请把 `litstream` 视为参考实现，把 `YuStream` 视为它所演示的框架。

## 什么是必需的，什么不是

- **必需：** 推荐闭环、用户/画像模型、HTTP API，以及满足物品契约的物品库。
- **必需的数据契约：** `原始物品信息 → 物品语义文本 → 嵌入向量`，写为 MySQL 的 `items` 行与 Milvus 的 `(id, vec)`，且两者 ID 相同。
- **非必需：** `searcher/`，它是面向 arXiv 的参考生产者。你的物品库可以来自官方 API、爬虫、CMS 导出、CSV、消息队列或内部任务；用户业务，本项目的用户业务较为简陋，你更面临的需要是将本框架的推荐服务拓展到你原有业务当中。
- **两种构建方式：** 新增垂类案例（案例扩展）或改动核心闭环（框架优化）。

## 从这里开始

| 文档 | 回答什么问题 |
| --- | --- |
| [快速开始](getting-started.zh-CN.md) | 如何在本地或使用 Docker Compose 运行 LitStream？ |
| [配置](configuration.zh-CN.md) | 每个 `.env` 变量的作用是什么？哪些是必填的？ |
| [架构](architecture.zh-CN.md) | 推荐闭环如何工作？为什么这样设计？ |
| [技术栈](tech-stack.zh-CN.md) | 使用了哪些框架、服务和版本？ |
| [后端结构](backend-structure.zh-CN.md) | 路由、业务逻辑、存储、提示词或 worker 应该在哪里修改？ |
| [前端结构](frontend-structure.zh-CN.md) | Feed、认证流程、卡片或交互应该在哪里修改？ |
| [数据管道](data-pipeline.zh-CN.md) | 入库契约是什么？参考 arXiv 管道如何工作？ |
| [HTTP API](api.zh-CN.md) | 有哪些端点、请求负载和响应语义？ |
| [垂类复用指南](reuse-guide.zh-CN.md) | 如何把 YuStream 适配到课程、招聘、商品或其他垂类？ |
| [扩展与优化](extending.zh-CN.md) | 如何安全地扩展一个案例或改动框架核心？ |

English docs: [docs/README.md](README.md).

## 阅读路径

**我想把它跑起来**

1. [快速开始](getting-started.zh-CN.md)
2. [配置](configuration.zh-CN.md)
3. [数据管道](data-pipeline.zh-CN.md) —— 打开 feed 之前先灌入物品库。

**我想理解设计**

1. [架构](architecture.zh-CN.md)
2. [技术栈](tech-stack.zh-CN.md)
3. [后端结构](backend-structure.zh-CN.md) 与[前端结构](frontend-structure.zh-CN.md)

**我想把它复用到另一个垂类**

1. [垂类复用指南](reuse-guide.zh-CN.md)
2. [数据管道](data-pipeline.zh-CN.md) —— 入库契约；`searcher/` 只是参考
3. [架构](architecture.zh-CN.md) —— 哪些部分是框架级、哪些是领域级

**我想改动框架本身**

1. [扩展与优化](extending.zh-CN.md) —— 契约、迁移，以及高杠杆的改动
2. [架构](architecture.zh-CN.md) —— 闭环与失效模式
3. [后端结构](backend-structure.zh-CN.md) —— 分层边界

**我想参与贡献**

1. [架构](architecture.zh-CN.md) —— 先读闭环和失效模式
2. [后端结构](backend-structure.zh-CN.md) 或[前端结构](frontend-structure.zh-CN.md)
3. [HTTP API](api.zh-CN.md) —— 保持契约稳定

## 这些文档中的约定

- **YuStream** —— 框架与设计模式。
- **LitStream** —— 本仓库中的 arXiv 应用。
- **窗口（Window）** —— `GET /api/items/recommend` 返回的一批推荐。"流式"指无限延续的窗口序列，而不是逐 token 的流式输出。
- **粗排（Coarse）** —— 不调用 LLM 的向量召回（`source=coarse`）。
- **精排（Reranked）** —— 由 LLM 选出、从推荐缓存返回的窗口（`source=llm`）。
- **画像（Profile）** —— 自然语言的 `summary` 加上短期 `interest_topics`。只有 `interest_topics` 会被嵌入用于召回。
- **item 契约（Item contract）** —— [数据管道](data-pipeline.zh-CN.md#数据契约)中描述的 MySQL 行 + Milvus 向量映射。

## 当前状态

YuStream 是一个研究与工程原型。推荐闭环、认证、入库和 Docker 搭建均已实现。性能与成本基准**尚未公布**；参见[主 README](../README.zh-CN.md#4-性能测试)中的占位说明。代码中还包含若干刻意记录在案的粗糙之处，列在[架构](architecture.zh-CN.md#9-已知限制)与[后端结构](backend-structure.zh-CN.md#10-已知的粗糙之处)的末尾。

文档以英文和中文维护。英文页面使用 `*.md`；中文页面使用 `*.zh-CN.md`。每个页面顶部都有语言切换。
