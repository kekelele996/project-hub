import { useState, type FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  Check,
  Pencil,
  Plus,
  RotateCw,
  Tags as TagsIcon,
  Trash2,
} from "lucide-react";
import { api, patch, post, type Tag } from "../api";
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
  useToast,
} from "../ui";
import { useTags } from "../tags";

type TagFormState =
  | { mode: "domain" }
  | { mode: "child"; parent: Tag }
  | { mode: "edit"; tag: Tag };

export function TagsPage() {
  const client = useQueryClient();
  const toast = useToast();
  const query = useTags();
  const [form, setForm] = useState<TagFormState>();
  const [remove, setRemove] = useState<Tag>();
  const [busy, setBusy] = useState(false);
  const domains = query.data?.items || [];
  const childCount = domains.reduce(
    (total, domain) => total + domain.children.length,
    0,
  );
  async function destroy() {
    if (!remove) return;
    setBusy(true);
    try {
      await api(`/api/v1/admin/tags/${remove.id}`, { method: "DELETE" });
      client.invalidateQueries({ queryKey: ["tags"] });
      client.invalidateQueries({ queryKey: ["projects"] });
      toast(remove.parent_id ? "子方向已删除" : "所属领域已删除");
      setRemove(undefined);
    } catch (e) {
      toast(e instanceof Error ? e.message : "删除失败", "error");
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <PageHeader
        eyebrow="WORKSPACE / TAXONOMY"
        title="标签管理"
        subtitle="维护所属领域与子方向，用于工程分类与筛选。"
      >
        <Button icon={Plus} onClick={() => setForm({ mode: "domain" })}>
          新增所属领域
        </Button>
      </PageHeader>
      <div className="section-toolbar">
        <div className="section-title">
          <h2>标签树</h2>
          <span className="count-badge">
            {query.data
              ? `${domains.length} 个领域 / ${childCount} 个子方向`
              : "—"}
          </span>
        </div>
        <IconButton
          icon={RotateCw}
          label="刷新标签"
          onClick={() => query.refetch()}
        />
      </div>
      {query.isPending ? (
        <Loading />
      ) : query.isError ? (
        <ErrorState error={query.error} retry={() => query.refetch()} />
      ) : !domains.length ? (
        <Empty
          icon={TagsIcon}
          title="还没有标签"
          description="先创建所属领域，再为领域添加子方向。"
          action={
            <Button icon={Plus} onClick={() => setForm({ mode: "domain" })}>
              新增所属领域
            </Button>
          }
        />
      ) : (
        <div className="tag-admin-list">
          {domains.map((domain) => (
            <section
              className="tag-domain"
              key={domain.id}
              aria-label={`所属领域 ${domain.name}`}
            >
              <header className="tag-domain-head">
                <div className="tag-domain-title">
                  <h3>{domain.name}</h3>
                  <span>
                    {domain.children.length} 个子方向 ·{" "}
                    {domain.engineering_count} 个工程
                  </span>
                </div>
                <div className="tag-domain-actions">
                  <button
                    type="button"
                    className="text-button"
                    onClick={() => setForm({ mode: "child", parent: domain })}
                  >
                    添加子方向
                  </button>
                  <IconButton
                    icon={Pencil}
                    label={`编辑所属领域 ${domain.name}`}
                    onClick={() => setForm({ mode: "edit", tag: domain })}
                  />
                  <IconButton
                    icon={Trash2}
                    label={`删除所属领域 ${domain.name}`}
                    onClick={() => setRemove(domain)}
                  />
                </div>
              </header>
              <div className="tag-admin-children">
                {domain.children.length ? (
                  domain.children.map((child) => (
                    <div className="tag-admin-child" key={child.id}>
                      <span className="tag-admin-child-name">{child.name}</span>
                      <span className="tag-admin-count">
                        {child.engineering_count} 个工程
                      </span>
                      <div className="tag-admin-child-actions">
                        <IconButton
                          icon={Pencil}
                          label={`编辑子方向 ${child.name}`}
                          onClick={() => setForm({ mode: "edit", tag: child })}
                        />
                        <IconButton
                          icon={Trash2}
                          label={`删除子方向 ${child.name}`}
                          onClick={() => setRemove(child)}
                        />
                      </div>
                    </div>
                  ))
                ) : (
                  <span className="muted tag-admin-empty">
                    该领域还没有子方向
                  </span>
                )}
              </div>
            </section>
          ))}
        </div>
      )}
      {form && <TagForm state={form} onClose={() => setForm(undefined)} />}
      {remove && (
        <Confirm
          title={
            remove.parent_id
              ? `删除子方向「${remove.name}」？`
              : `删除所属领域「${remove.name}」？`
          }
          description={
            remove.parent_id
              ? "仅当该子方向没有被工程使用时才能删除，删除后不可恢复。"
              : "仅当该领域下没有子方向、且没有被工程使用时才能删除，删除后不可恢复。"
          }
          onClose={() => setRemove(undefined)}
          onConfirm={destroy}
          loading={busy}
          danger
        />
      )}
    </>
  );
}

function TagForm({
  state,
  onClose,
}: {
  state: TagFormState;
  onClose: () => void;
}) {
  const client = useQueryClient();
  const toast = useToast();
  const editing = state.mode === "edit" ? state.tag : undefined;
  const [name, setName] = useState(editing?.name || "");
  const [sort, setSort] = useState(
    editing ? String(editing.sort_order) : "",
  );
  const [busy, setBusy] = useState(false);
  const title = editing
    ? "编辑标签"
    : state.mode === "domain"
      ? "新增所属领域"
      : "添加子方向";
  const subtitle =
    state.mode === "child"
      ? `所属领域：${state.parent.name}`
      : state.mode === "domain"
        ? "领域是标签树的第一级，用于给子方向分组。"
        : undefined;
  async function save(e: FormEvent) {
    e.preventDefault();
    const value = name.trim();
    if (!value) {
      toast("请填写标签名称", "error");
      return;
    }
    const sortValue = sort.trim() === "" ? undefined : Number(sort);
    if (sortValue !== undefined && !Number.isInteger(sortValue)) {
      toast("排序需要填写整数", "error");
      return;
    }
    setBusy(true);
    try {
      const sortPatch = sortValue === undefined ? {} : { sort_order: sortValue };
      if (editing) {
        await patch(`/api/v1/admin/tags/${editing.id}`, {
          name: value,
          ...sortPatch,
        });
      } else {
        await post("/api/v1/admin/tags", {
          name: value,
          parent_id: state.mode === "child" ? state.parent.id : null,
          ...sortPatch,
        });
      }
      client.invalidateQueries({ queryKey: ["tags"] });
      client.invalidateQueries({ queryKey: ["projects"] });
      toast(
        editing
          ? "标签已更新"
          : state.mode === "domain"
            ? "所属领域已创建"
            : "子方向已创建",
      );
      onClose();
    } catch (e) {
      toast(e instanceof Error ? e.message : "保存失败", "error");
    } finally {
      setBusy(false);
    }
  }
  return (
    <Drawer title={title} subtitle={subtitle} onClose={onClose}>
      <form onSubmit={save}>
        <Field label="名称" required hint="最多 40 个字符">
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            maxLength={40}
            required
          />
        </Field>
        <Field
          label="排序"
          hint="数字越小越靠前；留空由后端自动分配"
        >
          <input
            type="number"
            value={sort}
            onChange={(e) => setSort(e.target.value)}
            placeholder="自动分配"
          />
        </Field>
        <div className="form-actions">
          <Button type="button" variant="secondary" onClick={onClose}>
            取消
          </Button>
          <Button type="submit" loading={busy} icon={editing ? Check : Plus}>
            {editing ? "保存标签" : "创建标签"}
          </Button>
        </div>
      </form>
    </Drawer>
  );
}
