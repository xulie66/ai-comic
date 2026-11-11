# 原生 Go 服务部署与迁移演练

第一次部署请先看[GitHub 首页快速教程](../../README.md)，只需选择本地源码运行或服务器 Docker 部署。本文保留详细配置、旧数据迁移和备份恢复说明。

模块 `comic.local/go-service`，入口 `cmd/server`，产物 `comic-server`（Windows 为 `comic-server.exe`），HTTP 使用 `net/http`。本文以当前 Go 1.27 编译器为基准；模块最低版本以 `go.mod` 为准。本目录提供部署资产及[实际验证记录](VALIDATION.md)。本轮已通过 Windows 全量 test/vet、Linux Docker Go 1.27 + PG16 全量实测、真实 PG 计费集成与镜像构建，并检查隔离实例及生产数据副本；这些结果不代表真实付费模型、云存储、宝塔或完整生产切换已验收。所有密码、域名和模型密钥均由操作者自行配置，仓库没有生产凭证。

## 运行模式与配置

| 模式 | 数据 | 适用能力 |
| --- | --- | --- |
| 未配置 PG | `data/go-store.json` 与项目文件 | 单机存储；运营、钱包、支付能力不可用。只有显式 bootstrap 的管理员，配置服务端模型环境变量后，才可调用本地模式生成。普通注册账号没有此生成权限。 |
| 配置 PG | PostgreSQL 账号、会话、项目索引及运营表；项目图片等仍在 data | 运营部署基础；还需在管理后台配置模型路由、价格、支付等，并分别验收。连接 PG 本身不代表支付已开通。 |

| 环境变量 | 默认值 / 含义 |
| --- | --- |
| `TZ` | Compose 默认 `Asia/Shanghai`；按实际部署地设置 IANA 时区，影响容器内每日额度日期 |
| `GO_SERVICE_ADDR` | `127.0.0.1:5210` |
| `GO_SERVICE_DATA_ROOT` | `./data`，相对进程工作目录 |
| `GO_SERVICE_PUBLIC_DIR` | `../web/public`，包含 ui/admin 前端产物 |
| `GO_SERVICE_DATABASE_URL` | 优先于 `DATABASE_URL`；两者都空才使用 JSON |
| `GO_SERVICE_SECURE_COOKIES` | 默认 false；HTTPS 反代后设 true，HTTP 本地调试为 false |
| `GO_SERVICE_ADMIN_USERNAME/PASSWORD` | 仅显式 `-bootstrap-admin` CLI 使用；密码至少 12 字符 |
| `GO_SERVICE_MAX_BODY_BYTES` | `16777216`（16 MiB），与反代上传限制配合 |
| `GO_SERVICE_COOKIE_NAME` | `ac_session`；演练建议与正式实例不同 |
| `GO_SERVICE_SESSION_TTL` | `720h`，Go duration 格式 |
| `COMIC_TEXT_URL/KEY/MODEL` | JSON 单机管理员的文本模型配置，三项都须设置 |
| `COMIC_IMAGE_URL/KEY/MODEL` | JSON 单机管理员的图片模型配置，三项都须设置 |

Go 二进制**不会自动加载 .env**。Compose 使用 .env 插值；Windows 的 [run.ps1](scripts/run.ps1) 会读取字面量 `KEY=value` 行；systemd 使用 EnvironmentFile。不要在环境文件中写可执行 shell 表达式，密钥也不应放进浏览器参数或前端源码。服务端日志仅位于数据根的 `logs/server.jsonl`，没有公开日志接口；启动失败时查看此私有文件，必要时结合服务管理器的退出状态。

## 前端与跨平台构建

在仓库根执行前端构建（Node 22、npm；首次安装依赖需网络）：

```powershell
Push-Location src/ui
npm install --no-audit --no-fund
npm run build
npm run build:admin
Pop-Location
```

两个产物分别为 `src/web/public/ui` 和 `src/web/public/admin`。当前仓库只有 pnpm 锁文件；上述 npm 与 Docker 中的 npm install 按 package.json 版本范围解析，不具备 npm ci 的锁定保证。需要可重复供应链时由项目维护者生成并审阅 package-lock.json 后切换 npm ci。可继续使用项目既有 pnpm 锁定构建，但不要混用来源不明的 node_modules。

从仓库根使用 [build.ps1](scripts/build.ps1) 生成本机或交叉编译产物：

```powershell
./src/go-service/scripts/build.ps1
./src/go-service/scripts/build.ps1 -TargetOS linux -TargetArch amd64
./src/go-service/scripts/build.ps1 -TargetOS linux -TargetArch arm64
```

Linux/macOS 可以执行 `bash src/go-service/scripts/build.sh linux amd64`（另可选 `linux arm64`、`darwin arm64` 或 `windows amd64`）。脚本固定关闭 CGO，仅构建二进制，不会打包前端或数据。Linux 服务部署时另复制已构建的 ui/admin。平台产物在 `src/go-service/bin/<os>-<arch>/`。

## Windows 本地启动

先完成前端和 Windows 构建，再执行：

```powershell
Set-Location src/go-service
Copy-Item .env.example .env
# 使用编辑器填写 .env 中管理员用户名、强密码，以及所需模型配置。
./scripts/run.ps1 -BootstrapAdmin
# bootstrap 成功后，从 .env 删除 GO_SERVICE_ADMIN_PASSWORD 或设空。
./scripts/run.ps1
```

默认监听 `http://127.0.0.1:5210/`，后台为 `/admin/`，健康检查 `/api/health`。管理员初始化只创建新账号，不会提升同名已有用户，也不会重置密码；不要放到每次启动脚本中。JSON 模式下保持数据库变量都空；如系统曾设置 `DATABASE_URL`，也要清空或显式在环境文件中设空。Windows 脚本使用固定工作目录，避免从其他目录启动产生另一份 data。

访问远端模型需把 `COMIC_*_URL` 设置为服务实现接受的兼容接口基地址（以模型提供商文档及生成客户端约定为准），填写真实 KEY/MODEL；这些值仅供 JSON 单机管理员使用。PG 模式从后台模型路由取配置，不使用这些变量作为运营路由替代。

## Docker Compose：PG 17 + Go

需要 Docker Engine / Docker Desktop 与 Compose v2。Compose 的构建上下文是仓库 `src`，不是 go-service。默认使用 [deploy/Dockerfile](deploy/Dockerfile) 及相邻的 [Dockerfile.dockerignore](deploy/Dockerfile.dockerignore)，该专属忽略文件排除数据、日志、密钥、node_modules 等。本目录 [.dockerignore](.dockerignore) 只在本目录作为构建上下文时生效；它不会过滤上一级 src 上下文。根 [Dockerfile](Dockerfile) 保留相同多阶段配方供审阅，推荐使用 deploy 路径来确保过滤生效。

镜像内 Node 多阶段同时执行 ui 和 admin Vite 构建；Go 阶段使用 `golang:1.27-bookworm`，最终镜像只含二进制、CA、健康检查工具和前端，由 UID 10001 运行。构建需要相应镜像标签可获取。离线或前端依赖构建受阻时，先在受控环境完成上一节前端构建，再由维护者调整前端 COPY 为已构建的 `web/public/ui` 与 `web/public/admin`，同时更新上下文白名单；不要跳过前端构建后宣称界面可用。

```powershell
Set-Location src/go-service
Copy-Item .env.example .env
# 在编辑器中配置 POSTGRES_PASSWORD（唯一随机值）与下面的匹配连接串。
# GO_SERVICE_DATABASE_URL=postgres://comic:<URI编码后的同一密码>@postgres:5432/comic?sslmode=disable
# 填写 GO_SERVICE_ADMIN_USERNAME/PASSWORD；不要照抄尖括号占位。
docker compose config --quiet
docker compose build
docker compose up -d postgres
docker compose run --rm app -bootstrap-admin
# 清空 .env 管理员密码；后续启动不会自动创建管理员。
docker compose up -d app
docker compose ps
Invoke-RestMethod http://127.0.0.1:5210/api/health
```

POSTGRES_PASSWORD 是 PG 的原始密码；连接串中的密码须 URI 编码（尤其包含 `@ : / # % $` 时）。Compose 值含 `$` 时要按 Compose 的字面量规则处理，优先在 .env 中用单引号包裹秘密并用 `docker compose config --quiet` 校验，避免输出完整 config 泄露值。不要使用示例密码。

每日额度按 Go 进程的本地日期分日。容器通常默认 UTC，而当前生产环境使用 Asia/Shanghai；Compose 显式设置 `TZ=${TZ:-Asia/Shanghai}`，镜像安装 tzdata。请在 .env 将 TZ 改成部署地实际 IANA 时区，并在重建镜像、重建容器后检查日期，避免额度在错误时刻跨日。Windows 本地按操作系统时区运行；Linux 原生 systemd 默认继承服务器时区，部署时核对 `timedatectl`，或在环境文件中显式设置 TZ。数据库会话时区也应与业务日期约定核对，不能只依赖镜像的默认 UTC。

PG 健康检查通过后应用才启动。PG 没有任何宿主端口映射；应用在容器内监听 `0.0.0.0:5210`，宿主仅绑定 `127.0.0.1:5210`。远程访问通过 HTTPS 反代。两个命名卷 `comic-data`、`postgres-data` 都应备份；不要执行 `docker compose down -v`。POSTGRES_PASSWORD 仅在数据库卷首次初始化时设置 PG 密码，修改 .env 不会旋转已存在数据库密码。

首次命名卷由镜像目录 UID 10001 初始化；如果改用宿主目录挂载，提前建立目录并赋予 UID/GID 10001 读写权限。容器内 127.0.0.1 指容器本身，不是宿主模型服务；按宿主平台配置可达地址，保持模型密钥只在服务端。

## Linux systemd

推荐独立低权限账号。先在构建机生成目标 Linux 二进制和前端，把二进制、ui/admin 上传到服务器暂存目录。下面命令从服务器仓库 `src/go-service` 目录执行，示例假定 amd64：

```bash
sudo useradd --system --home-dir /var/lib/comic --shell /usr/sbin/nologin comic
sudo install -d -o root -g root -m 755 /opt/comic /opt/comic/public
sudo install -d -o comic -g comic -m 700 /var/lib/comic
sudo install -d -o root -g root -m 700 /etc/comic
sudo install -m 755 bin/linux-amd64/comic-server /opt/comic/comic-server
sudo cp -a ../web/public/ui ../web/public/admin /opt/comic/public/
sudo install -m 600 .env.example /etc/comic/go-service.env
sudo install -m 644 deploy/comic-server.service /etc/systemd/system/comic-server.service
```

若账号已存在，跳过 useradd。用编辑器修改 `/etc/comic/go-service.env`，至少设置：

```dotenv
GO_SERVICE_ADDR=127.0.0.1:5210
GO_SERVICE_DATA_ROOT=/var/lib/comic
GO_SERVICE_PUBLIC_DIR=/opt/comic/public
GO_SERVICE_SECURE_COOKIES=true
```

填入所选 PG 连接串或 JSON 模型环境变量。HTTPS 尚未配置时本地测试用 false。systemd 的服务账号通过管理器读取 root-only EnvironmentFile；不用把该文件授予 Web 用户。首次管理员初始化可以在服务仍停止时用瞬态单次单元读取同一环境文件：

```bash
# 先在私有环境文件填写管理员用户名与密码。
sudo systemd-run --wait --pipe --collect --unit=comic-bootstrap \
  -p User=comic -p Group=comic -p WorkingDirectory=/opt/comic \
  -p EnvironmentFile=/etc/comic/go-service.env \
  /opt/comic/comic-server -bootstrap-admin
# 完成后从环境文件删去管理员密码。
sudo systemctl daemon-reload
sudo systemctl enable --now comic-server
curl --fail http://127.0.0.1:5210/api/health
sudo systemctl status comic-server --no-pager
```

[systemd 单元](deploy/comic-server.service) 仅允许 `/var/lib/comic` 写入，工作目录固定。若变更数据目录，必须同时修改 ReadWritePaths、目录权限和配置。应用日志在 `/var/lib/comic/logs/server.jsonl`；journal 仅用于进程启动失败/退出诊断。不要把日志目录映射到站点。升级前停止写入并备份，将新二进制和前端作为同一版本替换，再重启；回滚使用旧二进制、旧前端及相配套的数据备份，避免让旧版本直接写入未知新 schema。

## 宝塔与 nginx / SSE

宝塔可选两条路径：Go 项目管理器以独立用户运行 `/opt/comic/comic-server`、工作目录 `/opt/comic`、端口 5210 并配置同一组环境变量；或采用上面的 systemd，只由宝塔站点管理域名、证书与反代。选择一种进程管理方式，避免双重守护争抢端口。面板的默认环境不会自动读取 .env，应显式配置变量；若面板不能可靠管理秘密文件，使用 systemd。

在已签发证书的 HTTPS server 块加入 [nginx 配置片段](deploy/nginx.conf)，上游 `http://127.0.0.1:5210`。它包含 SSE 关闭缓冲、缓存、压缩以及 600 秒读写超时；上传限制 16m 与默认 Go JSON 请求上限一致，base64 图片通常会增加体积，应测量请求总大小再同时调整两端。不要额外启用宝塔静态缓存、API 缓存、SSE 压缩或短超时覆盖此配置。

```bash
sudo nginx -t
sudo systemctl reload nginx
curl --fail https://YOUR_DOMAIN/api/health
```

HTTPS 生效后设置 `GO_SERVICE_SECURE_COOKIES=true` 并重启 Go。`X-Forwarded-Proto` 用于保留代理信息，Cookie Secure 标志仍由配置控制。站点 root/alias 不得指向仓库、data、logs 或环境文件目录；所有文件访问交给 Go 的鉴权路由。检查 `/.env`、`/logs/server.jsonl`、`/api/logs/` 仅返回 404。SSE 测试需在已登录真实生成流程观察事件逐步到达；仅健康检查不证明流式传输正常。

## 备份与恢复

完整恢复点包含：PG 逻辑备份（如使用 PG）、整个数据根（含项目图片与 JSON）、对应版本二进制/前端、单独加密保管的环境文件。数据可能含会话与账号信息，备份目录应私有并加密异地保存。日志也在数据根内；定期安排停机或与进程协调的轮转，避免直接删除正在写入的日志句柄。

Linux/systemd：停止所有实例、定时生成任务和其他写入者后，使用 [backup.sh](scripts/backup.sh)。PG 模式要求客户端 pg_dump 与服务端主版本兼容（建议 PG17 客户端），并在当前 shell 设置同一数据库 URL；脚本通过环境传递给 pg_dump，不打印连接串。

```bash
sudo systemctl stop comic-server
# 在私有 shell 环境设置 GO_SERVICE_DATABASE_URL，JSON 模式清空两种数据库变量。
sudo --preserve-env=GO_SERVICE_DATABASE_URL,DATABASE_URL \
  bash scripts/backup.sh --confirm-stopped /var/lib/comic /PRIVATE_BACKUP/new-snapshot
sudo systemctl start comic-server
```

目录必须全新且在数据根之外；STATUS=complete 才算该次文件归档完成。若备份失败，先确认服务是否仍停止，再决定重试或恢复启动。不要在终端历史中直接输入包含密码的连接串。

[restore.sh](scripts/restore.sh) 只接受空数据目录；有 PG dump 时还检查目标库没有应用表，使用不清表的 pg_restore。恢复前准备新空库和新目录，配置 URL 指向**恢复库**，保持原服务停止或使用独立演练端口；脚本只恢复文件和数据库，不切换服务配置。

```bash
bash scripts/restore.sh --confirm-empty-targets /PRIVATE_BACKUP/new-snapshot /var/lib/comic-restore
sudo chown -R comic:comic /var/lib/comic-restore
# 审核恢复环境与 systemd ReadWritePaths 后，才切换数据根和数据库。
```

仅恢复来自可信备份的 tar；脚本不是处理任意上传归档的解包器。空库检查不代替操作者确认 URL。恢复成功后核对项目数、账号权限、图片、会话及运营账本，再启用写入。

Compose 备份：停 app，保留 PG 运行，备份 PG 与文件卷。以下从 go-service 目录的 bash 执行，备份目录必须新建：

```bash
umask 077
mkdir ./backups-NEW
docker compose stop app
# -T 不分配终端；二进制重定向应使用 bash，不用 Windows PowerShell 5。
docker compose exec -T postgres pg_dump -U comic -d comic -Fc > ./backups-NEW/postgres.dump
docker compose run --rm --no-deps --user 0 --entrypoint tar app \
  -czf - -C /app/data . > ./backups-NEW/data.tar.gz
docker compose start app
```

新 Compose 项目恢复演练可用 `-p comic-restore` 分离命名卷，先为该项目准备独立 .env/compose 副本，改宿主端口（如 `127.0.0.1:15210:5210`）。仅启动其 postgres，然后用 `exec -T postgres pg_restore -U comic -d comic --exit-on-error --no-owner --no-acl < postgres.dump`；文件使用 `run --rm --no-deps --user 0 --entrypoint sh app -c 'tar -xzf - -C /app/data && chown -R 10001:10001 /app/data' < data.tar.gz`，再启动 app。仅针对新空卷和空库操作。备份时的 shell 每一步都须检查退出状态，失败的归档不可作为恢复点。

Windows JSON 模式：Ctrl+C 停止进程后，将整个 data 复制到新的私有备份目录（可使用下面的复制脚本）；恢复到新的空目录，设置 GO_SERVICE_DATA_ROOT 指向新目录验证。PG 模式另外用 pg_dump/pg_restore，勿把 PG 数据目录直接当作逻辑备份跨平台复制。

## Node → Go：副本演练后再切换

迁移是独立验收工作。Go 与 Node 的数据库/schema、会话、密码格式和旧项目 root 可能不同；直接指向原 Node 数据库或原 data 不是迁移。现有离线 `cmd/migrate` 工具支持显式导入旧 JSON 账号/会话/项目索引，以及对 `root='.'` 的旧项目执行备份优先的目录迁移；常规服务启动不自动运行这些转换。Go 项目根必须是 `projects/<projectID>`。所有命令先在停写后的副本上演练，审阅归属和权限再应用。

1. 记录 Node 版本、依赖、数据路径、PG schema、用户/项目数量、模型与支付设置；停止 Node 写入并生成可恢复备份。保留 Node 原目录与原库，记录回切步骤。
2. Windows 使用 [copy-node-rehearsal.ps1](scripts/copy-node-rehearsal.ps1) 生成副本；该脚本仅复制，不宣称转换 schema。目标必须不存在且位于原数据目录之外，不跟随目录联接。

   ```powershell
   ./src/go-service/scripts/copy-node-rehearsal.ps1 -SourceData 'D:\private\node-data' -Destination 'D:\private\go-rehearsal' -ConfirmSourceStopped
   ```

3. PG 从 Node 逻辑备份恢复到独立演练库，用私有网络和单独凭证。先对照两端 schema/约束与账号哈希，设计并审阅显式转换；发现不兼容时停止转换，保留副本以供修正。不要让 Go 启动的自动建表修改 Node 原库。
4. 演练实例设 `GO_SERVICE_ADDR=127.0.0.1:15210`、独立 data root、独立数据库与 `GO_SERVICE_COOKIE_NAME=comic_go_rehearsal`；换用测试模型与支付沙箱，断开正式回调/任务，避免真实扣费。JSON 仅用于单机验证，不承担运营迁移目标。
5. 完成并审阅转换后，在副本上验证账号登录/改密、租户隔离、项目与状态、历史图片/导出、生成及取消、SSE、模型路由、额度/钱包/支付幂等与补偿。逐项留证，记录未覆盖项。显式 bootstrap 只用于新管理员，不会修复旧账号。共享旧 root 应显式重排到各自项目目录并同步索引，不能只改 root 字段让其指向共享目录。
6. 校验数据总量、项目归属和关键账本余额，运行恢复演练；确定停机窗口，再冻结 Node 写入、重做最终快照与转换，核对差异后切换反代。禁止 Node 与 Go 同时写入同一数据根或数据库。
7. 回切时先停止 Go 写入，恢复 Node 对应快照并把反代切回 Node。切换后产生的新账目/项目需人工审阅与对账后显式回灌，不能靠混合两端 data 解决。

### 离线 JSON 导入与 legacy 项目 CLI

迁移程序单独构建，二进制不在运行镜像的 ENTRYPOINT 中。以下从 go-service 目录执行；只构建工具，不启动服务：

```powershell
go build -trimpath -o ./bin/comic-migrate.exe ./cmd/migrate
$env:GO_SERVICE_DATA_ROOT='D:\private\go-rehearsal'
$env:GO_SERVICE_DATABASE_URL=''
$env:DATABASE_URL=''
# 默认只验证、列计划，不写 go-store.json。
./bin/comic-migrate.exe -migrate-json
# 审核计划后，仍保持所有服务停止，再显式导入。
./bin/comic-migrate.exe -migrate-json -apply
```

Linux 编译为 `comic-migrate` 并使用同样标志。工具直接读取进程环境，不自动读 .env。导入读取 `accounts.json`、可选 `sessions.json` 与 `projects/index.json`，验证 ID、用户名、16 字节盐与 32 字节哈希、会话引用和项目归属；过期会话跳过。当前兼容规则保留原 isAdmin，并将旧账号数组第一位视为管理员；缺少 ownerId 的项目归属第一位账号。因此必须审核 dry-run 的 ownerAssignments 和源账号顺序，不能把导入当成自动权限审计。该命令只支持 JSON 目标，存在 go-store.json 时停止，避免覆盖已有 Go 快照。

应用时将原 JSON 备份到数据根的 `migration-backups`，对共享根项目复制指定目录及改写受支持的图片路径，再独占发布 Go 快照；原数据保留。操作需要额外磁盘空间。失败或部分完成时保留副本、manifest 和日志，检查现场后从新的原始副本重演，不要反复盲目重试或清理原件。JSON 导入不是 JSON→PG 账本迁移，也没有把 JSON 单机模式变为运营模式。

对于已经存在 Go 快照或已验证 schema 的 PG 副本，可分项目执行 legacy 迁移：

```powershell
# GO_SERVICE_DATA_ROOT 指向副本；PG 模式另设置该副本数据库连接串。
./bin/comic-migrate.exe -dry-run
./bin/comic-migrate.exe -apply -project PROJECT_ID
# 如需仅回滚项目 root 元数据，使用该次实际 manifest 路径。
./bin/comic-migrate.exe -apply -rollback-manifest 'D:\private\go-rehearsal\migration-backups\ACTUAL_RUN\manifest.json'
```

工具在 PG 模式以 SkipSchema 打开副本，不自动初始化 schema。legacy 迁移范围为 `projects/recent-project.json`、config、output、references、assets，先备份，后复制到 `projects/<ID>`，最后提交索引。符号链接及特殊文件会中止迁移；目标目录必须不存在。`-rollback-manifest` 只回滚 root/legacy 元数据并保留所有复制文件，不会合并迁移后的新写入，回滚成共享根的项目也不会因此变得可被 Go 正常访问。它不代替前述整库与数据根恢复方案。

### 存储、计费与异步任务的运行边界

PG 管理接口已具备存储目标配置、主/写目标选择、local、S3 兼容和 WebDAV 写入，以及写读删探测与写入审计。S3 兼容类别包含 s3/oss/cos/r2/kodo，采用原生 SigV4；不同供应商仍需分别验证区域、桶权限、endpoint 与公共读取地址。远端请求不跟随重定向，避免凭证被转发。凭证留在服务端，后台列表仅显示是否已配置；探测会实际创建及删除测试对象，应使用独立测试前缀与最小权限。当前应用生成图片仍写入项目 data，配置远端目标不代表全部既有文件已经迁走；保持整个数据根备份。主程序应设置 operations.Service.DataRoot 并接入 QueueSnapshot/RenderJobs 回调，部署前核对该集成。

计费使用微积分整型、事务账本和稳定引用去重；钱包扣费与支付结算依赖 PG。逻辑路由按优先级/权重选择，用户选择的可用模型优先，再回退逻辑默认。调用前按启用状态及管理员是否收费策略预扣；失败路径发起补偿退款，token 模式按实际输入/缓存/输出用量结算差额。调用、补偿、实际用量缺失等异常仍须专项验收和对账，不能仅凭 HTTP 成功认定账本完整。管理端付费模型探测保持关闭；渠道连通性和有界模型列表 GET 不等价于生成测试。支付订单支持套餐或 1–100000 元自定义金额，只有显式创建订单才请求网关，回调需验证商户、签名、状态、交易号和金额。

异步渲染通过 `/api/render/start|pause|resume|stop|status` 与鉴权 `/api/events` SSE 提供状态，队列持久化在数据根的 `queue/runs.json`。单项目并发限制 1–4、进程全局在途最多 8。暂停停止派发新任务，允许已在途任务完成；stop 后新 run 的状态应与旧任务完成隔离，不应把停止按钮理解为已撤回发给上游的生成请求。重启时 running 任务按尝试次数重新排队，达到 3 次的中断任务记失败；重试可能让上游再次处理请求，应对账并核对图片结果。暂停状态保留，不会自动恢复派发。当前是进程内调度及本地队列文件，避免多实例共享同一 data/queue；即使使用 PG，也不能直接横向扩容成共享工作队列。

## 交付验证边界

已执行的结果详见 [VALIDATION.md](VALIDATION.md)：Windows 全量 test/vet 与 Linux Docker Go 1.27 + PG16 全量测试通过；真实 PG 已覆盖低余额实际用量结算、无 usage 单价扣费、ready 恢复、页额度及邀请注册。镜像构建通过；隔离 5211 实例 UI/admin 返回 200，日志路径 404，匿名 SSE 401。生产数据仅在副本演练：13 个项目，当前项目 8 个角色、13 页关联 21 张图片可读，空图保存仍保留 8 个角色图；原生产未写入。生产 2.8.15 UI 重建后浏览器访问 `?logs=1` 未见运行/任务日志按钮。

仍需分别验收：真实付费模型与图中文字形、云 S3/WebDAV（当前为 httptest 协议测试）、PG17 模板、宝塔/systemd/HTTPS/SSE 与上传实际部署、完整备份恢复及生产切换。上游响应至 ready stage 落库间存在崩溃后重复调用窗口；分钟限流和队列面向单进程，多副本共享部署未支持。归档与最终一轮复测状态由主代理完成后更新实际验证记录，本轮文档更新不重复执行测试。
