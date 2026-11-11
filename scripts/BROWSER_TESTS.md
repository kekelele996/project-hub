# 浏览器流程验收

本测试只允许访问 `127.0.0.1:8089` 或 `localhost:8089` 的隔离环境。通过页面完成管理员登录、工程 ZIP/截图上传、名称/语言/文件数筛选、项目创建与工程/成员选择、独立添加工程弹窗排除已有引用并追加工程，再以成员登录完成领取、浏览器下载并校验 SHA-256、归还、注销和刷新验证。只有测试成员账号用 API 初始化。

`browser-tests/tags.spec.mjs` 是工程池标签体系流程：先用真实上传 API 创建两份工程并等待分析完成，再在「标签管理」新增空领域与子方向，随后在工程详情弹窗的「编辑信息」中为工程多选标签（并验证名称/描述不丢），回到工程池按领域、子方向和多选 OR 筛选（校验行内标签与 4+N 折叠）、验证有子方向或被工程使用时的删除保护 409，并检查 390×844 移动端 0 横向溢出；截图写入 `output/tags-verification/`。该用例只依赖隔离环境管理员账号和种子标签，不要求 GitHub 灾备账号。

`browser-tests/workflow.spec.mjs` 的上传表单要求隔离环境至少存在一个「已验证、启用且未到期」的 GitHub 灾备账号（账户列表 `usable_total>0`），否则「上传并分析」保持禁用。没有真实账号时只能在隔离库插入不可解密的模拟账号满足表单门禁，不能借此向真实 GitHub 建仓或上传，备份任务会失败且不影响上传、分析、项目与领取流程。

从项目根目录安装验收依赖和 Chromium：

```sh
npm --prefix scripts ci
npm --prefix scripts exec -- playwright install chromium
```

由环境变量提供隔离环境的管理员账号后运行：

```sh
export PROJECT_HUB_BASE_URL=http://127.0.0.1:8089
export PROJECT_HUB_ADMIN_USERNAME=admin
export PROJECT_HUB_ADMIN_PASSWORD='隔离环境管理员密码'
npm --prefix scripts run test:browser
```

可先用 `npm --prefix scripts run test:browser:list` 确认测试发现。HTML 报告、失败 trace 和本次真实页面截图位于 `output/playwright/browser-tests/`。测试使用独立的无头 Chromium，不操作内置浏览器标签。

每次生成唯一名称的“验收”工程、项目和成员，保留在隔离数据库供核对。源码 ZIP 在临时目录生成，下载核验后删除。上传图片来自本次真实的平台登录页和工程池页，仅验证图片上传流程，并非临时源码的运行证明。GitHub 示例地址不是交付仓库。报告可能包含会话 trace，请与测试数据一起仅保留在受控本地环境。
