import { useEffect, useState, type FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import {
  ArrowDownToLine,
  ArrowRight,
  ArrowUpRight,
  Boxes,
  Check,
  CheckCircle2,
  Copy,
  ExternalLink,
  Eye,
  EyeOff,
  FileText,
  FolderKanban,
  History,
  KeyRound,
  MoreHorizontal,
  Pencil,
  Plus,
  RotateCw,
  ShieldCheck,
  Trash2,
  UserPlus,
  Users,
} from "lucide-react";
import { useAuth } from "../App";
import {
  actionName,
  api,
  formatDate,
  params,
  patch,
  post,
  type AuditEvent,
  type Engineering,
  type List,
  type Overview,
  type Project,
  type Token,
  type User,
} from "../api";
import {
  Avatar,
  Button,
  Confirm,
  Drawer,
  Empty,
  ErrorState,
  Field,
  IconButton,
  LanguageBar,
  Loading,
  PageHeader,
  Pagination,
  SearchInput,
  Status,
  Tabs,
  useToast,
} from "../ui";
import { Screenshot, engineeringID } from "./Engineering";
import { projectPhase } from "./Projects";

function targetName(type: string) { return ({ engineering: '工程', project: '项目', user: '用户', claim: '领取', token: '令牌', api_token: 'API 令牌', engineering_version: '工程版本', analysis_job: '分析任务', github_account: 'GitHub 账号', backup_target: '备份仓库', backup_job: '备份任务' } as Record<string, string>)[type] || type; }

export function OverviewPage() {
  const { user } = useAuth();
  const overview = useQuery({
    queryKey: ["overview"],
    queryFn: () => api<Overview>("/api/v1/admin/overview"),
  });
  const projects = useQuery({
    queryKey: ["overview-projects"],
    queryFn: () => api<List<Project>>("/api/v1/admin/projects?page_size=5"),
  });
  const engineering = useQuery({
    queryKey: ["overview-engineering"],
    queryFn: () =>
      api<List<Engineering>>(
        "/api/v1/admin/engineering?page_size=4&status=ready",
      ),
  });
  const data = overview.data;
  return (
    <>
      <PageHeader
        eyebrow="WORKSPACE / OVERVIEW"
        title="工作总览"
        subtitle={`${user.name}，今天的工程与项目都在这里。`}
      >
        <Link className="button secondary" to="/engineering">
          <Boxes size={16} />
          工程池
        </Link>
        <Link className="button primary" to="/projects">
          <Plus size={16} />
          项目管理
        </Link>
      </PageHeader>
      {overview.isPending ? (
        <Loading />
      ) : overview.isError ? (
        <ErrorState error={overview.error} retry={() => overview.refetch()} />
      ) : (
        data && (
          <>
            <div className="overview-stats">
              <div>
                <span>
                  工程资产 <Boxes size={16} />
                </span>
                <b>{data.engineering_count.toLocaleString()}</b>
                <small>
                  <i className="dot green" />
                  {data.ready_count} 个可用工程
                </small>
              </div>
              <div>
                <span>
                  团队项目 <FolderKanban size={16} />
                </span>
                <b>{data.project_count.toLocaleString()}</b>
                <small>
                  <i className="dot coral" />
                  {data.active_project_count} 个进行中
                </small>
              </div>
              <div>
                <span>
                  当前领取 <ArrowDownToLine size={16} />
                </span>
                <b>{data.claim_count.toLocaleString()}</b>
                <small>固定版本 · 独占领取</small>
              </div>
              <div>
                <span>
                  工作空间成员 <Users size={16} />
                </span>
                <b>{data.user_count.toLocaleString()}</b>
                <small>基于项目的访问权限</small>
              </div>
            </div>
            <div className="overview-middle">
              <section className="overview-projects">
                <div className="section-toolbar">
                  <div className="section-title">
                    <h2>近期项目</h2>
                    <span className="section-kicker">PROJECTS</span>
                  </div>
                  <Link className="text-link" to="/projects">
                    全部项目
                    <ArrowRight size={14} />
                  </Link>
                </div>
                {projects.isPending ? (
                  <Loading />
                ) : projects.isError ? (
                  <ErrorState error={projects.error} />
                ) : !projects.data?.items.length ? (
                  <Empty
                    icon={FolderKanban}
                    title="还没有项目"
                    action={
                      <Link className="button secondary" to="/projects">
                        <Plus size={16} />
                        新建项目
                      </Link>
                    }
                  />
                ) : (
                  <div className="overview-project-list">
                    {projects.data.items.map((p, i) => (
                      <Link to={`/projects/${p.id}`} key={p.id}>
                        <span className="list-index">
                          {String(i + 1).padStart(2, "0")}
                        </span>
                        <div className="overview-project-main">
                          <h3>{p.name}</h3>
                          <span>
                            {p.engineering_count || 0} 个工程
                            <span className="middot">·</span>
                            {p.member_count || 0} 位成员
                          </span>
                        </div>
                        <div className="overview-project-progress">
                          <b>
                            {p.claimed_count || 0}
                            <small> / {p.engineering_count || 0}</small>
                          </b>
                          <span className="progress-track">
                            <i
                              style={{
                                width: `${p.engineering_count ? ((p.claimed_count || 0) / p.engineering_count) * 100 : 0}%`,
                              }}
                            />
                          </span>
                        </div>
                        <span className={`status ${projectPhase(p)}`}>
                          <i />
                          {
                            (
                              {
                                active: "进行中",
                                upcoming: "未开始",
                                expired: "已结束",
                                paused: "已暂停",
                                archived: "已归档",
                              } as Record<string, string>
                            )[projectPhase(p)]
                          }
                        </span>
                        <ArrowUpRight size={16} />
                      </Link>
                    ))}
                  </div>
                )}
                <div className="pool-health">
                  <div>
                    <span className="health-title">工程池状态</span>
                    <b>
                      {data.pending_count
                        ? `${data.pending_count} 个工程正在分析`
                        : "所有分析任务已处理"}
                    </b>
                  </div>
                  <div className="health-track">
                    <span
                      style={{
                        width: `${data.engineering_count ? (data.ready_count / data.engineering_count) * 100 : 0}%`,
                      }}
                    />
                  </div>
                  <Link to="/engineering" className="text-link">
                    <ArrowRight size={16} />
                  </Link>
                </div>
              </section>
              <section className="overview-activity">
                <div className="section-toolbar">
                  <div className="section-title">
                    <h2>最近动态</h2>
                    <span className="section-kicker">ACTIVITY</span>
                  </div>
                  <Link to="/audit" className="text-link">
                    <History size={15} />
                  </Link>
                </div>
                {data.recent_events?.length ? (
                  <div className="activity-timeline">
                    {data.recent_events.slice(0, 6).map((event) => (
                      <div key={event.id}>
                        <span className="timeline-dot" />
                        <div>
                          <p>
                            <b>{event.actor_name || "系统"}</b>
                            <span>{actionName(event.action)}</span>
                          </p>
                          <small>{formatDate(event.created_at, true)}</small>
                        </div>
                      </div>
                    ))}
                  </div>
                ) : (
                  <Empty icon={History} title="暂无操作记录" />
                )}
              </section>
            </div>
            <section className="overview-engineering">
              <div className="section-toolbar">
                <div className="section-title">
                  <h2>最新可用工程</h2>
                  <span className="section-kicker">CODE ASSETS</span>
                </div>
                <Link className="text-link" to="/engineering">
                  查看工程池
                  <ArrowRight size={14} />
                </Link>
              </div>
              {engineering.isPending ? (
                <Loading />
              ) : engineering.isError ? (
                <ErrorState error={engineering.error} />
              ) : !engineering.data?.items.length ? (
                <Empty
                  icon={Boxes}
                  title="还没有可用工程"
                  action={
                    <Link className="button secondary" to="/engineering">
                      前往工程池
                      <ArrowRight size={15} />
                    </Link>
                  }
                />
              ) : (
                <div className="overview-engineering-list">
                  {engineering.data.items.map((item) => (
                    <Link key={engineeringID(item)} to="/engineering">
                      <Screenshot item={item} large />
                      <div>
                        <div className="overview-engineering-title">
                          <h3>{item.name}</h3>
                          <ArrowUpRight size={15} />
                        </div>
                        <span>
                          {item.source_file_count} 个源码文件
                          <span className="middot">·</span>v
                          {item.version_number}
                        </span>
                        <LanguageBar values={item.languages} compact />
                      </div>
                    </Link>
                  ))}
                </div>
              )}
            </section>
          </>
        )
      )}
    </>
  );
}
export function UsersPage() {
  const { user } = useAuth();
  const client = useQueryClient();
  const toast = useToast();
  const [q, setQ] = useState("");
  const [role, setRole] = useState("all");
  const [active, setActive] = useState("all");
  const [page, setPage] = useState(1);
  const [edit, setEdit] = useState<User | boolean>(false);
  const [toggle, setToggle] = useState<User>();
  const [busy, setBusy] = useState(false);
  const query = useQuery({
    queryKey: ["users", q, role, active, page],
    queryFn: () =>
      api<List<User>>(`/api/v1/admin/users?${params({ q, role, active: active === 'all' ? 'all' : String(active === 'active'), page, page_size: 20 })}`),
  });
  useEffect(() => setPage(1), [q, role, active]);
  const users = query.data?.items || [];
  async function toggleUser() {
    setBusy(true);
    try {
      await patch(`/api/v1/admin/users/${toggle?.id}`, {
        name: toggle?.name,
        role: toggle?.role,
        active: !toggle?.active,
      });
      client.invalidateQueries({ queryKey: ["users"] });
      client.invalidateQueries({ queryKey: ["users-options"] });
      setToggle(undefined);
      toast(toggle?.active ? "用户已停用" : "用户已启用");
    } catch (e) {
      toast(e instanceof Error ? e.message : "更新失败", "error");
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <PageHeader
        eyebrow="TEAM / MEMBERS"
        title="用户维护"
        subtitle="成员账号、角色与团队访问权限。"
      >
        <Button icon={UserPlus} onClick={() => setEdit(true)}>
          创建用户
        </Button>
      </PageHeader>
      <div className="section-toolbar">
        <div className="section-title">
          <h2>团队成员</h2>
          <span className="count-badge">{query.data?.total ?? "—"}</span>
        </div>
        <IconButton
          icon={RotateCw}
          label="刷新用户"
          onClick={() => query.refetch()}
        />
      </div>
      <div className="filter-bar">
        <SearchInput value={q} onChange={setQ} placeholder="搜索姓名、用户名" />
        <select
          value={role}
          onChange={(e) => setRole(e.target.value)}
          aria-label="用户角色"
        >
          <option value="all">全部角色</option>
          <option value="admin">管理员</option>
          <option value="member">成员</option>
        </select>
        <select
          value={active}
          onChange={(e) => setActive(e.target.value)}
          aria-label="账号状态"
        >
          <option value="all">全部状态</option>
          <option value="active">正常</option>
          <option value="inactive">已停用</option>
        </select>
      </div>
      {query.isPending ? (
        <Loading />
      ) : query.isError ? (
        <ErrorState error={query.error} retry={() => query.refetch()} />
      ) : !users.length ? (
        <Empty icon={Users} title="没有符合条件的用户" />
      ) : (
        <div className="table-wrap">
          <table className="data-table users-table">
            <thead>
              <tr>
                <th>成员</th>
                <th>用户名</th>
                <th>角色</th>
                <th>账号状态</th>
                <th>加入时间</th>
                <th className="table-action-heading">操作</th>
              </tr>
            </thead>
            <tbody>
              {users.map((u) => (
                <tr key={u.id}>
                  <td>
                    <div className="user-cell">
                      <Avatar name={u.name} />
                      <b>{u.name}</b>
                      {u.id === user.id && <span className="tag">我</span>}
                    </div>
                  </td>
                  <td className="muted">@{u.username}</td>
                  <td>
                    <span className={`role-label ${u.role}`}>
                      {u.role === "admin" && <ShieldCheck size={13} />}{" "}
                      {u.role === "admin" ? "管理员" : "成员"}
                    </span>
                  </td>
                  <td>
                    <Status status={u.active ? "enabled" : "disabled"} />
                  </td>
                  <td className="muted">{formatDate(u.created_at)}</td>
                  <td>
                    <div className="row-actions">
                      <IconButton
                        icon={Pencil}
                        label={`编辑 ${u.name}`}
                        onClick={() => setEdit(u)}
                      />
                      <button
                        className={`text-button ${u.active ? "danger-text" : ""}`}
                        disabled={u.id === user.id}
                        title={
                          u.id === user.id ? "不能停用当前账号" : undefined
                        }
                        onClick={() => setToggle(u)}
                      >
                        {u.active ? "停用" : "启用"}
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <Pagination
        page={page}
        total={query.data?.total || 0}
        onChange={setPage}
      />
      {edit && (
        <UserForm
          user={typeof edit === "object" ? edit : undefined}
          onClose={() => setEdit(false)}
        />
      )}{" "}
      {toggle && (
        <Confirm
          title={`${toggle.active ? "停用" : "启用"}用户「${toggle.name}」？`}
          description={
            toggle.active
              ? "停用后所有会话与令牌立即失效。"
              : "启用后此用户可重新登录。"
          }
          onClose={() => setToggle(undefined)}
          onConfirm={toggleUser}
          loading={busy}
          danger={toggle.active}
        />
      )}
    </>
  );
}
function UserForm({ user, onClose }: { user?: User; onClose: () => void }) {
  const client = useQueryClient();
  const toast = useToast();
  const [name, setName] = useState(user?.name || "");
  const [username, setUsername] = useState(user?.username || "");
  const [role, setRole] = useState(user?.role || "member");
  const [password, setPassword] = useState("");
  const [show, setShow] = useState(false);
  const [busy, setBusy] = useState(false);
  async function save(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      if (user)
        await patch(`/api/v1/admin/users/${user.id}`, {
          name,
          role,
          active: user.active,
          ...(password ? { password } : {}),
        });
      else
        await post("/api/v1/admin/users", { name, username, role, password });
      client.invalidateQueries({ queryKey: ["users"] });
      client.invalidateQueries({ queryKey: ["users-options"] });
      client.invalidateQueries({ queryKey: ["overview"] });
      toast(user ? "用户信息已更新" : "用户已创建");
      onClose();
    } catch (e) {
      toast(e instanceof Error ? e.message : "保存失败", "error");
    } finally {
      setBusy(false);
    }
  }
  return (
    <Drawer title={user ? "编辑用户" : "创建用户"} onClose={onClose}>
      <form onSubmit={save}>
        <Field label="姓名" required>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
            maxLength={80}
          />
        </Field>
        <Field label="用户名" required>
          <input
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            required
            disabled={!!user}
            pattern="[a-zA-Z0-9_.-]+"
            maxLength={80}
            autoComplete="off"
          />
        </Field>
        <Field label="角色">
          <select
            value={role}
            onChange={(e) => setRole(e.target.value as "admin" | "member")}
          >
            <option value="member">成员</option>
            <option value="admin">管理员</option>
          </select>
        </Field>
        <Field
          label={user ? "重设密码" : "初始密码"}
          required={!user}
          hint={user ? "留空保留当前密码" : "至少 8 个字符"}
        >
          <div className="password-input">
            <input
              type={show ? "text" : "password"}
              minLength={8}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required={!user}
              autoComplete="new-password"
            />
            <IconButton
              type="button"
              icon={show ? EyeOff : Eye}
              label={show ? "隐藏密码" : "显示密码"}
              onClick={() => setShow(!show)}
            />
          </div>
        </Field>
        <div className="form-actions">
          <Button type="button" variant="secondary" onClick={onClose}>
            取消
          </Button>
          <Button type="submit" loading={busy} icon={user ? Check : UserPlus}>
            {user ? "保存用户" : "创建用户"}
          </Button>
        </div>
      </form>
    </Drawer>
  );
}
export function TokensPage() {
  const { user } = useAuth();
  const toast = useToast();
  const client = useQueryClient();
  const [tab, setTab] = useState("tokens");
  const [create, setCreate] = useState(false);
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [revoke, setRevoke] = useState<Token>();
  const [newToken, setNewToken] = useState("");
  const [copied, setCopied] = useState(false);
  const query = useQuery({
    queryKey: ["tokens"],
    queryFn: () => api<List<Token>>("/api/v1/tokens"),
  });
  async function save(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      const result = await post<{ token: string }>("/api/v1/tokens", { name });
      setNewToken(result.token);
      setName("");
      client.invalidateQueries({ queryKey: ["tokens"] });
      toast("令牌已创建");
    } catch (e) {
      toast(e instanceof Error ? e.message : "创建失败", "error");
    } finally {
      setBusy(false);
    }
  }
  async function remove() {
    setBusy(true);
    try {
      await api(`/api/v1/tokens/${revoke?.id}`, { method: "DELETE" });
      client.invalidateQueries({ queryKey: ["tokens"] });
      setRevoke(undefined);
      toast("令牌已撤销");
    } catch (e) {
      toast(e instanceof Error ? e.message : "撤销失败", "error");
    } finally {
      setBusy(false);
    }
  }
  async function copy(value: string) {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      toast("已复制");
      setTimeout(() => setCopied(false), 2000);
    } catch {
      toast("浏览器无法访问剪贴板，请手动选择文本", "error");
    }
  }
  return (
    <>
      <PageHeader
        eyebrow="INTEGRATION / API"
        title="API 与令牌"
        subtitle="你的程序与工程台之间的连接。"
      >
        <Button
          icon={Plus}
          onClick={() => {
            setCreate(true);
            setNewToken("");
          }}
        >
          创建令牌
        </Button>
      </PageHeader>
      <Tabs
        value={tab}
        onChange={setTab}
        items={[
          {
            value: "tokens",
            label: "访问令牌",
            count: query.data?.items.filter((t) => !t.revoked_at).length,
          },
          { value: "docs", label: "接口文档" },
        ]}
      />
      {tab === "tokens" ? (
        <>
          <div className="token-permission">
            <ShieldCheck size={19} />
            <div>
              <b>{user.role === "admin" ? "管理员权限" : "成员只读权限"}</b>
              <span>
                {user.role === "admin"
                  ? "所属账号可访问全部工程、项目及管理接口。"
                  : "仅可访问所属项目、可领取工程与本人已领取工程。"}
              </span>
            </div>
          </div>
          {query.isPending ? (
            <Loading />
          ) : query.isError ? (
            <ErrorState error={query.error} retry={() => query.refetch()} />
          ) : !query.data?.items.length ? (
            <Empty
              icon={KeyRound}
              title="还没有访问令牌"
              action={
                <Button icon={Plus} onClick={() => setCreate(true)}>
                  创建令牌
                </Button>
              }
            />
          ) : (
            <div className="table-wrap">
              <table className="data-table token-table">
                <thead>
                  <tr>
                    <th>令牌名称</th>
                    <th>令牌前缀</th>
                    <th>权限范围</th>
                    <th>创建时间</th>
                    <th>最近使用</th>
                    <th className="table-action-heading">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {query.data.items.map((token) => (
                    <tr
                      className={token.revoked_at ? "revoked-row" : ""}
                      key={token.id}
                    >
                      <td>
                        <div className="token-name">
                          <KeyRound size={16} />
                          <b>{token.name}</b>
                          {token.revoked_at && (
                            <span className="tag">已撤销</span>
                          )}
                        </div>
                      </td>
                      <td className="mono muted">{token.prefix}••••</td>
                      <td>
                        <span className="tag">
                          {token.scope ||
                            (user.role === "admin" ? "admin" : "read")}
                        </span>
                      </td>
                      <td className="muted nowrap">
                        {formatDate(token.created_at)}
                      </td>
                      <td className="muted nowrap">
                        {token.last_used_at
                          ? formatDate(token.last_used_at, true)
                          : "尚未使用"}
                      </td>
                      <td>
                        <div className="row-actions">
                          <IconButton
                            icon={Trash2}
                            label={`撤销 ${token.name}`}
                            disabled={!!token.revoked_at}
                            onClick={() => setRevoke(token)}
                          />
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </>
      ) : (
        <APIDocumentation onCopy={copy} />
      )}{" "}
      {create && (
        <Drawer
          title="创建访问令牌"
          onClose={() => {
            setCreate(false);
            setNewToken("");
          }}
        >
          {newToken ? (
            <>
              <div className="token-created">
                <CheckCircle2 size={28} />
                <h3>令牌创建成功</h3>
                <p>明文只会展示一次，请妥善保存。</p>
              </div>
              <div className="secret-token">
                <code>{newToken}</code>
                <IconButton
                  icon={copied ? Check : Copy}
                  label="复制令牌"
                  onClick={() => copy(newToken)}
                />
              </div>
              <div className="form-actions">
                <Button
                  onClick={() => {
                    setCreate(false);
                    setNewToken("");
                  }}
                >
                  完成
                </Button>
              </div>
            </>
          ) : (
            <form onSubmit={save}>
              <Field label="令牌名称" required>
                <input
                  placeholder="例如：本地工程同步"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  maxLength={100}
                  required
                  autoFocus
                />
              </Field>
              <div className="token-permission">
                <ShieldCheck size={18} />
                <div>
                  <b>{user.role === "admin" ? "管理员" : "只读"}</b>
                  <span>权限继承当前用户及项目成员关系。</span>
                </div>
              </div>
              <div className="form-actions">
                <Button
                  type="button"
                  variant="secondary"
                  onClick={() => setCreate(false)}
                >
                  取消
                </Button>
                <Button type="submit" loading={busy} icon={KeyRound}>
                  创建令牌
                </Button>
              </div>
            </form>
          )}
        </Drawer>
      )}{" "}
      {revoke && (
        <Confirm
          title={`撤销令牌「${revoke.name}」？`}
          description="此令牌将立即失效，使用该令牌的请求会被拒绝。"
          onClose={() => setRevoke(undefined)}
          onConfirm={remove}
          loading={busy}
          danger
        />
      )}
    </>
  );
}
function APIDocumentation({ onCopy }: { onCopy: (value: string) => void }) {
  const { user } = useAuth();
  const [endpoint, setEndpoint] = useState("projects");
  const endpoints = [
    {
      key: "projects",
      name: "我的项目列表",
      path: "/open/v1/me/projects",
      response:
        '{\n  "items": [{ "id": "project-id", "name": "项目名称",\n    "engineering_count": 12, "claimed_count": 4 }],\n  "total": 1\n}',
    },
    {
      key: "engineering",
      name: "项目工程列表",
      path: "/open/v1/projects/{project_id}/engineering",
      response:
        '{\n  "items": [{ "engineering_id": "engineering-id",\n    "version_id": "version-id", "name": "工程名称",\n    "can_download": true, "source_file_count": 128 }],\n  "total": 1\n}',
    },
    {
      key: "detail",
      name: "工程信息与下载地址",
      path: "/open/v1/engineering/{engineering_id}?project_id={project_id}",
      response:
        '{\n  "engineering_id": "engineering-id",\n  "name": "工程名称", "source_file_count": 128,\n  "languages": [{ "name": "Go", "percentage": 74.5 }],\n  "zip_url": "/open/v1/projects/{project_id}/engineering/{engineering_id}/zip"\n}',
    },
    {
      key: "download",
      name: "下载已领取工程",
      path: "/open/v1/projects/{project_id}/engineering/{engineering_id}/zip",
      response:
        'Content-Type: application/zip\nContent-Disposition: attachment; filename="engineering.zip"\n\n<ZIP binary>',
    },
  ];
  const selected = endpoints.find((e) => e.key === endpoint)!;
  const command = `curl -H "Authorization: Bearer YOUR_TOKEN" \\\n  "${window.location.origin}${selected.path}"`;
  return (
    <div className="api-docs">
      <aside className="api-nav">
        <span className="nav-section-label">开放接口 · v1</span>
        {endpoints.map((e) => (
          <button
            className={endpoint === e.key ? "selected" : ""}
            key={e.key}
            onClick={() => setEndpoint(e.key)}
          >
            <span>GET</span>
            {e.name}
            <ChevronRightIcon />
          </button>
        ))}
        <a
          href="/openapi.yaml"
          target="_blank"
          rel="noreferrer"
          className="text-link"
        >
          <FileText size={15} />
          OpenAPI 规范
          <ExternalLink size={12} />
        </a>
      </aside>
      <article className="api-article">
        <div className="api-doc-title">
          <span className="http-method">GET</span>
          <h2>{selected.name}</h2>
        </div>
        <code className="endpoint-path">{selected.path}</code>
        <h3>请求</h3>
        <div className="code-block">
          <IconButton
            icon={Copy}
            label="复制请求示例"
            onClick={() => onCopy(command)}
          />
          <pre>{command}</pre>
        </div>
        <div className="api-header-table">
          <div>
            <span>Authorization</span>
            <code>Bearer YOUR_TOKEN</code>
            <b>必填</b>
          </div>
          <div>
            <span>page</span>
            <code>1</code>
            <small>页码</small>
          </div>
          <div>
            <span>page_size</span>
            <code>20</code>
            <small>最多 100</small>
          </div>
        </div>
        {endpoint === "engineering" && (
          <div className="api-header-table">
            <div>
              <span>scope</span>
              <code>available / mine</code>
              <small>可领取 / 本人已领取</small>
            </div>
            <div>
              <span>language</span>
              <code>Go / Python / …</code>
              <small>语言筛选</small>
            </div>
          </div>
        )}
        <h3>响应示例</h3>
        <div className="code-block response">
          <pre>{selected.response}</pre>
        </div>
        <h3>状态码</h3>
        <dl className="api-status-list">
          <dt>200</dt>
          <dd>请求成功</dd>
          <dt>401</dt>
          <dd>令牌无效、已撤销或账号停用</dd>
          <dt>403</dt>
          <dd>不属于该项目或没有工程访问权限</dd>
          <dt>404</dt>
          <dd>项目或工程不存在</dd>
        </dl>
        {user.role === "admin" && (
          <div className="api-admin-note">
            <ShieldCheck size={18} />
            <span>
              管理员令牌也可调用 <code>/api/v1/admin</code> 写入接口，详见
              OpenAPI 规范。
            </span>
          </div>
        )}
      </article>
    </div>
  );
}
function ChevronRightIcon() {
  return <ArrowRight size={13} />;
}
export function AuditPage() {
  const [q, setQ] = useState("");
  const [action, setAction] = useState("");
  const [actor, setActor] = useState("");
  const [project, setProject] = useState("");
  const [engineering, setEngineering] = useState("");
  const [page, setPage] = useState(1);
  const [detail, setDetail] = useState<AuditEvent>();
  const users = useQuery({
    queryKey: ["users-options"],
    queryFn: () => api<List<User>>("/api/v1/admin/users?page_size=100"),
  });
  const projects = useQuery({
    queryKey: ["audit-project-options"],
    queryFn: () => api<List<Project>>("/api/v1/admin/projects?page_size=100"),
  });
  const engines = useQuery({
    queryKey: ["audit-engineering-options"],
    queryFn: () =>
      api<List<Engineering>>("/api/v1/admin/engineering?page_size=100"),
  });
  const query = useQuery({
    queryKey: ["audit", q, action, actor, project, engineering, page],
    queryFn: () =>
      api<List<AuditEvent>>(
        `/api/v1/admin/audit-events?${params({ q, action, user_id: actor, project_id: project, engineering_id: engineering, page, page_size: 20 })}`,
      ),
  });
  useEffect(() => setPage(1), [q, action, actor, project, engineering]);
  const actions = [
    "engineering.version.upload",
    "engineering.analysis.retry",
    "engineering.update",
    "project.create",
    "project.update",
    "project.scope",
    "project.members",
    "claim.create",
    "claim.release",
    "claim.reclaim",
    "claim.transfer",
    "user.create",
    "user.update",
    "token.create",
    "token.revoke",
    "password.change",
    "github.account.create",
    "github.account.update",
    "github.account.verify",
    "backup.target.create",
    "backup.target.update",
    "backup.sync.enqueue",
    "backup.restore.enqueue",
    "backup.retry",
    "backup.sync.ready",
    "backup.sync.failed",
    "backup.restore.ready",
    "backup.restore.failed",
  ];
  return (
    <>
      <PageHeader
        eyebrow="WORKSPACE / ACTIVITY"
        title="操作记录"
        subtitle="工程、项目与权限变更的完整轨迹。"
      >
        <IconButton
          icon={RotateCw}
          label="刷新记录"
          onClick={() => query.refetch()}
        />
      </PageHeader>
      <div className="audit-filter filter-bar">
        <SearchInput value={q} onChange={setQ} placeholder="搜索操作记录" />
        <select
          aria-label="操作类型"
          value={action}
          onChange={(e) => setAction(e.target.value)}
        >
          <option value="">全部操作</option>
          {actions.map((a) => (
            <option value={a} key={a}>
              {actionName(a)}
            </option>
          ))}
        </select>
        <select
          aria-label="操作用户"
          value={actor}
          onChange={(e) => setActor(e.target.value)}
        >
          <option value="">全部用户</option>
          {users.data?.items.map((u) => (
            <option value={u.id} key={u.id}>
              {u.name}
            </option>
          ))}
        </select>
        <select
          aria-label="关联项目"
          value={project}
          onChange={(e) => setProject(e.target.value)}
        >
          <option value="">全部项目</option>
          {projects.data?.items.map((p) => (
            <option value={p.id} key={p.id}>
              {p.name}
            </option>
          ))}
        </select>
        <select
          aria-label="关联工程"
          value={engineering}
          onChange={(e) => setEngineering(e.target.value)}
        >
          <option value="">全部工程</option>
          {engines.data?.items.map((e) => (
            <option value={engineeringID(e)} key={engineeringID(e)}>
              {e.name}
            </option>
          ))}
        </select>
      </div>
      {query.isPending ? (
        <Loading />
      ) : query.isError ? (
        <ErrorState error={query.error} retry={() => query.refetch()} />
      ) : !query.data?.items.length ? (
        <Empty icon={History} title="没有符合条件的操作记录" />
      ) : (
        <div className="table-wrap">
          <table className="data-table audit-table">
            <thead>
              <tr>
                <th>操作时间</th>
                <th>操作人</th>
                <th>操作</th>
                <th>对象</th>
                <th>记录 ID</th>
                <th className="action-col" />
              </tr>
            </thead>
            <tbody>
              {query.data.items.map((event) => (
                <tr key={event.id}>
                  <td className="muted nowrap">
                    {formatDate(event.created_at, true)}
                  </td>
                  <td>
                    <span className="user-cell">
                      <Avatar name={event.actor_name || "系统"} small />
                      {event.actor_name || "系统"}
                    </span>
                  </td>
                  <td>
                    <span className="audit-action">
                      <i />
                      {actionName(event.action)}
                    </span>
                  </td>
                  <td>
                    <span className="target-label">
                      {targetName(event.target_type)}
                    </span>
                    <small className="cell-sub mono">
                      {event.target_id?.slice(0, 8) || "—"}
                    </small>
                  </td>
                  <td className="mono muted">{event.id.slice(0, 8)}</td>
                  <td>
                    <IconButton
                      icon={MoreHorizontal}
                      label="查看操作详情"
                      onClick={() => setDetail(event)}
                    />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <Pagination
        page={page}
        total={query.data?.total || 0}
        onChange={setPage}
      />
      {detail && (
        <Drawer title="操作详情" onClose={() => setDetail(undefined)}>
          <dl className="detail-list">
            <dt>操作</dt>
            <dd>{actionName(detail.action)}</dd>
            <dt>操作人</dt>
            <dd>{detail.actor_name || "系统"}</dd>
            <dt>时间</dt>
            <dd>{formatDate(detail.created_at, true)}</dd>
            <dt>记录 ID</dt>
            <dd className="mono hash">{detail.id}</dd>
            <dt>对象</dt>
            <dd>{targetName(detail.target_type)}</dd>
            <dt>对象 ID</dt>
            <dd className="mono hash">{detail.target_id}</dd>
            {detail.project_id && (
              <>
                <dt>项目</dt>
                <dd>
                  <Link to={`/projects/${detail.project_id}`}>
                    {detail.project_id}
                    <ExternalLink size={13} />
                  </Link>
                </dd>
              </>
            )}
          </dl>
          <h3 className="form-section-title">变更内容</h3>
          <div className="code-block">
            <pre>{JSON.stringify(detail.details || {}, null, 2)}</pre>
          </div>
        </Drawer>
      )}
    </>
  );
}
