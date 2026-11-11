# 工程台接口约定

所有 ID 为 UUID，JSON 字段为 snake_case，时间为 ISO8601 UTC。列表统一 `{items: [], total: 0}`，分页 `page`、`page_size`（默认 20，上限 100），错误 `{error: "中文信息"}`。Cookie 写请求携带 `X-CSRF-Token`，Bearer 请求不需要 CSRF。

## 认证与用户
- POST `/api/v1/auth/login` `{username,password}` -> `{user,csrf}`
- GET `/api/v1/auth/me` -> `{user,csrf}`
- POST `/api/v1/auth/logout`
- POST `/api/v1/auth/password` `{current_password,password}`
- User `{id,username,name,role,active,created_at}`，角色 admin/member。
- GET `/api/v1/admin/users` 支持 `q,role,active,page,page_size`：`q` 搜索用户名与姓名，`role=admin/member/all`，`active=true/false/all`；省略角色或状态表示全部，`total` 为筛选后总数。
- POST `/api/v1/admin/users` 接收 `{username,name,password,role}`。
- PATCH `/api/v1/admin/users/{id}` `{name,role,active,password?}`。

## 工程与分析
- GET `/api/v1/admin/engineering`，筛选 `q,language,tech_stack,tag_ids,min_files,max_files,status,archived,exclude_project`，支持分页。`tech_stack` 为逗号分隔或重复参数的多选 OR 条件；`tag_ids` 支持重复参数或逗号分隔（重复标签去重），最多 50 个；超过 50 个、不是 UUID 或标签不存在均 400 `标签筛选无效`；选择领域命中其全部子方向，多个标签为 OR，省略或空表示不过滤。`archived=false/true/all`，默认仅未归档；`exclude_project=项目UUID` 排除该项目仍有效的工程引用，软移除后的引用不排除。无效项目标识返回400；管理员使用此筛选无需加入项目。`total` 为筛选后总数。
- GET `/api/v1/admin/engineering/{id}` -> `{engineering,versions}`；`engineering` 为 `{id,name,description,archived,created_at,tags:[EngineeringTag]}`，`versions` 每项为工程版本对象（含 `tags`），无标签均为 `[]`。
- POST `/api/v1/admin/engineering-versions` multipart：`name,description,tech_stack`（JSON 字符串数组）、`tag_ids`（可选 JSON 字符串数组，仅新建工程生效，追加版本忽略）、`github_url,github_ref,engineering_id?,backup_repo_name?`、`zip`、`startup_screenshot`、`feature_screenshots`（可重复，1至6张）。返回201及 `{engineering_id,version_id,job_id,backup_status,backup_targets:[{id,repository_url}],backup_target_id?,backup_repository_url?}`。自动备份到全局已验证、启用且未到期的全部 GitHub 账号，无需传账号或绑定仓库。仓库短名 3～48 位小写字母/数字/单个连字符，首尾字母或数字，禁止连续连字符；完整名 `ph-<短名>-<工程UUID去掉连字符>`，各账号各建私有仓库。短名省略时新工程默认 `engineering`，追加版本沿用；绑定后变名409。分析 ready 后异步建仓和保存版本 Release，按工程查询所有 backup-jobs 确认 ready；201 不等于远端成功。新增账号自动补齐已有未归档工程的 ready 版本，停用账号/目标和关闭自动同步均被尊重。无密钥或可用账号时仍保存本地上传，返回 `backup_status=unconfigured`，否则 `waiting_analysis`。旧 `backup_account_id` 已弃用，传入仅验证有效性，不限制全局范围；旧响应单目标字段对应首个目标。管理员 Bearer 无需 Cookie 或 CSRF；成员令牌上传403。平台不执行工程代码。新建工程可用 `tag_ids` 同时设置工程标签（最多 32 个且只能是子方向标签），非法返回 400 `工程标签无效` / `只能为工程选择子方向标签` / `工程标签最多 32 个`。
- GET `/api/v1/admin/analysis-jobs/{id}` -> `{id,status,error?,version_id}`。
- POST `/api/v1/admin/analysis-jobs/{id}/retry`。
- 工程版本 `{id,engineering_id,version_number,name,description,tech_stack,status,source_file_count,total_file_count,languages:[{name,code,files,percentage}],zip_sha256,zip_size,github_url,github_ref,created_at,assets:[{id,kind,url}],tags:[EngineeringTag]}`，`tags` 为工程标签，无标签为 `[]`。
- GET `/api/v1/assets/{id}` 返回受权限保护的真实截图。
- PATCH `/api/v1/admin/engineering/{id}` `{name,description,archived,tag_ids?}`；传 `tag_ids` 整体替换工程标签（最多 32 个且只能是子方向标签，非法 400 `工程标签无效` / `只能为工程选择子方向标签` / `工程标签最多 32 个`），不传保持原值；审计 `engineering.update` 的 details 含 tag_ids。
- GET `/api/v1/admin/engineering-versions/{id}/zip` 管理员下载。

## 项目与领取
- GET `/api/v1/me/projects` 和 GET `/api/v1/admin/projects`，支持分页、`q,status`；`q` 搜索名称与描述。`status=active/upcoming/expired/paused/archived/all`，省略表示全部；active/upcoming/expired 只匹配基础状态为 active 且分别在开放日期内、未开始、已结束的项目。分页和 `total` 均按筛选计算。
- POST `/api/v1/admin/projects` `{name,description,start_at,end_at,tech_stack,min_files,max_files,status,engineering_ids:[],member_ids:[]}`。
- GET `/api/v1/projects/{id}` -> Project。
- PATCH `/api/v1/admin/projects/{id}` 项目基础字段。
- Project `{id,name,description,start_at,end_at,tech_stack,min_files,max_files,status,created_at,engineering_count,claimed_count,member_count}`，status active/paused/archived。
- GET `/api/v1/projects/{id}/engineering`，筛选 `q,language,min_files,max_files,scope`；scope all/available/mine。
- 关联记录：版本字段加 `{project_id,engineering_id,version_id,claim_id,claimed_by,claimed_name,claimed_at,can_download}`，有下载权限时增加 `zip_url`。无权限时隐藏 github_url/github_ref/zip_sha256，仅保留公开摘要与截图。
- POST `/api/v1/admin/projects/{id}/engineering-scope` `{add:工程ID数组,remove:工程ID数组,upgrades:[{engineering_id,version_id}]}`。添加固定最新 ready 版本，已领取引用禁止移除或升级。
- GET `/api/v1/projects/{id}/members`；POST `/api/v1/admin/projects/{id}/members` `{add:用户ID数组,remove:用户ID数组}`。删除成员须事务回收领取。
- POST `/api/v1/projects/{id}/engineering/{engineeringId}/claim` -> Claim。
- POST `/api/v1/projects/{id}/engineering/{engineeringId}/release` `{claim_id}`，成功返回204。
- GET `/api/v1/projects/{id}/engineering/{engineeringId}/zip` 鉴权流式下载。
- POST `/api/v1/admin/claims/{id}/reclaim`。
- POST `/api/v1/admin/claims/{id}/transfer` `{user_id}`。

## 标签
- 标签树固定两级：所属领域（根标签）与子方向（叶子标签）。每个领域可有一个名为「其他」的子方向，各自独立且固定排在子方向最后。工程可打 0..N 个子方向标签，不能直接使用领域标签；标签属于工程池工程，与工程当前版本无关。标签全局可读，只有管理员能增删改。
- GET `/api/v1/tags` -> `{items:[Tag],total}`，返回完整两级树，`items` 为领域；`total` 为含子方向的全部标签数。Tag `{id,name,parent_id,sort_order,engineering_count,children:[Tag]}`，`parent_id` 领域为 null，`children` 叶子为 `[]`。领域按 `sort_order,name` 排序；子方向按「其他」最后、再按 `sort_order,name` 排序。`engineering_count` 为该标签本身及全部后代标签关联的不同工程数。
- POST `/api/v1/admin/tags` `{name,parent_id?,sort_order?}` -> 201 Tag（`engineering_count` 为当前值，`children:[]`）。省略 `parent_id` 或传 null 创建领域；非空必须是已存在的领域，父级本身有父级则 400 `标签树最多两级`，父级不存在 400 `所属领域不存在`。名称 trim 后 1..40 字符，否则 400 `标签名称不能为空` / `标签名称不能超过40个字符`。省略 `sort_order` 时取同父级最大排序 +10（无同级为 10）。同一层级重名 409：领域 `已存在同名所属领域`，子方向 `同一所属领域下已存在同名子方向`。审计 `tag.create`。
- PATCH `/api/v1/admin/tags/{id}` `{name?,sort_order?}` -> Tag，不支持移动父级：传非空 `parent_id` 返回 400 `不支持移动标签所属领域`，`parent_id:null` 忽略。404 `标签不存在`；名称无效 400；重名 409（文案同上）。审计 `tag.update`。
- DELETE `/api/v1/admin/tags/{id}`。成功 204。404 `标签不存在`；有子方向 409 `请先删除该领域下的子方向`；被工程使用 409 `该工程正使用此标签，无法删除`。审计 `tag.delete`。
- 工程对象（工程池列表与工程详情）增加 `tags:[EngineeringTag]`（无标签为 `[]`），EngineeringTag `{id,name,parent_id,parent_name}`，按领域排序、子方向「其他」最后再按排序展示。迁移 008 新建 engineering_tags，并把旧 project_tags 的标签按项目引用的工程迁移过去后删除旧表。

## 令牌、概览、审计、开放接口
- GET/POST `/api/v1/tokens`；POST `{name}` 返回明文 token 一次；DELETE `/api/v1/tokens/{id}`。令牌行 `{id,name,prefix,created_at,last_used_at,revoked_at,scope}`。
- GET `/api/v1/admin/overview` -> `{engineering_count,ready_count,pending_count,project_count,active_project_count,claim_count,user_count,recent_events:[]}`。
- GET `/api/v1/admin/audit-events` 筛选 `q,user_id,project_id,engineering_id,action`；事件 `{id,actor_name,action,target_type,target_id,project_id,engineering_id,details,created_at}`。
- GET `/open/v1/me/projects` 使用同样的项目分页与 `q,status` 筛选；GET `/open/v1/projects/{id}/engineering` 使用工程分页与 `q,language,min_files,max_files,scope` 筛选，成员始终仅返回可领取及本人领取工程；GET `/open/v1/engineering/{id}?project_id=...` 有下载权限时返回 `zip_url`；GET `/open/v1/projects/{id}/engineering/{engineeringId}/zip` 逐次鉴权下载。
- 成员 Bearer 令牌只读；管理员令牌允许调用 admin 写接口。网页成员通过 Cookie 可以领取归还。

## GitHub 账户与工程快照备份

全部为管理员接口，支持管理员 Cookie 或 Bearer；Cookie 写请求需要 CSRF。主密钥未配置时账户列表返回 `configured:false`，其他备份写接口返回503。账户 token 从不返回，只返回末4位；工程版本、项目引用和领取记录不会因备份或恢复改变。

- GET `/api/v1/admin/github-accounts` -> `{items:GitHubAccount[],total,usable_total,configured}`，支持分页。usable_total 为所有已验证、启用且未到期账号数量，不受当前页限制。GitHubAccount `{id,name,login,token_suffix,active,expires_at,verified_at,last_error,created_at,updated_at,status}`，时间可为null，status 为 `active/disabled/expired/error`。
- POST `/api/v1/admin/github-accounts` `{name,token,expires_at?}` -> 201 GitHubAccount；请求真实 GitHub `/user` 验证后加密保存。
- PATCH `/api/v1/admin/github-accounts/{id}` `{name?,token?,active?,expires_at?}` -> GitHubAccount；新 token 必须真实验证，expires_at:null 清除到期时间。停用保留历史并禁止新备份/恢复。
- POST `/api/v1/admin/github-accounts/{id}/verify` -> GitHubAccount；真实验证并更新verified_at/last_error，失败返回422中文错误。
- GET `/api/v1/admin/backup-targets?engineering_id=&account_id=&q=&enabled=&page=&page_size=` -> `{items:BackupTarget[],total}`。q 按工程名或 owner/repo 字面子串匹配（最多200字节），enabled 可为 true/false 或省略；所有筛选在服务端执行。默认20条，最多100条，按 created_at DESC、id DESC 排序，先分页再计算任务摘要。BackupTarget `{id,engineering_id,engineering_name,account_id,account_name,login,owner,repo,auto_sync,managed,enabled,created_at,updated_at,last_status,last_error,last_synced_at}`；暂无任务时last_status为空、last_synced_at为null。传 `group_by=engineering` 时改为按工程分页，返回 `BackupTargetGroup[]`，total 为工程数；摘要含 `engineering_id,engineering_name,account_count,repository_count,enabled_count,disabled_count`。展开工程后省略 group_by 并用 engineering_id 查询该工程的目标明细。
- POST `/api/v1/admin/backup-targets` `{engineering_id,account_id,owner,repo,auto_sync}` -> 201 BackupTarget；仅接受已有私有且已初始化、token有push权限的GitHub仓库，相同工程/owner/repo只允许一个目标。
- PATCH `/api/v1/admin/backup-targets/{id}` `{auto_sync?,enabled?}` -> BackupTarget；绑定账户与仓库不可修改。停用不删除远端Release。
- GET `/api/v1/admin/backup-jobs?engineering_id=&target_id=&account_id=&q=&status=&kind=&page=&page_size=` -> `{items:BackupJob[],total}`。默认20条、最多100条，created_at DESC、id DESC 排序；q 按工程名或 owner/repo 字面子串匹配（最多200字节），状态和类型可组合筛选，无效值400。BackupJob `{id,target_id,version_id,version_number,engineering_id,engineering_name,account_name,owner,repo,status,kind,error,release_url,zip_sha256,verified_at,created_at,finished_at}`，kind 为 `sync/restore`，status 为 `pending/running/ready/failed`，verified_at/finished_at可为null。
- POST `/api/v1/admin/backup-targets/{id}/sync` `{version_id?}` -> 202 BackupJob；省略版本时使用最新ready版本，同目标同版本sync幂等返回已有任务，失败任务需明确重试。
- POST `/api/v1/admin/backup-jobs/{id}/retry` -> 202 BackupJob；仅failed允许，其他状态409。
- POST `/api/v1/admin/backup-jobs/{id}/restore` -> 202 新restore BackupJob；只接受已成功校验的ready sync任务。restore失败仍可retry。

自动同步为每个ready版本及开启auto_sync的有效目标创建任务。快照Release含固定版本ZIP、全部截图和manifest.json；完成前逐文件验证SHA-256。恢复从远端取manifest、ZIP及截图，校验与数据库保存manifest一致后原子替换原存储文件，不创建版本或覆盖版本指标。账户到期、无效token和目标停用均明确报错；处理中远端请求可能完成，但提交成功状态前再次核验账户与目标开关。

同步记录分组：GET `/api/v1/admin/backup-jobs?group_by=repository` 返回 `{items:BackupRepositoryGroup[],total}`。按忽略大小写的 owner/repo 合并全部匹配记录（包括不同工程目标），先分组再分页；total 是仓库数，排序为最近记录时间降序、owner/repo 升序。摘要 `{owner,repo,engineering_name,engineering_count,version_count,job_count,ready_count,failed_count,pending_count,running_count,latest_at}`；version_count 按不同版本ID去重，所有数量均按当前筛选计算。展开仓库时省略 group_by，传 `repository=owner/repo`，复用其余筛选并独立分页加载 BackupJob；原有任务列表调用保持兼容。非法 group_by 或 repository 格式返回400。

跨账号收纳：`group_by=engineering` 返回按工程ID合并的摘要，新增 engineering_id、account_count、repository_count；total 为工程数，同版本多个账号只计一个 version_count。`group_by=version` 要求 engineering_id，返回 `{items:[{version_id,version_number,jobs:BackupJob[]}],total}`，total 为版本数；先分页版本再加载全部匹配账号记录，新版在前，单版本不跨页拆散。页面采用工程→版本→账号备份三级展示；原有 repository 分组与任务列表接口保持兼容。

## Go 并行开发内部约定

`backend/internal/app` 包：`App{DB *pgxpool.Pool, UploadDir string, SecureCookie bool}`。`User{ID,Username,Name,Role string; Active bool}`。认证上下文 `userFrom(r) User`。JSON 助手 `writeJSON(w,status,value)`、`fail(w,status,message)`。核心 agent 定义 `Routes() http.Handler` 并调用 `a.registerEngineering(r chi.Router)`；分析 agent 实现该函数和 `a.StartAnalysisWorker(ctx)`。

数据库 engineering(id,name,description,archived,created_at)；engineering_versions(id,engineering_id,version_number,tech_stack jsonb,status,source_file_count,total_file_count,languages jsonb,zip_sha256,zip_size,zip_path,github_url,github_ref,created_at)；engineering_assets(id,version_id,kind,path,mime_type,created_at)；analysis_jobs(id,version_id,status,error,created_at,updated_at)。分析 agent 使用这套固定字段，补充 schema 修改需和核心 agent 协商。
