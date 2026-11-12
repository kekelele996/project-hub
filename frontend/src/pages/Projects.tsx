import { useEffect, useState, type FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
  ArrowLeft,
  ArrowRight,
  ArrowUpCircle,
  Boxes,
  CalendarDays,
  Check,
  ChevronRight,
  CircleUserRound,
  Download,
  ExternalLink,
  FolderKanban,
  Grid2X2,
  List as ListIcon,
  Minus,
  Pencil,
  Plus,
  RotateCw,
  Trash2,
  UserPlus,
  Users,
  X,
} from "lucide-react";
import { useAuth } from "../App";
import {
  api,
  formatDate,
  formatSize,
  languages,
  params,
  patch,
  post,
  type Engineering,
  type List,
  type Project,
  type User,
} from "../api";
import {
  Avatar,
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
import {
  EngineeringCard,
  EngineeringFilters,
  Screenshot,
  engineeringID,
} from "./Engineering";
import { TagChips, TagFilter, useTags } from "../tags";

export function projectPhase(project: Project) {
  if (project.status !== "active") return project.status;
  const now = Date.now();
  if (now < Date.parse(project.start_at)) return "upcoming";
  if (now > Date.parse(project.end_at)) return "expired";
  return "active";
}
const phaseName: Record<string, string> = {
  upcoming: "未开始",
  expired: "已结束",
};
function ProjectStatus({ project }: { project: Project }) {
  const phase = projectPhase(project);
  return phaseName[phase] ? (
    <span className={`status ${phase}`}>
      <i />
      {phaseName[phase]}
    </span>
  ) : (
    <Status status={phase} />
  );
}
export function ProjectPage({ member = false }: { member?: boolean }) {
  const [q, setQ] = useState("");
  const [status, setStatus] = useState("all");
  const [page, setPage] = useState(1);
  const [create, setCreate] = useState(false);
  const navigate = useNavigate();
  const query = useQuery({
    queryKey: ["projects", member, q, status, page],
    queryFn: () =>
      api<List<Project>>(
        `${member ? "/api/v1/me/projects" : "/api/v1/admin/projects"}?${params({ q, status, page, page_size: 20 })}`,
      ),
  });
  useEffect(() => setPage(1), [q, status]);
  const projects = query.data?.items || [];
  return (
    <>
      <PageHeader
        eyebrow={member ? "YOUR WORKSPACE" : "PROJECT WORKSPACE"}
        title={member ? "我的项目" : "项目管理"}
        subtitle={
          member
            ? "属于你的项目与正在进行的协作。"
            : "圈定工程范围，分配成员，管理每一次领取。"
        }
      >
        {!member && (
          <Button icon={Plus} onClick={() => setCreate(true)}>
            新建项目
          </Button>
        )}
      </PageHeader>
      <div className="section-toolbar">
        <Tabs
          value={status}
          onChange={setStatus}
          items={[
            { value: "all", label: "全部项目", count: query.data?.total },
            { value: "active", label: "进行中" },
            { value: "upcoming", label: "未开始" },
            { value: "expired", label: "已结束" },
            ...(!member
              ? [
                  { value: "paused", label: "已暂停" },
                  { value: "archived", label: "已归档" },
                ]
              : []),
          ]}
        />
        <SearchInput value={q} onChange={setQ} placeholder="搜索项目" />
      </div>
      {query.isPending ? (
        <Loading />
      ) : query.isError ? (
        <ErrorState error={query.error} retry={() => query.refetch()} />
      ) : !projects.length ? (
        <Empty
          icon={FolderKanban}
          title={
            q || status !== "all"
              ? "没有符合筛选的项目"
              : member
                ? "尚未加入项目"
                : "还没有项目"
          }
          action={
            !member && !q && status === "all" ? (
              <Button icon={Plus} onClick={() => setCreate(true)}>
                新建项目
              </Button>
            ) : undefined
          }
        />
      ) : (
        <div className="project-list">
          {projects.map((project, index) => (
            <Link
              className="project-row"
              to={`/projects/${project.id}`}
              key={project.id}
            >
              <div className="project-row-index">
                {String((page - 1) * 20 + index + 1).padStart(2, "0")}
              </div>
              <div className="project-main">
                <div className="project-title">
                  <h2>{project.name}</h2>
                  <ProjectStatus project={project} />
                </div>
                <p>{project.description || "暂无项目描述"}</p>
                <div className="project-tech">
                  {project.tech_stack?.length ? (
                    project.tech_stack.map((t) => (
                      <span className="tag" key={t}>
                        {t}
                      </span>
                    ))
                  ) : (
                    <span className="muted">全部技术栈</span>
                  )}
                </div>
              </div>
              <div className="project-date">
                <CalendarDays size={15} />
                <span>
                  {formatDate(project.start_at)}
                  <small>至 {formatDate(project.end_at)}</small>
                </span>
              </div>
              <div className="project-count">
                <b>{project.engineering_count || 0}</b>
                <span>工程</span>
              </div>
              <div className="project-count">
                <b>{project.member_count || 0}</b>
                <span>成员</span>
              </div>
              <div className="project-progress">
                <div>
                  <span>已领取</span>
                  <b>
                    {project.claimed_count || 0} /{" "}
                    {project.engineering_count || 0}
                  </b>
                </div>
                <span className="progress-track">
                  <i
                    style={{
                      width: `${project.engineering_count ? ((project.claimed_count || 0) / project.engineering_count) * 100 : 0}%`,
                    }}
                  />
                </span>
              </div>
              <ArrowRight size={19} />
            </Link>
          ))}
        </div>
      )}
      <Pagination
        page={page}
        total={query.data?.total || 0}
        onChange={setPage}
      />
      {create && (
        <ProjectForm
          onClose={() => setCreate(false)}
          onSaved={(id) => navigate(`/projects/${id}`)}
        />
      )}
    </>
  );
}
function toDateInput(value?: string) {
  if (!value) return "";
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone: "Asia/Shanghai",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).formatToParts(new Date(value));
  return `${parts.find((p) => p.type === "year")?.value}-${parts.find((p) => p.type === "month")?.value}-${parts.find((p) => p.type === "day")?.value}`;
}
export function ProjectForm({
  project,
  onClose,
  onSaved,
}: {
  project?: Project;
  onClose: () => void;
  onSaved?: (id: string) => void;
}) {
  const client = useQueryClient();
  const toast = useToast();
  const [name, setName] = useState(project?.name || "");
  const [description, setDescription] = useState(project?.description || "");
  const [start, setStart] = useState(
    toDateInput(project?.start_at) || toDateInput(new Date().toISOString()),
  );
  const [end, setEnd] = useState(
    toDateInput(project?.end_at) ||
      toDateInput(new Date(Date.now() + 30 * 86400000).toISOString()),
  );
  const [tech, setTech] = useState<string[]>(project?.tech_stack || []);
  const [min, setMin] = useState(String(project?.min_files || ""));
  const [max, setMax] = useState(String(project?.max_files || ""));
  const [status, setStatus] = useState(project?.status || "active");
  const [members, setMembers] = useState<string[]>([]);
  const [engineering, setEngineering] = useState<string[]>([]);
  const [tab, setTab] = useState("base");
  const [busy, setBusy] = useState(false);
  const [engineeringQ, setEngineeringQ] = useState("");
  const [engineeringPage, setEngineeringPage] = useState(1);
  const [memberQ, setMemberQ] = useState("");
  const [memberPage, setMemberPage] = useState(1);
  const [tagIDs, setTagIDs] = useState<string[]>([]);
  const tagsQuery = useTags();
  const userQuery = useQuery({
    queryKey: ["users-options", memberQ, memberPage],
    queryFn: () =>
      api<List<User>>(
        `/api/v1/admin/users?${params({ q: memberQ, active: "true", page: memberPage, page_size: 20 })}`,
      ),
    enabled: !project,
  });
  const engineQuery = useQuery({
    queryKey: [
      "project-create-engineering",
      engineeringQ,
      engineeringPage,
      tech,
      min,
      max,
      tagIDs,
    ],
    queryFn: () =>
      api<List<Engineering>>(
        `/api/v1/admin/engineering?${params({ q: engineeringQ, status: "ready", tech_stack: tech.join(","), tag_ids: tagIDs, page: engineeringPage, page_size: 20, min_files: min, max_files: max })}`,
      ),
    enabled: !project,
  });
  useEffect(
    () => setEngineeringPage(1),
    [engineeringQ, tech, min, max, tagIDs],
  );
  useEffect(() => setMemberPage(1), [memberQ]);
  const candidates = engineQuery.data?.items || [];
  async function submit(e: FormEvent) {
    e.preventDefault();
    if (start > end) {
      toast("结束时间不能早于开始时间", "error");
      return;
    }
    if (min && max && +min > +max) {
      toast("最大文件数量不能小于最小值", "error");
      return;
    }
    if (!name.trim()) {
      setTab("base");
      toast("请填写项目名称", "error");
      return;
    }
    setBusy(true);
    try {
      const body = {
        name,
        description,
        start_at: `${start}T00:00:00+08:00`,
        end_at: `${end}T23:59:59+08:00`,
        tech_stack: tech,
        min_files: +min || 0,
        max_files: +max || 0,
        status,
        engineering_ids: engineering,
        member_ids: members,
      };
      const result = project
        ? await patch<Project>(`/api/v1/admin/projects/${project.id}`, body)
        : await post<Project>("/api/v1/admin/projects", body);
      client.invalidateQueries({ queryKey: ["projects"] });
      client.invalidateQueries({ queryKey: ["project"] });
      client.invalidateQueries({ queryKey: ["overview"] });
      toast(project ? "项目信息已更新" : "项目已创建");
      onClose();
      if (onSaved) onSaved(result?.id || project!.id);
    } catch (e) {
      toast(e instanceof Error ? e.message : "保存失败", "error");
    } finally {
      setBusy(false);
    }
  }
  return (
    <Drawer title={project ? "编辑项目" : "新建项目"} onClose={onClose} wide>
      <form onSubmit={submit}>
        {!project && (
          <Tabs
            value={tab}
            onChange={setTab}
            items={[
              { value: "base", label: "基本信息" },
              {
                value: "engineering",
                label: "工程范围",
                count: engineering.length,
              },
              { value: "members", label: "项目成员", count: members.length },
            ]}
          />
        )}
        <div hidden={tab !== "base"}>
          <Field label="项目名称" required>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              maxLength={120}
            />
          </Field>
          <Field label="项目描述">
            <textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              rows={3}
            />
          </Field>
          <div className="field-row">
            <Field label="开始时间" required>
              <input
                type="date"
                value={start}
                onChange={(e) => setStart(e.target.value)}
                required
              />
            </Field>
            <Field label="结束时间" required>
              <input
                type="date"
                value={end}
                min={start}
                onChange={(e) => setEnd(e.target.value)}
                required
              />
            </Field>
          </div>
          <Field label="采用技术栈" hint="不选择时表示全部技术栈">
            <TechPicker value={tech} onChange={setTech} options={languages} />
          </Field>
          <div className="field-row">
            <Field label="最少源码文件">
              <input
                type="number"
                min="0"
                value={min}
                onChange={(e) => setMin(e.target.value)}
                placeholder="不限"
              />
            </Field>
            <Field label="最多源码文件">
              <input
                type="number"
                min="0"
                value={max}
                onChange={(e) => setMax(e.target.value)}
                placeholder="不限"
              />
            </Field>
          </div>
          <Field label="项目状态">
            <select value={status} onChange={(e) => setStatus(e.target.value)}>
              <option value="active">进行中</option>
              <option value="paused">已暂停</option>
              <option value="archived">已归档</option>
            </select>
          </Field>
        </div>
        {!project && tab === "engineering" && (
          <>
            <SearchInput value={engineeringQ} onChange={setEngineeringQ} placeholder="搜索符合筛选的工程" />
            <TagFilter
              tree={tagsQuery.data?.items || []}
              value={tagIDs}
              onChange={setTagIDs}
            />
            <div className="selector-summary">
              <span>符合筛选的工程</span>
              <b>{engineQuery.data?.total || 0} 个</b>
              <Button
                type="button"
                variant="ghost"
                onClick={() =>
                  setEngineering(
                    candidates.every((e) =>
                      engineering.includes(engineeringID(e)),
                    )
                      ? engineering.filter(id => !candidates.some(e => engineeringID(e) === id))
                      : [...new Set([...engineering, ...candidates.map(engineeringID)])],
                  )
                }
              >
                选择本页
              </Button>
            </div>
            {engineQuery.isPending ? (
              <Loading />
            ) : engineQuery.isError ? (
              <ErrorState error={engineQuery.error} />
            ) : candidates.length ? (
              <div className="picker-list">
                {candidates.map((item) => (
                  <button
                    type="button"
                    className={
                      engineering.includes(engineeringID(item))
                        ? "selected"
                        : ""
                    }
                    key={engineeringID(item)}
                    onClick={() =>
                      setEngineering((v) =>
                        v.includes(engineeringID(item))
                          ? v.filter((i) => i !== engineeringID(item))
                          : [...v, engineeringID(item)],
                      )
                    }
                  >
                    <span className="pseudo-check">
                      {engineering.includes(engineeringID(item)) && (
                        <Check size={13} />
                      )}
                    </span>
                    <Screenshot item={item} />
                    <span className="picker-name">
                      <b>{item.name}</b>
                      <small>
                        {item.source_file_count} 个源码文件 · v
                        {item.version_number}
                      </small>
                      <TagChips tags={item.tags} max={3} />
                    </span>
                    <span className="muted">{item.languages?.[0]?.name}</span>
                  </button>
                ))}
              </div>
            ) : (
              <Empty icon={Boxes} title="没有符合条件的可用工程" />
            )}
            <Pagination page={engineeringPage} total={engineQuery.data?.total || 0} onChange={setEngineeringPage} />
          </>
        )}
        {!project && tab === "members" && (
          <>
            <SearchInput value={memberQ} onChange={setMemberQ} placeholder="搜索姓名、用户名" />
            {userQuery.isPending ? (
              <Loading />
            ) : userQuery.isError ? (
              <ErrorState error={userQuery.error} />
            ) : (
              <div className="picker-list">
                {userQuery.data?.items
                  .filter((u) => u.active)
                  .map((u) => (
                    <button
                      type="button"
                      className={members.includes(u.id) ? "selected" : ""}
                      key={u.id}
                      onClick={() =>
                        setMembers((v) =>
                          v.includes(u.id)
                            ? v.filter((i) => i !== u.id)
                            : [...v, u.id],
                        )
                      }
                    >
                      <span className="pseudo-check">
                        {members.includes(u.id) && <Check size={13} />}
                      </span>
                      <Avatar name={u.name} />
                      <span className="picker-name">
                        <b>{u.name}</b>
                        <small>@{u.username}</small>
                      </span>
                      <span className="muted">
                        {u.role === "admin" ? "管理员" : "成员"}
                      </span>
                    </button>
                  ))}
              </div>
            )}
            <Pagination page={memberPage} total={userQuery.data?.total || 0} onChange={setMemberPage} />
          </>
        )}
        <div className="form-actions">
          <Button variant="secondary" type="button" onClick={onClose}>
            取消
          </Button>
          {!project && tab === "base" && (
            <Button
              type="button"
              variant="secondary"
              icon={ArrowRight}
              onClick={() => setTab("engineering")}
            >
              选择工程与成员
            </Button>
          )}
          <Button icon={project ? Check : Plus} loading={busy} type="submit">
            {project ? "保存项目" : "创建项目"}
          </Button>
        </div>
      </form>
    </Drawer>
  );
}
export function ProjectDetail() {
  const { id } = useParams();
  const { user } = useAuth();
  const admin = user.role === "admin";
  const client = useQueryClient();
  const toast = useToast();
  const [tab, setTab] = useState("engineering");
  const [scope, setScope] = useState(admin ? "all" : "available");
  const [q, setQ] = useState("");
  const [language, setLanguage] = useState("");
  const [min, setMin] = useState("");
  const [max, setMax] = useState("");
  const [page, setPage] = useState(1);
  const [view, setView] = useState("list");
  const [edit, setEdit] = useState(false);
  const [add, setAdd] = useState(false);
  const [manageMembers, setManageMembers] = useState(false);
  const [detail, setDetail] = useState<Engineering>();
  const [selected, setSelected] = useState<string[]>([]);
  const [busy, setBusy] = useState("");
  const [operation, setOperation] = useState<{
    type: string;
    item?: Engineering;
    ids?: string[];
  }>();
  const [transferUser, setTransferUser] = useState("");
  const [upgradeVersion, setUpgradeVersion] = useState("");
  const projectQuery = useQuery({
    queryKey: ["project", id],
    queryFn: () => api<Project>(`/api/v1/projects/${id}`),
  });
  const project = projectQuery.data;
  const query = useQuery({
    queryKey: [
      "project-engineering",
      id,
      scope,
      q,
      language,
      min,
      max,
      page,
      tab,
    ],
    queryFn: () =>
      api<List<Engineering>>(
        `/api/v1/projects/${id}/engineering?${params({ scope: tab === "claims" ? "all" : scope, q, language, min_files: min, max_files: max, page, page_size: 20 })}`,
      ),
    refetchInterval: 15000,
  });
  const memberQuery = useQuery({
    queryKey: ["project-members", id],
    queryFn: () => api<List<User>>(`/api/v1/projects/${id}/members`),
  });
  const upgradeQuery = useQuery({
    queryKey: ["upgrade", operation?.item?.engineering_id],
    queryFn: () =>
      api<{ versions: Engineering[] }>(
        `/api/v1/admin/engineering/${engineeringID(operation!.item!)}`,
      ),
    enabled: operation?.type === "upgrade",
  });
  useEffect(() => {
    setPage(1);
    setSelected([]);
  }, [q, language, min, max, scope, tab]);
  const items = query.data?.items || [];
  const invalidate = () => {
    client.invalidateQueries({ queryKey: ["project-engineering", id] });
    client.invalidateQueries({ queryKey: ["project", id] });
    client.invalidateQueries({ queryKey: ["projects"] });
    client.invalidateQueries({ queryKey: ["overview"] });
    client.invalidateQueries({ queryKey: ["audit"] });
    // 工程范围变化后，添加工程候选（exclude_project）需要重新拉取
    client.invalidateQueries({ queryKey: ["scope-candidates"] });
  };
  const toggle = (eid: string) =>
    setSelected((v) =>
      v.includes(eid) ? v.filter((i) => i !== eid) : [...v, eid],
    );
  async function claim(item: Engineering) {
    setBusy(engineeringID(item));
    try {
      await post(
        `/api/v1/projects/${id}/engineering/${engineeringID(item)}/claim`,
      );
      invalidate();
      toast("已领取工程");
      setDetail(undefined);
    } catch (e) {
      toast(e instanceof Error ? e.message : "领取失败", "error");
      invalidate();
    } finally {
      setBusy("");
    }
  }
  async function perform() {
    if (!operation) return;
    setBusy("operation");
    try {
      const item = operation.item;
      if (operation.type === "remove")
        await post(`/api/v1/admin/projects/${id}/engineering-scope`, {
          remove: operation.ids,
        });
      if (operation.type === "release")
        await post(
          `/api/v1/projects/${id}/engineering/${engineeringID(item!)}/release`,
          { claim_id: item?.claim_id },
        );
      if (operation.type === "reclaim")
        await post(`/api/v1/admin/claims/${item?.claim_id}/reclaim`);
      if (operation.type === "transfer")
        await post(`/api/v1/admin/claims/${item?.claim_id}/transfer`, {
          user_id: transferUser,
        });
      if (operation.type === "upgrade")
        await post(`/api/v1/admin/projects/${id}/engineering-scope`, {
          upgrades: [
            {
              engineering_id: engineeringID(item!),
              version_id: upgradeVersion,
            },
          ],
        });
      invalidate();
      setSelected([]);
      setOperation(undefined);
      setDetail(undefined);
      toast("操作完成");
    } catch (e) {
      toast(e instanceof Error ? e.message : "操作失败", "error");
    } finally {
      setBusy("");
    }
  }
  const canClaim = project && projectPhase(project) === "active" && memberQuery.data?.items.some(u => u.id === user.id);
  const actions = (item: Engineering) => (
    <div className="row-actions">
      {item.can_download && (
        <a
          className="icon-button"
          aria-label={`下载 ${item.name}`}
          title="下载 ZIP"
          href={`/api/v1/projects/${id}/engineering/${engineeringID(item)}/zip`}
        >
          <Download size={17} />
        </a>
      )}
      {item.claimed_by === user.id ? (
        <Button
          variant="secondary"
          onClick={() => setOperation({ type: "release", item })}
        >
          归还
        </Button>
      ) : !item.claimed_by && canClaim ? (
        <Button
          variant="secondary"
          loading={busy === engineeringID(item)}
          onClick={() => claim(item)}
        >
          领取
        </Button>
      ) : null}
      {admin &&
        (item.claimed_by ? (
          <>
            <IconButton
              icon={CircleUserRound}
              label="转交工程"
              onClick={() => {
                setTransferUser("");
                setOperation({ type: "transfer", item });
              }}
            />
            <IconButton
              icon={RotateCw}
              label="回收工程"
              onClick={() => setOperation({ type: "reclaim", item })}
            />
          </>
        ) : (
          <>
            <IconButton
              icon={ArrowUpCircle}
              label="升级引用版本"
              onClick={() => {
                setUpgradeVersion("");
                setOperation({ type: "upgrade", item });
              }}
            />
            <IconButton
              icon={Trash2}
              label="移出项目"
              onClick={() =>
                setOperation({ type: "remove", ids: [engineeringID(item)] })
              }
            />
          </>
        ))}
      <IconButton
        icon={ChevronRight}
        label="查看工程详情"
        onClick={() => setDetail(item)}
      />
    </div>
  );
  if (projectQuery.isPending) return <Loading />;
  if (projectQuery.isError)
    return (
      <ErrorState
        error={projectQuery.error}
        retry={() => projectQuery.refetch()}
      />
    );
  if (!project) return null;
  return (
    <>
      <Link className="back-link" to={admin ? "/projects" : "/"}>
        <ArrowLeft size={15} />
        返回{admin ? "项目列表" : "我的项目"}
      </Link>
      <PageHeader
        eyebrow="PROJECT / WORKSPACE"
        title={project.name}
        subtitle={project.description || undefined}
      >
        {admin && (
          <Button
            variant="secondary"
            icon={Pencil}
            onClick={() => setEdit(true)}
          >
            编辑项目
          </Button>
        )}
        <ProjectStatus project={project} />
      </PageHeader>
      <div className="project-summary-band">
        <div>
          <CalendarDays size={17} />
          <span>
            {formatDate(project.start_at)}
            <span className="date-to">至</span>
            {formatDate(project.end_at)}
          </span>
        </div>
        <div>
          <Boxes size={17} />
          <span>
            <b>{project.engineering_count || 0}</b> 个工程
          </span>
        </div>
        <div>
          <Users size={17} />
          <span>
            <b>{project.member_count || 0}</b> 位成员
          </span>
        </div>
        <div className="summary-tags">
          {project.tech_stack?.map((t) => (
            <span className="tag" key={t}>
              {t}
            </span>
          ))}
        </div>
      </div>
      <div className="section-toolbar project-tabs">
        <Tabs
          value={tab}
          onChange={setTab}
          items={[
            {
              value: "engineering",
              label: admin ? "工程范围" : "工程列表",
              count: project.engineering_count,
            },
            {
              value: "members",
              label: "项目成员",
              count: project.member_count,
            },
            ...(admin
              ? [
                  {
                    value: "claims",
                    label: "领取矩阵",
                    count: project.claimed_count,
                  },
                ]
              : []),
          ]}
        />
        {admin &&
          (tab === "members" ? (
            <Button icon={UserPlus} onClick={() => setManageMembers(true)}>
              管理成员
            </Button>
          ) : (
            <Button icon={Plus} onClick={() => setAdd(true)}>
              添加工程
            </Button>
          ))}
      </div>
      {tab === "members" ? (
        memberQuery.isPending ? (
          <Loading />
        ) : memberQuery.isError ? (
          <ErrorState error={memberQuery.error} />
        ) : (
          <div className="table-wrap">
            <table className="data-table">
              <thead>
                <tr>
                  <th>成员</th>
                  <th>用户名</th>
                  <th>角色</th>
                  <th>账号状态</th>
                </tr>
              </thead>
              <tbody>
                {memberQuery.data?.items.map((u) => (
                  <tr key={u.id}>
                    <td>
                      <span className="user-cell">
                        <Avatar name={u.name} small />
                        <b>{u.name}</b>
                        {u.id === user.id && <span className="tag">我</span>}
                      </span>
                    </td>
                    <td className="muted">@{u.username}</td>
                    <td>{u.role === "admin" ? "管理员" : "成员"}</td>
                    <td>
                      <Status status={u.active ? "enabled" : "disabled"} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            {!memberQuery.data?.items.length && (
              <Empty icon={Users} title="项目还没有成员" />
            )}
          </div>
        )
      ) : (
        <>
          {tab === "engineering" && (
            <div className="scope-toolbar">
              <Tabs
                value={scope}
                onChange={setScope}
                items={[
                  ...(admin ? [{ value: "all", label: "全部工程" }] : []),
                  { value: "available", label: "可领取" },
                  { value: "mine", label: "我的工程" },
                ]}
              />
              <Segmented
                value={view}
                onChange={setView}
                items={[
                  { value: "list", icon: ListIcon, label: "列表视图" },
                  { value: "grid", icon: Grid2X2, label: "截图视图" },
                ]}
              />
            </div>
          )}
          <EngineeringFilters
            {...{ q, setQ, language, setLanguage, min, setMin, max, setMax }}
          />
          {projectPhase(project) !== "active" && (
            <div className="notice">
              {projectPhase(project) === "expired"
                ? "项目已结束，已领取工程仍可下载。"
                : projectPhase(project) === "upcoming"
                  ? "项目尚未开始。"
                  : "项目当前已暂停或归档。"}
            </div>
          )}
          {selected.length > 0 && (
            <div className="selection-bar">
              <span>
                <Check size={15} />
                已选择 {selected.length} 个未领取工程
              </span>
              <div>
                <Button variant="ghost" onClick={() => setSelected([])}>
                  取消选择
                </Button>
                <Button
                  variant="secondary"
                  icon={Minus}
                  onClick={() =>
                    setOperation({ type: "remove", ids: selected })
                  }
                >
                  移出项目
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
              title={
                scope === "mine"
                  ? "你还没有领取工程"
                  : scope === "available"
                    ? "没有可领取工程"
                    : "项目还没有工程"
              }
              action={
                admin && scope === "all" ? (
                  <Button icon={Plus} onClick={() => setAdd(true)}>
                    添加工程
                  </Button>
                ) : undefined
              }
            />
          ) : view === "grid" && tab === "engineering" ? (
            <div className="engineering-grid">
              {items.map((item) => (
                <div
                  className="project-engineering-card"
                  key={engineeringID(item)}
                >
                  <EngineeringCard
                    item={item}
                    onClick={() => setDetail(item)}
                  />
                  <div className="project-card-actions">
                    <span className="claim-label">
                      {item.claimed_by ? (
                        <>
                          <Avatar name={item.claimed_name} small />
                          {item.claimed_by === user.id
                            ? "我已领取"
                            : item.claimed_name}
                        </>
                      ) : (
                        <Status status="available" />
                      )}
                    </span>
                    {actions(item)}
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <div className="table-wrap">
              <table className="data-table project-engineering-table">
                <thead>
                  <tr>
                    {admin && tab !== "claims" && (
                      <th className="select-col">
                        <CheckBox
                          checked={
                            items.some((i) => !i.claimed_by) &&
                            items
                              .filter((i) => !i.claimed_by)
                              .every((i) => selected.includes(engineeringID(i)))
                          }
                          onChange={() =>
                            setSelected(
                              items
                                .filter((i) => !i.claimed_by)
                                .every((i) =>
                                  selected.includes(engineeringID(i)),
                                )
                                ? []
                                : items
                                    .filter((i) => !i.claimed_by)
                                    .map(engineeringID),
                            )
                          }
                          label="选择全部未领取工程"
                        />
                      </th>
                    )}
                    <th>工程 / 版本</th>
                    <th>语言构成</th>
                    <th>源码文件</th>
                    <th>分类</th>
                    <th>领取状态</th>
                    {tab === "claims" && <th>领取时间</th>}
                    <th className="table-action-heading">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((item) => (
                    <tr
                      key={engineeringID(item)}
                      className={
                        selected.includes(engineeringID(item))
                          ? "selected-row"
                          : ""
                      }
                    >
                      {admin && tab !== "claims" && (
                        <td>
                          {!item.claimed_by && (
                            <CheckBox
                              checked={selected.includes(engineeringID(item))}
                              onChange={() => toggle(engineeringID(item))}
                              label={`选择 ${item.name}`}
                            />
                          )}
                        </td>
                      )}
                      <td>
                        <button
                          className="engineering-cell"
                          onClick={() => setDetail(item)}
                        >
                          <Screenshot item={item} />
                          <span>
                            <b>{item.name}</b>
                            <small>
                              v{item.version_number} ·{" "}
                              {item.tech_stack?.slice(0, 3).join(" / ") || "—"}
                            </small>
                          </span>
                        </button>
                      </td>
                      <td>
                        <LanguageBar values={item.languages} compact />
                      </td>
                      <td className="numeric">{item.source_file_count || 0}</td>
                      <td>
                        <TagChips tags={item.tags} max={3} />
                      </td>
                      <td>
                        {item.claimed_by ? (
                          <span className="user-cell">
                            <Avatar name={item.claimed_name} small />
                            <span>
                              {item.claimed_by === user.id
                                ? "我"
                                : item.claimed_name}
                            </span>
                          </span>
                        ) : (
                          <Status status="available" />
                        )}
                      </td>
                      {tab === "claims" && (
                        <td className="muted nowrap">
                          {formatDate(item.claimed_at, true)}
                        </td>
                      )}
                      <td>{actions(item)}</td>
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
        </>
      )}
      {edit && <ProjectForm project={project} onClose={() => setEdit(false)} />}{" "}
      {add && (
        <AddEngineering
          project={project}
          onClose={() => setAdd(false)}
          onSaved={invalidate}
        />
      )}{" "}
      {manageMembers && (
        <ManageMembers
          projectID={id!}
          members={memberQuery.data?.items || []}
          onClose={() => setManageMembers(false)}
          onSaved={() => {
            memberQuery.refetch();
            invalidate();
          }}
        />
      )}{" "}
      {detail && (
        <ProjectEngineeringDetail
          item={detail}
          projectID={id!}
          onClose={() => setDetail(undefined)}
          actions={actions(detail)}
        />
      )}{" "}
      {operation &&
        ["remove", "release", "reclaim"].includes(operation.type) && (
          <Confirm
            title={
              operation.type === "remove"
                ? "将工程移出项目？"
                : operation.type === "release"
                  ? "归还工程？"
                  : "回收工程？"
            }
            description={
              operation.type === "remove"
                ? `将移除 ${operation.ids?.length} 个未领取工程。`
                : operation.type === "release"
                  ? "归还后工程将重新可领取，你将失去下载权限。"
                  : `将回收 ${operation.item?.claimed_name} 领取的「${operation.item?.name}」。`
            }
            onClose={() => setOperation(undefined)}
            onConfirm={perform}
            loading={busy === "operation"}
          />
        )}{" "}
      {operation?.type === "transfer" && (
        <Drawer
          title="转交工程"
          subtitle={operation.item?.name}
          onClose={() => setOperation(undefined)}
        >
          <Field label="转交给项目成员">
            <select
              value={transferUser}
              onChange={(e) => setTransferUser(e.target.value)}
            >
              <option value="">选择成员</option>
              {memberQuery.data?.items
                .filter((u) => u.active && u.id !== operation.item?.claimed_by)
                .map((u) => (
                  <option key={u.id} value={u.id}>
                    {u.name} (@{u.username})
                  </option>
                ))}
            </select>
          </Field>
          <div className="form-actions">
            <Button
              loading={busy === "operation"}
              disabled={!transferUser}
              icon={CircleUserRound}
              onClick={perform}
            >
              确认转交
            </Button>
          </div>
        </Drawer>
      )}{" "}
      {operation?.type === "upgrade" && (
        <Drawer
          title="升级引用版本"
          subtitle={operation.item?.name}
          onClose={() => setOperation(undefined)}
        >
          {upgradeQuery.isPending ? (
            <Loading />
          ) : upgradeQuery.isError ? (
            <ErrorState error={upgradeQuery.error} />
          ) : (
            <>
              <div className="notice">
                当前项目固定引用 v{operation.item?.version_number}。
              </div>
              <Field label="目标版本">
                <select
                  value={upgradeVersion}
                  onChange={(e) => setUpgradeVersion(e.target.value)}
                >
                  <option value="">选择可用版本</option>
                  {upgradeQuery.data?.versions
                    .filter(
                      (v) =>
                        v.status === "ready" &&
                        v.id !==
                          (operation.item?.version_id || operation.item?.id),
                    )
                    .map((v) => (
                      <option key={v.id} value={v.id}>
                        v{v.version_number} · {formatDate(v.created_at)} ·{" "}
                        {v.source_file_count} 个文件
                      </option>
                    ))}
                </select>
              </Field>
              <div className="form-actions">
                <Button
                  disabled={!upgradeVersion}
                  loading={busy === "operation"}
                  icon={ArrowUpCircle}
                  onClick={perform}
                >
                  升级版本
                </Button>
              </div>
            </>
          )}
        </Drawer>
      )}
    </>
  );
}
function AddEngineering({
  project,
  onClose,
  onSaved,
}: {
  project: Project;
  onClose: () => void;
  onSaved: () => void;
}) {
  const toast = useToast();
  const [q, setQ] = useState("");
  const [language, setLanguage] = useState("");
  const [min, setMin] = useState(String(project.min_files || ""));
  const [max, setMax] = useState(String(project.max_files || ""));
  const [selected, setSelected] = useState<string[]>([]);
  const [page, setPage] = useState(1);
  const [busy, setBusy] = useState(false);
  const [tagIDs, setTagIDs] = useState<string[]>([]);
  const tagsQuery = useTags();
  const query = useQuery({
    queryKey: [
      "scope-candidates",
      project.id,
      q,
      language,
      min,
      max,
      tagIDs,
      page,
    ],
    queryFn: () =>
      api<List<Engineering>>(
        `/api/v1/admin/engineering?${params({ q, language, exclude_project: project.id, min_files: min, max_files: max, status: "ready", tag_ids: tagIDs, page, page_size: 20 })}`,
      ),
  });
  const items = query.data?.items || [];
  useEffect(() => setPage(1), [q, language, min, max, tagIDs]);
  async function save() {
    setBusy(true);
    try {
      await post(`/api/v1/admin/projects/${project.id}/engineering-scope`, {
        add: selected,
      });
      toast(`已添加 ${selected.length} 个工程`);
      onSaved();
      onClose();
    } catch (e) {
      toast(e instanceof Error ? e.message : "添加失败", "error");
    } finally {
      setBusy(false);
    }
  }
  return (
    <Drawer title="添加工程" subtitle={project.name} onClose={onClose} wide>
      <EngineeringFilters
        {...{ q, setQ, language, setLanguage, min, setMin, max, setMax }}
      />
      <TagFilter
        tree={tagsQuery.data?.items || []}
        value={tagIDs}
        onChange={setTagIDs}
      />
      {query.isPending ? (
        <Loading />
      ) : query.isError ? (
        <ErrorState error={query.error} />
      ) : items.length ? (
        <div className="picker-list">
          {items.map((item) => {
            const eid = engineeringID(item);
            return (
              <button
                key={eid}
                className={selected.includes(eid) ? "selected" : ""}
                onClick={() =>
                  setSelected((v) =>
                    v.includes(eid) ? v.filter((i) => i !== eid) : [...v, eid],
                  )
                }
              >
                <span className="pseudo-check">
                  {selected.includes(eid) && <Check size={13} />}
                </span>
                <Screenshot item={item} />
                <span className="picker-name">
                  <b>{item.name}</b>
                  <small>
                    {item.source_file_count} 个源码文件 · v{item.version_number}
                  </small>
                  <TagChips tags={item.tags} max={3} />
                </span>
                <span className="muted">{item.languages?.[0]?.name}</span>
              </button>
            );
          })}
        </div>
      ) : (
        <Empty title="没有符合条件的工程" />
      )}
      <Pagination
        page={page}
        total={query.data?.total || 0}
        onChange={setPage}
      />
      <div className="form-actions">
        <span className="selected-count">已选择 {selected.length} 个工程</span>
        <Button variant="secondary" onClick={onClose}>
          取消
        </Button>
        <Button
          icon={Plus}
          disabled={!selected.length}
          loading={busy}
          onClick={save}
        >
          加入项目
        </Button>
      </div>
    </Drawer>
  );
}
function ManageMembers({
  projectID,
  members,
  onClose,
  onSaved,
}: {
  projectID: string;
  members: User[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const toast = useToast();
  const [q, setQ] = useState("");
  const [selected, setSelected] = useState(members.map((u) => u.id));
  const [busy, setBusy] = useState(false);
  const [page, setPage] = useState(1);
  const query = useQuery({
    queryKey: ["users-options", q, page, 'all'],
    queryFn: () => api<List<User>>(`/api/v1/admin/users?${params({ q, active: 'all', page, page_size: 20 })}`),
  });
  useEffect(() => setPage(1), [q]);
  const remove = members
    .filter((u) => !selected.includes(u.id))
    .map((u) => u.id);
  async function save() {
    setBusy(true);
    try {
      await post(`/api/v1/admin/projects/${projectID}/members`, {
        add: selected.filter((id) => !members.some((u) => u.id === id)),
        remove,
      });
      toast("项目成员已更新");
      onSaved();
      onClose();
    } catch (e) {
      toast(e instanceof Error ? e.message : "保存失败", "error");
    } finally {
      setBusy(false);
    }
  }
  return (
    <Drawer title="管理项目成员" onClose={onClose}>
      <SearchInput value={q} onChange={setQ} placeholder="搜索姓名、用户名" />
      {remove.length > 0 && (
        <div className="notice">
          移除 {remove.length} 名成员时，其当前领取工程会被回收。
        </div>
      )}
      {query.isPending ? (
        <Loading />
      ) : query.isError ? (
        <ErrorState error={query.error} />
      ) : (
        <div className="picker-list">
          {query.data?.items
            .map((u) => (
              <button
                className={selected.includes(u.id) ? "selected" : ""}
                key={u.id}
                disabled={!u.active && !members.some(member => member.id === u.id)}
                onClick={() =>
                  setSelected((v) =>
                    v.includes(u.id)
                      ? v.filter((i) => i !== u.id)
                      : [...v, u.id],
                  )
                }
              >
                <span className="pseudo-check">
                  {selected.includes(u.id) && <Check size={13} />}
                </span>
                <Avatar name={u.name} />
                <span className="picker-name">
                  <b>{u.name}</b>
                  <small>@{u.username}</small>
                </span>
                <span className="muted">
                  {!u.active ? '已停用' : u.role === "admin" ? "管理员" : "成员"}
                </span>
              </button>
            ))}
        </div>
      )}
      <Pagination page={page} total={query.data?.total || 0} onChange={setPage} />
      <div className="form-actions">
        <Button variant="secondary" onClick={onClose}>
          取消
        </Button>
        <Button icon={Check} loading={busy} onClick={save}>
          保存成员
        </Button>
      </div>
    </Drawer>
  );
}
function ProjectEngineeringDetail({
  item,
  projectID,
  onClose,
  actions,
}: {
  item: Engineering;
  projectID: string;
  onClose: () => void;
  actions: React.ReactNode;
}) {
  const [preview, setPreview] = useState<string>();
  return (
    <>
      <Drawer
        title={item.name}
        subtitle={`v${item.version_number} · ${formatDate(item.created_at)}`}
        onClose={onClose}
        wide
      >
        <p className="detail-description">{item.description || "暂无描述"}</p>
        <div className="detail-stats">
          <div>
            <span>源码文件</span>
            <b>{item.source_file_count}</b>
          </div>
          <div>
            <span>全部文件</span>
            <b>{item.total_file_count}</b>
          </div>
          <div>
            <span>领取状态</span>
            <b className="text-stat">{item.claimed_name || "未领取"}</b>
          </div>
        </div>
        <h3 className="form-section-title">语言构成</h3>
        <LanguageBar values={item.languages} />
        <h3 className="form-section-title">技术栈</h3>
        <div className="tag-list">
          {item.tech_stack?.map((t) => (
            <span className="tag" key={t}>
              {t}
            </span>
          ))}
        </div>
        {item.can_download && (
          <>
            <h3 className="form-section-title">代码来源</h3>
            <dl className="detail-list">
              <dt>GitHub</dt>
              <dd>
                {item.github_url ? (
                  <a href={item.github_url} target="_blank" rel="noreferrer">
                    {item.github_url}
                    <ExternalLink size={13} />
                  </a>
                ) : (
                  "—"
                )}
              </dd>
              <dt>固定引用</dt>
              <dd className="mono">{item.github_ref || "—"}</dd>
              <dt>代码包</dt>
              <dd>{formatSize(item.zip_size)}</dd>
              <dt>SHA-256</dt>
              <dd className="mono hash">{item.zip_sha256 || "—"}</dd>
            </dl>
            <a
              href={`/api/v1/projects/${projectID}/engineering/${engineeringID(item)}/zip`}
              className="button primary"
            >
              <Download size={16} />
              下载 ZIP
            </a>
          </>
        )}
        <h3 className="form-section-title">工程截图</h3>
        {item.assets?.length ? (
          <div className="screenshot-gallery">
            {item.assets.map((a, index) => (
              <figure key={a.id}>
                <button
                  onClick={() => setPreview(a.url || `/api/v1/assets/${a.id}`)}
                >
                  <img
                    src={a.url || `/api/v1/assets/${a.id}`}
                    alt={
                      a.kind === "startup"
                        ? "真实启动截图"
                        : `功能截图 ${index}`
                    }
                    loading="lazy"
                  />
                </button>
                <figcaption>
                  {a.kind === "startup" ? "真实启动" : `主要功能 ${index}`}
                </figcaption>
              </figure>
            ))}
          </div>
        ) : (
          <Empty title="没有截图" />
        )}
        <div className="detail-actions bottom-actions">{actions}</div>
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
    </>
  );
}
