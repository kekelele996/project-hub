# 工程台

面向团队代码资产管理的 Web 平台。工程池保存不可变 ZIP 版本、代码语言统计与真实截图；管理项目明确引用工程版本，成员在项目内独占领取。管理员可以通过 API 自动上传与分析工程。

## 启动

需要 Docker 和 Docker Compose。首次启动将 `.env.example` 作为 `.env` 的配置基础，填写数据库和管理员密码，然后执行：

```sh
docker compose up -d --build
docker compose ps
```

默认访问 [工程台](http://localhost:8088)。初始管理员由 `ADMIN_USERNAME`、`ADMIN_PASSWORD` 配置；仅在数据库没有管理员时创建，修改环境变量不会重置已有密码。在账户菜单中修改密码。

本机 HTTP 预览使用 `SECURE_COOKIE=false`；服务器请通过 HTTPS 反向代理访问并设为 `true`。默认仅绑定 `127.0.0.1`，可以通过反向代理转发。如需内网直接访问，修改 `WEB_BIND` 和 Cookie 配置。数据库和 API 不暴露公网端口。

## 核心规则

- 工程上传使用 ZIP、GitHub 地址与固定 commit/tag、至少一张启动截图和一张功能截图。平台不会启动上传的代码，截图由人工或外部脚本采集。
- 使用 scc 统计有效代码行，排除依赖、Git、构建目录和非源码语言。筛选使用源码文件数量，另外展示 ZIP 总文件数量。分析失败可重试。
- 新的 ZIP 创建新版本。项目固定引用版本，新增工程不会自动进入现有项目。升级未领取工程必须由管理员明确操作。
- 相同工程在不同项目可以复用；同项目同时只有一个领取者。成员可以归还，管理员可以回收或转交。
- 工程使用两级标签分类：所属领域与子方向，每个领域有独立的「其他」子方向并固定排最后。工程只能打子方向标签，工程池可按标签筛选；项目新建或添加工程时也可按工程标签筛选候选。标签对所有登录用户可读，仅管理员维护，存在子方向或正被工程使用的标签不能删除。
- 项目日期存 UTC，项目日期输入按上海时区转换。只有开放且在起止时间范围内的项目可新领取；到期后当前领取者仍可下载。
- 成员可查看所属项目的摘要与截图；ZIP 和 GitHub 地址只对当前领取者与管理员开放。公开 GitHub 仓库的外部访问不由平台控制。
- 移出成员会在同一事务中回收该成员的工程。停用账号会撤销会话和令牌并阻止访问，领取历史保留，管理员可回收或转交。
- Cookie 会话有效期 365 天，活跃访问续期。退出、改密码和停用账号使服务端会话失效。写请求需要 CSRF；Bearer 令牌仅管理员可写，普通成员只读。

## 自动化 API

登录后在「API 令牌」创建令牌。明文只展示一次，可随时撤销。成员令牌只读，管理员令牌可以上传、分析和保存工程。令牌身份决定查询范围，无需传用户名。

```sh
curl -H "Authorization: Bearer $PROJECT_HUB_TOKEN" \
  http://localhost:8088/open/v1/me/projects

curl -H "Authorization: Bearer $PROJECT_HUB_TOKEN" \
  "http://localhost:8088/open/v1/projects/$PROJECT_ID/engineering"

curl -H "Authorization: Bearer $PROJECT_HUB_TOKEN" \
  http://localhost:8088/api/v1/admin/engineering-versions \
  -F 'name=代码工程' -F 'description=工程说明' \
  -F 'tech_stack=["Go","React"]' \
  -F 'backup_repo_name=elderly-care' \
  -F 'github_url=https://github.com/your-org/your-repo' \
  -F 'github_ref=v1.0.0' -F 'zip=@source.zip' \
  -F 'startup_screenshot=@startup.png' \
  -F 'feature_screenshots=@feature.png'
```

上传响应包含 `job_id`；轮询 `/api/v1/admin/analysis-jobs/{id}`，`ready` 后可以加入项目。全局配置中的每个可用灾备账号自动建立独立副本，上传无需选择账号或绑定仓库；响应 `backup_targets` 列出所有目标，`backup_status=unconfigured` 表示暂无可用灾备配置。查询 `/api/v1/admin/backup-jobs?engineering_id=...` 至所有目标任务 `ready` 才表示远端校验完成。追加版本沿用原仓库，新增账号自动补齐已有工程。完整请求约定见 `API_CONTRACT.md`，OpenAPI 见 `docs/openapi.yaml`。

管理员上传使用 `/api/v1/admin/engineering-versions`，携带 Bearer 令牌时不需要 Cookie 或 CSRF；成员令牌不能上传。新建工程可在 multipart 中附 `tag_ids`（JSON 字符串数组）设置工程标签，追加版本忽略该字段。`/open/v1` 提供只读查询与受鉴权下载，工程详情中的下载地址字段为 `zip_url`。

工程列表支持语言、技术栈、标签（`tag_ids`：选择所属领域会命中其全部子方向，多个标签取 OR，参数可重复或逗号分隔）、源码文件数量、分析状态及 `archived=false/true/all` 筛选。添加工程时可传 `exclude_project=项目UUID`，由数据库排除项目有效引用。用户列表支持 `q,role,active`；项目列表支持名称与描述搜索及 `active/upcoming/expired/paused/archived` 筛选。所有分页总数按筛选结果计算。

## 本地开发

前端需要 Node 22+，后端需要 Go 1.25+、PostgreSQL 17 和 scc 3.7.0。

```sh
go install github.com/boyter/scc/v3@v3.7.0
```

后端在 `backend` 目录运行，配置 `DATABASE_URL`、`ADMIN_PASSWORD`、`UPLOAD_DIR`、`SCC_BINARY`。前端在 `frontend` 目录运行 `npm ci`、`npm run dev -- --host 127.0.0.1`。Vite 转发 API 到 8080。

验收使用独立 Compose 项目和 [8089 验收入口](http://localhost:8089)，数据库与上传卷同正式项目隔离。构建镜像后，在单独终端执行：

```sh
export POSTGRES_PASSWORD=verification-database-local
export ADMIN_USERNAME=admin
export ADMIN_PASSWORD=verification-admin-local
export SECURE_COOKIE=false WEB_BIND=127.0.0.1 WEB_PORT=8089
docker compose -p project-hub-verification -f docker-compose.yml -f compose.verification.yml up -d --no-build --wait
npm --prefix scripts ci
PROJECT_HUB_BASE_URL=http://localhost:8089 \
PROJECT_HUB_ADMIN_USERNAME="$ADMIN_USERNAME" \
PROJECT_HUB_ADMIN_PASSWORD="$ADMIN_PASSWORD" \
node scripts/e2e.mjs
```

上述密码仅用于本机隔离验收环境。脚本会创建测试数据，应使用 8089 验收环境运行。完整验收、备份与恢复方式见 `docs/operations.md`。

实际验收结果见 [验收记录](docs/verification.md)。可重复执行的 Playwright 流程及截图报告说明见 [浏览器验收](scripts/BROWSER_TESTS.md)。

## GitHub 多仓库灾备

GitHub 账号作为全局灾备配置，上传工程自动备份到所有已启用且可用的账号，无需逐工程选账号或绑定仓库。按 `ph-<短名>-<工程ID>` 自动创建私有仓库，异步保存 ZIP、截图与校验清单；后续版本更新 Release，新增账号自动补齐已有工程。也支持手动绑定已有仓库、失败重试和校验恢复。Token 使用独立 `BACKUP_MASTER_KEY` 加密，查询仅显示尾号。首次配置、Token 权限及恢复说明见 [GitHub 灾备](docs/github-backups.md)。
