# AI Proxy - 多上游 AI API 代理服务

## 软件介绍

AI Proxy 是一个用 Go 语言开发的高性能 AI API 代理服务器，旨在为企业和开发者提供统一、安全、可管理的 AI 服务访问接口。

### 核心特性

- **多上游聚合**：支持接入多个 AI 服务提供商（OpenAI、DeepSeek、Gemini、NVIDIA 等），提供统一的访问入口
- **项目级鉴权**：基于 API Key 的项目级别访问控制，精确管理各项目的访问权限
- **模型权限管理**：支持精确到模型级别的权限控制，可配置项目可访问的特定模型
- **智能路由**：自动根据模型名称匹配到对应的上游服务
- **模型映射**：支持模型名称自动映射，隐藏真实模型名称，便于切换供应商
- **负载均衡**：单个上游支持多个 API Key 的轮询使用，提高并发能力
- **故障重试**：自动检测请求失败，支持配置重试次数和冷却时间
- **配置热加载**：修改配置文件后自动生效，无需重启服务
- **OpenAI 兼容**：完全兼容 OpenAI API 格式，可直接使用 OpenAI SDK 调用

### 适用场景

- 企业内部统一 AI 服务接入
- 多 AI 服务供应商的成本优化
- 项目级别的资源隔离和计费
- API Key 的集中管理和轮换
- 模型服务的统一对外接口

---

## 配置文件说明

AI Proxy 使用 YAML 格式的配置文件 `config.yaml`，位于程序运行目录下。

### 完整配置示例

```yaml
server:
  port: ":8080"                              # 服务监听端口
  notification_webhook: ""                   # 通知 Webhook 地址（可选）
  max_retries: 3                             # 失败重试次数
  cool_down_minutes: 10                      # 失败后 Key 冷却时间（分钟）
  auto_models:                               # 模型自动映射配置
    "auto-models": "nvidia:minimaxai/minimax-m2.1"
    "auto-gemini": "nvidia:deepseek-ai/deepseek-r1"

upstreams:
  # 上游服务配置 1
  - name: "OpenAI-Pool"
    base_url: "https://api.openai.com"
    models: ["gpt-4", "gpt-4-turbo", "gpt-3.5-turbo"]
    keys:
      - "sk-oa-1"
      - "sk-oa-2"

  # 上游服务配置 2
  - name: "DeepSeek-Pool"
    base_url: "https://api.deepseek.com"
    models: ["deepseek-chat", "deepseek-coder"]
    keys:
      - "sk-ds-1"

  # 上游服务配置 3
  - name: "Gemini"
    base_url: "https://generativelanguage.googleapis.com"
    models: ["gemini-pro", "gemini-1.5-flash"]
    keys:
      - "sk-gm-1"

  # 上游服务配置 4
  - name: "nvidia"
    base_url: "https://integrate.api.nvidia.com"
    models: ["deepseek-ai/deepseek-r1", "minimaxai/minimax-m2.1", "deepseek-ai/deepseek-v3.2"]
    keys:
      - "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"

project_auth:
  # 项目授权配置 1
  - project_name: "Marketing-App"
    api_key: "ak-mkt-123456"
    allowed_models: ["gpt-4", "gpt-3.5-turbo"]

  # 项目授权配置 2
  - project_name: "测试项目"
    api_key: "ak-cs-7890121"
    allowed_models: ["*"]  # 允许所有模型
```

### 配置项详解

#### 1. Server（服务器配置）

| 配置项 | 类型 | 说明 | 默认值 |
|--------|------|------|--------|
| port | string | 服务监听端口，支持 `:8080` 或 `8080` 格式 | `:8080` |
| notification_webhook | string | 通知 Webhook 地址 | `""` |
| max_retries | int | 请求失败后的最大重试次数 | `3` |
| cool_down_minutes | int | API Key 失败后的冷却时间（分钟） | `10` |
| auto_models | map[string]string | 模型自动映射表 | `{}` |

**auto_models 映射格式说明**：
- 格式：`"客户端请求的模型名": "上游名:真实模型名"` 或 `"客户端请求的模型名": "真实模型名"`
- 示例 1：`"auto-models": "nvidia:minimaxai/minimax-m2.1"` - 请求 `auto-models` 时，自动转发到 NVIDIA 的 minimaxai/minimax-m2.1 模型
- 示例 2：`"auto-gpt4": "gpt-4"` - 请求 `auto-gpt4` 时，自动转发到名为 `gpt-4` 的真实模型
- 使用场景：隐藏真实供应商信息，便于后续切换供应商而不影响客户端代码

#### 2. Upstreams（上游服务配置）

每个上游服务包含以下字段：

| 字段 | 类型 | 说明 |
|------|------|------|
| name | string | 上游服务的唯一标识名称 |
| base_url | string | 上游 API 的基础 URL |
| models | []string | 该上游支持的模型列表，`"*"` 表示支持所有模型 |
| keys | []string | 该上游的 API Key 列表，支持多个 Key 轮询使用 |

**配置建议**：
- 为每个上游服务配置多个 API Key 以提高并发能力
- 在上游支持的情况下，可以使用 `"*"` 作为通配符支持所有模型

#### 3. Project Auth（项目授权配置）

每个项目授权包含以下字段：

| 字段 | 类型 | 说明 |
|------|------|------|
| project_name | string | 项目名称，用于日志记录 |
| api_key | string | 项目的访问密钥，客户端使用该 Key 进行鉴权 |
| allowed_models | []string | 项目允许访问的模型列表，`["*"]` 表示允许所有模型 |

**配置建议**：
- 为不同项目分配独立的 API Key
- 根据项目需求精确配置允许访问的模型
- 使用 `"*"` 可快速授权测试项目访问所有模型

---

## 软件使用

### 前置要求

- Go 1.25.4 或更高版本（如从源码编译）
- 一个或多个 AI 服务的 API Key

### 快速开始

#### 1. 从源码编译

```bash
# 克隆或下载源码
cd ai_proxy

# 下载依赖
go mod download

# 编译
go build -o ai_proxy main.go

# 运行
./ai_proxy
```

#### 2. 基本使用

启动服务后，AI Proxy 会监听配置的端口（默认 `:8080`），并兼容 OpenAI API 格式。

**获取模型列表**

```bash
curl http://localhost:8080/v1/models \
  -H "Authorization: Bearer ak-mkt-123456"
```

**发送聊天请求**

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ak-mkt-123456" \
  -d '{
    "model": "gpt-4",
    "messages": [
      {"role": "user", "content": "你好，请介绍一下 AI Proxy"}
    ]
  }'
```

#### 3. 使用模型映射功能

通过配置 `auto_models`，可以使用自定义的模型名称来请求真实模型。

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ak-cs-7890121" \
  -d '{
    "model": "auto-models",
    "messages": [
      {"role": "user", "content": "你好"}
    ]
  }'
```

上述请求会自动映射到 `nvidia:minimaxai/minimax-m2.1` 模型。

#### 4. Python SDK 示例

```python
from openai import OpenAI

# 配置代理服务地址
client = OpenAI(
    api_key="ak-mkt-123456",  # 使用项目的 API Key
    base_url="http://localhost:8080/v1"
)

# 获取模型列表
models = client.models.list()
for model in models.data:
    print(model.id)

# 发送聊天请求
response = client.chat.completions.create(
    model="gpt-4",
    messages=[
        {"role": "user", "content": "你好，请介绍一下 AI Proxy"}
    ],
    max_tokens=100
)

print(response.choices[0].message.content)
```

#### 5. 配置热加载

修改 `config.yaml` 文件后，AI Proxy 会自动检测变更并重新加载配置，无需重启服务。

日志输出示例：
```
[CONFIG] 配置文件变更，重载中...
[CONFIG] 加载成功
```

---

## 软件部署

### Linux 部署

#### 方式一：直接运行

1. **编译程序**

```bash
# 在 Linux 环境下编译
GOOS=linux GOARCH=amd64 go build -o ai_proxy main.go
```

2. **准备配置文件**

将 `config.yaml` 复制到目标目录，并根据实际环境修改配置。

3. **运行服务**

```bash
# 直接运行
./ai_proxy

# 或后台运行
nohup ./ai_proxy > ai_proxy.log 2>&1 &
```

4. **停止服务**

```bash
# 查找进程
ps aux | grep ai_proxy

# 停止进程
kill <PID>
```

#### 方式二：使用 Systemd（推荐）

1. **创建服务文件**

```bash
sudo vim /etc/systemd/system/ai_proxy.service
```

2. **服务配置内容**

```ini
[Unit]
Description=AI Proxy Service
After=network.target

[Service]
Type=simple
User=your_username
WorkingDirectory=/path/to/ai_proxy
ExecStart=/path/to/ai_proxy/ai_proxy
Restart=on-failure
RestartSec=5s
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

3. **启用并启动服务**

```bash
# 重载 systemd 配置
sudo systemctl daemon-reload

# 设置开机自启
sudo systemctl enable ai_proxy

# 启动服务
sudo systemctl start ai_proxy

# 查看服务状态
sudo systemctl status ai_proxy

# 查看日志
sudo journalctl -u ai_proxy -f

# 重启服务
sudo systemctl restart ai_proxy

# 停止服务
sudo systemctl stop ai_proxy
```

#### 方式三：使用 Docker

1. **创建 Dockerfile**

```dockerfile
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY . .
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o ai_proxy main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/ai_proxy .
COPY config.yaml .
EXPOSE 8080
CMD ["./ai_proxy"]
```

2. **构建镜像**

```bash
docker build -t ai_proxy:latest .
```

3. **运行容器**

```bash
docker run -d \
  --name ai_proxy \
  -p 8080:8080 \
  -v $(pwd)/config.yaml:/root/config.yaml \
  --restart unless-stopped \
  ai_proxy:latest
```

4. **容器管理命令**

```bash
# 查看日志
docker logs -f ai_proxy

# 重启容器
docker restart ai_proxy

# 停止容器
docker stop ai_proxy

# 删除容器
docker rm ai_proxy

# 进入容器
docker exec -it ai_proxy sh
```

#### 方式四：使用 Docker Compose

1. **创建 docker-compose.yml**

```yaml
version: '3.8'

services:
  ai_proxy:
    build: .
    container_name: ai_proxy
    ports:
      - "8080:8080"
    volumes:
      - ./config.yaml:/root/config.yaml
    restart: unless-stopped
    environment:
      - TZ=Asia/Shanghai
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"
```

2. **启动服务**

```bash
# 启动
docker-compose up -d

# 查看状态
docker-compose ps

# 查看日志
docker-compose logs -f

# 停止服务
docker-compose down

# 重启服务
docker-compose restart
```

#### 方式五：使用 Nginx 反向代理

1. **配置 Nginx**

```nginx
server {
    listen 80;
    server_name your-domain.com;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # 支持流式响应
        proxy_buffering off;
        proxy_cache off;
    }
}
```

2. **启用 HTTPS（可选）**

使用 Let's Encrypt 获取免费 SSL 证书：

```bash
sudo certbot --nginx -d your-domain.com
```

---

### Windows 部署

#### 方式一：直接运行

1. **编译程序**

```bash
# 在 Windows 环境下编译
go build -o ai_proxy.exe main.go

# 或交叉编译
GOOS=windows GOARCH=amd64 go build -o ai_proxy.exe main.go
```

2. **准备配置文件**

确保 `config.yaml` 与 `ai_proxy.exe` 在同一目录下。

3. **运行服务**

双击 `ai_proxy.exe` 或在命令行中运行：

```cmd
ai_proxy.exe
```

4. **配置开机自启（通过任务计划程序）**

- 打开"任务计划程序"
- 创建基本任务
- 设置触发器为"计算机启动时"
- 操作选择"启动程序"，浏览选择 `ai_proxy.exe`
- 完成配置

#### 方式二：使用 PowerShell 后台运行

1. **启动服务**

```powershell
# 后台运行
Start-Process -FilePath ".\ai_proxy.exe" -WindowStyle Hidden

# 或使用 Start-Job
Start-Job -ScriptBlock { .\ai_proxy.exe }
```

2. **停止服务**

```powershell
# 查找进程
Get-Process ai_proxy

# 停止进程
Stop-Process -Name ai_proxy -Force
```

#### 方式三：使用 NSSM（推荐）

NSSM（Non-Sucking Service Manager）可以将程序注册为 Windows 服务。

1. **下载 NSSM**

访问 https://nssm.cc/download 下载并解压。

2. **注册服务**

```cmd
# 以管理员身份运行 CMD
nssm install AI_Proxy

# 在弹出的窗口中配置：
# Path: F:\golang\ai_proxy\ai_proxy.exe
# Startup directory: F:\golang\ai_proxy
# 服务名称: AI_Proxy
# 点击 Install service
```

3. **服务管理**

```cmd
# 启动服务
nssm start AI_Proxy

# 停止服务
nssm stop AI_Proxy

# 重启服务
nssm restart AI_Proxy

# 删除服务
nssm remove AI_Proxy

# 编辑服务配置
nssm edit AI_Proxy
```

#### 方式四：使用 WSL（Windows Subsystem for Linux）

1. **安装 WSL**

```powershell
wsl --install
```

2. **在 WSL 中部署**

参考 Linux 部署方式，在 WSL Ubuntu 中部署 AI Proxy。

3. **启动服务**

```bash
# 在 WSL 中启动
./ai_proxy

# 或使用 systemd
sudo systemctl start ai_proxy
```

#### 方式五：使用 Docker Desktop

1. **安装 Docker Desktop**

下载并安装 Docker Desktop for Windows。

2. **准备配置文件**

确保 `config.yaml` 和 `Dockerfile` 在同一目录。

3. **构建并运行**

```powershell
# 构建镜像
docker build -t ai_proxy:latest .

# 运行容器
docker run -d `
  --name ai_proxy `
  -p 8080:8080 `
  -v ${PWD}/config.yaml:/root/config.yaml `
  --restart unless-stopped `
  ai_proxy:latest
```

4. **容器管理**

```powershell
# 查看日志
docker logs -f ai_proxy

# 重启容器
docker restart ai_proxy

# 停止容器
docker stop ai_proxy

# 删除容器
docker rm ai_proxy
```

---

## 故障排查

### 常见问题

**1. 服务启动失败**

- 检查端口是否被占用：`netstat -tuln | grep 8080`
- 检查配置文件格式是否正确
- 查看日志输出确定具体错误

**2. 请求返回 401 Unauthorized**

- 检查请求头中的 `Authorization` 是否正确
- 确认配置文件中的 `api_key` 是否匹配

**3. 请求返回 403 Forbidden**

- 检查项目是否有权限访问请求的模型
- 查看配置文件中的 `allowed_models` 设置

**4. 请求返回 429 Too Many Requests**

- 检查上游服务的 API Key 配额
- 考虑增加上游服务的 API Key 数量
- 调整 `cool_down_minutes` 配置

**5. 配置文件修改未生效**

- 确认配置文件格式正确（YAML 缩进）
- 查看服务日志是否有配置加载错误
- 尝试重启服务

### 日志级别

当前版本使用标准 log 包输出日志，主要日志类型：

- `[SYSTEM]`: 系统启动和关闭相关日志
- `[CONFIG]`: 配置加载和重载相关日志
- `[ROUTE]`: 请求路由和转发相关日志
- `[INFO]`: 一般信息日志
- `[WARN]`: 警告日志（如网络错误、上游响应异常）
- `[ERROR]`: 错误日志

---

## 性能优化建议

1. **多 Key 配置**：为每个上游服务配置多个 API Key，提高并发处理能力
2. **合理设置重试次数**：根据上游服务的稳定性调整 `max_retries` 参数
3. **配置冷却时间**：根据实际情况设置 `cool_down_minutes`，避免频繁重试
4. **使用 CDN**：对于公网部署，建议使用 CDN 加速
5. **监控告警**：配置监控和告警，及时发现服务异常
6. **负载均衡**：在高并发场景下，可以部署多个实例并使用负载均衡

---

## 安全建议

1. **保护配置文件**：配置文件包含敏感信息，应设置适当的文件权限
2. **使用 HTTPS**：在生产环境中，建议使用 HTTPS 传输数据
3. **访问控制**：通过防火墙限制服务访问范围
4. **定期轮换密钥**：定期更换 API Key 和项目密钥
5. **日志审计**：记录访问日志，便于审计和追踪

---

## 技术栈

- **编程语言**：Go 1.25.4
- **Web 框架**：Gin
- **配置管理**：Viper
- **文件监控**：fsnotify

---

## 许可证

本项目采用 MIT 许可证。

---

## 联系方式

如有问题或建议，请通过以下方式联系：

- 提交 Issue
- 发送邮件

---

## 更新日志

### v1.0.0
- 初始版本发布
- 支持多上游服务聚合
- 支持项目级别鉴权
- 支持模型权限管理
- 支持模型自动映射
- 支持配置热加载
- 支持失败重试和冷却机制
