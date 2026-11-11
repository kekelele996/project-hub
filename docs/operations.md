# 运行、备份与验收

## 运行

- 用 `docker compose logs --tail=100 api` 检查 API 和分析任务错误。
- 用 `docker compose ps` 检查三个容器健康状态。
- `/healthz` 对数据库执行连通性检查；API 容器不可用时 Web 反向代理返回错误。
- 分析任务保存在 PostgreSQL，服务重启后继续处理未完成任务。
- 数据库 migrations 在启动时运行；上线前备份数据库和 uploads。
- 运行单 API/单分析 worker，第一版不需 Redis。几百到几千工程使用服务端分页和数据库筛选。

配置 `BACKUP_MASTER_KEY` 后启用 GitHub 灾备 worker；凭证密文保存在数据库，主密钥必须另行备份。仓库副本保存工程 ZIP、截图和清单，不能替代数据库备份。详见 [GitHub 多仓库灾备](github-backups.md)。

## 备份与恢复

数据库与 ZIP/截图应在同一维护窗口备份。以下操作暂停写入，数据库保持运行。

```sh
docker compose stop web api
mkdir -p backups
docker compose exec -T postgres pg_dump -U projecthub -d projecthub -Fc > backups/database.dump
docker compose run --rm --no-deps --entrypoint tar api -czf - -C /data uploads > backups/uploads.tar.gz
docker compose up -d api web
```

恢复到相同版本或更新版本的空环境。停止 API/Web，在数据库已启动的情况下执行：

```sh
docker compose exec -T postgres pg_restore -U projecthub -d projecthub --clean --if-exists < backups/database.dump
docker compose run --rm --no-deps --entrypoint tar api -xzf - -C /data < backups/uploads.tar.gz
docker compose up -d api web
```

恢复操作会覆盖现有数据库。备份文件包含源码和账户数据，应保存于受控位置。不要把 uploads、`.env` 或数据库备份提交到 Git。

## 验收

先完成静态构建与 Go 检查：

```sh
# backend 目录
SCC_BINARY="$(go env GOPATH)/bin/scc" go test ./...
go vet ./...

# frontend 目录
npm run build

```

Go 的 PostgreSQL 测试使用独立临时 schema，结束后删除该 schema；设置 `TEST_DATABASE_URL` 才会运行，未设置时明确跳过。使用专门测试数据库执行：

```sh
# backend 目录，连接字符串指向测试数据库
TEST_DATABASE_URL="$PROJECT_HUB_TEST_DATABASE_URL" \
SCC_BINARY="$(go env GOPATH)/bin/scc" go test -race ./...
```

API 集成验收应使用独立的 Compose 项目。`compose.verification.yml` 复用已构建的 `project-hub-api:latest`、`project-hub-web:latest` 镜像；`-p project-hub-verification` 为容器、网络、数据库卷和 uploads 卷建立独立名称，Web 仅开放本机 8089 端口。先在项目根目录运行 `docker compose build`，再在单独终端执行：

```sh
# 项目根目录；密码仅用于本机隔离验收
export POSTGRES_PASSWORD=verification-database-local
export ADMIN_USERNAME=admin
export ADMIN_PASSWORD=verification-admin-local
export SECURE_COOKIE=false WEB_BIND=127.0.0.1 WEB_PORT=8089

docker compose -p project-hub-verification \
  -f docker-compose.yml -f compose.verification.yml \
  up -d --no-build --wait

npm --prefix scripts ci
PROJECT_HUB_BASE_URL=http://localhost:8089 \
PROJECT_HUB_ADMIN_USERNAME="$ADMIN_USERNAME" \
PROJECT_HUB_ADMIN_PASSWORD="$ADMIN_PASSWORD" \
node scripts/e2e.mjs

# 结束后停止验收容器，保留验收卷供复查
docker compose -p project-hub-verification \
  -f docker-compose.yml -f compose.verification.yml down
```

初始管理员只在没有管理员的数据库中创建；复用已有验收卷时，应使用该卷当前管理员密码，修改环境变量不会重置密码。脚本通过真实 PostgreSQL、真实上传及 scc 分析验证领取、版本固定和权限撤销，创建带唯一前缀的用户、工程和项目并保留数据。不要指向正式 8088 环境；测试图像仅是上传格式夹具，不代表工程运行截图。

验收工程添加时使用 `/api/v1/admin/engineering?exclude_project=项目UUID`，只排除该项目仍有效的引用，软移除的历史引用不排除。用户 `role/active` 与项目 `q/status` 筛选在服务端执行，返回的 `total` 与分页使用同一条件。

管理员 Bearer 令牌可通过 `POST /api/v1/admin/engineering-versions` 上传 ZIP 和截图，再轮询 `GET /api/v1/admin/analysis-jobs/{id}`。Bearer 写请求无需 CSRF；成员令牌始终只读，上传会返回403。上传不属于 `/open/v1` 查询接口。

浏览器证据保存在忽略的 `output/playwright` 目录。ZIP 下载流开始后允许传输完成；撤销权限会阻止之后的新请求。
