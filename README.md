# NetworkDisk

基于 Go 构建的生产级私有云存储服务，通过现代 SPA 界面提供文件管理、分享、全文检索与多因素认证功能。

## 功能特性

### 文件管理
- 支持文件上传，含 MIME 类型检测与扩展名白名单/黑名单
- 大文件分片上传，支持进度追踪、续传与取消
- 创建、重命名、移动、复制、删除文件与目录
- 批量删除与批量移动
- 回收站机制，支持软删除、恢复与彻底删除
- 收藏（星标）文件以便快速访问
- 最近文件与仪表盘概览
- 多文件 ZIP 打包下载
- 临时下载链接，可配置有效期
- 浏览器内文件预览（图片、视频、音频、PDF、文本、代码）
- WebP 缩略图生成（图片与视频帧，视频依赖 ffmpeg）

### 文件分享
- 创建分享链接，可选密码保护
- 可配置过期时间与最大下载次数
- 公开分享访问页面，支持密码验证
- 分享列表查看与撤销
- 分享密码暴力破解防护

### 搜索与标签
- 文件名与内容全文检索
- 搜索建议
- 标签系统 — 创建、删除标签，为文件添加标签
- 按标签筛选文件

### 认证与安全
- 基于 JWT 的 access/refresh 令牌认证
- TOTP 双因素认证（启用、验证、禁用）
- 会话管理 — 查看与撤销设备
- 所有变更类接口均受 CSRF 保护
- 认证与公开接口频率限制
- 安全响应头（CSP、HSTS、X-Content-Type-Options 等）
- CORS 支持，可配置允许来源
- bcrypt 密码哈希，可配置加密成本

### 运维
- 支持级别过滤与文件输出的结构化日志
- 请求追踪，Trace-ID 头传播
- 审计日志，可配置保留天数
- 健康检查接口
- 按用户统计存储用量
- SIGINT/SIGTERM 信号优雅关闭
- SIGHUP 信号热重载配置
- 通过 TOML 文件与环境变量进行配置

### 后台维护任务
- 每日自动清理过期回收站项目
- 每小时清理过期分片上传会话
- 每日存储用量校准
- 每日过期审计日志清理
- 每小时过期临时下载链接清理
- 每小时过期 TOTP 状态清理

## 技术栈

- **语言**：Go 1.26
- **数据库**：MySQL 8.0+
- **前端**：服务端渲染 HTML + 原生 JavaScript SPA（无框架）
- **核心依赖**：`golang-jwt/jwt`、`golang.org/x/crypto`、`go-sql-driver/mysql`、`BurntSushi/toml`、`pquerna/otp`

## 快速启动

### 环境要求

- Go 1.26+
- MySQL 8.0+
- ffmpeg（可选，用于视频缩略图生成）

### 1. 创建数据库

```sql
CREATE DATABASE networkdisk CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER 'networkdisk'@'localhost' IDENTIFIED BY '你的密码';
GRANT ALL PRIVILEGES ON networkdisk.* TO 'networkdisk'@'localhost';
```

### 2. 配置

```bash
cp config.example.toml config.toml
```

编辑 `config.toml`，至少需要设置 `database.password` 和 `auth.jwt_secret`。

### 3. 编译并运行

```bash
go build -o server ./cmd/server
./server
```

亦可指定自定义配置文件路径：

```bash
./server /etc/networkdisk/config.toml
```

默认监听 `http://0.0.0.0:24003`。

## 配置说明

配置从 TOML 文件加载，并可通过环境变量覆盖。各配置段下的键映射为 `NETWORKDISK_<段名>_<键名>` 环境变量。

### `[server]`

| 键 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `host` | string | `0.0.0.0` | 监听地址 |
| `port` | int | `24003` | 监听端口 |
| `read_timeout` | duration | `300s` | HTTP 读取超时 |
| `write_timeout` | duration | `300s` | HTTP 写入超时 |
| `idle_timeout` | duration | `120s` | HTTP 空闲超时 |
| `shutdown_timeout` | duration | `10s` | 优雅关闭等待时间 |
| `rate_limit` | int | `60` | 每个时间窗口允许的请求数 |
| `rate_limit_window` | duration | `1m` | 频率限制时间窗口 |
| `allowed_origins` | []string | — | CORS 允许来源列表 |
| `trusted_proxy` | string | — | 可信代理 CIDR，用于提取真实 IP |

### `[database]`

| 键 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `host` | string | `127.0.0.1` | MySQL 主机 |
| `port` | int | `3306` | MySQL 端口 |
| `user` | string | — | MySQL 用户 |
| `password` | string | — | MySQL 密码 |
| `database` | string | — | MySQL 数据库名 |
| `max_open_conns` | int | `25` | 最大打开连接数 |
| `max_idle_conns` | int | `5` | 最大空闲连接数 |
| `conn_max_lifetime` | duration | `5m` | 连接最大存活时间 |

### `[storage]`

| 键 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `root` | string | `./data` | 文件存储根目录 |
| `max_file_size` | int64 | `104857600` | 单文件最大大小（100 MiB） |
| `thumbnail_max_size` | int64 | `52428800` | 可生成缩略图的最大文件大小（50 MiB） |
| `thumbnail_quality` | int | `80` | WebP 缩略图质量（1–100） |
| `chunk_size` | int64 | `10485760` | 上传分片大小（10 MiB） |
| `chunk_clean_timeout` | duration | `24h` | 过期分片会话清理超时 |
| `auto_clean_recycle_days` | int | `30` | 回收站项目自动清理天数 |
| `ffmpeg_path` | string | — | ffmpeg 可执行文件路径（视频缩略图） |
| `temp_link_ttl` | duration | `1h` | 临时下载链接有效期 |

### `[upload]`

| 键 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `allowed_extensions` | []string | — | 允许的文件扩展名列表（空为全部允许） |
| `blocked_extensions` | []string | `.exe,.sh,.bat,.cmd,.com,.dll,.so,.dylib` | 禁止的文件扩展名列表 |
| `detect_mime` | bool | `true` | 从文件内容检测 MIME 类型 |
| `on_name_conflict` | string | `rename` | 文件名冲突处理：`rename` 或 `error` |

### `[auth]`

| 键 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `jwt_secret` | string | — | JWT 签名密钥（最少 32 字符，必填） |
| `jwt_expire` | duration | `15m` | 访问令牌有效期 |
| `refresh_expire` | duration | `7d` | 刷新令牌有效期 |
| `bcrypt_cost` | int | `12` | 密码哈希 bcrypt 加密成本 |
| `totp_issuer` | string | `NetworkDisk` | TOTP 颁发者名称 |

### `[share]`

| 键 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `default_expire` | duration | — | 分享链接默认过期时间 |
| `max_password_attempts` | int | `5` | 密码最大尝试次数（触发限流） |
| `bcrypt_cost` | int | `6` | 分享密码 bcrypt 加密成本 |
| `password_rate_limit_reset` | duration | `15m` | 分享密码限流重置窗口 |

### `[log]`

| 键 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `level` | string | `info` | 日志级别：`debug`、`info`、`warn`、`error` |
| `format` | string | `text` | 日志格式：`text`、`pattern`（结构化） |
| `file` | string | — | 日志文件路径（空则仅输出到 stdout） |
| `audit_retention_days` | int | `365` | 审计日志保留天数 |

## API 概览

所有受保护接口需在 `Authorization` 头中携带 `Bearer` 令牌。变更类接口需通过 `X-CSRF-Token` 头携带 CSRF 令牌（从 `csrf_token` cookie 中获取）。

### 认证
| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| `POST` | `/api/auth/register` | 否 | 注册新用户 |
| `POST` | `/api/auth/login` | 否 | 登录，返回令牌 |
| `POST` | `/api/auth/logout` | 是 | 登出当前会话 |
| `POST` | `/api/auth/refresh` | Cookie | 刷新访问令牌 |
| `GET` | `/api/auth/me` | 是 | 获取当前用户信息 |
| `PATCH` | `/api/auth/password` | 是 | 修改密码 |
| `POST` | `/api/auth/totp/enable` | 是 | 启用 TOTP 双因素认证 |
| `POST` | `/api/auth/totp/verify` | 是 | 验证并激活 TOTP |
| `POST` | `/api/auth/totp/disable` | 是 | 禁用 TOTP |
| `POST` | `/api/auth/logout/{device}` | 是 | 撤销指定设备会话 |

### 文件
| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| `GET` | `/api/files` | 是 | 列出目录内容 |
| `POST` | `/api/files/upload` | 是 | 上传文件（multipart） |
| `GET` | `/api/files/download/{id}` | 是 | 下载文件 |
| `GET` | `/api/files/thumbnail/{id}` | 是 | 获取文件缩略图 |
| `GET` | `/api/files/preview/{id}` | 是 | 预览文件内容 |
| `POST` | `/api/files/mkdir` | 是 | 创建目录 |
| `PATCH` | `/api/files/{id}` | 是 | 重命名或移动文件 |
| `DELETE` | `/api/files/{id}` | 是 | 移入回收站 |
| `POST` | `/api/files/{id}/copy` | 是 | 复制文件 |
| `POST` | `/api/files/{id}/star` | 是 | 切换收藏状态 |
| `POST` | `/api/files/{id}/temp-link` | 是 | 创建临时下载链接 |
| `GET` | `/api/files/starred` | 是 | 列出收藏文件 |
| `GET` | `/api/files/recent` | 是 | 列出最近文件 |
| `GET` | `/api/files/shared` | 是 | 列出与我分享的文件 |
| `GET` | `/api/files/dashboard` | 是 | 仪表盘概览 |
| `POST` | `/api/files/batch-delete` | 是 | 批量移入回收站 |
| `POST` | `/api/files/batch-move` | 是 | 批量移动文件 |
| `GET` | `/api/files/download-zip` | 是 | 多文件 ZIP 打包下载 |

### 回收站
| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| `GET` | `/api/files/recycle` | 是 | 列出已删除文件 |
| `POST` | `/api/files/{id}/restore` | 是 | 从回收站恢复 |
| `DELETE` | `/api/files/{id}/permanent` | 是 | 彻底删除 |
| `GET` | `/api/files/permanent-preview/{id}` | 是 | 预览彻底删除影响范围 |

### 分片上传
| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| `POST` | `/api/files/upload/init` | 是 | 初始化分片上传 |
| `POST` | `/api/files/upload/chunk` | 是 | 上传分片 |
| `POST` | `/api/files/upload/complete` | 是 | 完成并组装分片 |
| `GET` | `/api/files/upload/status/{uploadId}` | 是 | 查询上传进度 |
| `DELETE` | `/api/files/upload/cancel/{uploadId}` | 是 | 取消上传 |

### 搜索与标签
| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| `GET` | `/api/search` | 是 | 全文检索 |
| `GET` | `/api/search/suggest` | 是 | 搜索建议 |
| `GET` | `/api/tags` | 是 | 列出用户标签 |
| `POST` | `/api/tags` | 是 | 创建标签 |
| `DELETE` | `/api/tags/{id}` | 是 | 删除标签 |
| `GET` | `/api/tags/{id}/files` | 是 | 按标签列出文件 |
| `POST` | `/api/files/tags` | 是 | 批量添加标签到文件 |

### 分享
| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| `POST` | `/api/shares` | 是 | 创建分享链接 |
| `GET` | `/api/shares` | 是 | 列出我的分享 |
| `DELETE` | `/api/shares/{id}` | 是 | 撤销分享 |
| `GET` | `/s/{token}` | 否 | 公开分享访问页面 |
| `POST` | `/s/{token}/verify` | 否 | 验证分享密码 |
| `GET` | `/s/{token}/download` | 否 | 下载分享文件 |
| `GET` | `/d/{token}` | 否 | 临时下载链接 |

### 系统
| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| `GET` | `/api/health` | 否 | 健康检查 |
| `GET` | `/api/stats` | 是 | 存储用量统计 |
| `GET` | `/api/audit-logs` | 是 | 查询审计日志 |
| `GET` | `/api/devices` | 是 | 列出活跃设备/会话 |

### 页面
| 路径 | 说明 |
|---|---|
| `/` | 主文件浏览器（SPA） |
| `/login` | 登录页 |
| `/register` | 注册页 |
| `/dashboard` | 仪表盘 |
| `/search` | 搜索页 |
| `/shares` | 分享管理 |
| `/transfer` | 上传进度 |
| `/recycle` | 回收站 |
| `/audit` | 审计日志查看 |

## 项目结构

```
.
├── cmd/server/main.go          # 应用入口
├── internal/
│   ├── app/app.go              # 应用启动与生命周期管理
│   ├── config/config.go        # 配置加载、默认值、环境变量覆盖、热重载
│   ├── handler/                # HTTP 请求处理器
│   │   ├── auth.go             # 认证处理器
│   │   ├── file.go             # 文件管理处理器
│   │   ├── search.go           # 搜索处理器
│   │   ├── share.go            # 分享处理器
│   │   ├── system.go           # 系统/健康检查处理器
│   │   └── tag.go              # 标签处理器
│   ├── logging/                # 结构化日志
│   ├── middleware/              # HTTP 中间件
│   │   ├── auth.go             # JWT 认证
│   │   ├── cors.go             # CORS 处理
│   │   ├── csrf.go             # CSRF 防护
│   │   ├── errors.go           # 自定义错误页面
│   │   ├── logger.go           # 请求日志
│   │   ├── ratelimit.go        # 频率限制
│   │   ├── recover.go          # Panic 恢复
│   │   ├── security.go         # 安全响应头
│   │   └── trace.go            # 请求追踪
│   ├── model/model.go          # 数据模型
│   ├── router/router.go        # 路由注册
│   ├── service/                # 业务逻辑
│   │   ├── file.go             # 文件操作
│   │   ├── search.go           # 搜索服务
│   │   ├── share.go            # 分享服务
│   │   ├── tag.go              # 标签服务
│   │   ├── thumbnail.go        # 缩略图生成
│   │   └── user.go             # 用户/认证服务
│   ├── storage/                # 文件存储抽象层
│   │   └── local.go            # 本地文件系统后端
│   └── store/                  # 数据库访问层
│       ├── audit.go            # 审计日志查询
│       ├── db.go               # 连接管理与数据库迁移
│       ├── file.go             # 文件记录查询
│       ├── share.go            # 分享记录查询
│       ├── tag.go              # 标签查询
│       └── user.go             # 用户查询
├── migrations/                 # SQL 迁移文件
├── web/
│   ├── static/
│   │   ├── css/app.css         # 应用样式
│   │   └── js/
│   │       ├── app.js          # 主应用逻辑
│   │       ├── router.js       # SPA 客户端路由
│   │       └── transfer.js     # 上传传输管理器
│   └── templates/              # Go HTML 模板
│       ├── base.html           # 基础布局
│       ├── index.html          # 文件浏览器
│       ├── login.html          # 登录表单
│       ├── register.html       # 注册表单
│       ├── dashboard.html      # 仪表盘
│       ├── search.html         # 搜索结果
│       ├── shares.html         # 分享管理
│       ├── share_view.html     # 分享文件查看
│       ├── share_access.html   # 分享密码输入
│       ├── transfer.html       # 上传进度
│       ├── recycle.html        # 回收站
│       ├── admin_audit.html    # 审计日志查看
│       ├── partial.html        # SPA 局部模板
│       ├── error_404.html      # 404 错误页
│       └── error_500.html      # 500 错误页
├── config.example.toml         # 示例配置文件
├── go.mod
├── go.sum
└── CLAUDE.md                   # 项目开发规范
```

## 数据库迁移

迁移在启动时自动执行。`migrations/` 目录下的文件遵循 `NNN_name.sql` 命名规范，按版本号顺序执行。每次迁移的校验和会被记录在 `schema_versions` 表中，已执行的迁移文件不可再次修改，否则启动将报错。

## 环境变量

所有配置项均可通过环境变量覆盖，格式为 `NETWORKDISK_<段名>_<键名>`。示例：

```bash
export NETWORKDISK_DATABASE_PASSWORD="secret"
export NETWORKDISK_AUTH_JWT_SECRET="一个至少32字符的随机密钥..."
export NETWORKDISK_STORAGE_ROOT="/data/networkdisk"
export NETWORKDISK_LOG_LEVEL="debug"
```

环境变量优先级高于 TOML 配置文件。

## 配置热重载

向进程发送 `SIGHUP` 信号即可重载配置文件，无需重启：

```bash
kill -HUP $(pgrep server)
```

注意：数据库连接参数与 JWT 密钥出于安全考虑不支持热重载，需完整重启生效。

## 开源许可

MIT
