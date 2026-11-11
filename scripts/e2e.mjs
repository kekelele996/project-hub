import assert from 'node:assert/strict';
import crypto from 'node:crypto';
import JSZip from 'jszip';

const base = process.env.PROJECT_HUB_BASE_URL || 'http://127.0.0.1:8080';
const prefix = `验收-${Date.now()}`;
const password = 'Verification-pass-2026';
const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAYAAABytg0kAAAAEUlEQVR4AWP8DwQMQMDEAAUAPfgEADYYS7QAAAAASUVORK5CYII=', 'base64');
let passed = 0;

class Client {
  constructor(token = '') { this.token = token; this.cookie = ''; this.csrf = ''; }
  async request(path, {method = 'GET', body, status = 200, csrf = true, raw = false} = {}) {
    const headers = {};
    if (this.token) headers.Authorization = `Bearer ${this.token}`;
    if (this.cookie) headers.Cookie = this.cookie;
    if (csrf && this.csrf) headers['X-CSRF-Token'] = this.csrf;
    if (body && !(body instanceof FormData)) { headers['Content-Type'] = 'application/json'; body = JSON.stringify(body); }
    const r = await fetch(base + path, {method, headers, body});
    const setCookie = r.headers.get('set-cookie');
    if (setCookie) this.cookie = setCookie.split(';')[0];
    if (raw && r.status === status) return Buffer.from(await r.arrayBuffer());
    const text = await r.text();
    let data; try { data = text ? JSON.parse(text) : null; } catch { data = text; }
    assert.equal(r.status, status, `${method} ${path}: ${r.status}, ${JSON.stringify(data)}`);
    return data;
  }
  async login(username, pass) { const r = await this.request('/api/v1/auth/login', {method:'POST', body:{username,password:pass}}); this.csrf = r.csrf; this.user = r.user; return r; }
}

async function test(name, action) { await action(); passed++; console.log(`PASS ${String(passed).padStart(2,'0')} ${name}`); }
async function zipBytes(extra = false) {
  const zip = new JSZip();
  const options = {date: new Date('2026-01-01T00:00:00Z'), createFolders:false};
  zip.file('source/main.go', 'package main\n\n// ignored comment\nfunc main() {\n println("hi // code")\n}\n', options);
  zip.file('source/app.py', '# ignored comment\nprint("hello")\n', options);
  zip.file('source/README.md', '# not source\n', options);
  zip.file('source/node_modules/dependency.js', 'let ignored = 1;\n', options);
  if (extra) zip.file('source/extra.ts', 'const extra: number = 42;\n', options);
  return zip.generateAsync({type:'nodebuffer',compression:'DEFLATE'});
}
async function upload(client, bytes, engineeringId = '') {
  const form = new FormData();
  form.set('name', `${prefix} 代码工程`); form.set('description', '自动化集成验收夹具，截图仅用于验证上传格式。');
  form.set('tech_stack', '["Go","Python"]'); form.set('github_url', 'https://github.com/example/verification'); form.set('github_ref', 'v1.0.0');
  if (engineeringId) form.set('engineering_id', engineeringId);
  form.set('zip', new Blob([bytes]), 'source.zip');
  form.set('startup_screenshot', new Blob([png], {type:'image/png'}), 'startup.png');
  form.append('feature_screenshots', new Blob([png], {type:'image/png'}), 'feature.png');
  const result = await client.request('/api/v1/admin/engineering-versions', {method:'POST', body:form, status:201});
  for (let i=0;i<100;i++) {
    const job = await client.request(`/api/v1/admin/analysis-jobs/${result.job_id}`);
    if (job.status === 'ready') return result;
    if (job.status === 'failed') throw new Error(`分析失败: ${job.error}`);
    await new Promise(r=>setTimeout(r,300));
  }
  throw new Error('分析任务超时');
}

const admin = new Client();
await admin.login(process.env.PROJECT_HUB_ADMIN_USERNAME || 'admin', process.env.PROJECT_HUB_ADMIN_PASSWORD || 'admin-local-change-me');
console.log(`验证环境 ${base}，数据前缀 ${prefix}`);
let a, b, ca, cb, outsider, engineering, p, p2, adminToken, memberToken, claimA, claimB;
let tagDomain, tagChild, tagSecurityDomain, tagVulnerability;
const bytes = await zipBytes();

await test('CSRF 写请求保护与一年会话', async()=> {
  await admin.request('/api/v1/tokens', {method:'POST', body:{name:'csrf-negative'}, csrf:false, status:403});
  const response = await fetch(base+'/api/v1/auth/me',{headers:{Cookie:admin.cookie}});
  assert.equal(response.status,200);
  const cookie = response.headers.get('set-cookie') || '';
  assert.match(cookie,/HttpOnly/i); assert.match(cookie,/Max-Age=31536000/i);
});
await test('管理员创建成员及个人令牌', async()=> {
  a = await admin.request('/api/v1/admin/users',{method:'POST',body:{username:`a${Date.now()}`,name:`${prefix} 甲`,password,role:'member'},status:201});
  b = await admin.request('/api/v1/admin/users',{method:'POST',body:{username:`b${Date.now()}`,name:`${prefix} 乙`,password,role:'member'},status:201});
  ca = new Client(); cb = new Client(); await ca.login(a.username,password); await cb.login(b.username,password);
  const u = await admin.request('/api/v1/admin/users',{method:'POST',body:{username:`c${Date.now()}`,name:`${prefix} 外部成员`,password,role:'member'},status:201});
  outsider = new Client(); await outsider.login(u.username,password);
  adminToken = await admin.request('/api/v1/tokens',{method:'POST',body:{name:`${prefix} admin`},status:201});
  memberToken = await ca.request('/api/v1/tokens',{method:'POST',body:{name:`${prefix} member`},status:201});
  assert.ok(adminToken.token); assert.ok(memberToken.token);
});
await test('管理员令牌上传与成熟代码分析', async()=> {
  engineering = await upload(new Client(adminToken.token), bytes);
  const detail = await admin.request(`/api/v1/admin/engineering/${engineering.engineering_id}`);
  const v = detail.versions.find(x=>x.id === engineering.version_id);
  assert.equal(v.status,'ready'); assert.equal(v.source_file_count,2); assert.equal(v.total_file_count,4);
  assert.equal(v.zip_sha256,crypto.createHash('sha256').update(bytes).digest('hex'));
  assert.equal(v.languages.find(x=>x.name === 'Go').code,4);
  assert.equal(v.languages.find(x=>x.name === 'Python').code,1);
  assert.ok(Math.abs(v.languages.reduce((sum,x)=>sum+x.percentage,0)-100)<0.01);
});

await test('标签树种子数据对成员可读且排序完整', async()=> {
  const tree = await ca.request('/api/v1/tags');
  assert.equal(tree.total, 10);
  tagSecurityDomain = tree.items.find(x=>x.name === '安全');
  assert.ok(tagSecurityDomain); assert.equal(tagSecurityDomain.parent_id, null);
  assert.equal(tagSecurityDomain.children.length, 2);
  assert.equal(tagSecurityDomain.children[1].name, '其他'); assert.deepEqual(tagSecurityDomain.children[1].children, []);
  tagVulnerability = tagSecurityDomain.children.find(x=>x.name === '漏洞检测与攻防');
  assert.ok(tagVulnerability); assert.equal(tagVulnerability.parent_id, tagSecurityDomain.id);
  const game = tree.items.find(x=>x.name === '游戏、图形与多媒体');
  assert.deepEqual(game.children.map(x=>x.name), ['音视频处理','图形渲染','其他']);
  assert.equal(tagSecurityDomain.engineering_count, 0);
  await ca.request('/api/v1/admin/tags',{method:'POST',body:{name:`${prefix} 成员不能建标签`},status:403});
});
await test('管理员创建标签领域与子方向并校验重名与三级', async()=> {
  tagDomain = await admin.request('/api/v1/admin/tags',{method:'POST',status:201,body:{name:`${prefix} 验收领域`}});
  assert.equal(tagDomain.parent_id, null); assert.equal(tagDomain.engineering_count, 0); assert.deepEqual(tagDomain.children, []);
  tagChild = await admin.request('/api/v1/admin/tags',{method:'POST',status:201,body:{name:`${prefix} 验收子方向`,parent_id:tagDomain.id}});
  assert.equal(tagChild.parent_id, tagDomain.id);
  await admin.request('/api/v1/admin/tags',{method:'POST',body:{name:`${prefix} 验收子方向`,parent_id:tagDomain.id},status:409});
  await admin.request('/api/v1/admin/tags',{method:'POST',body:{name:`${prefix} 三级标签`,parent_id:tagChild.id},status:400});
  await admin.request('/api/v1/admin/tags',{method:'POST',body:{name:'   ',parent_id:tagDomain.id},status:400});
});
await test('工程整体替换标签并在省略 tag_ids 时保持', async()=> {
  await admin.request(`/api/v1/admin/engineering/${engineering.engineering_id}`,{method:'PATCH',body:{tag_ids:[tagChild.id]}});
  const detail = await admin.request(`/api/v1/admin/engineering/${engineering.engineering_id}`);
  assert.equal(detail.engineering.tags.length, 1);
  assert.equal(detail.engineering.tags[0].id, tagChild.id);
  assert.equal(detail.engineering.tags[0].parent_name, tagDomain.name);
  assert.equal(detail.versions[0].tags.length, 1);
  const listed = await admin.request('/api/v1/admin/engineering');
  const item = listed.items.find(x=>x.engineering_id === engineering.engineering_id);
  assert.ok(item); assert.equal(item.tags.length, 1);
  await admin.request(`/api/v1/admin/engineering/${engineering.engineering_id}`,{method:'PATCH',body:{description:'自动化验收工程（已打标签）'}});
  const kept = await admin.request(`/api/v1/admin/engineering/${engineering.engineering_id}`);
  assert.equal(kept.engineering.tags.length, 1);
  await admin.request(`/api/v1/admin/engineering/${engineering.engineering_id}`,{method:'PATCH',body:{tag_ids:[tagSecurityDomain.id]},status:400});
});
await test('工程池按领域、子方向与 OR 筛选', async()=> {
  const byDomain = await admin.request(`/api/v1/admin/engineering?tag_ids=${tagDomain.id}`);
  assert.equal(byDomain.total, 1); assert.equal(byDomain.items[0].engineering_id, engineering.engineering_id);
  const byChild = await admin.request(`/api/v1/admin/engineering?tag_ids=${tagChild.id}`);
  assert.equal(byChild.total, 1); assert.equal(byChild.items[0].engineering_id, engineering.engineering_id);
  const or = await admin.request(`/api/v1/admin/engineering?tag_ids=${tagDomain.id},${tagSecurityDomain.id}`);
  assert.equal(or.total, 1); assert.equal(or.items[0].engineering_id, engineering.engineering_id);
  const other = await admin.request(`/api/v1/admin/engineering?tag_ids=${tagSecurityDomain.id}`);
  assert.equal(other.total, 0);
  const combined = await admin.request(`/api/v1/admin/engineering?tag_ids=${tagDomain.id}&q=${encodeURIComponent(prefix)}`);
  assert.equal(combined.total, 1);
  await admin.request('/api/v1/admin/engineering?tag_ids=not-a-uuid',{status:400});
  await admin.request(`/api/v1/admin/engineering?tag_ids=${'00000000-0000-4000-8000-0000000000ff'}`,{status:400});
});
await test('标签删除保护与工程解绑清理', async()=> {
  await admin.request(`/api/v1/admin/tags/${tagDomain.id}`,{method:'DELETE',status:409});
  await admin.request(`/api/v1/admin/tags/${tagChild.id}`,{method:'DELETE',status:409});
  await admin.request(`/api/v1/admin/engineering/${engineering.engineering_id}`,{method:'PATCH',body:{tag_ids:[]}});
  const cleared = await admin.request(`/api/v1/admin/engineering/${engineering.engineering_id}`);
  assert.equal(cleared.engineering.tags.length, 0);
  await admin.request(`/api/v1/admin/tags/${tagChild.id}`,{method:'DELETE',status:204});
  await admin.request(`/api/v1/admin/tags/${tagDomain.id}`,{method:'DELETE',status:204});
  await admin.request(`/api/v1/admin/engineering?tag_ids=${tagChild.id}`,{status:400});
});
const start = new Date(Date.now()-86400000).toISOString(); const end = new Date(Date.now()+86400000*7).toISOString();
async function project(name, members = [a.id,b.id]) { return admin.request('/api/v1/admin/projects',{method:'POST',status:201,body:{name:`${prefix} ${name}`,description:'自动化验收项目',start_at:start,end_at:end,tech_stack:['Go'],min_files:0,max_files:0,status:'active',engineering_ids:[engineering.engineering_id],member_ids:members}}); }
await test('项目固定范围与跨项目工程引用', async()=> { p=await project('项目一'); p2=await project('项目二'); assert.ok(p.id); assert.ok(p2.id); });

const engineeringPath = (projectId)=>`/api/v1/projects/${projectId}/engineering/${engineering.engineering_id}`;
await test('成员只能浏览摘要，领取前不能下载', async()=> {
  const list = await ca.request(`/api/v1/projects/${p.id}/engineering`);
  assert.equal(list.items.length,1); assert.equal(list.items[0].can_download,false); assert.ok(!list.items[0].github_url);
  await ca.request(engineeringPath(p.id)+'/zip',{status:403});
});
await test('非项目成员不能通过ID读取或下载', async()=> {
  await outsider.request(`/api/v1/projects/${p.id}/engineering`,{status:403});
  await outsider.request(engineeringPath(p.id)+'/zip',{status:403});
  const mine = await outsider.request('/api/v1/me/projects'); assert.equal(mine.total,0);
});
await test('并发领取严格只有一个成功', async()=> {
  const attempts = await Promise.all([ca,cb].map(async(client)=> {
    const r = await fetch(base+engineeringPath(p.id)+'/claim',{method:'POST',headers:{Cookie:client.cookie,'X-CSRF-Token':client.csrf}});
    return {client,status:r.status,data:await r.json()};
  }));
  assert.equal(attempts.filter(x=>x.status===201 || x.status===200).length,1);
  assert.equal(attempts.filter(x=>x.status===409).length,1);
  if (attempts[0].status===409) { [a,b]=[b,a]; [ca,cb]=[cb,ca]; }
  claimA=attempts.find(x=>x.client===ca).data;
  assert.ok(claimA.id);
});
await test('领取者下载 ZIP，其他成员无法下载', async()=> {
  assert.deepEqual(await ca.request(engineeringPath(p.id)+'/zip',{raw:true}), bytes);
  await cb.request(engineeringPath(p.id)+'/zip',{status:403});
});
await test('相同工程不同项目可以分别领取', async()=> { claimB=await cb.request(engineeringPath(p2.id)+'/claim',{method:'POST',status:201}); assert.ok(claimB.id); });
await test('移除或升级已领取范围返回冲突', async()=> {
  await admin.request(`/api/v1/admin/projects/${p.id}/engineering-scope`,{method:'POST',body:{remove:[engineering.engineering_id]},status:409});
  await admin.request(`/api/v1/admin/projects/${p.id}/engineering-scope`,{method:'POST',body:{upgrades:[{engineering_id:engineering.engineering_id,version_id:engineering.version_id}]},status:409});
});
let newer;
await test('工程新版本不改变旧引用或下载内容', async()=> {
  newer=await upload(admin,await zipBytes(true),engineering.engineering_id);
  assert.notEqual(newer.version_id,engineering.version_id);
  assert.deepEqual(await ca.request(engineeringPath(p.id)+'/zip',{raw:true}),bytes);
  const list=await ca.request(`/api/v1/projects/${p.id}/engineering`); assert.equal(list.items[0].version_id,engineering.version_id);
});
await test('归还、重新领取与旧 claim 防护', async()=> {
  await ca.request(engineeringPath(p.id)+'/release',{method:'POST',body:{claim_id:claimA.id},status:204});
  await ca.request(engineeringPath(p.id)+'/zip',{status:403});
  claimA=await cb.request(engineeringPath(p.id)+'/claim',{method:'POST',status:201});
  await ca.request(engineeringPath(p.id)+'/release',{method:'POST',body:{claim_id:claimA.id},status:409});
  assert.deepEqual(await cb.request(engineeringPath(p.id)+'/zip',{raw:true}),bytes);
});
await test('管理员转交与回收保持权限一致', async()=> {
  await admin.request(`/api/v1/admin/claims/${claimA.id}/transfer`,{method:'POST',body:{user_id:a.id}});
  await cb.request(engineeringPath(p.id)+'/zip',{status:403});
  assert.deepEqual(await ca.request(engineeringPath(p.id)+'/zip',{raw:true}),bytes);
  const rows=await ca.request(`/api/v1/projects/${p.id}/engineering`);
  claimA={id:rows.items[0].claim_id};
  await admin.request(`/api/v1/admin/claims/${claimA.id}/reclaim`,{method:'POST'});
  await ca.request(engineeringPath(p.id)+'/zip',{status:403});
});
await test('未领取工程显式升级项目版本', async()=> {
  await admin.request(`/api/v1/admin/projects/${p.id}/engineering-scope`,{method:'POST',body:{upgrades:[{engineering_id:engineering.engineering_id,version_id:newer.version_id}]}});
  claimA=await ca.request(engineeringPath(p.id)+'/claim',{method:'POST',status:201});
  const rows=await ca.request(`/api/v1/projects/${p.id}/engineering`); assert.equal(rows.items[0].version_id,newer.version_id);
  assert.deepEqual(await ca.request(engineeringPath(p.id)+'/zip',{raw:true}),await zipBytes(true));
});
await test('项目过期后持有人可下载、不可新领取', async()=> {
  await admin.request(`/api/v1/admin/projects/${p.id}`,{method:'PATCH',body:{end_at:new Date(Date.now()-60000).toISOString()}});
  assert.deepEqual(await ca.request(engineeringPath(p.id)+'/zip',{raw:true}),await zipBytes(true));
  await ca.request(engineeringPath(p.id)+'/release',{method:'POST',body:{claim_id:claimA.id},status:204});
  await ca.request(engineeringPath(p.id)+'/claim',{method:'POST',status:409});
});
await test('成员令牌只读并过滤别人的领取', async()=> {
  const token = await ca.request('/api/v1/tokens',{method:'POST',body:{name:`${prefix} readonly`},status:201});
  const client=new Client(token.token);
  await client.request(engineeringPath(p2.id)+'/claim',{method:'POST',status:403});
  await client.request('/api/v1/admin/users',{status:403});
  const projects=await client.request('/open/v1/me/projects'); assert.ok(projects.items.find(x=>x.id===p.id));
  const list=await client.request(`/open/v1/projects/${p2.id}/engineering`); assert.equal(list.items.length,0);
  await ca.request(`/api/v1/tokens/${token.id}`,{method:'DELETE'});
  await client.request('/open/v1/me/projects',{status:401});
});
await test('移出成员在同一事务回收工程与访问', async()=> {
  await admin.request(`/api/v1/admin/projects/${p2.id}/members`,{method:'POST',body:{remove:[b.id]}});
  await cb.request(`/api/v1/projects/${p2.id}/engineering`,{status:403});
  await cb.request(engineeringPath(p2.id)+'/zip',{status:403});
  const rows=await admin.request(`/api/v1/projects/${p2.id}/engineering`); assert.ok(!rows.items[0].claimed_by);
});
await test('历史领取存在时可移除范围并重新追加', async()=> {
  await admin.request(`/api/v1/admin/projects/${p2.id}/engineering-scope`,{method:'POST',body:{remove:[engineering.engineering_id]}});
  const removed = await admin.request(`/api/v1/projects/${p2.id}/engineering`); assert.equal(removed.total,0);
  await ca.request(engineeringPath(p2.id)+'/claim',{method:'POST',status:404});
  await admin.request(`/api/v1/admin/projects/${p2.id}/engineering-scope`,{method:'POST',body:{add:[engineering.engineering_id]}});
  const added = await ca.request(`/api/v1/projects/${p2.id}/engineering`); assert.equal(added.total,1); assert.equal(added.items[0].version_id,newer.version_id);
  await ca.request(engineeringPath(p2.id)+'/claim',{method:'POST',status:201});
});
await test('开放接口详情返回受鉴权下载地址', async()=> {
  const token=await ca.request('/api/v1/tokens',{method:'POST',body:{name:`${prefix} download`},status:201});
  const client=new Client(token.token);
  const detail=await client.request(`/open/v1/engineering/${engineering.engineering_id}?project_id=${p2.id}`);
  assert.ok(detail.can_download); assert.equal(detail.github_ref,'v1.0.0'); assert.ok(detail.zip_url);
  assert.deepEqual(await client.request(detail.zip_url,{raw:true}),await zipBytes(true));
  await ca.request(`/api/v1/tokens/${token.id}`,{method:'DELETE'});
  await client.request(detail.zip_url,{status:401});
});
await test('停用账号撤销会话与所有令牌', async()=> {
  const token=await ca.request('/api/v1/tokens',{method:'POST',body:{name:`${prefix} disabled`},status:201});
  await admin.request(`/api/v1/admin/users/${a.id}`,{method:'PATCH',body:{active:false}});
  await ca.request('/api/v1/auth/me',{status:401});
  await new Client(token.token).request('/open/v1/me/projects',{status:401});
});
await test('审计与服务端分页记录真实操作', async()=> {
  const events=await admin.request(`/api/v1/admin/audit-events?project_id=${p.id}&page_size=2`);
  assert.ok(events.total>=5); assert.equal(events.items.length,2);
  const search=await admin.request(`/api/v1/admin/engineering?q=${encodeURIComponent(prefix)}&min_files=2&max_files=3`); assert.ok(search.total>=1);
});
console.log(`\n全部通过：${passed} 组真实 API 集成验收。测试数据保留在 ${prefix}。`);
