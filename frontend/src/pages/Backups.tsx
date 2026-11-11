import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router-dom";
import {
  ArchiveRestore,
  ArrowLeft,
  ArrowUpRight,
  Check,
  ChevronDown,
  ChevronRight,
  DatabaseBackup,
  ExternalLink,
  Eye,
  EyeOff,
  Folder,
  Github,
  History,
  KeyRound,
  LoaderCircle,
  Pencil,
  Plus,
  RefreshCw,
  RotateCw,
  ShieldCheck,
  ToggleLeft,
  ToggleRight,
  X,
} from "lucide-react";
import {
  api,
  formatDate,
  params,
  patch,
  post,
  type BackupJob,
  type BackupRepositoryGroup,
  type BackupVersionGroup,
  type BackupTarget,
  type BackupTargetGroup,
  type Engineering,
  type GitHubAccount,
  type GitHubAccountList,
  type List,
} from "../api";
import {
  Button,
  Confirm,
  Drawer,
  Empty,
  ErrorState,
  Field,
  IconButton,
  Loading,
  PageHeader,
  Pagination,
  SearchInput,
  Tabs,
  useToast,
} from "../ui";
import { Screenshot, engineeringID } from "./Engineering";

const base = "/api/v1/admin";
function useBackupSearch() {
  const [q, setQ] = useState("");
  const [search, setSearch] = useState("");
  useEffect(() => {
    const timer = setTimeout(() => setSearch(q.trim()), 300);
    return () => clearTimeout(timer);
  }, [q]);
  return { q, setQ, search };
}
const dateInput = (value: string | null) =>
  value
    ? new Intl.DateTimeFormat("en-CA", {
        timeZone: "Asia/Shanghai",
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
      }).format(new Date(value))
    : "";
function safeMessage(error: unknown, secret = "") {
  const message =
    error instanceof Error ? error.message : "请求失败，请稍后重试";
  return secret ? message.split(secret).join("••••") : message;
}
function GitHubLink({
  owner,
  repo,
  children,
}: {
  owner: string;
  repo: string;
  children?: ReactNode;
}) {
  return (
    <a
      className="backup-repo-link"
      href={`https://github.com/${encodeURIComponent(owner)}/${encodeURIComponent(repo)}`}
      target="_blank"
      rel="noreferrer"
      title={`${owner}/${repo}`}
    >
      <span>{children || `${owner}/${repo}`}</span>
      <ExternalLink size={12} />
    </a>
  );
}
function BackupStatus({
  status,
  kind = "sync",
}: {
  status: string;
  kind?: string;
}) {
  const labels: Record<string, string> = {
    active: "正常",
    disabled: "已停用",
    expired: "已到期",
    error: "验证失败",
    pending: "等待中",
    running: "进行中",
    ready: kind === "restore" ? "已恢复" : "已校验",
    failed: "失败",
  };
  const state: Record<string, string> = {
    active: "enabled",
    disabled: "disabled",
    expired: "expired",
    error: "failed",
    pending: "pending",
    running: "pending",
    ready: "ready",
    failed: "failed",
  };
  if (!status) return <span className="muted">尚未同步</span>;
  return (
    <span className={`status ${state[status] || status}`}>
      {status === "running" ? (
        <LoaderCircle size={10} className="spin" />
      ) : (
        <i />
      )}
      {labels[status] || status}
    </span>
  );
}
function BinarySwitch({
  value,
  label,
  disabled,
  onChange,
}: {
  value: boolean;
  label: string;
  disabled?: boolean;
  onChange: () => void;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={value}
      aria-label={label}
      title={label}
      disabled={disabled}
      className={`backup-switch ${value ? "on" : ""}`}
      onClick={onChange}
    >
      <span>
        <i />
      </span>
      <b>{value ? "开启" : "关闭"}</b>
    </button>
  );
}

export function BackupsPage() {
  const [search, setSearch] = useSearchParams();
  const engineering = search.get("engineering_id") || "";
  const targetFilter = search.get("target_id") || "";
  const [tab, setTab] = useState(
    search.get("tab") || (engineering ? "targets" : "accounts"),
  );
  const [accountForm, setAccountForm] = useState<GitHubAccount | true | null>(
    null,
  );
  const [targetForm, setTargetForm] = useState(false);
  const [syncTarget, setSyncTarget] = useState<BackupTarget>();
  const client = useQueryClient();
  const accounts = useQuery({
    queryKey: ["github-account-config"],
    queryFn: () =>
      api<GitHubAccountList>(`${base}/github-accounts?page_size=1`),
  });
  const scope = useQuery({
    queryKey: ["backup-engineering-name", engineering],
    queryFn: () =>
      api<{ engineering: Engineering }>(`${base}/engineering/${engineering}`),
    enabled: !!engineering,
  });
  const configured = accounts.data?.configured === true;
  function changeTab(value: string) {
    setTab(value);
    setSearch((previous) => {
      const next = new URLSearchParams(previous);
      next.set("tab", value);
      if (value !== "jobs") next.delete("target_id");
      return next;
    });
  }
  function showJobs(target: BackupTarget) {
    setTab("jobs");
    setSearch({
      engineering_id: target.engineering_id,
      target_id: target.id,
      tab: "jobs",
    });
  }
  const invalidate = () => {
    client.invalidateQueries({ queryKey: ["github-accounts"] });
    client.invalidateQueries({ queryKey: ["github-account-config"] });
    client.invalidateQueries({ queryKey: ["backup-targets"] });
    client.invalidateQueries({ queryKey: ["backup-jobs"] });
    client.invalidateQueries({ queryKey: ["audit"] });
  };
  const clearFilter = () => setSearch({ tab });
  return (
    <>
      {engineering && (
        <Link className="back-link" to="/engineering">
          <ArrowLeft size={15} />
          返回工程池
        </Link>
      )}
      <PageHeader
        eyebrow="GITHUB / DISASTER RECOVERY"
        title="GitHub 灾备"
        subtitle="工程快照、截图与校验清单的独立副本。"
      >
        {tab === "accounts" ? (
          <Button
            icon={Plus}
            disabled={!configured}
            onClick={() => setAccountForm(true)}
          >
            添加账号
          </Button>
        ) : tab === "targets" ? (
          <Button
            icon={Plus}
            disabled={!configured}
            onClick={() => setTargetForm(true)}
          >
            绑定已有仓库
          </Button>
        ) : (
          <IconButton
            icon={RefreshCw}
            label="刷新同步记录"
            onClick={() =>
              client.invalidateQueries({ queryKey: ["backup-jobs"] })
            }
          />
        )}
      </PageHeader>
      {accounts.isError && (
        <ErrorState error={accounts.error} retry={() => accounts.refetch()} />
      )}
      {accounts.data && !configured && (
        <div className="backup-config-notice" role="status">
          <KeyRound size={20} />
          <div>
            <b>尚未设置加密密钥</b>
            <p>请在服务配置中设置备份密钥后添加 GitHub 账号。</p>
          </div>
        </div>
      )}
      <Tabs
        value={tab}
        onChange={changeTab}
        items={[
          { value: "accounts", label: "GitHub 账号" },
          { value: "targets", label: "备份仓库" },
          { value: "jobs", label: "同步记录" },
        ]}
      />
      {engineering && tab !== "accounts" && (
        <div className="backup-filter-scope">
          <Folder size={14} />
          <span>{scope.data?.engineering.name || "所选工程"}</span>
          {targetFilter && <span className="tag">指定仓库</span>}
          <IconButton icon={X} label="清除工程筛选" onClick={clearFilter} />
        </div>
      )}
      {tab === "accounts" && (
        <AccountsTable
          configured={configured}
          onEdit={setAccountForm}
          onChanged={invalidate}
        />
      )}
      {tab === "targets" && (
        <TargetsTable
          engineering={engineering}
          configured={configured}
          onSync={setSyncTarget}
          onJobs={showJobs}
          onChanged={invalidate}
        />
      )}
      {tab === "jobs" && (
        <JobsTable
          engineering={engineering}
          target={targetFilter}
          configured={configured}
          onChanged={invalidate}
        />
      )}
      {accountForm && (
        <AccountForm
          account={accountForm === true ? undefined : accountForm}
          onClose={() => setAccountForm(null)}
          onSaved={invalidate}
        />
      )}
      {targetForm && (
        <TargetForm
          initialEngineering={engineering}
          onClose={() => setTargetForm(false)}
          onSaved={invalidate}
        />
      )}
      {syncTarget && (
        <SyncForm
          target={syncTarget}
          onClose={() => setSyncTarget(undefined)}
          onSaved={() => {
            invalidate();
            showJobs(syncTarget);
          }}
        />
      )}
    </>
  );
}

function AccountsTable({
  configured,
  onEdit,
  onChanged,
}: {
  configured: boolean;
  onEdit: (account: GitHubAccount) => void;
  onChanged: () => void;
}) {
  const [page, setPage] = useState(1);
  const [busy, setBusy] = useState("");
  const [toggle, setToggle] = useState<GitHubAccount>();
  const toast = useToast();
  const query = useQuery({
    queryKey: ["github-accounts", page],
    queryFn: () =>
      api<GitHubAccountList>(
        `${base}/github-accounts?page=${page}&page_size=20`,
      ),
  });
  async function verify(account: GitHubAccount) {
    setBusy(account.id);
    try {
      await post(`${base}/github-accounts/${account.id}/verify`);
      toast("账号验证通过");
    } catch (e) {
      toast(safeMessage(e), "error");
    } finally {
      setBusy("");
      query.refetch();
      onChanged();
    }
  }
  async function changeActive() {
    if (!toggle) return;
    setBusy(toggle.id);
    try {
      await patch(`${base}/github-accounts/${toggle.id}`, {
        active: !toggle.active,
      });
      toast(toggle.active ? "账号已停用" : "账号已启用");
      setToggle(undefined);
      onChanged();
    } catch (e) {
      toast(safeMessage(e), "error");
    } finally {
      setBusy("");
    }
  }
  return (
    <div className="backup-section">
      <div className="section-toolbar">
        <div className="section-title">
          <h2>账号凭据</h2>
          <span className="count-badge">{query.data?.total ?? "—"}</span>
        </div>
        <IconButton
          icon={RefreshCw}
          label="刷新 GitHub 账号"
          onClick={() => query.refetch()}
        />
      </div>
      {query.isPending ? (
        <Loading />
      ) : query.isError ? (
        <ErrorState error={query.error} retry={() => query.refetch()} />
      ) : !query.data?.items.length ? (
        <Empty icon={Github} title="尚未添加 GitHub 账号" />
      ) : (
        <div className="table-wrap">
          <table className="data-table backup-account-table">
            <thead>
              <tr>
                <th>账号</th>
                <th>令牌掩码</th>
                <th>状态 / 到期时间</th>
                <th>验证结果</th>
                <th className="table-action-heading">操作</th>
              </tr>
            </thead>
            <tbody>
              {query.data.items.map((account) => (
                <tr key={account.id}>
                  <td>
                    <div className="backup-account-name">
                      <Github size={17} />
                      <span>
                        <b>{account.name}</b>
                        <small>@{account.login || "—"}</small>
                      </span>
                    </div>
                  </td>
                  <td className="mono">••••{account.token_suffix}</td>
                  <td>
                    <BackupStatus status={account.status} />
                    <small className="cell-sub">
                      {account.expires_at
                        ? `${formatDate(account.expires_at, true)} 到期`
                        : "未设置到期时间"}
                    </small>
                  </td>
                  <td>
                    <span className="backup-verified">
                      <ShieldCheck size={13} />
                      {account.verified_at
                        ? formatDate(account.verified_at, true)
                        : "尚未验证"}
                    </span>
                    {account.last_error && (
                      <p
                        className="backup-error-line"
                        title={account.last_error}
                      >
                        {account.last_error}
                      </p>
                    )}
                  </td>
                  <td>
                    <div className="row-actions">
                      <Button
                        variant="secondary"
                        loading={busy === account.id}
                        disabled={!configured || !!busy}
                        icon={ShieldCheck}
                        onClick={() => verify(account)}
                      >
                        验证
                      </Button>
                      <IconButton
                        icon={Pencil}
                        label={`编辑 ${account.name} 或替换令牌`}
                        disabled={!configured || !!busy}
                        onClick={() => onEdit(account)}
                      />
                      <IconButton
                        icon={account.active ? ToggleRight : ToggleLeft}
                        label={`${account.active ? "停用" : "启用"} ${account.name}`}
                        disabled={!configured || !!busy}
                        onClick={() => setToggle(account)}
                      />
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
      {toggle && (
        <Confirm
          title={`${toggle.active ? "停用" : "启用"}账号「${toggle.name}」？`}
          description={
            toggle.active
              ? "停用后，此账号关联仓库将暂停新的同步与恢复。已保存的远端备份保留。"
              : "账号启用后，可以继续验证和同步备份。"
          }
          onClose={() => setToggle(undefined)}
          onConfirm={changeActive}
          loading={busy === toggle.id}
        />
      )}
    </div>
  );
}

function AccountForm({
  account,
  onClose,
  onSaved,
}: {
  account?: GitHubAccount;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [name, setName] = useState(account?.name || "");
  const [token, setToken] = useState("");
  const [expiry, setExpiry] = useState(dateInput(account?.expires_at || null));
  const [show, setShow] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const toast = useToast();
  function close() {
    setToken("");
    setShow(false);
    onClose();
  }
  async function save(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    const secret = token.trim();
    try {
      const body = {
        name: name.trim(),
        ...(secret ? { token: secret } : {}),
        expires_at: expiry ? `${expiry}T23:59:59+08:00` : null,
      };
      if (account) await patch(`${base}/github-accounts/${account.id}`, body);
      else await post(`${base}/github-accounts`, body);
      setToken("");
      toast(account ? "GitHub 账号已更新" : "GitHub 账号已验证并保存");
      onSaved();
      close();
    } catch (e) {
      setError(safeMessage(e, secret));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Drawer
      title={account ? "编辑 GitHub 账号" : "添加 GitHub 账号"}
      subtitle={
        account
          ? `@${account.login} · 令牌末尾 ${account.token_suffix}`
          : undefined
      }
      onClose={close}
    >
      <form onSubmit={save} autoComplete="off">
        <Field label="账号名称" required>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            maxLength={100}
            required
            placeholder="例如：团队归档账号"
          />
        </Field>
        <Field
          label={account ? "替换访问令牌" : "GitHub 访问令牌"}
          required={!account}
          hint={account ? "留空保留现有令牌" : "需要目标私有仓库的读写权限"}
        >
          <div className="password-input">
            <input
              type={show ? "text" : "password"}
              value={token}
              onChange={(e) => setToken(e.target.value)}
              required={!account}
              name="github_access_token"
              autoComplete="new-password"
              spellCheck={false}
              placeholder={
                account ? "输入新令牌" : "输入 GitHub Personal Access Token"
              }
            />
            <IconButton
              icon={show ? EyeOff : Eye}
              label={show ? "隐藏访问令牌" : "显示访问令牌"}
              onClick={() => setShow(!show)}
            />
          </div>
        </Field>
        <Field label="到期日期" hint="可选，按 GitHub 令牌的实际到期日期填写">
          <input
            type="date"
            value={expiry}
            onChange={(e) => setExpiry(e.target.value)}
          />
        </Field>
        <div className="backup-token-note">
          <ShieldCheck size={17} />
          <span>保存前验证账号身份，保存后只显示令牌末 4 位。</span>
        </div>
        {error && (
          <div className="inline-error" role="alert">
            {error}
          </div>
        )}
        <div className="form-actions">
          <Button variant="secondary" onClick={close} disabled={busy}>
            取消
          </Button>
          <Button type="submit" icon={ShieldCheck} loading={busy}>
            {account ? "保存账号" : "验证并保存"}
          </Button>
        </div>
      </form>
    </Drawer>
  );
}

function EngineeringTargetGroup({
  group,
  expanded,
  configured,
  onToggle,
  onSync,
  onJobs,
  onAutoSync,
  onToggleEnabled,
}: {
  group: BackupTargetGroup;
  expanded: boolean;
  configured: boolean;
  onToggle: () => void;
  onSync: (target: BackupTarget) => void;
  onJobs: (target: BackupTarget) => void;
  onAutoSync: (target: BackupTarget) => void;
  onToggleEnabled: (target: BackupTarget) => void;
}) {
  const detail = useQuery({
    queryKey: ["backup-targets", group.engineering_id],
    queryFn: () => api<List<BackupTarget>>(`${base}/backup-targets?${params({ engineering_id: group.engineering_id, page: 1, page_size: 100 })}`),
    enabled: expanded,
    refetchInterval: expanded && configured ? 5000 : false,
  });
  return <>
    <tr className="backup-engineering-row">
      <td>
        <button className="table-expander" onClick={onToggle} aria-label={`${expanded ? "收起" : "展开"}${group.engineering_name}`}>
          {expanded ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
        </button>
        <b>{group.engineering_name}</b>
      </td>
      <td>{group.account_count}</td>
      <td>{group.repository_count}</td>
      <td>
        <span className="status-summary">{group.enabled_count} 个已启用</span>
        {group.disabled_count > 0 && <small className="cell-sub">{group.disabled_count} 个已停用</small>}
      </td>
      <td><span className="cell-sub">展开查看账号仓库</span></td>
    </tr>
    {expanded && (detail.isPending ? <tr><td colSpan={5}><Loading /></td></tr> : detail.isError ? <tr><td colSpan={5}><ErrorState error={detail.error} retry={() => detail.refetch()} /></td></tr> : detail.data?.items.map(target => <tr className="backup-account-row" key={target.id}>
      <td>
        <div className="backup-target-name nested-target"><span>{target.account_name}</span><small className="cell-sub">@{target.login}</small><GitHubLink owner={target.owner} repo={target.repo} />{target.managed && <span className="tag">自动建仓</span>}{!target.enabled && <span className="tag">已停用</span>}</div>
      </td>
      <td colSpan={2}><BinarySwitch value={target.auto_sync} label={`自动同步 ${target.owner}/${target.repo}`} disabled={!configured} onChange={() => onAutoSync(target)} /></td>
      <td><BackupStatus status={target.last_status} /><small className="cell-sub">{target.last_synced_at ? formatDate(target.last_synced_at, true) : "尚无成功同步"}</small>{target.last_error && <p className="backup-error-line" title={target.last_error}>{target.last_error}</p>}</td>
      <td><div className="row-actions"><Button variant="secondary" icon={RefreshCw} disabled={!configured || !target.enabled || ["pending", "running"].includes(target.last_status)} onClick={() => onSync(target)}>同步</Button><IconButton icon={History} label={`查看 ${target.owner}/${target.repo} 同步记录`} onClick={() => onJobs(target)} /><IconButton icon={target.enabled ? ToggleRight : ToggleLeft} label={`${target.enabled ? "停用" : "启用"}备份仓库 ${target.owner}/${target.repo}`} disabled={!configured} onClick={() => onToggleEnabled(target)} /></div></td>
    </tr>))}
  </>;
}

function TargetsTable({
  engineering,
  configured,
  onSync,
  onJobs,
  onChanged,
}: {
  engineering: string;
  configured: boolean;
  onSync: (target: BackupTarget) => void;
  onJobs: (target: BackupTarget) => void;
  onChanged: () => void;
}) {
  const [page, setPage] = useState(1);
  const [busy, setBusy] = useState("");
  const [toggle, setToggle] = useState<BackupTarget>();
  const [expanded, setExpanded] = useState<string[]>([]);
  const { q, setQ, search } = useBackupSearch();
  const [enabledFilter, setEnabledFilter] = useState("");
  const toast = useToast();
  useEffect(() => setPage(1), [engineering, search, enabledFilter]);
  const query = useQuery({
    queryKey: ["backup-target-groups", engineering, search, enabledFilter, page],
    queryFn: () =>
      api<List<BackupTargetGroup>>(
        `${base}/backup-targets?${params({ group_by: "engineering", engineering_id: engineering, q: search, enabled: enabledFilter, page, page_size: 20 })}`,
      ),
    refetchInterval: configured ? 30000 : false,
  });
  async function autoSync(target: BackupTarget) {
    setBusy(target.id);
    try {
      await patch(`${base}/backup-targets/${target.id}`, {
        auto_sync: !target.auto_sync,
      });
      onChanged();
    } catch (e) {
      toast(safeMessage(e), "error");
    } finally {
      setBusy("");
    }
  }
  async function enabled() {
    if (!toggle) return;
    setBusy(toggle.id);
    try {
      await patch(`${base}/backup-targets/${toggle.id}`, {
        enabled: !toggle.enabled,
      });
      toast(toggle.enabled ? "备份仓库已停用" : "备份仓库已启用");
      onChanged();
      setToggle(undefined);
    } catch (e) {
      toast(safeMessage(e), "error");
    } finally {
      setBusy("");
    }
  }
  return (
    <div className="backup-section">
      <div className="section-toolbar">
        <div className="section-title">
          <h2>备份仓库</h2>
          <span className="count-badge">{query.data?.total ?? "—"}</span>
        </div>
        <IconButton
          icon={RefreshCw}
          label="刷新备份仓库"
          onClick={() => query.refetch()}
        />
      </div>
      <div className="filter-bar">
        <SearchInput value={q} onChange={setQ} placeholder="搜索工程名称或仓库" />
        <select aria-label="仓库启用状态" value={enabledFilter} onChange={e => setEnabledFilter(e.target.value)}>
          <option value="">全部仓库</option><option value="true">已启用</option><option value="false">已停用</option>
        </select>
      </div>
      {query.isPending ? (
        <Loading />
      ) : query.isError ? (
        <ErrorState error={query.error} retry={() => query.refetch()} />
      ) : !query.data?.items.length ? (
        <Empty icon={DatabaseBackup} title={search || enabledFilter ? "没有符合筛选的备份仓库" : "尚未设置备份仓库"} />
      ) : (
        <div className="table-wrap">
            <table className="data-table backup-target-table">
              <thead>
                <tr>
                <th>工程</th>
                <th>账号数</th>
                <th>仓库数</th>
                <th>状态</th>
                <th className="table-action-heading">操作</th>
                </tr>
              </thead>
              <tbody>
              {query.data.items.map((group) => {
                const isOpen = expanded.includes(group.engineering_id);
                return <EngineeringTargetGroup
                  key={group.engineering_id}
                  group={group}
                  expanded={isOpen}
                  configured={configured}
                  onToggle={() => setExpanded(current => isOpen ? current.filter(id => id !== group.engineering_id) : [...current, group.engineering_id])}
                  onSync={onSync}
                  onJobs={onJobs}
                  onAutoSync={autoSync}
                  onToggleEnabled={setToggle}
                />;
              })}
              </tbody>
          </table>
        </div>
      )}
      {query.data && <Pagination
        page={page}
        total={query.data.total}
        onChange={setPage}
        allowJump
      />}
      {toggle && (
        <Confirm
          title={`${toggle.enabled ? "停用" : "启用"}备份仓库？`}
          description={
            toggle.enabled
              ? `停用 ${toggle.owner}/${toggle.repo} 后暂停同步与恢复，远端 Release 保留。`
              : `启用 ${toggle.owner}/${toggle.repo} 后可以继续同步与恢复。`
          }
          onClose={() => setToggle(undefined)}
          onConfirm={enabled}
          loading={busy === toggle.id}
        />
      )}
    </div>
  );
}

function TargetForm({
  initialEngineering,
  onClose,
  onSaved,
}: {
  initialEngineering: string;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [engineering, setEngineering] = useState(initialEngineering);
  const [engineeringName, setEngineeringName] = useState("");
  const [q, setQ] = useState("");
  const [page, setPage] = useState(1);
  const [accountID, setAccountID] = useState("");
  const [accountPage, setAccountPage] = useState(1);
  const [accountOptions, setAccountOptions] = useState<GitHubAccount[]>([]);
  const [owner, setOwner] = useState("");
  const [repo, setRepo] = useState("");
  const [autoSync, setAutoSync] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const toast = useToast();
  const query = useQuery({
    queryKey: ["backup-engineering-picker", q, page],
    queryFn: () =>
      api<List<Engineering>>(
        `${base}/engineering?${params({ q, status: "ready", page, page_size: 20 })}`,
      ),
  });
  const accounts = useQuery({
    queryKey: ["backup-account-picker", accountPage],
    queryFn: () =>
      api<GitHubAccountList>(
        `${base}/github-accounts?page=${accountPage}&page_size=100`,
      ),
  });
  const detail = useQuery({
    queryKey: ["backup-selected-engineering", initialEngineering],
    queryFn: () =>
      api<{ engineering: Engineering }>(
        `${base}/engineering/${initialEngineering}`,
      ),
    enabled: !!initialEngineering,
  });
  useEffect(() => {
    if (detail.data) setEngineeringName(detail.data.engineering.name);
  }, [detail.data]);
  useEffect(() => {
    if (accounts.data)
      setAccountOptions((previous) => [
        ...new Map(
          [...previous, ...accounts.data.items].map((account) => [
            account.id,
            account,
          ]),
        ).values(),
      ]);
  }, [accounts.data]);
  useEffect(() => setPage(1), [q]);
  async function save(e: FormEvent) {
    e.preventDefault();
    if (!engineering) {
      setError("请选择一个已分析完成的工程");
      return;
    }
    setBusy(true);
    setError("");
    try {
      await post(`${base}/backup-targets`, {
        engineering_id: engineering,
        account_id: accountID,
        owner: owner.trim(),
        repo: repo.trim(),
        auto_sync: autoSync,
      });
      toast("备份仓库已添加");
      onSaved();
      onClose();
    } catch (e) {
      setError(safeMessage(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Drawer title="添加备份仓库" onClose={onClose} wide>
      <form onSubmit={save}>
        <div className="backup-repository-requirement">
          <Github size={20} />
          <div>
            <b>已有私有仓库</b>
            <p>仓库须已初始化，并允许所选账号推送。</p>
          </div>
        </div>
        <Field label="工程" required>
          <div className="backup-engineering-selection">
            {engineering && (
              <div className="backup-selected-engineering">
                <Folder size={15} />
                <b>{engineeringName || "已选择工程"}</b>
                <IconButton
                  icon={X}
                  label="取消工程选择"
                  onClick={() => {
                    setEngineering("");
                    setEngineeringName("");
                  }}
                />
              </div>
            )}
            <SearchInput
              value={q}
              onChange={setQ}
              placeholder="搜索可备份工程"
            />
            {query.isPending ? (
              <Loading />
            ) : query.isError ? (
              <ErrorState error={query.error} retry={() => query.refetch()} />
            ) : !query.data?.items.length ? (
              <Empty icon={Folder} title="没有已分析完成的工程" />
            ) : (
              <div className="picker-list backup-engineering-picker">
                {query.data.items.map((item) => (
                  <button
                    type="button"
                    className={
                      engineeringID(item) === engineering ? "selected" : ""
                    }
                    key={engineeringID(item)}
                    onClick={() => {
                      setEngineering(engineeringID(item));
                      setEngineeringName(item.name);
                    }}
                  >
                    <span className="pseudo-check">
                      {engineeringID(item) === engineering && (
                        <Check size={13} />
                      )}
                    </span>
                    <Screenshot item={item} />
                    <span className="picker-name">
                      <b>{item.name}</b>
                      <small>
                        v{item.version_number} · {item.source_file_count}{" "}
                        个源码文件
                      </small>
                    </span>
                  </button>
                ))}
              </div>
            )}
            <Pagination
              page={page}
              total={query.data?.total || 0}
              onChange={setPage}
            />
          </div>
        </Field>
        <Field label="GitHub 账号" required>
          <select
            value={accountID}
            onChange={(e) => {
              const id = e.target.value;
              setAccountID(id);
              if (!owner)
                setOwner(
                  accountOptions.find((account) => account.id === id)?.login ||
                    "",
                );
            }}
            required
            disabled={accounts.isPending}
          >
            <option value="">选择已验证账号</option>
            {accountOptions.map((account) => (
              <option
                key={account.id}
                value={account.id}
                disabled={account.status !== "active"}
              >
                {account.name} · @{account.login}
                {account.status !== "active" ? "（不可用）" : ""}
              </option>
            ))}
          </select>
        </Field>
        {accounts.isError && (
          <ErrorState error={accounts.error} retry={() => accounts.refetch()} />
        )}
        {accounts.data && accountOptions.length < accounts.data.total && (
          <Button
            variant="ghost"
            icon={ChevronDown}
            disabled={accounts.isFetching}
            onClick={() => setAccountPage((value) => value + 1)}
          >
            加载更多账号
          </Button>
        )}
        <div className="field-row">
          <Field label="仓库所有者" required>
            <input
              value={owner}
              onChange={(e) => setOwner(e.target.value)}
              required
              maxLength={100}
              placeholder="owner 或组织名称"
              autoComplete="off"
            />
          </Field>
          <Field label="仓库名称" required>
            <input
              value={repo}
              onChange={(e) => setRepo(e.target.value)}
              required
              maxLength={100}
              placeholder="repository"
              autoComplete="off"
            />
          </Field>
        </div>
        <div className="backup-form-toggle">
          <div>
            <b>自动同步</b>
            <span>工程新版本分析完成后加入同步队列</span>
          </div>
          <BinarySwitch
            value={autoSync}
            label="自动同步新工程版本"
            onChange={() => setAutoSync(!autoSync)}
          />
        </div>
        {error && (
          <div className="inline-error" role="alert">
            {error}
          </div>
        )}
        <div className="form-actions">
          <Button variant="secondary" onClick={onClose} disabled={busy}>
            取消
          </Button>
          <Button
            icon={Plus}
            type="submit"
            loading={busy}
            disabled={!engineering || !accountID}
          >
            验证并添加
          </Button>
        </div>
      </form>
    </Drawer>
  );
}

function SyncForm({
  target,
  onClose,
  onSaved,
}: {
  target: BackupTarget;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [version, setVersion] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const toast = useToast();
  const query = useQuery({
    queryKey: ["backup-sync-versions", target.engineering_id],
    queryFn: () =>
      api<{ versions: Engineering[] }>(
        `${base}/engineering/${target.engineering_id}`,
      ),
  });
  async function sync(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const job = await post<BackupJob>(
        `${base}/backup-targets/${target.id}/sync`,
        version ? { version_id: version } : {},
      );
      toast(
        job.status === "ready"
          ? "此版本已有校验完成的备份"
          : job.status === "failed"
            ? "此版本曾同步失败，请在记录中重试"
            : "同步任务已加入队列",
      );
      onSaved();
      onClose();
    } catch (e) {
      setError(safeMessage(e));
    } finally {
      setBusy(false);
    }
  }
  const versions =
    query.data?.versions.filter((version) => version.status === "ready") || [];
  return (
    <Drawer
      title="同步工程快照"
      subtitle={target.engineering_name}
      onClose={onClose}
    >
      <form onSubmit={sync}>
        <div className="backup-sync-destination">
          <Github size={20} />
          <div>
            <GitHubLink owner={target.owner} repo={target.repo} />
            <small>{target.account_name}</small>
          </div>
        </div>
        {query.isPending ? (
          <Loading />
        ) : query.isError ? (
          <ErrorState error={query.error} retry={() => query.refetch()} />
        ) : (
          <Field label="工程版本">
            <select
              value={version}
              onChange={(e) => setVersion(e.target.value)}
              disabled={!versions.length}
            >
              <option value="">最新可用版本</option>
              {versions.map((item) => (
                <option key={item.id} value={item.id}>
                  v{item.version_number} · {formatDate(item.created_at)} ·{" "}
                  {item.source_file_count} 个文件
                </option>
              ))}
            </select>
          </Field>
        )}
        {!query.isPending && !query.isError && !versions.length && (
          <div className="notice">此工程暂时没有可用版本。</div>
        )}
        <div className="backup-token-note">
          <ShieldCheck size={17} />
          <span>同步 ZIP、全部截图与校验清单，逐文件校验后完成。</span>
        </div>
        {error && (
          <div className="inline-error" role="alert">
            {error}
          </div>
        )}
        <div className="form-actions">
          <Button variant="secondary" onClick={onClose} disabled={busy}>
            取消
          </Button>
          <Button
            type="submit"
            icon={RefreshCw}
            loading={busy}
            disabled={query.isPending || query.isError || !versions.length}
          >
            开始同步
          </Button>
        </div>
      </form>
    </Drawer>
  );
}

function RepositoryVersions({
  engineering, target, search, status, kind, configured, actions, detailID, onDetailChange,
}: {
  engineering: string;
  target: string;
  search: string;
  status: string;
  kind: string;
  configured: boolean;
  actions: (job: BackupJob) => ReactNode;
  detailID?: string;
  onDetailChange: (job: BackupJob) => void;
}) {
  const [page, setPage] = useState(1);
  const query = useQuery({
    queryKey: ["backup-jobs", "engineering-versions", engineering, target, search, status, kind, page],
    queryFn: () => api<List<BackupVersionGroup>>(`${base}/backup-jobs?${params({group_by: "version", engineering_id: engineering, target_id: target, q: search, status, kind, page, page_size: 10})}`),
    refetchInterval: query => query.state.data?.items.some(version => version.jobs.some(job => ["pending", "running"].includes(job.status))) ? 2000 : configured ? 30000 : false,
  });
  useEffect(() => {
    const job = query.data?.items.flatMap(version => version.jobs).find(item => item.id === detailID);
    if (job) onDetailChange(job);
  }, [query.data, detailID, onDetailChange]);
  return <div className="backup-repository-versions" id={`versions-${engineering}`}>
    {query.isPending ? <Loading /> : query.isError ? <ErrorState error={query.error} retry={() => query.refetch()} /> : !query.data?.items.length ? <Empty icon={History} title="暂无符合筛选的版本记录" /> :
      <div className="table-wrap">
        <table className="data-table backup-version-table">
          <thead><tr><th>版本</th><th>备份账号 / 仓库</th><th>任务</th><th>状态</th><th>校验</th><th className="table-action-heading">操作</th></tr></thead>
          <tbody>{query.data.items.flatMap(version => version.jobs.map((job,index) => <tr key={job.id} className={index === 0 ? "backup-version-start" : ""}>
            {index === 0 && <td rowSpan={version.jobs.length} className="backup-version-cell"><b className="backup-version-number">v{version.version_number}</b><small className="cell-sub">{new Set(version.jobs.map(item => item.owner.toLowerCase())).size} 个账号</small></td>}
            <td><b className="backup-version-account">{job.account_name}</b><small className="cell-sub"><GitHubLink owner={job.owner} repo={job.repo} /></small></td>
            <td><span className="backup-kind">{job.kind === "restore" ? <ArchiveRestore size={14} /> : <DatabaseBackup size={14} />}{job.kind === "restore" ? "恢复" : "同步"}</span></td>
            <td><BackupStatus status={job.status} kind={job.kind} />{job.error && <p className="backup-error-line" title={job.error}>{job.error}</p>}</td>
            <td>{job.verified_at ? <span className="backup-verified"><ShieldCheck size={13} />{formatDate(job.verified_at, true)}</span> : <span className="muted">待校验</span>}<small className="cell-sub mono">{job.zip_sha256 ? `${job.zip_sha256.slice(0,12)}…` : "—"}</small></td>
            <td>{actions(job)}</td>
          </tr>))}</tbody>
        </table>
      </div>}
    {query.data && <Pagination page={page} size={10} total={query.data.total} onChange={setPage} allowJump unit="个版本" />}
  </div>;
}

function JobsTable({
  engineering,
  target,
  configured,
  onChanged,
}: {
  engineering: string;
  target: string;
  configured: boolean;
  onChanged: () => void;
}) {
  const [page, setPage] = useState(1);
  const [detail, setDetail] = useState<BackupJob>();
  const [restore, setRestore] = useState<BackupJob>();
  const [busy, setBusy] = useState("");
  const { q, setQ, search } = useBackupSearch();
  const [statusFilter, setStatusFilter] = useState("");
  const [kindFilter, setKindFilter] = useState("");
  const [expanded, setExpanded] = useState("");
  const toast = useToast();
  useEffect(() => { setPage(1); setExpanded(""); }, [engineering, target, search, statusFilter, kindFilter]);
  useEffect(() => setExpanded(""), [page]);
  const query = useQuery({
    queryKey: ["backup-jobs", "engineering-groups", engineering, target, search, statusFilter, kindFilter, page],
    queryFn: () =>
      api<List<BackupRepositoryGroup>>(
        `${base}/backup-jobs?${params({ group_by: "engineering", engineering_id: engineering, target_id: target, q: search, status: statusFilter, kind: kindFilter, page, page_size: 20 })}`,
      ),
    refetchInterval: (query) =>
      query.state.data?.items.some((group) =>
        group.pending_count > 0 || group.running_count > 0,
      )
        ? 2000
        : configured ? 30000 : false,
  });
  async function retry(job: BackupJob) {
    setBusy(job.id);
    try {
      await post(`${base}/backup-jobs/${job.id}/retry`);
      toast("重试任务已加入队列");
      onChanged();
    } catch (e) {
      toast(safeMessage(e), "error");
    } finally {
      setBusy("");
    }
  }
  async function restoreFiles() {
    if (!restore) return;
    setBusy(restore.id);
    try {
      await post(`${base}/backup-jobs/${restore.id}/restore`);
      toast("恢复任务已加入队列");
      setRestore(undefined);
      onChanged();
    } catch (e) {
      toast(safeMessage(e), "error");
    } finally {
      setBusy("");
    }
  }
  const actions = (job: BackupJob) => (
    <div className="row-actions">
      {job.status === "failed" && (
        <Button
          variant="secondary"
          icon={RotateCw}
          loading={busy === job.id}
          disabled={!configured || !!busy}
          onClick={() => retry(job)}
        >
          重试
        </Button>
      )}
      {job.kind === "sync" && job.status === "ready" && job.verified_at && (
        <Button
          variant="secondary"
          icon={ArchiveRestore}
          disabled={!configured || !!busy}
          onClick={() => setRestore(job)}
        >
          恢复本地
        </Button>
      )}
      {job.release_url && (
        <a
          className="icon-button"
          title="打开 GitHub Release"
          aria-label="打开 GitHub Release"
          href={job.release_url}
          target="_blank"
          rel="noreferrer"
        >
          <ArrowUpRight size={17} />
        </a>
      )}
      <IconButton
        icon={History}
        label={`查看 ${job.engineering_name} v${job.version_number} 任务详情`}
        onClick={() => setDetail(job)}
      />
    </div>
  );
  const currentJob = detail;
  return (
    <div className="backup-section">
      <div className="section-toolbar">
        <div className="section-title">
          <h2>工程备份记录</h2>
          <span className="count-badge">{query.data?.total ?? "—"}</span>
        </div>
      </div>
      <div className="filter-bar">
        <SearchInput value={q} onChange={setQ} placeholder="搜索工程名称或仓库" />
        <select aria-label="备份任务状态" value={statusFilter} onChange={e => setStatusFilter(e.target.value)}>
          <option value="">全部状态</option><option value="pending">排队中</option><option value="running">处理中</option><option value="ready">已校验</option><option value="failed">失败</option>
        </select>
        <select aria-label="备份任务类型" value={kindFilter} onChange={e => setKindFilter(e.target.value)}>
          <option value="">全部任务</option><option value="sync">同步</option><option value="restore">恢复</option>
        </select>
      </div>
      {query.isPending ? (
        <Loading />
      ) : query.isError ? (
        <ErrorState error={query.error} retry={() => query.refetch()} />
      ) : !query.data?.items.length ? (
        <Empty icon={History} title={search || statusFilter || kindFilter ? "没有符合筛选的同步记录" : "尚无同步记录"} />
      ) : (
        <div className="backup-repositories">
          <div className="backup-repository-columns" aria-hidden="true"><span>工程 / 灾备副本</span><span>版本 / 记录</span><span>任务状态</span><span>最近记录</span></div>
          {query.data.items.map(group => {
            const groupID = group.engineering_id;
            const open = expanded === groupID;
            return <section className={`backup-repository-group ${open ? "expanded" : ""}`} key={groupID}>
              <div className="backup-repository-row">
                <div className="backup-repository-identity">
                  <button className="backup-repository-toggle" aria-label={`${open ? "收起" : "展开"} ${group.engineering_name} 的版本`} aria-expanded={open} aria-controls={`versions-${groupID}`} title={open ? "收起版本" : "展开版本"} onClick={() => setExpanded(open ? "" : groupID)}>
                    {open ? <ChevronDown size={17} /> : <ChevronRight size={17} />}
                    <span><b>{group.engineering_name}{group.engineering_count > 1 && <small> 等 {group.engineering_count} 个工程</small>}</b></span>
                  </button>
                  <small className="backup-engineering-copies">{group.account_count} 个账号 · {group.repository_count} 个仓库</small>
                </div>
                <div className="backup-repository-count"><b>{group.version_count} 个版本</b><small>{group.job_count} 条记录</small></div>
                <div className="backup-repository-states">
                  {group.ready_count > 0 && <span className="backup-summary-ready">完成 {group.ready_count}</span>}
                  {group.failed_count > 0 && <span className="backup-summary-failed">失败 {group.failed_count}</span>}
                  {group.running_count > 0 && <span className="backup-summary-running">处理中 {group.running_count}</span>}
                  {group.pending_count > 0 && <span className="muted">排队 {group.pending_count}</span>}
                </div>
                <time className="backup-repository-date">{formatDate(group.latest_at,true)}</time>
              </div>
              {open && <RepositoryVersions key={`${groupID}-${search}-${statusFilter}-${kindFilter}`} engineering={groupID} target={target} search={search} status={statusFilter} kind={kindFilter} configured={configured} actions={actions} detailID={detail?.id} onDetailChange={setDetail} />}
            </section>;
          })}
        </div>
      )}
      {query.data && <Pagination
        page={page}
        total={query.data.total}
        onChange={setPage}
        allowJump
        unit="个工程"
      />}
      {restore && (
        <Confirm
          title={`恢复「${restore.engineering_name}」v${restore.version_number}？`}
          description="按备份校验恢复此版本 ZIP 与截图，当前项目引用版本保持不变。"
          onClose={() => setRestore(undefined)}
          onConfirm={restoreFiles}
          loading={busy === restore.id}
        />
      )}
      {currentJob && (
        <Drawer
          title="备份任务详情"
          subtitle={`${currentJob.engineering_name} · v${currentJob.version_number}`}
          onClose={() => setDetail(undefined)}
        >
          <div className="backup-job-detail-status">
            <BackupStatus
              status={currentJob.status}
              kind={currentJob.kind}
            />
            <span className="tag">
              {currentJob.kind === "restore" ? "恢复" : "同步"}
            </span>
          </div>
          <dl className="detail-list backup-job-detail">
            <dt>GitHub 仓库</dt>
            <dd>
              <GitHubLink owner={currentJob.owner} repo={currentJob.repo} />
            </dd>
            <dt>使用账号</dt>
            <dd>{currentJob.account_name}</dd>
            <dt>版本</dt>
            <dd>v{currentJob.version_number}</dd>
            <dt>任务 ID</dt>
            <dd className="mono hash">{currentJob.id}</dd>
            <dt>创建时间</dt>
            <dd>{formatDate(currentJob.created_at, true)}</dd>
            <dt>完成时间</dt>
            <dd>{formatDate(currentJob.finished_at || undefined, true)}</dd>
            <dt>校验时间</dt>
            <dd>{formatDate(currentJob.verified_at || undefined, true)}</dd>
            <dt>ZIP SHA-256</dt>
            <dd className="mono hash">{currentJob.zip_sha256 || "—"}</dd>
            <dt>Release</dt>
            <dd>
              {currentJob.release_url ? (
                <a
                  className="backup-repo-link"
                  href={currentJob.release_url}
                  target="_blank"
                  rel="noreferrer"
                >
                  查看 Release
                  <ExternalLink size={12} />
                </a>
              ) : (
                "—"
              )}
            </dd>
          </dl>
          {currentJob.error && (
            <div className="inline-error" role="alert">
              {currentJob.error}
            </div>
          )}
          <div className="form-actions">
            {actions(currentJob)}
          </div>
        </Drawer>
      )}
    </div>
  );
}
