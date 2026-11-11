import { expect, test } from '@playwright/test';
import crypto from 'node:crypto';
import fs from 'node:fs/promises';
import JSZip from 'jszip';
import os from 'node:os';
import path from 'node:path';

const sha256 = (bytes) => crypto.createHash('sha256').update(bytes).digest('hex');
const endpoint = (response, method, pathname) =>
  response.request().method() === method && new URL(response.url()).pathname === pathname;

async function login(page, username, password) {
  await expect(page.getByRole('heading', { name: '登录工作空间' })).toBeVisible();
  await page.getByLabel('用户名', { exact: true }).fill(username);
  await page.getByLabel('密码', { exact: true }).fill(password);
  const response = page.waitForResponse((r) => endpoint(r, 'POST', '/api/v1/auth/login'));
  await page.getByRole('button', { name: '登录', exact: true }).click();
  const result = await response;
  expect(result.status()).toBe(200);
  const identity = await result.json();
  await expect(page.getByRole('button', { name: '登录', exact: true })).toBeHidden();
  return identity;
}

async function logout(page) {
  await page.locator('.account-trigger').click();
  const response = page.waitForResponse((r) => endpoint(r, 'POST', '/api/v1/auth/logout'));
  await page.getByRole('button', { name: '退出登录', exact: true }).click();
  expect((await response).status()).toBe(204);
  await expect(page.getByRole('heading', { name: '登录工作空间' })).toBeVisible();
  expect((await page.request.get('/api/v1/auth/me')).status()).toBe(401);
  await page.reload();
  await expect(page.getByRole('heading', { name: '登录工作空间' })).toBeVisible();
}

async function upload(page, name, zipPath, startup, feature) {
  await page.getByRole('button', { name: '上传工程', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '上传工程', exact: true });
  await dialog.getByRole('textbox', { name: '工程名称', exact: true }).fill(name);
  await dialog.getByRole('textbox', { name: '工程描述', exact: true }).fill('验收数据：临时源码和平台实拍页面，仅验证流程；GitHub 示例地址不是交付。');
  await dialog.getByRole('group', { name: '技术栈', exact: true }).getByRole('button', { name: 'Go', exact: true }).click();
  await dialog.getByRole('group', { name: '技术栈', exact: true }).getByRole('button', { name: 'Python', exact: true }).click();
  await dialog.getByLabel('GitHub 仓库地址', { exact: true }).fill('https://github.com/example/browser-verification');
  await dialog.getByLabel('固定 commit 或 tag', { exact: true }).fill('verification-fixture-v1');
  await dialog.getByLabel('代码 ZIP', { exact: true }).setInputFiles(zipPath);
  await dialog.getByLabel('真实启动截图', { exact: true }).setInputFiles(startup);
  await dialog.getByLabel('主要功能截图', { exact: true }).setInputFiles(feature);
  const uploadResponse = page.waitForResponse((r) => endpoint(r, 'POST', '/api/v1/admin/engineering-versions'));
  await dialog.getByRole('button', { name: '上传并分析', exact: true }).click();
  const response = await uploadResponse;
  expect(response.status()).toBe(201);
  const engineering = await response.json();
  await expect(dialog).toBeHidden({ timeout: 60_000 });
  return engineering;
}

test('验收：管理员上传与建项目，成员领取下载归还并注销', async ({ page, baseURL }, testInfo) => {
  const target = new URL(baseURL);
  expect(['127.0.0.1', 'localhost']).toContain(target.hostname);
  expect(target.port, '本测试会创建验收数据，仅允许本地隔离环境 8089').toBe('8089');
  const adminUsername = process.env.PROJECT_HUB_ADMIN_USERNAME;
  const adminPassword = process.env.PROJECT_HUB_ADMIN_PASSWORD;
  expect(adminUsername, '请设置 PROJECT_HUB_ADMIN_USERNAME').toBeTruthy();
  expect(adminPassword, '请设置 PROJECT_HUB_ADMIN_PASSWORD').toBeTruthy();

  const run = `${Date.now()}-${crypto.randomBytes(3).toString('hex')}`;
  const engineeringName = `验收-浏览器工程-${run}`;
  const additionalName = `验收-追加工程-${run}`;
  const projectName = `验收-浏览器项目-${run}`;
  const memberName = `验收-浏览器成员-${run}`;
  const memberUsername = `browser-verification-${run}`;
  const memberPassword = `Verification-${crypto.randomBytes(20).toString('hex')}`;
  const temporary = await fs.mkdtemp(path.join(os.tmpdir(), 'project-hub-browser-'));
  const artifacts = testInfo.outputPath('evidence');
  await fs.mkdir(artifacts, { recursive: true });
  const startup = path.join(artifacts, '验收-平台登录页面.png');
  const feature = path.join(artifacts, '验收-平台工程池页面.png');
  const zipPath = path.join(temporary, '验收-临时源码.zip');
  let engineering, additionalEngineering, project, member, expectedHash;
  const pageErrors = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));

  try {
    await test.step('登录管理员并采集真实平台页面作为上传格式夹具', async () => {
      await page.goto('/');
      await expect(page.getByRole('heading', { name: '登录工作空间' })).toBeVisible();
      await page.screenshot({ path: startup, fullPage: true });
      const identity = await login(page, adminUsername, adminPassword);
      await page.getByRole('navigation').getByRole('link', { name: '工程池', exact: true }).click();
      await expect(page.getByRole('heading', { name: '工程池', exact: true })).toBeVisible();
      await expect(page.getByLabel('工程语言')).toBeVisible();
      await page.evaluate(() => document.fonts.ready);
      await page.screenshot({ path: feature, fullPage: true });
      const result = await page.request.post('/api/v1/admin/users', {
        headers: { 'X-CSRF-Token': identity.csrf },
        data: { username: memberUsername, name: memberName, password: memberPassword, role: 'member' },
      });
      expect(result.status()).toBe(201);
      member = await result.json();
    });

    await test.step('通过页面上传临时源码 ZIP 和真实截图，等待分析完成', async () => {
      const zip = new JSZip();
      zip.file('source/main.go', 'package main\n\nfunc main() {\n println("browser verification")\n}\n', { createFolders: false });
      zip.file('source/app.py', 'print("browser verification")\n', { createFolders: false });
      zip.file('source/README.md', '验收夹具：临时源码用于上传、分析与下载校验。截图来自真实平台页面，仅验证图片上传，并非此源码的运行截图。GitHub 示例地址不是交付仓库。\n', { createFolders: false });
      const bytes = await zip.generateAsync({ type: 'nodebuffer', compression: 'DEFLATE' });
      expectedHash = sha256(bytes);
      await fs.writeFile(zipPath, bytes);
      engineering = await upload(page, engineeringName, zipPath, startup, feature);
    });

    await test.step('语言、源码文件数与名称筛选真实上传结果', async () => {
      await page.getByRole('button', { name: '列表视图', exact: true }).click();
      await page.getByLabel('搜索工程名称、描述', { exact: true }).fill(engineeringName);
      await page.getByLabel('工程语言').selectOption('Go');
      await page.getByLabel('最少文件数').fill('2');
      await page.getByLabel('最多文件数').fill('2');
      await page.getByLabel('分析状态').selectOption('ready');
      const row = page.getByRole('row').filter({ hasText: engineeringName });
      await expect(row).toBeVisible();
      await expect(row.getByText('共 3 个文件', { exact: true })).toBeVisible();
      await expect(row.getByText('可用', { exact: true })).toBeVisible();
      await page.getByLabel('工程语言').selectOption('Java');
      await expect(row).toBeHidden();
      await expect(page.getByText('没有符合筛选的工程', { exact: true })).toBeVisible();
      await page.getByLabel('工程语言').selectOption('Go');
      await expect(row).toBeVisible();
      await page.getByLabel('最少文件数').fill('3');
      await expect(row).toBeHidden();
      await page.getByLabel('最少文件数').fill('2');
      await expect(row).toBeVisible();
      await expect.poll(() => row.getByRole('img').evaluate((image) => image.complete && image.naturalWidth > 0)).toBe(true);
      await page.screenshot({ path: path.join(artifacts, '验收-工程筛选结果.png'), fullPage: true });
    });

    await test.step('通过页面创建项目并明确选择工程与成员', async () => {
      await page.getByRole('navigation').getByRole('link', { name: '项目管理', exact: true }).click();
      await page.getByRole('button', { name: '新建项目', exact: true }).first().click();
      const dialog = page.getByRole('dialog', { name: '新建项目', exact: true });
      await dialog.getByRole('textbox', { name: '项目名称', exact: true }).fill(projectName);
      await dialog.getByRole('textbox', { name: '项目描述', exact: true }).fill('验收项目：检查页面工程范围、成员选择和独占领取。');
      await dialog.getByRole('group', { name: '采用技术栈', exact: true }).getByRole('button', { name: 'Go', exact: true }).click();
      await dialog.getByRole('spinbutton', { name: '最少源码文件', exact: true }).fill('2');
      await dialog.getByRole('spinbutton', { name: '最多源码文件', exact: true }).fill('2');
      await dialog.getByRole('tab', { name: /^工程范围/ }).click();
      await dialog.getByLabel('搜索符合筛选的工程', { exact: true }).fill(engineeringName);
      const choice = dialog.getByRole('button').filter({ hasText: engineeringName });
      await expect(choice).toBeVisible();
      await choice.click();
      await expect(choice).toHaveClass(/selected/);
      await dialog.getByRole('tab', { name: /^项目成员/ }).click();
      await dialog.getByLabel('搜索姓名、用户名', { exact: true }).fill(memberUsername);
      const memberChoice = dialog.getByRole('button').filter({ hasText: memberName });
      await expect(memberChoice).toBeVisible();
      await memberChoice.click();
      await expect(memberChoice).toHaveClass(/selected/);
      const projectResponse = page.waitForResponse((r) => endpoint(r, 'POST', '/api/v1/admin/projects'));
      await dialog.getByRole('button', { name: '创建项目', exact: true }).click();
      const response = await projectResponse;
      expect(response.status()).toBe(201);
      project = await response.json();
      expect(project.engineering_count).toBe(1);
      expect(project.member_count).toBe(1);
      await expect(page.getByRole('heading', { name: projectName, exact: true })).toBeVisible();
      await page.getByRole('tab', { name: /^项目成员/ }).click();
      await expect(page.getByRole('row').filter({ hasText: memberName })).toBeVisible();
      await page.screenshot({ path: path.join(artifacts, '验收-项目成员.png'), fullPage: true });
    });

    await test.step('独立添加工程弹窗排除现有引用并可追加另一份工程', async () => {
      await page.getByRole('navigation').getByRole('link', { name: '工程池', exact: true }).click();
      additionalEngineering = await upload(page, additionalName, zipPath, startup, feature);
      await page.getByRole('navigation').getByRole('link', { name: '项目管理', exact: true }).click();
      await page.getByLabel('搜索项目', { exact: true }).fill(projectName);
      await page.getByRole('link').filter({ hasText: projectName }).click();
      await page.getByRole('button', { name: '添加工程', exact: true }).click();
      const dialog = page.getByRole('dialog', { name: '添加工程', exact: true });
      await dialog.getByLabel('搜索工程名称、描述', { exact: true }).fill(engineeringName);
      await expect(dialog.getByRole('button').filter({ hasText: engineeringName })).toHaveCount(0);
      await expect(dialog.getByText('没有符合条件的工程', { exact: true })).toBeVisible();
      await dialog.getByLabel('搜索工程名称、描述', { exact: true }).fill(additionalName);
      const choice = dialog.getByRole('button').filter({ hasText: additionalName });
      await expect(choice).toBeVisible();
      await choice.click();
      await expect(choice).toHaveClass(/selected/);
      const addResponse = page.waitForResponse((r) => endpoint(r, 'POST', `/api/v1/admin/projects/${project.id}/engineering-scope`));
      await dialog.getByRole('button', { name: '加入项目', exact: true }).click();
      expect((await addResponse).status()).toBe(200);
      await expect(dialog).toBeHidden();
      await expect(page.getByRole('row').filter({ hasText: additionalName })).toBeVisible();
      await page.screenshot({ path: path.join(artifacts, '验收-项目追加工程.png'), fullPage: true });
      await logout(page);
    });

    await test.step('成员登录、查看所属项目并领取工程', async () => {
      await page.goto('/');
      await login(page, memberUsername, memberPassword);
      await expect(page.getByRole('heading', { name: '我的项目', exact: true })).toBeVisible();
      await page.getByLabel('搜索项目', { exact: true }).fill(projectName);
      await page.getByRole('link').filter({ hasText: projectName }).click();
      await expect(page.getByRole('heading', { name: projectName, exact: true })).toBeVisible();
      const row = page.getByRole('row').filter({ hasText: engineeringName });
      await expect(row.getByRole('button', { name: '领取', exact: true })).toBeVisible();
      expect((await page.request.get(`/api/v1/projects/${project.id}/engineering/${engineering.engineering_id}/zip`)).status()).toBe(403);
      const claimResponse = page.waitForResponse((r) => endpoint(r, 'POST', `/api/v1/projects/${project.id}/engineering/${engineering.engineering_id}/claim`));
      await row.getByRole('button', { name: '领取', exact: true }).click();
      expect((await claimResponse).status()).toBe(201);
      await page.getByRole('tab', { name: '我的工程', exact: true }).click();
      await expect(row.getByRole('button', { name: '归还', exact: true })).toBeVisible();
      await page.screenshot({ path: path.join(artifacts, '验收-成员领取.png'), fullPage: true });
    });

    await test.step('浏览器真实下载并验证 SHA-256', async () => {
      const downloadEvent = page.waitForEvent('download');
      await page.getByRole('link', { name: `下载 ${engineeringName}`, exact: true }).click();
      const download = await downloadEvent;
      expect(await download.failure()).toBeNull();
      const destination = path.join(temporary, 'download.zip');
      await download.saveAs(destination);
      expect(sha256(await fs.readFile(destination))).toBe(expectedHash);
    });

    await test.step('成员通过页面归还，下载权限消失并可重新领取', async () => {
      const row = page.getByRole('row').filter({ hasText: engineeringName });
      await row.getByRole('button', { name: '归还', exact: true }).click();
      const confirm = page.getByRole('alertdialog');
      await expect(confirm.getByRole('heading', { name: '归还工程？', exact: true })).toBeVisible();
      const releaseResponse = page.waitForResponse((r) => endpoint(r, 'POST', `/api/v1/projects/${project.id}/engineering/${engineering.engineering_id}/release`));
      await confirm.getByRole('button', { name: '确认', exact: true }).click();
      expect((await releaseResponse).status()).toBe(204);
      await expect(confirm).toBeHidden();
      await expect(page.getByText('你还没有领取工程', { exact: true })).toBeVisible();
      expect((await page.request.get(`/api/v1/projects/${project.id}/engineering/${engineering.engineering_id}/zip`)).status()).toBe(403);
      await page.getByRole('tab', { name: '可领取', exact: true }).click();
      await expect(row.getByRole('button', { name: '领取', exact: true })).toBeVisible();
      await expect(row.getByRole('link', { name: `下载 ${engineeringName}`, exact: true })).toHaveCount(0);
    });

    await test.step('注销后刷新仍回到登录页', async () => {
      await logout(page);
      expect(pageErrors).toEqual([]);
      const manifest = {
        run, base_url: baseURL, engineering_name: engineeringName, project_name: projectName,
        engineering_id: engineering.engineering_id, version_id: engineering.version_id,
        additional_engineering_id: additionalEngineering.engineering_id,
        project_id: project.id, member_id: member.id, zip_sha256: expectedHash,
        note: '验收数据保留用于核对；截图为本次真实平台页面，临时源码和 GitHub 示例地址不是交付内容。',
      };
      await fs.writeFile(path.join(artifacts, '验收结果.json'), JSON.stringify(manifest, null, 2));
      await testInfo.attach('验收结果', { body: JSON.stringify(manifest, null, 2), contentType: 'application/json' });
    });
  } finally {
    await fs.rm(temporary, { recursive: true, force: true });
  }
});
