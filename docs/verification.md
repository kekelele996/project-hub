# 验收记录

验收日期：2026-10-03（Asia/Shanghai）。下列结果来自实际运行，正式预览与验收环境使用独立数据库及上传卷。

## 标签体系验收（工程池工程）

2026-11-10 新增两级标签体系，OpenAPI 版本 1.8.0。标签树固定两级：所属领域（根标签）与子方向（叶子标签）；迁移 `007_tags.sql` 写入固定 UUID 种子：安全（漏洞检测与攻防、其他）、游戏、图形与多媒体（音视频处理、图形渲染、其他）、数据工程（数据采集、爬虫与 RPA、其他），共 3 个领域、7 个子方向、10 个标签，`其他` 固定在子方向最后。初版实现曾把标签挂在「项目」上，经确认后修正为挂在「工程池的工程（engineering）」上：迁移 `008_engineering_tags.sql` 新建 `engineering_tags`，把旧 `project_tags` 中可归属的数据按项目引用迁移到对应工程，然后删除旧表；正式库旧表已删除，既有工程与项目数据保留。

- 工程可挂 0..N 个子方向标签，接口拒绝直接挂领域；`PATCH /api/v1/admin/engineering/{id}` 传 `tag_ids` 整体替换、省略保持；新建工程上传 multipart 可附 `tag_ids`（仅新建生效，追加版本忽略）；工程列表/详情与版本对象返回 `tags`（含 `parent_name`）。
- 工程池列表（`GET /api/v1/admin/engineering`）新增 `tag_ids` 筛选：重复参数或逗号分隔、最多 50 个，选择领域自动含其全部子方向，多标签为 OR，非法或不存在的标签 400 `标签筛选无效`，与 `q`、`language`、`tech_stack`、`status`、`min_files`、`max_files`、`archived`、`exclude_project` 组合生效。
- `GET /api/v1/tags`（全部登录用户可读）返回完整树与 `engineering_count`（该标签本身及全部后代标签去重的工程数），`children` 恒为数组；管理员 `POST/PATCH/DELETE /api/v1/admin/tags` 支持两级限制、名称/父级校验、默认排序、同父级重名 409、有子方向或被工程使用时删除 409（`该工程正使用此标签，无法删除`），写入 `tag.create/update/delete` 审计。
- 前端：工程池页新增按领域分组的标签树筛选（领域整域、子方向多选、清除、`aria-pressed`）；表格行与截图卡片展示标签（分别最多 4 / 3 个，其余 `+N`）；工程详情展示「所属领域标签」，编辑信息可多选标签；上传新工程表单提供标签选择；「标签管理」页 `/tags` 维护领域/子方向并显示工程计数。项目页不再有项目标签 UI 或对项目的 `tag_ids` 请求；成员无标签管理入口。
- 项目管理选择工程时同样可按工程标签筛选：新建项目的「工程范围」Tab 与项目详情「添加工程」抽屉均提供标签树筛选，并与搜索/语言/文件数/技术栈组合生效；候选行显示工程标签（最多 3 个）。该查询复用 `GET /api/v1/admin/engineering` 的 `tag_ids` 参数，未新增接口。

验证结果：

| 检查 | 结果 | 范围 |
| --- | --- | --- |
| Go `gofmt` / `go vet` | 通过，无输出 | `backend/internal/app` |
| Go `go test -race ./...` | 通过，54 项通过、0 失败、1 项手动浏览器 helper 跳过（20.7 秒） | 真实隔离 PostgreSQL 17；10 个标签测试覆盖种子树与排序、成员只读、管理员 CRUD、两级/重名、工程标签整体替换与省略保持、工程池筛选含后代与 OR、`engineering_count` 去重、删除保护与审计、008 迁移与旧表迁移分支、项目回退、上传 `tag_ids`、空领域 `children` 回归 |
| 前端 `npm run build` | 通过（tsc strict + Vite） | JS 440.26 kB、CSS 72.47 kB |
| 8089 隔离环境 API 集成验收 `scripts/e2e.mjs` | 26/26 通过 | PASS 04-08 为工程标签用例（种子树、建标签、工程 `tag_ids` 整体替换与省略保持、工程池领域/子方向/OR 筛选、删除保护与解绑清理），原 21 组用例回归通过 |
| 8089 隔离环境浏览器验收 Playwright | 2/2 通过 | 新增 `scripts/browser-tests/tags.spec.mjs`：API 创建两份真实工程 → 标签管理建空领域/子方向 → 工程详情编辑标签 → 工程池领域/子方向/多选 OR 筛选 → 行内 4+N 折叠 → 新建项目「工程范围」按标签筛选候选并创建项目 → 项目详情「添加工程」按标签筛选并加入 → 删除保护 409（新文案）→ 390×844 无横向溢出；原 `workflow.spec.mjs` 全流程回归通过 |
| OpenAPI | 1.8.0 合法；`docs/openapi.yaml` 与 `frontend/public/openapi.yaml` 逐字节一致，8088 线上副本逐字节一致 | 3 个标签路径、Tag/EngineeringTag schema、`engineering_count`、工程列表 TagIDs、工程 PATCH/上传 `tag_ids`；项目路径无标签字段 |
| 正式 8088 | 三个容器健康；迁移 008 已应用，`project_tags` 已删除、`engineering_tags` 存在；标签树 total=10；既有 92 个工程保留且均暂无标签；线上 OpenAPI 与源文件逐字节一致 | 未执行任何真实 GitHub 写入 |

浏览器证据位于 `output/tags-verification/`：`01-tag-admin.png`（标签管理与工程计数）、`02-engineering-detail-tags.png`（工程详情标签与编辑后展示）、`03-engineering-cards-tags.png`（卡片标签）、`04-engineering-filter.png`（工程池领域筛选与行内标签）、`05-engineering-filter-or.png`（多选 OR）、`06-engineering-tags-folded.png`（4+N 折叠）、`07-mobile-engineering-tags.png`、`08-mobile-tag-admin.png`、`09-project-create-tag-filter.png`（新建项目工程范围按标签筛选候选）、`10-add-engineering-tag-filter.png`（添加工程抽屉按标签筛选候选）。截图均为 8089 隔离环境的验收数据。

限制与说明：

- 原浏览器流程 `workflow.spec.mjs` 的上传表单要求至少一个「已验证、启用且未到期」的 GitHub 灾备账号才会启用「上传并分析」；隔离环境没有真实账号，本轮仅在 8089 临时数据库插入一条不可解密的模拟账号以满足该表单门禁，未向 GitHub 发起建仓或上传，备份任务按预期失败且不影响上传、分析、项目与领取流程；该账号随临时环境一并销毁，正式库未插入。
- 首发整套浏览器验收时 workflow 用例曾因登录页截图等待字体加载超时（本机字体加载偶发），单独重跑及最终整套重跑均通过（最终 2/2）。
- 标签筛选的多选语义为「或」；选择领域时前端展开为其全部子方向后按子方向 ID 请求（等价于后端递归展开）。新增子方向后，已有筛选选择需要重新点选才会纳入。
- 标签树固定两级；本轮不提供移动标签所属领域；旧有工程不会自动打标签，需要管理员在工程详情「编辑信息」中逐个补充，或新建工程时随上传表单提交。
- 迁移 008 会把旧 `project_tags` 中能通过项目引用定位到工程的数据迁移到 `engineering_tags`；正式库旧表仅有 1 条项目标签且其项目没有任何工程引用，因此没有可迁移数据，旧表已按计划删除。

## 千级工程灾备列表验收

### 跨账号与多版本收纳

最终界面使用「工程→版本→账号备份」层级，OpenAPI 1.6.0。同一工程在不同账号下的仓库合并为一个工程行，默认折叠；展开后同版本账号共用版本栏，分别显示仓库、状态和操作。外层每页20个工程，展开每页10个版本；先分页版本再加载账号记录，单版本不跨页拆散。已有任务列表与按仓库分组接口保留兼容。

真实隔离 PostgreSQL 的全量 race 测试通过（16.372秒），新增验证1000工程、2000仓库、20000任务的工程分组与跨账号版本去重、两页版本顺序及每版本包含两个账号、状态过滤、同仓库跨目标合并和恢复任务不增加版本计数。前端构建、go vet、Swagger 校验通过。

内置浏览器确认默认每页20个工程、不渲染版本；搜索00042并筛选已校验同步后，显示一个工程、两个账号、两个仓库、两个版本、四条记录。展开只渲染两个版本栏（rowspan均为2）和四条账号记录。桌面1280px和手机390×844无页面横向溢出，移动端表格内部可滚动；没有浏览器错误。证据：`output/repository-group-verification/accounts-merged-desktop.png` 与 `accounts-merged-mobile.png`，均为隔离模拟数据。

正式8088已更新，三个容器健康；线上 OpenAPI 与源文件逐字节一致。正式数据库保留同一工程的两个版本、两个账号及四条ready备份；本轮未向真实 GitHub 建仓或上传新资产。临时验收环境结束后关闭。

2026-10-03 更新 OpenAPI 至 1.4.0。备份仓库和同步记录增加服务端搜索、状态筛选与页码跳转；每页20条。仓库摘要先分页再查询最近任务，新增10个索引。后台历史补齐扫描与文件上传分别运行，完成页轮询降为30秒。上传表单读取全局 `usable_total`，不再加载和拼接全部账号。

- 隔离 PostgreSQL 批量生成1000个工程、2000个备份目标、10000个工程版本和20000条任务；使用模拟账号，规模浏览器环境不启动后台备份 worker，没有向真实 GitHub 批量建仓或上传。
- 集成测试遍历全部100页备份目标，无重复；任务首尾及深页共8页均为20条、无重复、单页 JSON 小于32 KiB。搜索、账号、启用状态、任务状态和任务类型组合筛选的总数符合预期，非法过滤条件返回400。EXPLAIN 确认最近任务查询使用 `backup_jobs_target_created` 索引。
- 103个账号的回归检查只请求一条列表记录，仍返回全局3个可用账号；停用、到期和未验证账号不计入。
- 内置浏览器确认任务第1000页显示 `1000 / 1000`、20行；工程00042筛选出4条已校验同步任务和2个启用仓库。加载期间不显示虚假的总页数。
- 桌面1280px、手机390×844页面宽度均未超过视口，表格可在内部横向滚动。工程池含1000个生成工程及1个基础夹具，1001条总数仅渲染本页20行；浏览器未记录错误。
- 最终 `go test -race ./...` 使用真实隔离数据库及 scc，通过（15.572秒）；`go vet ./...`、前端构建、Swagger 校验、公开 OpenAPI 副本一致性均通过。正式8088已部署，三个容器健康，迁移 `006_backup_scale_indexes.sql` 和10个新索引已生效；线上 OpenAPI 与源文件逐字节一致，原有4条 sync 任务均为 ready。

证据位于 `output/backup-scale-verification/`：`jobs-page-1000-desktop.png`、`jobs-filtered-desktop.png`、`targets-desktop.png`、`targets-filtered-mobile.png`、`engineering-desktop.png`。截图中的模拟仓库链接及状态仅用于渲染验收。

本轮验证覆盖上述数据量的查询及渲染，未执行并发负载或真实 GitHub 批量上传测试。单实例文件上传仍串行执行，吞吐取决于文件大小、网络及 GitHub 限流；当前 OFFSET 分页与子串搜索并不保证任意规模下的固定响应时间。

## 全局配置自动灾备验收

2026-10-03 按全局 GitHub 灾备账号自动备份，OpenAPI 版本 1.3.0。上传不需要选择账号或手动建立工程目标；每个可用账号自动对应一个私有副本。后台自动补齐已有工程和新启用账号的历史 ready 版本。

真实隔离 PostgreSQL 的 `go test -race ./...`、`go vet ./...`、前端构建及 Swagger 校验均通过。新增集成测试用真实上传 API 和 ZIP 分析验证：无需账号字段自动备份、新账号补齐已有工程、全部账号的新版本更新、幂等补齐、停用账号和目标被尊重、缺少密钥时返回明确的 unconfigured 状态。自动测试使用本机 GitHub HTTP 模拟服务。

正式 8088 更新后，已上传的「社区老人关怀服务平台」自动补齐真实 GitHub 副本：`tiezhu996` 与 `kekelele996` 两个账号各创建一个私有仓库 `ph-engineering-50d63ebffe9e42509ae4810803079af3`，各保存 v1、v2 两个 Release，共四条 sync 任务均为 `ready`，逐文件远端 SHA-256 校验完成且无错误。另用保存的各账号凭证执行 GitHub 只读请求，确认两个仓库均为 private，已完成的 Release 地址及 ZIP、截图、manifest 资产存在；未输出明文 Token。原工程和两个版本没有重新上传或更改。

## 初版自动建仓验收历史

2026-10-03 新增工程池上传自动灾备，OpenAPI 版本 1.2.0。`backup_account_id` 与 `backup_repo_name` 成对保存，分析完成后自动私有建仓；追加版本沿用同一仓库，按版本保留 Release。

- 在临时 PostgreSQL 容器 `project-hub-auto-backup-test-db` 执行 `go test -race ./...`，全部通过；`go vet ./...` 通过。测试使用 GitHub HTTP mock，没有向真实 GitHub 建仓或上传。
- 新增测试验证短名限制、不同工程同短名不冲突、私有仓库初始化、归属标记匹配、无关同名仓库和公开仓库拒绝、账号不匹配拒绝、建仓权限不足失败及重试。
- 真实上传 API 与真实 ZIP 分析验证同事务保存自动同步目标、ready 后入队、建仓失败重试成功、新版本复用仓库和保留旧 Release、相同短名幂等复用、改名冲突回滚、版本恢复。
- 内置浏览器在隔离 schema、模拟账号环境真实上传两个版本，两个版本均显示「已校验」，仓库只有一个。桌面无浏览器错误；390×844 上传表单无横向溢出，截图确认长仓库名换行。
- 前端生产构建与 Docker 镜像构建通过。正式 8088 已更新，三个容器健康；在线 `/openapi.yaml` 与源文件逐字节一致，Swagger 校验通过。
- 证据为 `output/autobackup-verification/browser-auto-backup.png`、`browser-two-versions.png`、`mobile-upload.png`。ZIP 和图片均是测试夹具；模拟 GitHub 的 Release 链接不代表真实远端仓库。

初版验收当时未执行真实 GitHub 建仓或历史工程迁移。该限制已由后续的全局配置自动灾备流程调整。

## 最终结果

| 检查 | 结果 | 范围 |
| --- | --- | --- |
| Go `go test -race ./...` | 通过，40 个顶层测试 | 含真实 PostgreSQL、GitHub HTTP mock、代码分析、权限、日期边界和并发流转；手动浏览器 helper 默认跳过 |
| Go `go vet ./...` | 通过 | 静态检查 |
| 前端 `npm run build` | 通过 | TypeScript 严格检查及 Vite 生产构建 |
| Docker Compose | 通过 | API、Web、PostgreSQL 三个容器均健康 |
| HTTP 集成验收 | 21 组通过 | 通过 Nginx 访问真实 API、PostgreSQL 和 scc |
| Playwright Chromium | 1 条完整流程通过，10.6 秒 | 9 个步骤，登录、上传、筛选、创建项目、追加工程、领取、下载、归还、注销 |
| OpenAPI | 合法 | `swagger-cli validate docs/openapi.yaml`；公开副本与源文件一致 |
| 内置浏览器 | 通过 | 管理员和成员操作、桌面 1440×1000、手机 390×844、版本详情及图片加载 |
| GitHub 灾备 | 通过 | 6 组真实 PostgreSQL 备份测试、11 组安全测试，以及内置浏览器双账号、双仓库、重试与恢复 |

## 关键业务验证

- 两名成员并发领取同一项目工程，只有一人成功，另一人得到 409。
- 同一工程在两个项目可分别领取；新的工程版本不改变原项目引用或原下载内容。
- 当前持有人在项目过期后仍可下载，归还后不能重新领取。
- 回收、转交、移出成员和账号停用改变后续访问权限；成员 API 令牌不能写入，也不能查询其他人的独占领取。
- 已领取工程不能直接移出或升级；历史领取保留时可移除已归还的范围，再重新追加。
- 开放工程详情返回受鉴权下载地址；越权项目读取和 ZIP 下载被拒绝。
- ZIP 路径穿越、符号链接、压缩炸弹、重复路径和 CRC 损坏被拒绝；代码分析不会执行上传源码。

## 认证回归

补充了三个可控 PostgreSQL 并发测试：旧密码登录与密码重置交错、耗时上传中途撤销令牌、写事务与凭证撤销锁顺序。密码校验和会话创建同事务；写事务在提交前复查当前凭证。已经开始的 ZIP 下载流允许完成，后续请求重新鉴权。

注销使用真实浏览器验证：点击退出后直接回登录页面，刷新仍未登录。修复了清空 Query 缓存导致认证观察器失去订阅的问题。

## GitHub 灾备验收

使用本机 HTTP mock 模拟 GitHub，后端连接真实 PostgreSQL 临时 schema。没有使用真实 GitHub Token，没有创建或修改外部仓库。

- 两个模拟账号 `mock-owner`、`mock-backup` 分别保存凭证，只显示末四位；验证、停用、启用和替换令牌通过浏览器检查。
- 同一工程版本分别备份至 `mock-owner/archive` 和 `mock-backup/flaky`，生成两个独立 Release。跨仓库 Release 和资产访问在 mock 中返回 404。
- 主仓库自动同步既有 ready 版本；备用仓库首次上传 503 失败，浏览器点击重试后逐文件校验成功。
- 将本地 ZIP 和截图移至临时保留路径，再从备用仓库恢复。`cmp` 逐字节比较通过，恢复前后 SHA-256 一致。
- 损坏截图或远端 manifest 不一致时拒绝恢复，不先替换 ZIP；项目引用、版本 ID 与领取记录保持不变。
- 未配置密钥、账号到期、无效 Token、停用账号或目标、配置改变、租约过期和旧 worker 提交均有测试覆盖；处理中停用会阻止成功提交或本地替换。
- AES-GCM 篡改、错误密钥和错误账号 AAD 均拒绝解密；跨域下载跳转删除 Authorization，恶意上传或跳转地址被拒绝，上游错误不返回 Token。
- 单连接池不会因事务内重复申请连接而阻塞；管理员降权或停用后无法读取灾备列表；管理员 Bearer 写入与撤销测试通过。
- 手机账号表单和任务详情均为 390px 宽，页面无横向溢出；长 SHA-256 正常换行。修复登录密码自动填入令牌表单，以及自动任务入队后列表不刷新的问题。

本次恢复使用最小 ZIP 和 PNG 作为测试夹具，不代表实际工程运行截图。ZIP 校验值：

```text
70fc040baa6b0269afd417f661801fbcdb85c17c4440a5afaa4c63ba80aca66a
```

灾备截图位于 `output/playwright/`：`desktop-github-accounts.jpg`、`desktop-github-failed-job.jpg`、`desktop-github-backup-jobs.jpg`、`mobile-github-accounts.jpg`、`mobile-github-account-form.jpg`、`mobile-github-job-detail.jpg`。

## 浏览器证据

内置浏览器上传的 ZIP 是工程台本身的源码快照，启动与功能截图来自实际平台页面。浏览器下载后 SHA-256 与原 ZIP 一致：

```text
76077c747172e3d92776cab73f72d38f4e8d0c5987ef92baf51081628d4f023c
```

Playwright 自动化使用临时源码与实际平台页面作为上传格式夹具，已在工程描述明确标注。验收中的 GitHub 示例地址不代表真实交付仓库。平台仅保存管理员提供的 GitHub 地址及 commit/tag，截图由管理员或外部脚本上传。

证据保存在不纳入 Git 的 `output/playwright/`：

- `desktop-engineering.jpg`、`desktop-version.jpg`：桌面工程池与版本详情。
- `mobile-engineering.jpg`、`mobile-version.jpg`：手机工程池与版本详情。
- `production-overview.jpg`：正式预览初始总览。
- `browser-tests/report/index.html`：可重复执行的 Playwright HTML 报告，含 6 张真实截图、步骤和验收结果 JSON。

桌面与手机检查均确认页面宽度未超过视口，工程截图 `naturalWidth > 0`，版本抽屉长描述及 SHA-256 不引起页面横向溢出。

## 环境和复现

正式预览：http://127.0.0.1:8088/，保留运行，未写入验收工程或项目，已有 GitHub 账号等配置保留。

验收环境：http://127.0.0.1:8089/，本次使用新的 Compose 项目 `project-hub-backup-verification`，完成后停止容器并保留卷。HTTP 数据前缀为 `验收-1790967121684`，日志为 `output/playwright/e2e-regression-8089.log` 和 `browser-regression-8089.log`。旧验收卷保留。

灾备浏览器验收使用临时 API `127.0.0.1:8080` 与前端 `127.0.0.1:5173`，停止后清理临时 schema 和文件，不向正式 8088 写入验收数据。

启动与 API 验收命令见 [运行说明](operations.md)，浏览器测试见 [浏览器验收说明](../scripts/BROWSER_TESTS.md)。Go 集成测试使用独立临时 schema，需设置 `TEST_DATABASE_URL` 与 `SCC_BINARY`，未配置数据库时会明确跳过。

本次未部署到外网，未创建或验证外部 GitHub 仓库，不会自动执行工程以生成截图。
