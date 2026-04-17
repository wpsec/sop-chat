# SOP Chat

阿里云 SLS 和 CMS 智能问答助手的客户端应用，提供独立的 Web UI、OpenAI 兼容接口、钉钉/飞书/企业微信渠道接入，并支持 `builtin` 本地账号与 `OIDC / IDaaS` 登录，让你无需二次开发即可快速落地 SOP 智能对话能力。

## 分支说明

当前分支为**独立维护分支**，用于承载本地定制功能、界面改造和与上游主分支不同的业务需求。

这意味着：

- 本分支的功能范围、默认配置和界面表现可能与上游主分支不同
- 本分支会自行维护所需功能，不要求上游主分支合并这些改动
- 上游主分支中的通用修复、问题修复和部分优化，后续会按需选择性同步到本分支

如果你在阅读上游文档、历史截图或其它分支说明时发现行为不一致，请优先以**当前分支的 README 和代码**为准。

## 主要功能
- **独立 Web UI** — 开箱即用的聊天界面，支持 Markdown 渲染、多会话管理、SSE 流式输出和工具调用过程可见
- **多平台机器人对接** — 支持钉钉、飞书、企业微信三大 IM 平台接入 SOP Agent，同一服务可同时运行多个机器人实例
  - **钉钉** — 基于 DingTalk Stream SDK，无需公网 IP，支持群内 @机器人 及单聊
  - **飞书** — 基于飞书 WebSocket 长连接，无需公网 IP，支持群聊与单聊
  - **企业微信** — 支持应用回调模式和群机器人长连接模式
- **定时任务（Cron）** — 支持配置多个定时任务，定期向指定数字员工发起提问，并将回答自动推送至钉钉、飞书或企业微信 Webhook
- **OpenAI 兼容接口** — 暴露 `/openai/v1/chat/completions` 接口，方便使用 Cherry Studio、ChatBox 等兼容 OpenAI 协议的聊天客户端直接接入
- **可视化配置管理** — 内置 `admin-ui` 配置页，启动后直接在浏览器中完成配置维护，无需先手工编辑 `config.yaml`
- **认证与用户** — 支持 `builtin` 本地登录、`OIDC / IDaaS` 单点登录、本地 JWT 会话，以及 `builtin` 用户 Excel 模板下载与批量导入
- **AI 幻觉抑制基线** — 主聊天链路已接入“有据再答、无据说明”的回答约束；高风险问题要求保守回答；前端可展示“含依据 / 依据不足”提示

## 当前支持内容

| 能力域 | 当前支持情况 |
|------|------|
| Web 对话 | 独立登录页、多会话、SSE 流式输出、Markdown 渲染、工具调用过程可见 |
| 配置管理 | 内置 `http://<host>:<port>/admin-ui?token=...` 配置页，支持保存认证、渠道、定时任务、数字员工等配置 |
| 认证方式 | `builtin` 本地账号登录、`OIDC / IDaaS` 登录/回调/退出、本地 JWT 会话 |
| 本地用户存储 | `yaml` 兼容模式与 `SQLite` 模式；`SQLite` 适合长期保留少量兜底管理员账号 |
| 用户导入 | 支持下载 `builtin` 用户导入模板、上传 `xlsx` 批量导入、查看导入结果与错误明细 |
| IM 渠道 | 钉钉 Stream、飞书 WebSocket、企业微信应用回调、企业微信群机器人长连接 |
| 调度能力 | 基于 cron 的定时提问、Webhook 推送、配置页立即触发测试 |
| OpenAI 兼容 | 标准 `/openai/v1/chat/completions` 接口，可对接第三方客户端 |
| AI 幻觉抑制 | 已完成 Prompt 规则基线和前端提示态；证据绑定、来源引用和评测回归仍在继续完善 |

## 快速开始（推荐）

### 1. 下载二进制

从 [Releases](../../releases) 页面下载对应平台的二进制文件：

| 平台 | 文件名 |
|------|--------|
| Linux x86_64 | `sop-chat-server-linux-amd64` |
| macOS Intel | `sop-chat-server-darwin-amd64` |
| macOS Apple Silicon | `sop-chat-server-darwin-arm64` |

### 2. 启动服务

```bash
# 赋予执行权限（macOS / Linux）
chmod +x sop-chat-server

# 前台运行（默认，日志直接输出到终端，Ctrl+C 退出）
./sop-chat-server

# 后台守护进程模式运行（日志写入 logs/sop-chat-server.log）
./sop-chat-server --daemon
```

启动后终端会输出配置管理 UI 的访问地址，形如 `http://<host>:<port>/admin-ui?token=...`。首次启动建议优先完成以下配置：

- 选择认证方式：`builtin`、`oidc` 或两者并存
- 如使用本地账号，优先将 `builtin` 存储切换为 `SQLite`
- 如使用企业身份系统，填写 `OIDC / IDaaS` 的 `issuerURL`、`clientId`、`clientSecret`
- 按需下载 `builtin` 导入模板并批量导入本地兜底账号

#### 守护进程常用命令

```bash
# 查看管理 UI 地址
./sop-chat-server adminurl

# 停止后台进程
./sop-chat-server stop
```

> **说明：** 前台模式同样会写入 `logs/sop-chat-server.pid` 和 `logs/sop-chat-server.url`，
> 因此 `adminurl` / `stop` 子命令在前台和后台两种模式下均可用。

---

## 阿里云前置配置

在使用前，需要在阿里云侧完成以下授权配置。

### 1. AK账号需要的权限策略（AccessKey 所属账号）：

```json
{
  "Version": "1",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "cms:CreateChat",
        "cms:GetDigitalEmployee",
        "cms:ListDigitalEmployees",
        "cms:GetThread",
        "cms:GetThreadData",
        "cms:ListThreads",
        "cms:CreateDigitalEmployee",
        "cms:UpdateDigitalEmployee",
        "cms:DeleteDigitalEmployee",
        "cms:CreateThread",
        "cms:UpdateThread",
        "cms:DeleteThread"
      ],
      "Resource": [
        "acs:cms:*:*:digitalemployee/*",
        "acs:cms:*:*:digitalemployee/*/thread/*"
      ]
    },
    {
      "Effect": "Allow",
      "Action": "ram:PassRole",
      "Resource": "*",
      "Condition": {
        "StringEquals": {
          "acs:Service": "cloudmonitor.aliyuncs.com"
        }
      }
    }
  ]
}
```

> **提示**：可将 `ram:PassRole` 的 Resource 限制为第一步创建的 RAM 角色 ARN。

---

## 手动构建

### 环境要求

- Go 1.25+
- Node.js 18+

### 一键构建（推荐）

```bash
# 构建当前平台的二进制（前端已嵌入）
make build

# 仅构建 Linux amd64 版本
make build-linux

# 多平台构建（Linux + macOS）
make build-all
```

**产物：**
- 单平台：`backend/sop-chat-server`、`backend/sop-chat-cli`
- Linux：`dist/linux/sop-chat-server`、`dist/linux/sop-chat-cli`
- 多平台：`dist/linux/sop-chat-server`、`dist/darwin/sop-chat-server`、`dist/darwin/sop-chat-server-arm64`

### 其他构建命令

```bash
make build-frontend  # 仅构建前端
make build-backend   # 仅构建后端（需先构建前端）
make build-cli       # 仅构建 CLI 工具
make build-linux     # 构建 Linux amd64 服务端和 CLI
make clean           # 清理所有构建产物
make clean-dist      # 仅清理多平台构建产物
```

### 分开构建

```bash
# 前端
cd frontend
npm install
npm run build

# 复制前端静态资源到后端 embed 目录
cd ..
mkdir -p backend/internal/embed/frontend
rm -rf backend/internal/embed/frontend/*
cp -r frontend/dist/* backend/internal/embed/frontend/

# 后端
cd backend
go mod download
go build -o sop-chat-server ./cmd/sop-chat-server
go build -o sop-chat-cli ./cmd/sop-chat-cli
```

### Linux 服务器从源码构建

如果你的测试服务器上只有源码，没有 Go 环境，那么**不能直接在服务器上执行 `go build`**。可选两种方式：

#### 方式一：在服务器安装构建环境后直接构建

```bash
# 1. 安装 Go 和 Node.js（版本要求见上文）
# 2. 在项目根目录执行
make build
```

构建完成后产物位于：

- `backend/sop-chat-server`
- `backend/sop-chat-cli`

#### 方式二：在本地构建 Linux 二进制后上传到服务器

这是更推荐的方式，尤其适合测试机、生产机或不希望在服务器安装完整开发环境的场景。

在本地开发机执行：

```bash
# 构建 Linux amd64 版本
make build-linux

# 构建产物
ls -lh dist/linux/sop-chat-server
```

将二进制上传到服务器后执行：

```bash
chmod +x sop-chat-server
./sop-chat-server
```

推荐上传这些文件：

- `dist/linux/sop-chat-server`
- `backend/config.yaml.example`

例如在本地执行：

```bash
scp dist/linux/sop-chat-server root@<server>:/opt/sop-chat/
scp backend/config.yaml.example root@<server>:/opt/sop-chat/
```

在服务器执行：

```bash
cd /opt/sop-chat
cp config.yaml.example config.yaml
chmod +x sop-chat-server
./sop-chat-server
```

### 服务器手动构建命令示例

以下命令适用于**服务器已经安装好 Go 和 Node.js**，并且你希望在源码目录直接构建：

```bash
# 项目根目录
cd /path/to/sop-chat

# 构建前端
cd frontend
npm install
npm run build

# 复制前端资源到后端 embed 目录
cd ..
mkdir -p backend/internal/embed/frontend
rm -rf backend/internal/embed/frontend/*
cp -r frontend/dist/* backend/internal/embed/frontend/

# 构建后端和 CLI
cd backend
go mod download
go build -o sop-chat-server ./cmd/sop-chat-server
go build -o sop-chat-cli ./cmd/sop-chat-cli
```

如果只是为了在 Linux 服务器上运行，实际上更直接的做法是在本地执行：

```bash
make build-linux
```

然后把 `dist/linux/sop-chat-server` 上传到服务器运行，不需要在服务器安装 Go。

如果你的服务器上执行 `go` 报：

```bash
bash: go: command not found
```

说明当前机器尚未安装 Go，不能直接源码构建。此时应选择：

1. 先安装 Go 和 Node.js，再执行上面的构建命令
2. 在本地执行 `make build-linux`，然后上传 `dist/linux/sop-chat-server` 到服务器运行

### 开发模式

```bash
# 后端（热重载）
cd backend
go run cmd/sop-chat-server/main.go

# 前端（Vite 开发服务器，http://localhost:5173）
cd frontend
npm install
npm run dev
```

---

## 架构说明

```
┌──────────────────────────────────────────────────────────────┐
│          用户 / 钉钉群 / 飞书群 / 企业微信                    │
└───┬──────────────┬──────────────┬──────────────┬─────────────┘
    │ HTTP/SSE      │ DingTalk     │ Feishu       │ WeCom
    │               │ Stream SDK   │ WebSocket    │ Callback
    ▼               ▼              ▼              ▼
┌──────────────────────────────────────────────────────────────┐
│                      SOP Chat Server                         │
│                                                              │
│  ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌──────────┐  │
│  │  Web UI    │ │  钉钉机器人 │ │  飞书机器人 │ │企业微信  │  │
│  │ (内嵌前端) │ │(Stream模式)│ │(WebSocket) │ │机器人    │  │
│  └────────────┘ └────────────┘ └────────────┘ └──────────┘  │
│                                                              │
│  ┌──────────────────────────────────────────────────────┐   │
│  │       认证 / 配置管理 / API 路由 / OpenAI 兼容接口    │   │
│  └──────────────────────────────────────────────────────┘   │
└──────────────────────────┬───────────────────────────────────┘
                           │ API Calls
                           ▼
┌─────────────────────────────────────────────────────────────┐
│                        SOP Agent                            │
│                     (阿里云云监控)                           │
│                                                             │
│  ┌──────────────────────────────────────────────────────┐  │
│  │              ReAct Loop（Agent 运行时）               │  │
│  │                                                      │  │
│  │    ┌────────┐         ┌──────────────────────┐     │  │
│  │    │        │────────▶│   SOP 知识库          │     │  │
│  │    │  AI    │         └──────────────────────┘     │  │
│  │    │ Agent  │                                       │  │
│  │    │ (角色) │         ┌──────────────────────┐     │  │
│  │    │        │────────▶│  SLS & OpenAPI 工具   │     │  │
│  │    └────────┘         └──────────────────────┘     │  │
│  │                                                      │  │
│  │         （推理 → 行动 → 观察）                       │  │
│  └──────────────────────────────────────────────────────┘  │
│                                                             │
│  认证方式：RAM 角色                                          │
└─────────────────────────────────────────────────────────────┘
```

**核心组件：**

| 组件 | 说明 |
|------|------|
| SOP Chat Frontend | React 前端，构建后嵌入二进制 |
| SOP Chat Server | Go 后端，负责认证、会话管理、多平台机器人对接 |
| 配置管理页面 | 内置 `admin-ui` 页面，启动时打印带 token 的访问地址 |
| 认证与用户 | 支持 `builtin`、`OIDC / IDaaS`、JWT 会话、`SQLite builtin` 和 Excel 导入 |
| SOP Agent | 阿里云云监控数字员工，基于 ReAct 框架 |
| 钉钉机器人 | 通过 DingTalk Stream SDK 接入，无需公网 IP，支持串行会话隔离 |
| 飞书机器人 | 通过飞书 WebSocket 长连接接入，无需公网 IP，支持群聊与单聊 |
| 企业微信机器人 | 同时支持应用回调与群机器人长连接两种接入方式 |
| 定时任务调度器 | 基于 cron 表达式定时提问数字员工，结果推送至 Webhook |
| OpenAI 兼容接口 | 暴露标准 `/openai/v1/chat/completions`，可对接第三方工具 |
| AI 幻觉抑制 | 已接入规则基线、高风险回答约束和“含依据 / 依据不足”前端提示 |

---

## 项目结构

```
sop-chat/
├── backend/
│   ├── cmd/
│   │   ├── sop-chat-server/   # 服务端入口
│   │   └── sop-chat-cli/      # CLI 调试工具
│   ├── internal/
│   │   ├── api/               # HTTP 路由与处理器
│   │   ├── auth/              # 认证逻辑
│   │   ├── config/            # 配置结构
│   │   ├── dingtalk/          # 钉钉机器人
│   │   ├── feishu/            # 飞书机器人
│   │   ├── scheduler/         # 定时任务调度器（cron）
│   │   └── wecom/             # 企业微信机器人
│   └── pkg/sopchat/           # SOP Agent SDK 封装
└── frontend/
    └── src/
        ├── components/        # React 组件
        └── services/          # API 客户端
```

---

## 定时任务（Cron）

定时任务允许你按计划自动向指定数字员工发起提问，并将回答推送至钉钉、飞书或企业微信群机器人 Webhook。

在配置管理 UI 的 **「定时任务」** 标签页中可直接管理，也可手动编辑 `config.yaml`：

```yaml
scheduledTasks:
  - name: daily-standup          # 任务唯一标识
    enabled: true
    cron: "0 9 * * 1-5"          # 工作日 09:00，标准 5 字段 cron 表达式
    prompt: "今天有哪些需要关注的告警？"
    employeeName: apsara-ops     # 数字员工名称
    webhook:
      type: dingtalk             # 平台：dingtalk | feishu | wecom
      url: "https://oapi.dingtalk.com/robot/send?access_token=xxx"
      msgType: text              # text | markdown（钉钉/飞书）；text | markdown | post（飞书）
```

**cron 表达式说明（5 字段，秒级可选）：**

| 示例 | 含义 |
|------|------|
| `*/5 * * * *` | 每 5 分钟 |
| `*/15 * * * *` | 每 15 分钟 |
| `0 9 * * *` | 每天 09:00 |
| `0 9 * * 1-5` | 工作日 09:00 |
| `0 9,18 * * 1-5` | 工作日 09:00 和 18:00 |

**支持的 Webhook 类型：**

| type | 说明 |
|------|------|
| `dingtalk` | 钉钉自定义机器人，支持 `text` / `markdown` 消息类型 |
| `feishu` | 飞书自定义机器人，支持 `text` / `post` / `markdown` 消息类型 |
| `wecom` | 企业微信群机器人，支持 `text` / `markdown` 消息类型 |

> 在配置管理 UI 中可对任务进行**立即触发测试**，无需等待定时到来即可验证配置是否正确。

---

## 认证与用户说明

当前认证与用户体系按“主认证 + 本地兜底”设计，已经支持以下能力：

- `auth.methods` 可配置为 `builtin`、`oidc` 或两者并存
- 登录页会根据可用入口自动显示“本地账号登录”或“使用 IDaaS 登录”
- `OIDC / IDaaS` 已支持登录、回调和退出链路
- `builtin` 支持 `yaml` 和 `SQLite` 两种存储模式
- `builtin` 用户可通过配置页下载模板并用 `xlsx` 批量导入

推荐做法：

- 日常用户优先走 `OIDC / IDaaS`
- 本地只保留少量 `SQLite builtin` 管理员作为兜底账号
- 具体模板说明见 [docs/用户导入模板说明.md](docs/用户导入模板说明.md)
- 认证与接入规划见 [docs/用户导入与IDaaS接入计划.md](docs/用户导入与IDaaS接入计划.md)

---

## AI 幻觉抑制现状

当前版本已经开始把 AI 防幻觉作为系统能力建设，而不只是单条 Prompt 调整。

目前已经落地：

- 主聊天链路统一附加“有据再答、无据说明”的回答约束
- 高风险问题要求尽量按“结论 / 依据 / 不确定项 / 下一步建议”回答
- 前端可识别并提示“含依据”或“依据不足”

当前还未完全落地：

- 基于本地 `docs/`、代码、配置的强制证据绑定
- 回答中的真实来源列表展示
- 幻觉样本评测与持续回归

详细方案见 [docs/AI幻觉抑制方案.md](docs/AI幻觉抑制方案.md)。

---

## License

本项目基于 Apache-2.0 协议开源，详见 LICENSE 文件。
