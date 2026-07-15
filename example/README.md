# AGGO 多 Agent 智能导诊平台

基于 AGGO、Eino、Go、MySQL 和嵌入式 HTML 页面实现的三端智能导诊系统，包含患者端、医生端和管理端。

> 本项目用于分诊辅助与流程协同，不提供医疗诊断，不能替代专业医疗服务或紧急救援。

## 系统入口

服务默认运行在 `http://localhost:8080`。

| 端口 | 入口 | 主要用途 |
| --- | --- | --- |
| 患者端 | `http://localhost:8080/patient/login` | 手机验证码登录、健康档案、多轮导诊、报告、预约与地图导航 |
| 医生端 | `http://localhost:8080/doctor/login` | 待处理患者、风险报告、协同诊断、人工复核、急诊通道与预约处理 |
| 管理端 | `http://localhost:8080/admin/login` | 导诊分析、Agent 轨迹、工单、预约监控、风险规则、账号权限与系统配置 |

登录账号由运行环境变量控制。请在父目录的 `.env` 中设置 `DOCTOR_USERNAME`、`DOCTOR_PASSWORD`、`ADMIN_USERNAME`、`ADMIN_PASSWORD`，不要把真实账号、密码、模型密钥或地图 AK 写入 README 或提交到 Git。

## 核心业务闭环

```text
患者验证码登录
  -> 填写/维护健康档案
  -> 发起多轮智能导诊
  -> 多 Agent 完成意图识别、风险评估、科室推荐和结果复核
  -> 保存导诊记录与 Agent 轨迹
  -> 按需要创建人工复核工单或预约挂号
  -> 医生处理患者、工单和预约
  -> 管理员查看分析、轨迹、审计与系统配置
```

### 多 Agent 分诊流程

- 意图识别：提取就诊诉求和症状重点。
- 病史摘要：融合患者档案与本轮对话上下文。
- 风险评估：输出 `P1`、`P2`、`P3` 风险等级。
- 科室推荐：给出推荐科室、备选科室和就诊准备事项。
- 条件分支：按需执行用药安全分析或急症识别。
- 监督汇总与回答复核：生成面向患者的可读结果，并保留结构化记录和处理轨迹。

患者年龄、性别、慢病、药物过敏、既往史、血压与静息心率可参与分析；紧急联系人信息不会发送给模型。

## 功能范围

### 患者端

- 手机验证码登录；开发环境返回本地验证码，生产环境可接入短信服务。
- 健康档案维护、BMI 计算、聊天历史与新建对话。
- 多 Agent 导诊、导诊进度、风险与科室建议。
- 报告历史、筛选、排序、分页和 PDF 下载。
- 宿迁医院地图、定位与导航入口。
- 预约创建、状态查看与确认前取消。

### 医生端

- 按风险、时间、状态筛选的待处理患者队列。
- 患者档案、风险报告历史和导诊记录查看。
- 协同诊断、人工复核工单处理、P1 急诊绿色通道。
- 预约确认、完成处理和导诊报告 PDF 导出。
- 随访任务中心集中展示待反馈、逾期、已完成和异常升级任务，支持患者、科室、状态、计划时间筛选，并可直达报告或创建下一轮随访。
- 医生确认处理完成时，同步关闭人工复核工单和关联的异常随访任务，患者“症状加重”反馈仍作为历史结果保留。
- 异常随访支持选择继续观察、建议复诊、转急诊或无需进一步处理，并将处置意见、处理医生和时间回传患者端、管理端及 CSV。

### 管理端

- 导诊分析、风险统计与记录筛选。
- Agent 轨迹查看、会话/Agent/状态筛选与耗时分析。
- 人工复核工单使用后端 SLA 截止时间，支持超时标记、自动升级计数、异常筛选和关联导诊/轨迹跳转。
- 预约监控、风险规则管理、医疗知识审核/驳回/启停/版本治理、医生账号与权限管理。
- 管理员审计日志与平台配置持久化。

## 导诊运行架构

患者每次调用 `/api/chat` 都进入 `TriageHarness`：

```text
患者档案 + 当前消息
  -> 会话记忆检索（最近 8 条）
  -> 医疗知识 RAG（Top 3，保留来源）
  -> 主导诊草稿
  -> 多 Agent 分析与 Supervisor 汇总
  -> AI Review Loop（最多 2 轮）
  -> SSE 结束标记 [DONE]
  -> 异步结构化提取与导诊记录保存
```

- 医疗知识写入 `medical_knowledge_documents`，初始资料覆盖急症、发热、呼吸道、腹痛、用药安全和去标识化教学场景。
- 检索、主导诊、各专业 Agent、Supervisor 与 AI Review 都保存至 `agent_traces`；管理端可通过会话 ID 追溯。
- RAG 是导诊链路的固定阶段，不是可选展示功能；未命中知识时仍继续规则和风险 Agent 分析，不降低急诊安全提示。
- `ai-review` 最多修正两轮；失败时保留保守候选回答并记录降级轨迹。

完整架构、数据库、知识来源、RAG、Harness、Loop Engine 与 SSE 实现说明见 [`../docs/TECHNICAL_SPECIFICATION.md`](../docs/TECHNICAL_SPECIFICATION.md)。
## 环境要求

- Go `1.25.x` 或兼容版本。
- MySQL `8.x`。
- OpenAI 兼容模型服务。
- 百度地图 JavaScript API GL AK（地图功能需要）。
- 项目通过本地依赖引用父目录 AGGO 仓库：`replace github.com/CoolBanHub/aggo => ../`。

请在当前 `example` 目录中运行，或按实际目录调整 `go.mod` 中的 `replace` 路径。

## 配置

将示例配置复制到父目录：

```powershell
Copy-Item .\.env.example ..\.env
```

至少配置以下项：

```text
BaseUrl
APIKey
MYSQL_DSN
DOCTOR_USERNAME
DOCTOR_PASSWORD
ADMIN_USERNAME
ADMIN_PASSWORD
BAIDU_MAP_AK
```

环境行为：

- `APP_ENV=development`：不会调用付费短信服务，`POST /api/patient/sms` 返回本地 `debugCode`，仅用于本地调试。
- `APP_ENV=production`：启动时执行严格配置校验，医生端和管理端账号密码、百度地图 AK、短信服务均不能为空。
- 生产环境医生和管理员密码必须不少于 12 位，并拒绝 `doctor123`、`MedTriage@2026!88` 和模板占位密码。
- 生产短信必须配置 `IHUYI_ACCOUNT` 与 `IHUYI_PASSWORD`，或配置自定义 `SMS_PROVIDER_URL`；不要将调试验证码暴露给前端或日志。
- `BaseUrl`、`APIKey`、`MYSQL_DSN` 在所有环境都必须显式配置，程序不再提供默认数据库账号或密码。
- 开发环境可以使用本地演示账号，但仅限本机调试，不应复用于部署环境。
- `.env`、模型密钥、数据库 DSN、短信服务密钥和地图 AK 必须保持本地私有。

## 数据库

先创建数据库，应用启动后会通过 GORM `AutoMigrate` 自动创建或更新数据表：

```sql
CREATE DATABASE aggo CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
```

主要数据表：

- `patient_profiles`、`patient_chats`
- `triage_records`、`agent_traces`
- `appointments`、`escalation_tickets`
- `staff_users`、`risk_rules`、`audit_logs`
- `platform_configs`

导诊记录、预约、人工复核工单与 Agent 轨迹通过 `sessionId` 关联，便于跨端追溯同一次导诊流程。

## 启动

在当前目录执行：

```powershell
.\start.cmd
```

指定端口：

```powershell
.\start.cmd -Port 8090
```

也可直接运行：

```powershell
go run .\sse
```

启动后按“系统入口”中的地址访问对应端。

服务探针：

- `GET /health`：进程存活检查，不依赖外部服务。
- `GET /ready`：就绪检查，会在 2 秒超时内 Ping MySQL；数据库不可用时返回 `503`。

## Docker 一键部署

在仓库根目录准备私有 `.env` 后执行：

```powershell
cd ..
docker compose up -d --build
docker compose ps
Invoke-RestMethod http://localhost:8081/ready
```

Docker 默认使用宿主机 `8081`，避免与本机直接运行在 `8080` 的服务冲突；可通过 `.env` 中的 `DOCKER_PORT` 修改。Compose 会启动 MySQL 8.4，等待数据库健康后再启动应用；应用继续使用 GORM `AutoMigrate` 初始化表结构。MySQL 数据保存在命名卷 `aggo_mysql_data` 中，普通 `docker compose down` 不会删除数据。

```powershell
# 查看应用日志
docker compose logs -f app

# 停止服务并保留数据
docker compose down

# 仅在确认不再需要本地数据时删除数据卷
docker compose down -v
```

本地 Compose 提供开发默认数据库账号。共享环境或生产环境必须在 `.env` 中设置独立的 `MYSQL_USER`、`MYSQL_PASSWORD`、`MYSQL_ROOT_PASSWORD`，同时启用 `APP_ENV=production` 并满足前述账号、短信和地图配置校验。容器以非 root 用户运行，并内置 `/ready` Docker 健康检查和中文 PDF 字体。

## 测试与构建

常规单元测试与静态检查：

```powershell
.\test.cmd
```

生成 Go 覆盖率报告：

```powershell
.\test.ps1 -Coverage
```

服务启动后，执行患者、医生和管理端只读验收：

```powershell
.\acceptance.ps1 -RequireDemoData
```

或一次运行单元测试、静态检查和三端验收：

```powershell
.\test.ps1 -Coverage -Acceptance -RequireDemoData
```

RAG 检索和 Agent 调度评测集：

```powershell
go test .\sse -run "Test(RAGRetrievalEvaluationDataset|AgentDispatchEvaluationDataset)$" -count=1 -v
```

本地只读接口性能基线：

```powershell
go run .\cmd\perfcheck -requests 50 -concurrency 10 -max-p95-ms 4000 -json performance-report.json
```

性能工具不调用 AI 对话，不消耗模型或地图额度。测试结果与优化前后对比见 [性能基线](../docs/PERFORMANCE_BASELINE.md)。

验收脚本使用现有患者账号，仅生成本地登录验证码、建立会话并读取业务数据，不会创建测试患者或修改导诊记录。单元测试使用 SQL Mock，不会修改运行中的 MySQL 业务库。

## 权限与数据安全

- 未登录访问患者、医生、管理端业务接口会被拒绝。
- 患者仅可访问自己的档案、对话、报告、工单与预约。
- 医生可处理临床队列中的允许状态，不可任意改写系统风险结论。
- 管理员负责平台、账号、规则和审计管理，不直接改写临床结论。
- 导诊、预约和人工复核均使用受限的状态流转。
- 停用医生或重置医生密码会使该医生既有会话失效。
- 患者验证码会过期，连续错误会触发次数限制。

## 验收基线

本地全链路验收已覆盖：

- 患者、医生、管理员登录与退出后会话失效。
- 匿名访问受保护接口返回 `401`。
- 导诊记录、预约、人工复核工单与 Agent 轨迹的数据关联。
- 管理端配置、审计、导诊、预约、工单和轨迹接口读取。
- `go test .\sse -count=1`、`go vet .\sse` 和 Go 构建。

部署或切换配置后，应重新执行一次上述检查，并确认百度地图 AK 的域名白名单与线上访问域名一致。

## 项目结构

```text
sse/
|-- main.go                    # HTTP 路由与应用启动
|-- multi_agent.go             # 专家 Agent 调度与监督汇总
|-- triage_agent.go            # 主导诊 Agent
|-- extraction_agent.go        # 结构化记录提取
|-- triage_records.go          # 导诊持久化与状态流转
|-- patient_services.go        # 档案、预约、地图与 PDF
|-- patient_chats.go           # 患者历史对话
|-- escalation_tickets.go      # 人工复核工单
|-- doctor_auth.go             # 医生认证与会话
|-- admin.go                   # 管理端 API 与审计
|-- risk_rules.go              # 可配置风险规则
|-- staff_accounts.go          # 员工账号、密码与平台配置
|-- pages.go                   # 嵌入前端页面
|-- web/                       # 患者、医生、管理端及登录页
`-- *_test.go                  # 接口与业务测试
```

## 提交前检查

1. 确认 `.env` 未被 Git 跟踪。
2. 检查改动中没有 API Key、密码、Token、DSN 或地图 AK。
3. 执行 `go test .\sse -count=1` 与 `go vet .\sse`。
4. 执行构建并用三端入口完成一次登录与基础数据联调。

## 会话持久化

患者、医生和管理员的登录会话统一保存在 `persistent_session` 表中，数据库只保存 Cookie Token 的 SHA-256 摘要，不保存原始 Token。服务重启后会根据 Cookie 恢复登录状态；退出登录、停用医生或重置密码时会删除对应会话。系统启动时及每小时清理过期记录。

## 随访数据导出

管理端随访管理支持按时间范围、患者、科室、创建医生、随访状态、提醒类型、接收对象和已读状态筛选。随访计划与提醒记录可导出为 UTF-8 CSV，手机号默认脱敏，导出操作写入审计日志。
