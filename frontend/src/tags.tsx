import { useQuery } from "@tanstack/react-query";
import { Check } from "lucide-react";
import { api, type EngineeringTag, type List, type Tag } from "./api";

/** 标签树是全局数据，所有登录用户可读；工程池列表与工程表单各自查询一次。 */
export function useTags() {
  return useQuery({
    queryKey: ["tags"],
    queryFn: async () => {
      const list = await api<List<Tag>>("/api/v1/tags");
      // 空领域在后端可能把 children 序列化为 null，这里统一规范成数组。
      return { items: normalizeTree(list.items), total: list.total };
    },
  });
}

function normalizeTree(nodes: Tag[] | null | undefined): Tag[] {
  return (nodes || []).map((node) => ({
    ...node,
    children: normalizeTree(node.children),
  }));
}

function leafIDs(tag: Tag) {
  return tag.children.map((child) => child.id);
}

function toggleValue(value: string[], id: string) {
  return value.includes(id)
    ? value.filter((item) => item !== id)
    : [...value, id];
}

function toggleGroup(value: string[], tag: Tag) {
  const ids = leafIDs(tag);
  if (!ids.length) return value;
  return ids.every((id) => value.includes(id))
    ? value.filter((id) => !ids.includes(id))
    : [...value.filter((id) => !ids.includes(id)), ...ids];
}

/** 工程行 / 卡片 / 详情上的标签块：领域名小字 + 子方向名，超出 max 个折叠为 +N。 */
export function TagChips({
  tags,
  max,
}: {
  tags?: EngineeringTag[];
  max?: number;
}) {
  if (!tags?.length) return null;
  const visible = max === undefined ? tags : tags.slice(0, max);
  const rest = tags.slice(visible.length);
  return (
    <span className="tag-badges">
      {visible.map((tag) => (
        <span
          className="tag-badge"
          key={tag.id}
          title={`${tag.parent_name} / ${tag.name}`}
        >
          <small>{tag.parent_name}</small>
          <span>{tag.name}</span>
        </span>
      ))}
      {rest.length > 0 && (
        <span
          className="tag-badge-more"
          title={rest.map((tag) => tag.name).join("、")}
        >
          +{rest.length}
        </span>
      )}
    </span>
  );
}

/**
 * 工程池列表上方的标签树筛选：领域按钮整域选中，子方向 chip 多选。
 * value 只保存叶子（子方向）ID——领域选中即选中其全部子方向。
 */
export function TagFilter({
  tree,
  value,
  onChange,
}: {
  tree: Tag[];
  value: string[];
  onChange: (value: string[]) => void;
}) {
  if (!tree.length) return null;
  const selected = new Set(value);
  const groupSelected = (tag: Tag) =>
    tag.children.length > 0 &&
    tag.children.every((child) => selected.has(child.id));
  return (
    <section className="tag-filter" aria-label="按标签筛选">
      <div className="tag-filter-head">
        <span className="tag-filter-title">标签筛选</span>
        {value.length > 0 && (
          <div className="tag-filter-actions">
            <span className="tag-filter-count">已选 {value.length} 个</span>
            <button
              type="button"
              className="text-button"
              onClick={() => onChange([])}
            >
              清除
            </button>
          </div>
        )}
      </div>
      {tree.map((domain) => (
        <div className="tag-filter-row" key={domain.id}>
          <button
            type="button"
            className={`tag-filter-domain ${groupSelected(domain) ? "selected" : ""}`}
            aria-pressed={groupSelected(domain)}
            disabled={!domain.children.length}
            title={
              domain.children.length
                ? `选中「${domain.name}」下全部子方向`
                : "该领域下暂无子方向"
            }
            onClick={() => onChange(toggleGroup(value, domain))}
          >
            <i aria-hidden="true" />
            {domain.name}
          </button>
          {domain.children.length > 0 && (
            <div className="tag-filter-children">
              {domain.children.map((child) => (
                <button
                  type="button"
                  key={child.id}
                  className={`tag-chip ${selected.has(child.id) ? "selected" : ""}`}
                  aria-pressed={selected.has(child.id)}
                  onClick={() => onChange(toggleValue(value, child.id))}
                >
                  {selected.has(child.id) && <Check size={11} />}
                  {child.name}
                </button>
              ))}
            </div>
          )}
        </div>
      ))}
    </section>
  );
}

/** 工程表单里的分组多选：领域仅作分组标题，只有子方向可以被选中。 */
export function TagPicker({
  tree,
  value,
  onChange,
}: {
  tree: Tag[];
  value: string[];
  onChange: (value: string[]) => void;
}) {
  if (!tree.length)
    return <span className="field-hint">暂无可用标签，请先在「标签管理」中维护。</span>;
  const selected = new Set(value);
  return (
    <div className="tag-picker">
      {tree.map((domain) => {
        const all =
          domain.children.length > 0 &&
          domain.children.every((child) => selected.has(child.id));
        return (
          <div className="tag-picker-group" key={domain.id}>
            <div className="tag-picker-domain">
              <span>{domain.name}</span>
              {domain.children.length > 0 && (
                <button
                  type="button"
                  className="text-button"
                  onClick={() => onChange(toggleGroup(value, domain))}
                >
                  {all ? "取消整组" : "选择整组"}
                </button>
              )}
            </div>
            {domain.children.length > 0 ? (
              <div className="tag-picker-chips">
                {domain.children.map((child) => (
                  <button
                    type="button"
                    key={child.id}
                    className={selected.has(child.id) ? "selected" : ""}
                    aria-pressed={selected.has(child.id)}
                    onClick={() => onChange(toggleValue(value, child.id))}
                  >
                    {selected.has(child.id) && <Check size={12} />}
                    {child.name}
                  </button>
                ))}
              </div>
            ) : (
              <span className="field-hint">该领域下暂无子方向</span>
            )}
          </div>
        );
      })}
    </div>
  );
}
