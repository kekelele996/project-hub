import { useEffect, useState, type FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from 'react-router-dom';
import {
  Archive,
  DatabaseBackup,
  ArrowUpRight,
  Boxes,
  Check,
  ChevronRight,
  Download,
  ExternalLink,
  FileArchive,
  Folder,
  Grid2X2,
  Image,
  List as ListIcon,
  Pencil,
  Plus,
  RotateCw,
  Upload,
  X,
} from "lucide-react";
import {
  api,
  formatDate,
  formatSize,
  languages,
  params,
  patch,
  post,
  type Engineering,
  type GitHubAccountList,
  type List,
} from "../api";
import {
  Button,
  CheckBox,
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
  Segmented,
  Status,
  Tabs,
  TechPicker,
  useToast,
} from "../ui";
import { TagChips, TagFilter, TagPicker, useTags } from "../tags";

export function engineeringID(item: Engineering) {
  return item.engineering_id || item.id;
}
export function versionID(item: Engineering) {
  return item.version_id || item.id;
}
export function EngineeringFilters({
  q,
  setQ,
  language,
  setLanguage,
  min,
  setMin,
  max,
  setMax,
  children,
}: {
  q: string;
  setQ: (v: string) => void;
  language: string;
  setLanguage: (v: string) => void;
  min: string;
  setMin: (v: string) => void;
  max: string;
  setMax: (v: string) => void;
  children?: React.ReactNode;
}) {
  return (
    <div className="filter-bar">
      <SearchInput value={q} onChange={setQ} placeholder="搜索工程名称、描述" />
      <select
        aria-label="工程语言"
        value={language}
        onChange={(e) => setLanguage(e.target.value)}
      >
        <option value="">全部语言</option>
        {languages.map((l) => (
          <option key={l}>{l}</option>
        ))}
      </select>
      <div className="range-filter">
        <input
          type="number"
          min="0"
          placeholder="最少文件"
          aria-label="最少文件数"
          value={min}
          onChange={(e) => setMin(e.target.value)}
        />
        <span>—</span>
        <input
          type="number"
          min="0"
          placeholder="最多文件"
          aria-label="最多文件数"
          value={max}
          onChange={(e) => setMax(e.target.value)}
        />
      </div>
      {children}
    </div>
  );
}
export function EngineeringPage() {
  const client = useQueryClient();
  const toast = useToast();
  const [q, setQ] = useState("");
  const [language, setLanguage] = useState("");
  const [min, setMin] = useState("");
  const [max, setMax] = useState("");
  const [status, setStatus] = useState("");
  const [archived, setArchived] = useState('false');
  const [tagIDs, setTagIDs] = useState<string[]>([]);
  const [page, setPage] = useState(1);
  const [view, setView] = useState(
    localStorage.getItem("engineering-view") || "list",
  );
  const [upload, setUpload] = useState<Engineering | boolean>(false);
  const [detail, setDetail] = useState<string>();
  const [selected, setSelected] = useState<string[]>([]);
  const [archive, setArchive] = useState(false);
  const [busy, setBusy] = useState(false);
  const tagsQuery = useTags();
  useEffect(() => {
    setPage(1);
    setSelected([]);
  }, [q, language, min, max, status, archived, tagIDs]);
  const query = useQuery({
    queryKey: [
      "engineering",
      q,
      language,
      min,
      max,
      status,
      archived,
      tagIDs,
      page,
    ],
    queryFn: () =>
      api<List<Engineering>>(
        `/api/v1/admin/engineering?${params({ q, language, min_files: min, max_files: max, status, archived, tag_ids: tagIDs, page, page_size: 20 })}`,
      ),
    refetchInterval: (data) =>
      data.state.data?.items.some((e) => e.status === "pending") ? 2500 : false,
  });
  const items = query.data?.items || [];
  const filtered = Boolean(
    q || language || min || max || status || archived !== "false" || tagIDs.length,
  );
  const toggle = (id: string) =>
    setSelected((s) =>
      s.includes(id) ? s.filter((v) => v !== id) : [...s, id],
    );
  async function archiveSelected() {
    setBusy(true);
    try {
      await Promise.all(
        selected.map((id) =>
          patch(`/api/v1/admin/engineering/${id}`, { archived: archived !== 'true' }),
        ),
      );
      client.invalidateQueries({ queryKey: ["engineering"] });
      client.invalidateQueries({ queryKey: ["overview"] });
      setSelected([]);
      setArchive(false);
      toast(archived === 'true' ? '工程已恢复' : '工程已归档');
    } catch (e) {
      toast(e instanceof Error ? e.message : "归档失败", "error");
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <PageHeader
        eyebrow="CODE ASSETS"
        title="工程池"
        subtitle="团队的代码资产，每一份工程都可追溯。"
      >
        <Button icon={Upload} onClick={() => setUpload(true)}>
          上传工程
        </Button>
      </PageHeader>
      <div className="section-toolbar">
        <div className="section-title">
          <h2>{archived === 'true' ? '归档工程' : '工程目录'}</h2>
          <span className="count-badge">{query.data?.total ?? "—"}</span>
        </div>
        <div className="toolbar-right">
          <IconButton
            icon={RotateCw}
            label="刷新工程"
            onClick={() => query.refetch()}
          />
          <Segmented
            value={view}
            onChange={(v) => {
              setView(v);
              localStorage.setItem("engineering-view", v);
            }}
            items={[
              { value: "list", icon: ListIcon, label: "列表视图" },
              { value: "grid", icon: Grid2X2, label: "截图视图" },
            ]}
          />
        </div>
      </div>
      <EngineeringFilters
        {...{ q, setQ, language, setLanguage, min, setMin, max, setMax }}
      >
        <select
          value={status}
          aria-label="分析状态"
          onChange={(e) => setStatus(e.target.value)}
        >
          <option value="">全部状态</option>
          <option value="ready">可用</option>
          <option value="pending">分析中</option>
          <option value="failed">分析失败</option>
        </select>
        <select value={archived} aria-label="归档状态" onChange={e => setArchived(e.target.value)}><option value="false">未归档</option><option value="true">已归档</option><option value="all">全部工程</option></select>
      </EngineeringFilters>
      <TagFilter
        tree={tagsQuery.data?.items || []}
        value={tagIDs}
        onChange={setTagIDs}
      />
      {selected.length > 0 && (
        <div className="selection-bar">
          <span>
            <Check size={15} />
            已选择 {selected.length} 个工程
          </span>
          <div>
            <Button variant="ghost" onClick={() => setSelected([])}>
              取消选择
            </Button>
            <Button
              variant="secondary"
              icon={Archive}
              onClick={() => setArchive(true)}
            >
              {archived === 'true' ? '批量恢复' : '批量归档'}
            </Button>
          </div>
        </div>
      )}
      {query.isPending ? (
        <Loading />
      ) : query.isError ? (
        <ErrorState error={query.error} retry={() => query.refetch()} />
      ) : !items.length ? (
        <Empty
          icon={Boxes}
          title={filtered ? "没有符合筛选的工程" : "工程池还没有工程"}
          action={
            !filtered ? (
              <Button icon={Plus} onClick={() => setUpload(true)}>
                上传第一份工程
              </Button>
            ) : (
              <Button
                variant="secondary"
                onClick={() => {
                  setQ("");
                  setLanguage("");
                  setMin("");
                  setMax("");
                  setStatus("");
                  setArchived('false');
                  setTagIDs([]);
                }}
              >
                清空筛选
              </Button>
            )
          }
        />
      ) : view === "grid" ? (
        <div className="engineering-grid">
          {items.map((item) => (
            <EngineeringCard
              key={engineeringID(item)}
              item={item}
              onClick={() => setDetail(engineeringID(item))}
              selected={selected.includes(engineeringID(item))}
              onSelect={() => toggle(engineeringID(item))}
            />
          ))}
        </div>
      ) : (
        <div className="table-wrap">
          <table className="data-table engineering-table">
            <thead>
              <tr>
                <th className="select-col">
                  <CheckBox
                    checked={
                      items.length > 0 &&
                      items.every((i) => selected.includes(engineeringID(i)))
                    }
                    onChange={() =>
                      setSelected(
                        items.every((i) => selected.includes(engineeringID(i)))
                          ? []
                          : items.map(engineeringID),
                      )
                    }
                    label="选择本页全部工程"
                  />
                </th>
                <th>工程</th>
                <th>语言构成</th>
                <th>源码文件</th>
                <th>版本 / 状态</th>
                <th>更新时间</th>
                <th className="action-col" />
              </tr>
            </thead>
            <tbody>
              {items.map((item) => (
                <tr
                  key={engineeringID(item)}
                  className={
                    selected.includes(engineeringID(item)) ? "selected-row" : ""
                  }
                >
                  <td>
                    <CheckBox
                      checked={selected.includes(engineeringID(item))}
                      onChange={() => toggle(engineeringID(item))}
                      label={`选择 ${item.name}`}
                    />
                  </td>
                  <td>
                    <button
                      className="engineering-cell"
                      onClick={() => setDetail(engineeringID(item))}
                    >
                      <Screenshot item={item} />
                      <span>
                        <b>{item.name}</b>
                        <small>{item.description || "暂无描述"}</small>
                        <TagChips tags={item.tags} max={4} />
                      </span>
                    </button>
                  </td>
                  <td>
                    <LanguageBar values={item.languages} compact />
                  </td>
                  <td className="numeric">
                    {(item.source_file_count || 0).toLocaleString()}
                    <small className="cell-sub">
                      共 {item.total_file_count || 0} 个文件
                    </small>
                  </td>
                  <td>
                    <span className="version-label">
                      v{item.version_number || 1}
                    </span>
                    <Status status={item.status} />
                  </td>
                  <td className="muted nowrap">
                    {formatDate(item.created_at)}
                  </td>
                  <td>
                    <IconButton
                      icon={ChevronRight}
                      label={`查看 ${item.name}`}
                      onClick={() => setDetail(engineeringID(item))}
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
      {upload && (
        <EngineeringUpload
          existing={typeof upload === "object" ? upload : undefined}
          onClose={() => setUpload(false)}
        />
      )}{" "}
      {detail && (
        <EngineeringDetail
          id={detail}
          onClose={() => setDetail(undefined)}
          onUpload={(item) => {
            setDetail(undefined);
            setUpload(item);
          }}
        />
      )}{" "}
      {archive && (
        <Confirm
          title={archived === 'true' ? '恢复选中的工程？' : '归档选中的工程？'}
          description={archived === 'true' ? `将恢复 ${selected.length} 个工程。` : `将归档 ${selected.length} 个工程，已加入项目的版本仍然保留。`}
          onClose={() => setArchive(false)}
          onConfirm={archiveSelected}
          loading={busy}
        />
      )}
    </>
  );
}
export function Screenshot({
  item,
  large = false,
}: {
  item: Engineering;
  large?: boolean;
}) {
  const asset =
    item.assets?.find((a) => a.kind === "startup") || item.assets?.[0];
  return (
    <span className={`screenshot ${large ? "large" : ""}`}>
      {asset ? (
        <img
          src={asset.url || `/api/v1/assets/${asset.id}`}
          alt={`${item.name} 启动截图`}
          loading="lazy"
        />
      ) : (
        <Folder size={large ? 32 : 20} strokeWidth={1.5} />
      )}
    </span>
  );
}
export function EngineeringCard({
  item,
  onClick,
  onSelect,
  selected,
}: {
  item: Engineering;
  onClick: () => void;
  onSelect?: () => void;
  selected?: boolean;
}) {
  return (
    <article className={`engineering-card ${selected ? "selected" : ""}`}>
      <button className="card-preview" onClick={onClick}>
        <Screenshot item={item} large />
        <span className="preview-open">
          <ArrowUpRight size={18} />
        </span>
      </button>
      {onSelect && (
        <div className="card-check">
          <CheckBox
            checked={!!selected}
            onChange={onSelect}
            label={`选择 ${item.name}`}
          />
        </div>
      )}
      <div className="engineering-card-content">
        <div className="card-title-row">
          <button onClick={onClick}>
            <h3>{item.name}</h3>
          </button>
          <Status status={item.status} />
        </div>
        <p>{item.description || "暂无描述"}</p>
        <TagChips tags={item.tags} max={3} />
        <LanguageBar values={item.languages} compact />
        <div className="card-meta">
          <span>
            <Folder size={14} />
            {item.source_file_count || 0} 个源码文件
          </span>
          <span>v{item.version_number || 1}</span>
        </div>
      </div>
    </article>
  );
}
export function EngineeringUpload({
  existing,
  onClose,
}: {
  existing?: Engineering;
  onClose: () => void;
}) {
  const client = useQueryClient();
  const toast = useToast();
  const [name, setName] = useState(existing?.name || "");
  const [description, setDescription] = useState(existing?.description || "");
  const [tech, setTech] = useState<string[]>(existing?.tech_stack || []);
  const [tagIDs, setTagIDs] = useState<string[]>(
    existing?.tags?.map((tag) => tag.id) || [],
  );
  const [github, setGithub] = useState(existing?.github_url || "");
  const [ref, setRef] = useState(existing?.github_ref || "");
  const [backupRepoName, setBackupRepoName] = useState("engineering");
  const tagsQuery = useTags();
  const backupAccounts = useQuery({
    queryKey: ["github-accounts", "upload"],
    queryFn: () => api<GitHubAccountList>("/api/v1/admin/github-accounts?page_size=1"),
  });
  const activeAccountCount = backupAccounts.data?.usable_total || 0;
  const backupReady = backupAccounts.data?.configured && activeAccountCount > 0 &&
    (existing || (/^[a-z0-9][a-z0-9-]{1,46}[a-z0-9]$/.test(backupRepoName) && !backupRepoName.includes("--")));
  const [zip, setZip] = useState<File>();
  const [startup, setStartup] = useState<File>();
  const [features, setFeatures] = useState<File[]>([]);
  const [busy, setBusy] = useState(false);
  const [job, setJob] = useState<string>();
  const analysis = useQuery({
    queryKey: ["analysis-job", job],
    queryFn: () =>
      api<{ status: string; error?: string }>(
        `/api/v1/admin/analysis-jobs/${job}`,
      ),
    enabled: !!job,
    refetchInterval: (query) =>
      query.state.data?.status === "pending" || !query.state.data
        ? 1500
        : false,
  });
  useEffect(() => {
    if (analysis.data?.status === "ready") {
      client.invalidateQueries({ queryKey: ["engineering"] });
      client.invalidateQueries({ queryKey: ["overview"] });
      client.invalidateQueries({ queryKey: ["backup-targets"] });
      client.invalidateQueries({ queryKey: ["backup-jobs"] });
      toast("工程分析完成，灾备已排队，可在 GitHub 灾备查看进度");
      onClose();
    }
  }, [analysis.data?.status]);
  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!zip || !startup) return;
    if (!backupReady) { toast("请先配置可用的自动灾备仓库", "error"); return; }
    if (features.length < 1 || features.length > 6) { toast('请选择 1 至 6 张主要功能截图', 'error'); return; }
    setBusy(true);
    try {
      const form = new FormData();
      form.append("name", name);
      form.append("description", description);
      form.append("tech_stack", JSON.stringify(tech));
      form.append("github_url", github);
      form.append("github_ref", ref);
      if (!existing) {
        form.append("backup_repo_name", backupRepoName);
        form.append("tag_ids", JSON.stringify(tagIDs));
      }
      if (existing) form.append("engineering_id", engineeringID(existing));
      form.append("zip", zip);
      form.append("startup_screenshot", startup);
      features.forEach((f) => form.append("feature_screenshots", f));
      const result = await post<{ job_id: string }>(
        "/api/v1/admin/engineering-versions",
        form,
      );
      client.invalidateQueries({ queryKey: ["engineering"] });
      client.invalidateQueries({ queryKey: ["overview"] });
      client.invalidateQueries({ queryKey: ["tags"] });
      setJob(result.job_id);
      toast("上传成功，正在分析代码");
    } catch (err) {
      toast(err instanceof Error ? err.message : "上传失败", "error");
    } finally {
      setBusy(false);
    }
  }
  return (
    <Drawer
      title={existing ? "上传新版本" : "上传工程"}
      subtitle={existing ? existing.name : "保存代码快照与真实截图"}
      onClose={onClose}
    >
      <form onSubmit={submit}>
        <Field label="工程名称" required>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            maxLength={120}
            required
          />
        </Field>
        <Field label="工程描述">
          <textarea
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            rows={3}
            maxLength={2000}
          />
        </Field>
        <Field label="技术栈">
          <TechPicker value={tech} onChange={setTech} options={languages} />
        </Field>
        {!existing && (
          <Field
            label="所属领域标签"
            hint="按所属领域选择子方向，可多选；不选择表示暂无标签"
          >
            {tagsQuery.isPending ? (
              <span className="field-hint">正在加载标签…</span>
            ) : tagsQuery.isError ? (
              <span className="field-hint">
                标签加载失败：{tagsQuery.error.message}
              </span>
            ) : (
              <TagPicker
                tree={tagsQuery.data?.items || []}
                value={tagIDs}
                onChange={setTagIDs}
              />
            )}
          </Field>
        )}
        <div className="form-section-title">自动灾备</div>
        {backupAccounts.isPending ? <Loading /> :
          backupAccounts.isError ? <ErrorState error={backupAccounts.error} retry={() => backupAccounts.refetch()} /> :
          !backupAccounts.data?.configured ? <div className="inline-error" role="alert">请先配置灾备加密密钥，再上传工程。</div> :
          activeAccountCount === 0 ? <div className="inline-error" role="alert">灾备配置中尚无已启用且未到期的账号。</div> : <>
            <p className="muted">{activeAccountCount} 个已启用灾备账号 · 全部自动备份</p>
            {!existing && <>
            <Field label="灾备仓库名" required hint="3～48 位小写字母、数字、单个连字符；首尾为字母或数字">
              <input value={backupRepoName} onChange={e => setBackupRepoName(e.target.value)}
                placeholder="例如 elderly-care" minLength={3} maxLength={48}
                pattern="[a-z0-9][a-z0-9\-]{1,46}[a-z0-9]" required autoComplete="off" />
            </Field>
            <p className="muted">仓库格式：ph-{backupRepoName || "仓库名"}-工程ID</p>
            </>}
            {existing && <p className="muted">新版本沿用已绑定的灾备仓库，新增账号自动建立副本。</p>}
          </>}
        <p><Link to={existing ? `/backups?engineering_id=${engineeringID(existing)}` : "/backups"} onClick={onClose}>管理灾备账号与同步记录</Link></p>
        <div className="form-section-title">代码来源</div>
        <Field label="GitHub 仓库地址" required>
          <input
            type="url"
            placeholder="https://github.com/owner/repository"
            value={github}
            onChange={(e) => setGithub(e.target.value)}
            required
          />
        </Field>
        <Field label="固定 commit 或 tag" required>
          <input
            placeholder="commit SHA 或版本 tag"
            value={ref}
            onChange={(e) => setRef(e.target.value)}
            required
          />
        </Field>
        <Field label="代码 ZIP" required>
          <label className={`file-drop ${zip ? "filled" : ""}`}>
            <FileArchive size={26} />
            <b>{zip ? zip.name : "选择代码压缩包"}</b>
            <span>{zip ? formatSize(zip.size) : ".zip 格式"}</span>
            <input
              type="file"
              accept=".zip,application/zip"
              required
              onChange={(e) => setZip(e.target.files?.[0])}
            />
          </label>
        </Field>
        <div className="form-section-title">工程截图</div>
        <Field label="真实启动截图" required>
          <label className={`file-drop small ${startup ? "filled" : ""}`}>
            <Image size={21} />
            <span>{startup?.name || "选择启动截图"}</span>
            <input
              type="file"
              accept="image/png,image/jpeg,image/webp"
              required
              onChange={(e) => setStartup(e.target.files?.[0])}
            />
          </label>
        </Field>
        <Field label="主要功能截图" required hint="1 至 6 张">
          <label
            className={`file-drop small ${features.length ? "filled" : ""}`}
          >
            <Image size={21} />
            <span>
              {features.length
                ? `已选择 ${features.length} 张截图`
                : "选择功能截图，可多选"}
            </span>
            <input
              type="file"
              accept="image/png,image/jpeg,image/webp"
              multiple
              required
              onChange={(e) => setFeatures(Array.from(e.target.files || []))}
            />
          </label>
        </Field>
        {job && (
          <div
            className={`analysis-progress ${analysis.data?.status === "failed" ? "failed" : ""}`}
          >
            <Status status={analysis.data?.status || "pending"} />
            <span>
              {analysis.data?.status === "failed"
                ? analysis.data.error
                : "正在统计文件与语言比例"}
            </span>
            {analysis.data?.status === "failed" && (
              <Button
                variant="secondary"
                icon={RotateCw}
                type="button"
                onClick={async () => {
                  try {
                    await post(`/api/v1/admin/analysis-jobs/${job}/retry`);
                    analysis.refetch();
                  } catch (e) {
                    toast(e instanceof Error ? e.message : "重试失败", "error");
                  }
                }}
              >
                重试
              </Button>
            )}
          </div>
        )}
        <div className="form-actions">
          <Button variant="secondary" type="button" onClick={onClose}>
            关闭
          </Button>
          <Button icon={Upload} type="submit" loading={busy} disabled={!!job || !backupReady}>
            上传并分析
          </Button>
        </div>
      </form>
    </Drawer>
  );
}
export function EngineeringDetail({
  id,
  onClose,
  onUpload,
}: {
  id: string;
  onClose: () => void;
  onUpload?: (e: Engineering) => void;
}) {
  const client = useQueryClient();
  const toast = useToast();
  const detail = useQuery({
    queryKey: ["engineering-detail", id],
    queryFn: () =>
      api<{ engineering: Engineering; versions: Engineering[] }>(
        `/api/v1/admin/engineering/${id}`,
      ),
  });
  const [selected, setSelected] = useState("");
  const [tab, setTab] = useState("overview");
  const [edit, setEdit] = useState(false);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [tagIDs, setTagIDs] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [preview, setPreview] = useState<string>();
  const tagsQuery = useTags();
  const [confirmArchive, setConfirmArchive] = useState(false);
  const versions = detail.data?.versions || [];
  const item = versions.find((v) => v.id === selected) || versions[0];
  const engineering = detail.data?.engineering;
  async function save(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      await patch(`/api/v1/admin/engineering/${id}`, {
        name,
        description,
        tag_ids: tagIDs,
      });
      client.invalidateQueries({ queryKey: ["engineering"] });
      client.invalidateQueries({ queryKey: ["engineering-detail"] });
      client.invalidateQueries({ queryKey: ["tags"] });
      setEdit(false);
      toast("工程信息已保存");
    } catch (e) {
      toast(e instanceof Error ? e.message : "更新失败", "error");
    } finally {
      setBusy(false);
    }
  }
  async function archive() {
    setBusy(true);
    try {
      await patch(`/api/v1/admin/engineering/${id}`, {
        archived: !engineering?.archived,
      });
      detail.refetch();
      client.invalidateQueries({ queryKey: ["engineering"] });
      setConfirmArchive(false);
      toast(engineering?.archived ? "工程已恢复" : "工程已归档");
    } catch (e) {
      toast(e instanceof Error ? e.message : "操作失败", "error");
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <Drawer
        title={engineering?.name || "工程详情"}
        subtitle={
          engineering
            ? `创建于 ${formatDate(engineering.created_at)}`
            : undefined
        }
        onClose={onClose}
        wide
      >
        {detail.isPending ? (
          <Loading />
        ) : detail.isError ? (
          <ErrorState error={detail.error} retry={() => detail.refetch()} />
        ) : (
          <>
            {edit ? (
              <form onSubmit={save}>
                <Field label="名称" required>
                  <input
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    required
                  />
                </Field>
                <Field label="描述">
                  <textarea
                    rows={3}
                    value={description}
                    onChange={(e) => setDescription(e.target.value)}
                  />
                </Field>
                <Field
                  label="所属领域标签"
                  hint="按所属领域选择子方向，可多选；不选择表示暂无标签"
                >
                  {tagsQuery.isPending ? (
                    <span className="field-hint">正在加载标签…</span>
                  ) : tagsQuery.isError ? (
                    <span className="field-hint">
                      标签加载失败：{tagsQuery.error.message}
                    </span>
                  ) : (
                    <TagPicker
                      tree={tagsQuery.data?.items || []}
                      value={tagIDs}
                      onChange={setTagIDs}
                    />
                  )}
                </Field>
                <div className="form-actions">
                  <Button
                    type="button"
                    variant="secondary"
                    onClick={() => setEdit(false)}
                  >
                    取消
                  </Button>
                  <Button type="submit" loading={busy}>
                    保存
                  </Button>
                </div>
              </form>
            ) : (
              <div className="detail-actions">
                <Link to={`/backups?engineering_id=${encodeURIComponent(id)}`} className="button secondary" onClick={onClose}><DatabaseBackup size={16} />仓库备份</Link>
                <Button
                  variant="secondary"
                  icon={Pencil}
                  onClick={() => {
                    setName(engineering?.name || "");
                    setDescription(engineering?.description || "");
                    setTagIDs(engineering?.tags?.map((tag) => tag.id) || []);
                    setEdit(true);
                  }}
                >
                  编辑信息
                </Button>
                {onUpload && item && (
                  <Button
                    icon={Upload}
                    onClick={() =>
                      onUpload({
                        ...item,
                        engineering_id: id,
                        name: engineering?.name || item.name,
                        description:
                          engineering?.description || item.description,
                      })
                    }
                  >
                    上传新版本
                  </Button>
                )}
                <IconButton
                  icon={Archive}
                  label={engineering?.archived ? "恢复工程" : "归档工程"}
                  onClick={() => setConfirmArchive(true)}
                />
              </div>
            )}
            {item && (
              <>
                <div className="version-select">
                  <span>版本快照</span>
                  <select
                    value={item.id}
                    onChange={(e) => setSelected(e.target.value)}
                  >
                    {versions.map((v) => (
                      <option key={v.id} value={v.id}>
                        v{v.version_number} · {formatDate(v.created_at)}
                      </option>
                    ))}
                  </select>
                  <Status status={item.status} />
                </div>
                <Tabs
                  value={tab}
                  onChange={setTab}
                  items={[
                    { value: "overview", label: "工程信息" },
                    {
                      value: "screenshots",
                      label: "截图",
                      count: item.assets?.length || 0,
                    },
                    {
                      value: "versions",
                      label: "版本记录",
                      count: versions.length,
                    },
                  ]}
                />
                {tab === "overview" && (
                  <div className="engineering-overview">
                    <p className="detail-description">
                      {engineering?.description || "暂无描述"}
                    </p>
                    <div className="tag-badge-line">
                      <span className="tag-badge-line-label">所属领域标签</span>
                      {engineering?.tags?.length ? (
                        <TagChips tags={engineering.tags} />
                      ) : (
                        <span className="muted">未添加标签</span>
                      )}
                    </div>
                    <div className="detail-stats">
                      <div>
                        <span>源码文件</span>
                        <b>{item.source_file_count || 0}</b>
                      </div>
                      <div>
                        <span>全部文件</span>
                        <b>{item.total_file_count || 0}</b>
                      </div>
                      <div>
                        <span>代码包大小</span>
                        <b>{formatSize(item.zip_size)}</b>
                      </div>
                    </div>
                    <h3 className="form-section-title">语言构成</h3>
                    <LanguageBar values={item.languages} />
                    <div className="language-details">
                      {item.languages?.map((l) => (
                        <div key={l.name}>
                          <span>{l.name}</span>
                          <b>{l.code.toLocaleString()} 行</b>
                          <span>{l.files} 个文件</span>
                        </div>
                      ))}
                    </div>
                    <h3 className="form-section-title">技术栈</h3>
                    <div className="tag-list">
                      {item.tech_stack?.map((t) => (
                        <span className="tag" key={t}>
                          {t}
                        </span>
                      ))}
                    </div>
                    <h3 className="form-section-title">仓库与代码</h3>
                    <dl className="detail-list">
                      <dt>GitHub</dt>
                      <dd>
                        {item.github_url ? (
                          <a
                            href={item.github_url}
                            target="_blank"
                            rel="noreferrer"
                          >
                            {item.github_url}
                            <ExternalLink size={13} />
                          </a>
                        ) : (
                          "—"
                        )}
                      </dd>
                      <dt>Commit / Tag</dt>
                      <dd className="mono">{item.github_ref || "—"}</dd>
                      <dt>SHA-256</dt>
                      <dd className="mono hash">
                        {item.zip_sha256 || "分析完成后生成"}
                      </dd>
                    </dl>
                    {item.status === "ready" && (
                      <a
                        className="button primary"
                        href={`/api/v1/admin/engineering-versions/${item.id}/zip`}
                      >
                        <Download size={16} />
                        下载 ZIP
                      </a>
                    )}
                  </div>
                )}
                {tab === "screenshots" &&
                  (item.assets?.length ? (
                    <div className="screenshot-gallery">
                      {item.assets.map((a, index) => (
                        <figure key={a.id}>
                          <button
                            onClick={() =>
                              setPreview(a.url || `/api/v1/assets/${a.id}`)
                            }
                          >
                            <img
                              src={a.url || `/api/v1/assets/${a.id}`}
                              alt={
                                a.kind === "startup"
                                  ? "真实启动截图"
                                  : `主要功能截图 ${index}`
                              }
                              loading="lazy"
                            />
                          </button>
                          <figcaption>
                            {a.kind === "startup"
                              ? "真实启动"
                              : `主要功能 ${index}`}
                          </figcaption>
                        </figure>
                      ))}
                    </div>
                  ) : (
                    <Empty icon={Image} title="此版本没有截图" />
                  ))}
                {tab === "versions" && (
                  <div className="version-history">
                    {versions.map((v) => (
                      <button
                        key={v.id}
                        className={v.id === item.id ? "active" : ""}
                        onClick={() => {
                          setSelected(v.id);
                          setTab("overview");
                        }}
                      >
                        <span className="version-marker">
                          v{v.version_number}
                        </span>
                        <span>
                          <b>{formatDate(v.created_at, true)}</b>
                          <small>
                            {v.source_file_count} 个源码文件 ·{" "}
                            {formatSize(v.zip_size)}
                          </small>
                        </span>
                        <Status status={v.status} />
                        <ChevronRight size={16} />
                      </button>
                    ))}
                  </div>
                )}
              </>
            )}
          </>
        )}
      </Drawer>
      {preview && (
        <div className="image-lightbox" onClick={() => setPreview(undefined)}>
          <IconButton
            icon={X}
            label="关闭图片"
            onClick={() => setPreview(undefined)}
          />
          <img src={preview} alt="工程截图大图" />
        </div>
      )}
      {confirmArchive && (
        <Confirm
          title={engineering?.archived ? "恢复工程？" : "归档工程？"}
          description={
            engineering?.archived
              ? "恢复后工程将重新出现在工程池。"
              : "已加入项目的版本仍会保留。"
          }
          onClose={() => setConfirmArchive(false)}
          onConfirm={archive}
          loading={busy}
        />
      )}
    </>
  );
}
