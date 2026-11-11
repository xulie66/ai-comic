# AI 漫画创作台

把小说原文转换为漫画分镜，生成角色、场景设定图和漫画页，支持项目保存、批量出图及整本导出。

技术栈：**Go + Vue 3 + TypeScript**。本地个人使用可直接运行；服务器推荐 Docker 部署。

> 项目仓库：https://github.com/xulie66/ai-comic 。下方命令可直接复制执行。

推荐生图API：https://oiai.pro
香港G口大带宽，1分钱一张，速度快

推荐文本中转：https://chat-chat.site
首字1s，自建号池，企业运营稳定性拉满，保真不掺水

## 上传 GitHub 时保留的目录

```text
README.md
src/
  go-service/    # Go后端、Docker配置和部署脚本
  ui/            # Vue前端源码
```

保留上述目录结构，Docker 构建会使用它。上传源码时排除 `.env`、`data`、`logs`、`node_modules`、编译程序及备份；本工作区的 `tools`、`server-snapshot` 和浏览器配置目录也不属于发布源码，里面可能包含部署凭证或私有数据。

## 一、本地拉取源码运行

适合自己在电脑上使用。准备好 **Git、Go 1.27、Node.js 22**。

### 1. 拉取代码并构建前端

Windows 使用 PowerShell；macOS / Linux 使用终端。依次执行：

```bash
git clone https://github.com/xulie66/ai-comic.git ai-comic
cd ai-comic/src/ui
npm install
npm run build
npm run build:admin
cd ../go-service
```

Windows 构建：

```powershell
go build -o comic-server.exe ./cmd/server
```

macOS / Linux 构建：

```bash
go build -o comic-server ./cmd/server
```

### 2. 配置并启动

**Windows（PowerShell）：**

```powershell
$env:GO_SERVICE_ADMIN_USERNAME="admin"
$env:GO_SERVICE_ADMIN_PASSWORD="换成你自己的至少12位密码"

$env:COMIC_TEXT_URL="https://你的接口地址/v1"
$env:COMIC_TEXT_KEY="你的文本接口密钥"
$env:COMIC_TEXT_MODEL="你的文本模型名称"

$env:COMIC_IMAGE_URL="https://你的接口地址/v1"
$env:COMIC_IMAGE_KEY="你的图片接口密钥"
$env:COMIC_IMAGE_MODEL="你的图片模型名称"

# 仅第一次执行，用于创建管理员
.\comic-server.exe -bootstrap-admin

# 启动
.\comic-server.exe
```

**macOS / Linux：**

```bash
export GO_SERVICE_ADMIN_USERNAME="admin"
export GO_SERVICE_ADMIN_PASSWORD="换成你自己的至少12位密码"

export COMIC_TEXT_URL="https://你的接口地址/v1"
export COMIC_TEXT_KEY="你的文本接口密钥"
export COMIC_TEXT_MODEL="你的文本模型名称"

export COMIC_IMAGE_URL="https://你的接口地址/v1"
export COMIC_IMAGE_KEY="你的图片接口密钥"
export COMIC_IMAGE_MODEL="你的图片模型名称"

# 仅第一次执行，用于创建管理员
./comic-server -bootstrap-admin

# 启动
./comic-server
```

浏览器打开：**http://127.0.0.1:5210/**，使用刚创建的管理员登录。

如果图片接口使用 `/chat/completions` 协议，再设置 `COMIC_IMAGE_PROTOCOL=chat`；默认使用 `/images/generations` 协议。URL 也可以直接填写完整接口路径。

本地模式不需要安装数据库，项目和图片保存在 `src/go-service/data`。本地模式供管理员个人创作；多用户、钱包和支付请使用下面的 Docker 方式。

**下次启动：**进入 `src/go-service`，在同一个终端设置好模型变量后运行程序即可，跳过创建管理员。也可以将配置保存在本机启动脚本中；密钥不要提交到 GitHub。

## 二、服务器 Docker 部署

服务器安装 **Git、Docker Engine 和 Docker Compose v2**，不需要另外安装 Go、Node.js 或 PostgreSQL。

### 1. 拉取代码

```bash
git clone https://github.com/xulie66/ai-comic.git ai-comic
cd ai-comic/src/go-service
cp .env.example .env
nano .env
```

### 2. 修改配置

在 `.env` 中填好以下几项，其他配置可保持默认：

```dotenv
TZ=Asia/Shanghai

# 生成你自己的数据库密码，建议使用32位随机字母和数字
POSTGRES_PASSWORD=替换为你自己的数据库密码
GO_SERVICE_DATABASE_URL=postgres://comic:替换为同一个数据库密码@postgres:5432/comic?sslmode=disable

GO_SERVICE_ADMIN_USERNAME=admin
GO_SERVICE_ADMIN_PASSWORD=换成你自己的至少12位密码

# 直接通过服务器IP访问
COMIC_BIND_IP=0.0.0.0
GO_SERVICE_SECURE_COOKIES=false
```

数据库密码的两处内容必须一致。使用纯字母和数字可以省去连接串的特殊字符编码。

### 3. 构建、创建管理员并启动

```bash
# 自动构建前端和Go后端
docker compose build

# 启动数据库
docker compose up -d postgres

# 仅第一次执行：创建管理员
docker compose run --rm app -bootstrap-admin

# 启动应用
docker compose up -d app

# 查看服务状态
docker compose ps
```

在服务器防火墙或云服务器安全组放行 **TCP 5210**，浏览器访问：

```text
http://你的服务器IP:5210/
```

管理后台：

```text
http://你的服务器IP:5210/admin/
```

使用管理员登录后，在后台配置 **渠道 → 渠道模型 → 逻辑模型及路由**，分别添加文本和图片模型。Docker 数据库模式使用后台模型配置，不使用本地模式的 `COMIC_*` 变量。

管理员创建成功后，从 `.env` 清空 `GO_SERVICE_ADMIN_PASSWORD`，再执行 `docker compose up -d app`。这不会修改已经创建的管理员密码。

### 4. 绑定域名（可选）

在宝塔或 Nginx 中创建网站，反向代理到 `http://127.0.0.1:5210`，开启 HTTPS。反代配置参考 [Nginx 模板](src/go-service/deploy/nginx.conf)。

随后修改 `.env`：

```dotenv
COMIC_BIND_IP=127.0.0.1
GO_SERVICE_SECURE_COOKIES=true
```

执行：

```bash
docker compose up -d app
```

之后通过 `https://你的域名/` 访问。生成进度使用 SSE，模板已包含关闭代理缓冲的配置。

### 5. 日常管理

以下命令都在 `src/go-service` 目录执行：

```bash
# 查看状态
docker compose ps

# 重启应用
docker compose restart app

# 停止服务，保留数据
docker compose down

# 拉取GitHub更新并重新构建
git pull
docker compose up -d --build
```

更新前先备份。项目图片保存在 `comic-data` 数据卷，账号和数据库保存在 `postgres-data` 数据卷，**两个都要保留**。`docker compose down -v` 会删除数据卷，日常停机不要加 `-v`。

## 常见问题

- **网页打不开：**先看 `docker compose ps` 是否正常，再检查服务器安全组和 5210 端口。使用域名反代时保持 `COMIC_BIND_IP=127.0.0.1`。
- **模型配置好了仍生成失败：**检查接口地址、协议、模型名和密钥。详细错误在服务端 `data/logs/server.jsonl`，前端只展示处理结果和必要进度。
- **重新打开终端后本地模型配置消失：**终端环境变量只对当前会话有效，需要重新设置或写入仅存本机的启动脚本。Go 程序本身不会自动读取 `.env`；Docker Compose 会读取。
- **已有旧版本数据：**先看[完整部署与迁移说明](src/go-service/README.md)，按副本迁移步骤操作，不要直接覆盖旧数据。

详细验证范围见[验收记录](src/go-service/VALIDATION.md)。
