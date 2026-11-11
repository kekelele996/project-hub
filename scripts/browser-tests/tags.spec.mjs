import { expect, test } from '@playwright/test';
import crypto from 'node:crypto';
import fs from 'node:fs/promises';
import path from 'node:path';
import JSZip from 'jszip';

const artifacts = path.resolve(import.meta.dirname, '../../output/tags-verification');
const png = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAYAAABytg0kAAAAEUlEQVR4AWP8DwQMQMDEAAUAPfgEADYYS7QAAAAASUVORK5CYII=',
  'base64',
);
const endpoint = (response, method, pathname) =>
  response.request().method() === method && new URL(response.url()).pathname === pathname;

async function login(page, username, password) {
  await expect(page.getByRole('heading', { name: '登录工作空间' })).toBeVisible();
  await page.locator('input[aria-label="用户名"]').fill(username);
  await page.locator('input[aria-label="密码"]').fill(password);
  const response = page.waitForResponse((r) => endpoint(r, 'POST', '/api/v1/auth/login'));
  await page.getByRole('button', { name: '登录', exact: true }).click();
  expect((await response).status()).toBe(200);
  await expect(page.locator('.app-layout')).toBeVisible();
}

/** 通过真实上传 API 创建一个可供标签验收的工程，并等待 scc 分析完成。 */
async function uploadEngineering(page, headers, name) {
  const zip = new JSZip();
  zip.file('source/main.go', 'package main\n\nfunc main() {\n println("tag verification")\n}\n');
  zip.file('source/app.py', 'print("tag verification")\n');
  const buffer = await zip.generateAsync({ type: 'nodebuffer', compression: 'DEFLATE' });
  const response = await page.request.post('/api/v1/admin/engineering-versions', {
    headers,
    multipart: {
      name,
      description: '标签体系自动验收工程，源码为临时夹具，仅用于工程池标签筛选。',
      tech_stack: '["Go","Python"]',
      github_url: 'https://github.com/example/tag-verification',
      github_ref: 'v1.0.0',
      zip: { name: 'source.zip', mimeType: 'application/zip', buffer },
      startup_screenshot: { name: 'startup.png', mimeType: 'image/png', buffer: png },
      feature_screenshots: { name: 'feature.png', mimeType: 'image/png', buffer: png },
    },
  });
  expect(response.status()).toBe(201);
  const upload = await response.json();
  for (let i = 0; i < 100; i += 1) {
    const job = await (await page.request.get(`/api/v1/admin/analysis-jobs/${upload.job_id}`)).json();
    if (job.status === 'ready') return upload;
    if (job.status === 'failed') throw new Error(`工程分析失败: ${job.error}`);
    await page.waitForTimeout(300);
  }
  throw new Error('工程分析超时');
}

test('工程池标签：标签管理、工程打标签、标签树筛选与移动端', async ({ page, baseURL }) => {
  const target = new URL(baseURL);
  expect(['127.0.0.1', 'localhost']).toContain(target.hostname);
  expect(target.port, '本测试会创建验收数据，仅允许本地隔离环境 8089').toBe('8089');
  const username = process.env.PROJECT_HUB_ADMIN_USERNAME;
  const password = process.env.PROJECT_HUB_ADMIN_PASSWORD;
  expect(username, '请设置 PROJECT_HUB_ADMIN_USERNAME').toBeTruthy();
  expect(password, '请设置 PROJECT_HUB_ADMIN_PASSWORD').toBeTruthy();

  await fs.mkdir(artifacts, { recursive: true });
  const run = `${Date.now()}-${crypto.randomBytes(3).toString('hex')}`;
  const prefix = `验收-标签工程-${run}`;
  const first = `${prefix}-A`;
  const second = `${prefix}-B`;
  const domainName = `验收领域-${run}`;
  const childName = `验收子方向-${run}`;
  const pageErrors = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));

  await page.goto('/');
  await login(page, username, password);
  const identity = await (await page.request.get('/api/v1/auth/me')).json();
  const headers = { 'X-CSRF-Token': identity.csrf };

  let firstEngineering;
  let secondEngineering;
  let tree;
  await test.step('准备两份真实工程并确认标签树种子基线', async () => {
    tree = await (await page.request.get('/api/v1/tags')).json();
    expect(tree.total).toBe(10);
    expect(tree.items.map((tag) => tag.name)).toEqual(['安全', '游戏、图形与多媒体', '数据工程']);
    for (const tag of [...tree.items, ...tree.items.flatMap((item) => item.children)]) {
      expect(typeof tag.engineering_count).toBe('number');
      expect(Array.isArray(tag.children)).toBe(true);
    }
    firstEngineering = await uploadEngineering(page, headers, first);
    secondEngineering = await uploadEngineering(page, headers, second);
  });

  await test.step('标签管理：新增空领域不白屏，再添加子方向，计数为工程数', async () => {
    await page.getByRole('navigation').getByRole('link', { name: '标签管理', exact: true }).click();
    await expect(page.getByRole('heading', { name: '标签管理', exact: true })).toBeVisible();
    await expect(page.locator('.section-title .count-badge')).toContainText('3 个领域 / 7 个子方向');
    await page.getByRole('button', { name: '新增所属领域', exact: true }).click();
    const domainDialog = page.getByRole('dialog', { name: '新增所属领域', exact: true });
    await domainDialog.locator('input').first().fill(domainName);
    await domainDialog.getByRole('button', { name: '创建标签', exact: true }).click();
    const section = page.locator('.tag-domain').filter({ hasText: domainName });
    await expect(section).toBeVisible();
    await expect(section).toContainText('0 个子方向');
    await expect(section).toContainText('该领域还没有子方向');
    await section.getByRole('button', { name: '添加子方向', exact: true }).click();
    const childDialog = page.getByRole('dialog', { name: '添加子方向', exact: true });
    await childDialog.locator('input').first().fill(childName);
    await childDialog.getByRole('button', { name: '创建标签', exact: true }).click();
    await expect(section).toContainText('1 个子方向');
    await expect(section.locator('.tag-admin-child-name').filter({ hasText: childName })).toBeVisible();
    await page.screenshot({ path: path.join(artifacts, '01-tag-admin.png'), fullPage: true });
  });

  await test.step('工程详情编辑标签并展示，列表行与卡片显示标签', async () => {
    await page.getByRole('navigation').getByRole('link', { name: '工程池', exact: true }).click();
    await page.getByLabel('搜索工程名称、描述', { exact: true }).fill(first);
    const row = page.locator('.engineering-cell').filter({ hasText: first });
    await expect(row).toBeVisible();
    await row.click();
    const detail = page.getByRole('dialog', { name: first, exact: true });
    await expect(detail.getByRole('heading', { name: first, exact: true })).toBeVisible();
    await detail.getByRole('button', { name: '编辑信息', exact: true }).click();
    const chip = detail.locator('.tag-picker-chips button').filter({ hasText: childName });
    await expect(chip).toBeVisible();
    await chip.click();
    await expect(chip).toHaveAttribute('aria-pressed', 'true');
    const patchResponse = page.waitForResponse(
      (r) =>
        r.request().method() === 'PATCH' &&
        new URL(r.url()).pathname === `/api/v1/admin/engineering/${firstEngineering.engineering_id}`,
    );
    await detail.getByRole('button', { name: '保存', exact: true }).click();
    const patch = await patchResponse;
    expect(patch.status()).toBe(200);
    await expect(page.locator('.toast.success').filter({ hasText: '工程信息已保存' })).toBeVisible();
    const badgeLine = detail.locator('.tag-badge-line');
    await expect(badgeLine).toContainText(childName);
    await page.screenshot({ path: path.join(artifacts, '02-engineering-detail-tags.png'), fullPage: true });
    await detail.getByRole('button', { name: '关闭', exact: true }).click();

    await page.locator('.engineering-cell').filter({ hasText: first }).locator('.tag-badge').filter({ hasText: childName }).waitFor();
    await page.getByRole('button', { name: '截图视图', exact: true }).click();
    const card = page.locator('.engineering-card').filter({ hasText: first });
    await expect(card.locator('.tag-badge')).toContainText(childName);
    await page.screenshot({ path: path.join(artifacts, '03-engineering-cards-tags.png'), fullPage: true });
    await page.getByRole('button', { name: '列表视图', exact: true }).click();
  });

  await test.step('标签树筛选：领域/子方向/多选 OR 与清除', async () => {
    await page.getByLabel('搜索工程名称、描述', { exact: true }).fill(prefix);
    const vulnerability = tree.items
      .find((tag) => tag.name === '安全')
      .children.find((tag) => tag.name === '漏洞检测与攻防');
    const secondPatch = await page.request.patch(
      `/api/v1/admin/engineering/${secondEngineering.engineering_id}`,
      { headers, data: { tag_ids: [vulnerability.id] } },
    );
    expect(secondPatch.status()).toBe(200);

    // 领域筛选：命中该领域子方向，只返回 A。
    let request = page.waitForResponse(
      (r) => r.url().includes('/api/v1/admin/engineering') && r.url().includes('tag_ids'),
    );
    await page.locator('.tag-filter-domain').filter({ hasText: domainName }).click();
    expect((await request).status()).toBe(200);
    await expect(page.locator('.tag-filter-count')).toHaveText('已选 1 个');
    await expect(page.locator('.engineering-cell')).toHaveCount(1);
    await expect(page.locator('.engineering-cell').filter({ hasText: first }).locator('.tag-badge')).toContainText(childName);
    await page.screenshot({ path: path.join(artifacts, '04-engineering-filter.png'), fullPage: true });

    // 清除后恢复两条。
    await page.locator('.tag-filter-actions .text-button').click();
    await expect(page.locator('.engineering-cell')).toHaveCount(2);

    // 子方向筛选：命中的是 B。
    request = page.waitForResponse(
      (r) => r.url().includes('/api/v1/admin/engineering') && r.url().includes('tag_ids'),
    );
    await page.locator('.tag-chip').filter({ hasText: '漏洞检测与攻防' }).click();
    expect((await request).status()).toBe(200);
    await expect(page.locator('.engineering-cell')).toHaveCount(1);
    await expect(page.locator('.engineering-cell').filter({ hasText: second })).toBeVisible();
    await expect(page.locator('.tag-filter-count')).toHaveText('已选 1 个');

    // 多选 OR：两个不同标签各命中一条。
    request = page.waitForResponse(
      (r) => r.url().includes('/api/v1/admin/engineering') && r.url().includes('tag_ids'),
    );
    await page.locator('.tag-filter-domain').filter({ hasText: domainName }).click();
    expect((await request).status()).toBe(200);
    await expect(page.locator('.tag-filter-count')).toHaveText('已选 2 个');
    await expect(page.locator('.engineering-cell')).toHaveCount(2);
    await page.screenshot({ path: path.join(artifacts, '05-engineering-filter-or.png'), fullPage: true });
    await page.locator('.tag-filter-actions .text-button').click();
    await expect(page.locator('.tag-filter-count')).toHaveCount(0);
  });

  await test.step('工程标签超过 4 个折叠为 +N，并在标签管理显示工程计数', async () => {
    tree = await (await page.request.get('/api/v1/tags')).json();
    const leaves = tree.items.flatMap((tag) => tag.children).map((tag) => tag.id);
    expect(leaves).toHaveLength(8);
    const patch = await page.request.patch(
      `/api/v1/admin/engineering/${firstEngineering.engineering_id}`,
      { headers, data: { tag_ids: leaves } },
    );
    expect(patch.status()).toBe(200);
    // 重新加载工程池，确保读取到刚通过 API 写入的完整标签集合。
    await page.goto('/engineering');
    await page.getByLabel('搜索工程名称、描述', { exact: true }).fill(first);
    const row = page.locator('.engineering-cell').filter({ hasText: first });
    await expect(row.locator('.tag-badge')).toHaveCount(4);
    await expect(row.locator('.tag-badge-more')).toHaveText('+4');
    await page.screenshot({ path: path.join(artifacts, '06-engineering-tags-folded.png'), fullPage: true });
    // 还原为单个子方向标签，供后续「项目选择工程」按标签筛选使用。
    const childTag = tree.items
      .find((tag) => tag.name === domainName)
      .children.find((tag) => tag.name === childName);
    const restore = await page.request.patch(
      `/api/v1/admin/engineering/${firstEngineering.engineering_id}`,
      { headers, data: { tag_ids: [childTag.id] } },
    );
    expect(restore.status()).toBe(200);

    await page.getByRole('navigation').getByRole('link', { name: '标签管理', exact: true }).click();
    const section = page.locator('.tag-domain').filter({ hasText: domainName });
    await expect(section.locator('.tag-admin-child').filter({ hasText: childName })).toContainText('个工程');
    await expect(section).not.toContainText('个项目');
  });

  await test.step('项目选择工程：新建项目与添加工程候选可按标签筛选', async () => {
    const projectName = `验收-标签项目-${run}`;
    await page.getByRole('navigation').getByRole('link', { name: '项目管理', exact: true }).click();
    await page.getByRole('button', { name: '新建项目', exact: true }).first().click();
    const form = page.getByRole('dialog', { name: '新建项目', exact: true });
    await form.locator('input[aria-label="项目名称"]').fill(projectName);
    await form.locator('textarea[aria-label="项目描述"]').fill('标签筛选选择工程验收项目。');
    await form.getByRole('tab', { name: /^工程范围/ }).click();
    await form.locator('.tag-filter-domain').filter({ hasText: domainName }).click();
    await expect(form.locator('.picker-list button')).toHaveCount(1);
    const candidate = form.locator('.picker-list button').filter({ hasText: first });
    await expect(candidate.locator('.tag-badge')).toContainText(childName);
    await candidate.click();
    await expect(candidate).toHaveClass(/selected/);
    await page.screenshot({ path: path.join(artifacts, '09-project-create-tag-filter.png'), fullPage: true });
    const projectResponse = page.waitForResponse(
      (r) => r.request().method() === 'POST' && new URL(r.url()).pathname === '/api/v1/admin/projects',
    );
    await form.getByRole('button', { name: '创建项目', exact: true }).click();
    const created = await projectResponse;
    expect(created.status()).toBe(201);
    const project = await created.json();
    expect(project.engineering_count).toBe(1);
    await expect(page.getByRole('heading', { name: projectName, exact: true })).toBeVisible();

    await page.getByRole('button', { name: '添加工程', exact: true }).click();
    const add = page.getByRole('dialog', { name: '添加工程', exact: true });
    await add.locator('.tag-chip').filter({ hasText: '漏洞检测与攻防' }).click();
    await expect(add.locator('.picker-list button')).toHaveCount(1);
    const secondChoice = add.locator('.picker-list button').filter({ hasText: second });
    await expect(secondChoice.locator('.tag-badge')).toContainText('漏洞检测与攻防');
    await secondChoice.click();
    await page.screenshot({ path: path.join(artifacts, '10-add-engineering-tag-filter.png'), fullPage: true });
    const addResponse = page.waitForResponse(
      (r) =>
        r.request().method() === 'POST' &&
        new URL(r.url()).pathname === `/api/v1/admin/projects/${project.id}/engineering-scope`,
    );
    await add.getByRole('button', { name: '加入项目', exact: true }).click();
    expect((await addResponse).status()).toBe(200);
    await expect(add).toBeHidden();
    await expect(page.getByRole('row').filter({ hasText: first })).toBeVisible();
    await expect(page.getByRole('row').filter({ hasText: second })).toBeVisible();
  });

  await test.step('标签管理：有子方向/被工程使用时删除保护 409', async () => {
    await page.getByRole('navigation').getByRole('link', { name: '标签管理', exact: true }).click();
    const section = page.locator('.tag-domain').filter({ hasText: domainName });
    await section.getByRole('button', { name: `删除所属领域 ${domainName}`, exact: true }).click();
    await page.getByRole('alertdialog').getByRole('button', { name: '确认', exact: true }).click();
    await expect(page.locator('.toast.error').filter({ hasText: '请先删除该领域下的子方向' })).toBeVisible();
    await page.getByRole('alertdialog').getByRole('button', { name: '取消', exact: true }).click();

    await section.getByRole('button', { name: `删除子方向 ${childName}`, exact: true }).click();
    await page.getByRole('alertdialog').getByRole('button', { name: '确认', exact: true }).click();
    await expect(page.locator('.toast.error').filter({ hasText: '该工程正使用此标签，无法删除' })).toBeVisible();
    await page.getByRole('alertdialog').getByRole('button', { name: '取消', exact: true }).click();
  });

  await test.step('移动端 390×844 工程池标签筛选无横向溢出', async () => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto('/engineering');
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );
    expect(overflow).toBeLessThanOrEqual(1);
    await page.screenshot({ path: path.join(artifacts, '07-mobile-engineering-tags.png'), fullPage: true });
    await page.goto('/tags');
    const tagsOverflow = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );
    expect(tagsOverflow).toBeLessThanOrEqual(1);
    await page.screenshot({ path: path.join(artifacts, '08-mobile-tag-admin.png'), fullPage: true });
  });

  await test.step('清理工程标签与临时标签，验证计数恢复', async () => {
    tree = await (await page.request.get('/api/v1/tags')).json();
    const domain = tree.items.find((tag) => tag.name === domainName);
    const child = domain?.children.find((tag) => tag.name === childName);
    expect(child).toBeTruthy();
    for (const engineering of [firstEngineering, secondEngineering]) {
      const clear = await page.request.patch(
        `/api/v1/admin/engineering/${engineering.engineering_id}`,
        { headers, data: { tag_ids: [] } },
      );
      expect(clear.status()).toBe(200);
    }
    expect((await page.request.delete(`/api/v1/admin/tags/${child.id}`, { headers })).status()).toBe(204);
    expect((await page.request.delete(`/api/v1/admin/tags/${domain.id}`, { headers })).status()).toBe(204);
    const after = await (await page.request.get('/api/v1/tags')).json();
    expect(after.total).toBe(10);
    expect(after.items.map((tag) => tag.name)).not.toContain(domainName);
    expect(pageErrors).toEqual([]);
  });
});
