# AGGO 智能导诊示例

这是一个基于 AGGO/Eino 的多 Agent 智能导诊应用，包含患者对话端、医生工作台、导诊记录和医生处理审计。

## 核心流程

```text
患者消息
→ 主导诊 Agent（多轮记忆）
→ 高风险场景 Review Agent
→ 患者最终回答
→ 记录提取 Agent
→ MySQL 导诊记录
→ 医生工作台
```

## 主要功能

- 患者端流式多轮导诊
- 危险信号紧急升级
- 特殊人群和用药问题二次审核
- 自动提取 `P1/P2/P3`、推荐科室和就诊准备
- 医生登录、备注、状态处理和操作审计
- 页面通过 Go `embed` 打包进可执行文件

## 环境要求

- Go 1.24 或兼容版本
- MySQL 8.x
- OpenAI 兼容模型接口

## 配置

在仓库根目录创建 `.env`：

```powershell
Copy-Item .\example\.env.example .\.env
```

必须配置 `BaseUrl` 和 `APIKey`。完整配置见 `example/.env.example`，`MYSQL_DSN` 指定的数据库需要提前创建。

## 启动

在仓库根目录运行：

```powershell
.\example\start.cmd
```

指定端口：

```powershell
.\example\start.cmd -Port 8090
```

默认地址：

- 患者端：`http://localhost:8080`
- 医生端：`http://localhost:8080/doctor`
- 登录页：`http://localhost:8080/doctor/login`

## 测试

```powershell
.\example\test.cmd
```

等价命令：

```powershell
cd .\example
go test ./sse
go vet ./sse
```

数据库 API 测试使用 SQL Mock，不会连接或修改真实 MySQL 数据。

## 代码结构

```text
example/sse/
|-- main.go
|-- triage_agent.go
|-- review_agent.go
|-- extraction_agent.go
|-- doctor_auth.go
|-- triage_records.go
|-- database.go
|-- pages.go
|-- web/
|   |-- patient.html
|   |-- doctor.html
|   `-- doctor_login.html
`-- *_test.go
```

## 安全说明

- 生产环境必须修改默认医生密码。
- `.env` 已被 Git 忽略，不要提交真实 API Key 或数据库密码。
- 本应用用于导诊辅助，不提供医学确诊，不能替代医生诊疗。
