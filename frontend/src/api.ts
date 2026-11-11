export interface User {
  id: string;
  username: string;
  name: string;
  role: "admin" | "member";
  active: boolean;
  created_at: string;
}
export interface Language {
  name: string;
  code: number;
  files: number;
  percentage: number;
}
export interface Asset {
  id: string;
  kind: string;
  url: string;
}
export interface Engineering {
  id: string;
  engineering_id?: string;
  version_id?: string;
  name: string;
  description: string;
  archived?: boolean;
  version_number: number;
  tech_stack: string[];
  status: string;
  source_file_count: number;
  total_file_count: number;
  languages: Language[];
  zip_sha256?: string;
  zip_size?: number;
  github_url?: string;
  github_ref?: string;
  created_at: string;
  assets: Asset[];
  tags: EngineeringTag[];
  claimed_by?: string;
  claimed_name?: string;
  claim_id?: string;
  claimed_at?: string;
  can_download?: boolean;
}
export interface Tag {
  id: string;
  name: string;
  parent_id: string | null;
  sort_order: number;
  engineering_count: number;
  children: Tag[];
}
export interface EngineeringTag {
  id: string;
  name: string;
  parent_id: string | null;
  parent_name: string;
}
export interface Project {
  id: string;
  name: string;
  description: string;
  start_at: string;
  end_at: string;
  tech_stack: string[];
  min_files: number;
  max_files: number;
  status: string;
  created_at: string;
  engineering_count: number;
  claimed_count: number;
  member_count: number;
}
export interface Token {
  id: string;
  name: string;
  prefix: string;
  created_at: string;
  last_used_at?: string;
  revoked_at?: string;
  scope: string;
}
export interface AuditEvent {
  id: string;
  actor_name: string;
  action: string;
  target_type: string;
  target_id: string;
  project_id?: string;
  engineering_id?: string;
  details: unknown;
  created_at: string;
}
export interface List<T> {
  items: T[];
  total: number;
}
export interface Overview {
  engineering_count: number;
  ready_count: number;
  pending_count: number;
  project_count: number;
  active_project_count: number;
  claim_count: number;
  user_count: number;
  recent_events: AuditEvent[];
}
export interface GitHubAccount {
  id: string;
  name: string;
  login: string;
  token_suffix: string;
  active: boolean;
  expires_at: string | null;
  verified_at: string | null;
  last_error: string;
  status: 'active' | 'disabled' | 'expired' | 'error';
  created_at: string;
  updated_at: string;
}
export interface GitHubAccountList extends List<GitHubAccount> { configured: boolean; usable_total: number }
export interface BackupTarget {
  id: string;
  engineering_id: string;
  engineering_name: string;
  account_id: string;
  account_name: string;
  login: string;
  owner: string;
  repo: string;
  auto_sync: boolean;
  managed: boolean;
  enabled: boolean;
  last_status: string;
  last_error: string;
  last_synced_at: string | null;
  created_at: string;
  updated_at: string;
}
export interface BackupTargetGroup {
  engineering_id: string;
  engineering_name: string;
  account_count: number;
  repository_count: number;
  enabled_count: number;
  disabled_count: number;
}
export interface BackupJob {
  id: string;
  target_id: string;
  version_id: string;
  version_number: number;
  engineering_id: string;
  engineering_name: string;
  account_name: string;
  owner: string;
  repo: string;
  kind: 'sync' | 'restore';
  status: 'pending' | 'running' | 'ready' | 'failed';
  error: string;
  release_url: string;
  zip_sha256: string;
  verified_at: string | null;
  created_at: string;
  finished_at: string | null;
}
export interface BackupRepositoryGroup {
  engineering_id: string;
  repository_count: number;
  account_count: number;
  owner: string;
  repo: string;
  engineering_name: string;
  engineering_count: number;
  version_count: number;
  job_count: number;
  ready_count: number;
  failed_count: number;
  pending_count: number;
  running_count: number;
  latest_at: string;
}
export interface BackupVersionGroup {
  version_id: string;
  version_number: number;
  jobs: BackupJob[];
}

let csrf = "";
export function setCSRF(value: string) {
  csrf = value;
}
export class ApiError extends Error {
  status: number;
  constructor(message: string, status: number) {
    super(message);
    this.status = status;
  }
}
export async function api<T>(
  path: string,
  options: RequestInit = {},
): Promise<T> {
  const headers = new Headers(options.headers);
  if (options.body && !(options.body instanceof FormData))
    headers.set("Content-Type", "application/json");
  if (options.method && !["GET", "HEAD"].includes(options.method))
    headers.set("X-CSRF-Token", csrf);
  const response = await fetch(path, {
    ...options,
    headers,
    credentials: "include",
  });
  if (!response.ok) {
    const error = await response
      .json()
      .catch(() => ({ error: `请求失败 (${response.status})` }));
    if (response.status === 401 && !path.includes("/auth/"))
      window.dispatchEvent(new Event("session-expired"));
    throw new ApiError(error.error || "请求失败", response.status);
  }
  if (response.status === 204) {
    // 消费掉空响应体，避免浏览器把未读取的 204 标记成 net::ERR_ABORTED。
    await response.text();
    return undefined as T;
  }
  const text = await response.text();
  return text ? JSON.parse(text) : (undefined as T);
}
export const post = <T>(path: string, body?: unknown) =>
  api<T>(path, {
    method: "POST",
    body: body instanceof FormData ? body : JSON.stringify(body ?? {}),
  });
export const patch = <T>(path: string, body: unknown) =>
  api<T>(path, { method: "PATCH", body: JSON.stringify(body) });
export function params(
  values: Record<string, string | number | string[] | undefined>,
) {
  const p = new URLSearchParams();
  Object.entries(values).forEach(([key, value]) => {
    if (value === undefined || value === "") return;
    if (Array.isArray(value)) {
      value.forEach((item) => {
        if (item !== "") p.append(key, item);
      });
      return;
    }
    p.set(key, String(value));
  });
  return p.toString();
}
export function formatDate(value?: string, time = false) {
  if (!value) return "—";
  return new Intl.DateTimeFormat("zh-CN", {
    timeZone: 'Asia/Shanghai',
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    ...(time ? { hour: "2-digit", minute: "2-digit" } : {}),
  }).format(new Date(value));
}
export function formatSize(bytes = 0) {
  return bytes > 1_048_576
    ? `${(bytes / 1_048_576).toFixed(1)} MB`
    : `${Math.round(bytes / 1024)} KB`;
}
export const languages = [
  "Go",
  "Java",
  "Python",
  "JavaScript",
  "TypeScript",
  "Rust",
  "C",
  "C++",
  "C#",
  "PHP",
  "Ruby",
  "Swift",
  "Kotlin",
  "HTML",
  "CSS",
  "Vue",
  "Shell",
];
export const languageColor: Record<string, string> = {
  Go: "#57a4b6",
  Java: "#b06c51",
  Python: "#caa448",
  JavaScript: "#d6b83c",
  TypeScript: "#578ba8",
  Rust: "#ae7060",
  HTML: "#ce785b",
  CSS: "#937dba",
  Vue: "#54866b",
  Shell: "#8a9a71",
};
export function actionName(action: string) {
  const dict: Record<string, string> = {
    "engineering.version.upload": "上传工程版本",
    "engineering.analysis.retry": "重试工程分析",
    "engineering.update": "更新工程",
    "engineering.archive": "归档工程",
    "project.create": "创建项目",
    "project.update": "更新项目",
    "project.scope": "调整工程范围",
    "project.members": "调整项目成员",
    "tag.create": "创建标签",
    "tag.update": "更新标签",
    "tag.delete": "删除标签",
    "claim.create": "领取工程",
    "claim.release": "归还工程",
    "claim.reclaim": "回收工程",
    "claim.transfer": "转交工程",
    "user.create": "创建用户",
    "user.update": "更新用户",
    "token.create": "创建令牌",
    "token.revoke": "撤销令牌",
    "auth.login": "登录",
    "password.change": "修改密码",
    "github.account.create": "创建 GitHub 账号",
    "github.account.update": "更新 GitHub 账号",
    "github.account.verify": "验证 GitHub 账号",
    "backup.target.create": "添加备份仓库",
    "backup.target.update": "更新备份仓库",
    "backup.sync.enqueue": "发起工程同步",
    "backup.restore.enqueue": "发起本地恢复",
    "backup.retry": "重试备份任务",
    "backup.sync.ready": "工程同步完成",
    "backup.sync.failed": "工程同步失败",
    "backup.restore.ready": "本地恢复完成",
    "backup.restore.failed": "本地恢复失败",
  };
  return dict[action] || action;
}
