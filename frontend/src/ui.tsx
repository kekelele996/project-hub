import {
  Children,
  cloneElement,
  createContext,
  isValidElement,
  useContext,
  useEffect,
  useRef,
  useId,
  useState,
  type ReactNode,
  type ReactElement,
  type ButtonHTMLAttributes,
} from "react";
import {
  AlertCircle,
  Check,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  CircleDashed,
  LoaderCircle,
  Search,
  X,
  type LucideIcon,
} from "lucide-react";
import { languageColor, type Language } from "./api";

export const ToastContext = createContext<
  (message: string, type?: "success" | "error") => void
>(() => {});
export function useToast() {
  return useContext(ToastContext);
}
export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<
    { id: number; message: string; type: string }[]
  >([]);
  const notify = (message: string, type: "success" | "error" = "success") => {
    const id = Date.now();
    setItems((v) => [...v, { id, message, type }]);
    window.setTimeout(
      () => setItems((v) => v.filter((item) => item.id !== id)),
      5000,
    );
  };
  return (
    <ToastContext.Provider value={notify}>
      {children}
      <div className="toast-stack" aria-live="polite">
        {items.map((item) => (
          <div className={`toast ${item.type}`} key={item.id}>
            {item.type === "error" ? (
              <AlertCircle size={18} />
            ) : (
              <CheckCircle2 size={18} />
            )}
            <span>{item.message}</span>
            <button
              onClick={() => setItems((v) => v.filter((i) => i.id !== item.id))}
              aria-label="关闭通知"
            >
              <X size={14} />
            </button>
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}
export function Button({
  children,
  icon: Icon,
  variant = "primary",
  loading,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  icon?: LucideIcon;
  variant?: "primary" | "secondary" | "ghost" | "danger";
  loading?: boolean;
}) {
  return (
    <button
      type="button"
      {...props}
      disabled={props.disabled || loading}
      className={`button ${variant} ${props.className || ""}`}
    >
      {loading ? (
        <LoaderCircle size={16} className="spin" />
      ) : (
        Icon && <Icon size={16} />
      )}
      {children}
    </button>
  );
}
export function IconButton({
  icon: Icon,
  label,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  icon: LucideIcon;
  label: string;
}) {
  return (
    <button
      type="button"
      {...props}
      className={`icon-button ${props.className || ""}`}
      aria-label={label}
      title={label}
    >
      <Icon size={17} />
    </button>
  );
}
export function PageHeader({
  eyebrow,
  title,
  subtitle,
  children,
}: {
  eyebrow?: string;
  title: string;
  subtitle?: string;
  children?: ReactNode;
}) {
  return (
    <div className="page-header">
      <div>
        {eyebrow && <div className="eyebrow">{eyebrow}</div>}
        <h1>{title}</h1>
        {subtitle && <p>{subtitle}</p>}
      </div>
      <div className="page-actions">{children}</div>
    </div>
  );
}
export function Status({ status }: { status: string }) {
  const labels: Record<string, string> = {
    ready: "可用",
    pending: "分析中",
    failed: "分析失败",
    active: "进行中",
    paused: "已暂停",
    archived: "已归档",
    available: "未领取",
    mine: "我已领取",
    claimed: "已领取",
    disabled: "已停用",
    enabled: "正常",
  };
  return (
    <span className={`status ${status}`}>
      <i />
      {labels[status] || status}
    </span>
  );
}
export function Empty({
  title = "暂无数据",
  description,
  action,
  icon: Icon = CircleDashed,
}: {
  title?: string;
  description?: string;
  action?: ReactNode;
  icon?: LucideIcon;
}) {
  return (
    <div className="empty-state">
      <Icon size={32} strokeWidth={1.25} />
      <h3>{title}</h3>
      {description && <p>{description}</p>}
      {action}
    </div>
  );
}
export function Loading() {
  return (
    <div className="loading-state">
      <LoaderCircle size={22} className="spin" />
      <span>正在加载</span>
    </div>
  );
}
export function ErrorState({
  error,
  retry,
}: {
  error: unknown;
  retry?: () => void;
}) {
  return (
    <div className="error-state">
      <AlertCircle size={24} />
      <div>
        <strong>加载失败</strong>
        <p>{error instanceof Error ? error.message : "请稍后重试"}</p>
      </div>
      {retry && (
        <Button variant="secondary" onClick={retry}>
          重试
        </Button>
      )}
    </div>
  );
}
export function Drawer({
  title,
  subtitle,
  children,
  onClose,
  wide = false,
}: {
  title: string;
  subtitle?: string;
  children: ReactNode;
  onClose: () => void;
  wide?: boolean;
}) {
  const dialog = useRef<HTMLElement>(null);
  const close = useRef(onClose);
  close.current = onClose;
  useEffect(() => {
    const old = document.body.style.overflow;
    const previous = document.activeElement as HTMLElement | null;
    document.body.style.overflow = "hidden";
    const targets = () =>
      Array.from(
        dialog.current?.querySelectorAll<HTMLElement>(
          'button:not([disabled]), a[href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex="0"]',
        ) || [],
      ).filter((el) => el.getClientRects().length > 0);
    const initial =
      dialog.current?.querySelector<HTMLElement>(
        "input:not([disabled]), select:not([disabled]), textarea:not([disabled])",
      ) || targets()[0];
    initial?.focus();
    const key = (e: KeyboardEvent) => {
      const dialogs = document.querySelectorAll('[aria-modal="true"]');
      if (dialogs[dialogs.length - 1] !== dialog.current) return;
      if (e.key === "Escape") close.current();
      if (e.key === "Tab") {
        const elements = targets();
        const first = elements[0];
        const last = elements[elements.length - 1];
        if (!first) {
          e.preventDefault();
          return;
        }
        if (
          e.shiftKey &&
          (document.activeElement === first ||
            !dialog.current?.contains(document.activeElement))
        ) {
          e.preventDefault();
          last.focus();
        } else if (
          !e.shiftKey &&
          (document.activeElement === last ||
            !dialog.current?.contains(document.activeElement))
        ) {
          e.preventDefault();
          first.focus();
        }
      }
    };
    document.addEventListener("keydown", key);
    return () => {
      document.body.style.overflow = old;
      document.removeEventListener("keydown", key);
      previous?.focus();
    };
  }, []);
  return (
    <div
      className="overlay"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <section
        ref={dialog}
        className={`drawer ${wide ? "wide" : ""}`}
        role="dialog"
        aria-modal="true"
        aria-label={title}
      >
        <header>
          <div>
            <h2>{title}</h2>
            {subtitle && <p>{subtitle}</p>}
          </div>
          <IconButton icon={X} label="关闭" onClick={onClose} />
        </header>
        <div className="drawer-content">{children}</div>
      </section>
    </div>
  );
}
export function Confirm({
  title,
  description,
  onConfirm,
  onClose,
  loading,
  danger = false,
}: {
  title: string;
  description: string;
  onConfirm: () => void;
  onClose: () => void;
  loading?: boolean;
  danger?: boolean;
}) {
  return (
    <div className="overlay modal-overlay">
      <section className="confirm-modal" role="alertdialog" aria-modal="true">
        <h2>{title}</h2>
        <p>{description}</p>
        <div className="form-actions">
          <Button variant="secondary" onClick={onClose}>
            取消
          </Button>
          <Button
            variant={danger ? "danger" : "primary"}
            loading={loading}
            onClick={onConfirm}
          >
            确认
          </Button>
        </div>
      </section>
    </div>
  );
}
export function Field({
  label,
  hint,
  children,
  required,
}: {
  label: string;
  hint?: string;
  children: ReactNode;
  required?: boolean;
}) {
  const id = useId();
  function control(node: ReactNode): ReactNode {
    if (!isValidElement(node)) return node;
    const element = node as ReactElement<{ children?: ReactNode; id?: string; 'aria-label'?: string }>;
    if (typeof element.type === 'string' && ['input', 'select', 'textarea'].includes(element.type)) return cloneElement(element, { id, 'aria-label': label });
    if (element.props.children) return cloneElement(element, { children: Children.map(element.props.children, control) });
    return node;
  }
  return (
    <div className="field" role="group" aria-labelledby={`${id}-label`}>
      <label htmlFor={id} id={`${id}-label`} className="field-label">
        {label}
        {required && <b>*</b>}
      </label>
      {Children.map(children, control)}
      {hint && <span className="field-hint">{hint}</span>}
    </div>
  );
}
export function SearchInput({
  value,
  onChange,
  placeholder = "搜索",
}: {
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
}) {
  return (
    <div className="search-input">
      <Search size={17} />
      <input
        aria-label={placeholder}
        placeholder={placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
      {value && (
        <button onClick={() => onChange("")} aria-label="清空搜索">
          <X size={14} />
        </button>
      )}
    </div>
  );
}
export function CheckBox({
  checked,
  onChange,
  label,
}: {
  checked: boolean;
  onChange: () => void;
  label: string;
}) {
  return (
    <label className="checkbox-label">
      <input
        type="checkbox"
        checked={checked}
        onChange={onChange}
        aria-label={label}
      />
      <span className="checkbox-visual">{checked && <Check size={12} />}</span>
    </label>
  );
}
export function TechPicker({
  value,
  onChange,
  options,
}: {
  value: string[];
  onChange: (v: string[]) => void;
  options: string[];
}) {
  return (
    <div className="tech-picker">
      {options.map((item) => (
        <button
          type="button"
          className={value.includes(item) ? "selected" : ""}
          key={item}
          onClick={() =>
            onChange(
              value.includes(item)
                ? value.filter((v) => v !== item)
                : [...value, item],
            )
          }
        >
          {value.includes(item) && <Check size={12} />}
          <span>{item}</span>
        </button>
      ))}
    </div>
  );
}
export function Pagination({
  page,
  total,
  size = 20,
  onChange,
  allowJump = false,
  unit = "条",
}: {
  page: number;
  total: number;
  size?: number;
  onChange: (v: number) => void;
  allowJump?: boolean;
  unit?: string;
}) {
  const pages = Math.max(1, Math.ceil(total / size));
  const [jump, setJump] = useState(String(page));
  useEffect(() => setJump(String(page)), [page]);
  return (
    <div className="pagination">
      <span>共 {total.toLocaleString()} {unit}</span>
      <div>
        <IconButton
          icon={ChevronLeft}
          label="上一页"
          disabled={page === 1}
          onClick={() => onChange(page - 1)}
        />
        <span>
          {page} / {pages}
        </span>
        {allowJump && pages > 1 && <input
          className="pagination-jump" type="number" aria-label="跳转页码"
          min={1} max={pages} value={jump} onChange={e => setJump(e.target.value)}
          onKeyDown={e => {
            if (e.key === "Enter") {
              e.preventDefault();
              const value = Number(jump);
              if (Number.isInteger(value) && value >= 1 && value <= pages) onChange(value);
              else setJump(String(page));
            }
          }}
        />}
        <IconButton
          icon={ChevronRight}
          label="下一页"
          disabled={page >= pages}
          onClick={() => onChange(page + 1)}
        />
      </div>
    </div>
  );
}
export function LanguageBar({
  values = [],
  compact = false,
}: {
  values?: Language[];
  compact?: boolean;
}) {
  return (
    <div className={`language-meter ${compact ? "compact" : ""}`}>
      <div className="language-bar">
        {values.map((l) => (
          <span
            key={l.name}
            style={{
              width: `${l.percentage}%`,
              background: languageColor[l.name] || "#8b9492",
            }}
            title={`${l.name} ${l.percentage.toFixed(1)}%`}
          />
        ))}
      </div>
      <div className="language-legend">
        {values.slice(0, compact ? 2 : 8).map((l) => (
          <span key={l.name}>
            <i style={{ background: languageColor[l.name] || "#8b9492" }} />
            {l.name}
            <b>{l.percentage.toFixed(1)}%</b>
          </span>
        ))}
        {!values.length && <span className="muted">暂无语言分析</span>}
      </div>
    </div>
  );
}
export function Tabs({
  value,
  onChange,
  items,
}: {
  value: string;
  onChange: (v: string) => void;
  items: { value: string; label: string; count?: number }[];
}) {
  return (
    <div className="tabs" role="tablist">
      {items.map((item) => (
        <button
          type="button"
          key={item.value}
          role="tab"
          aria-selected={value === item.value}
          className={value === item.value ? "selected" : ""}
          onClick={() => onChange(item.value)}
        >
          {item.label}
          {item.count !== undefined && <span>{item.count}</span>}
        </button>
      ))}
    </div>
  );
}
export function Segmented({
  value,
  onChange,
  items,
}: {
  value: string;
  onChange: (v: string) => void;
  items: { value: string; icon: LucideIcon; label: string }[];
}) {
  return (
    <div className="segmented">
      {items.map(({ icon: Icon, ...item }) => (
        <button
          title={item.label}
          aria-label={item.label}
          aria-pressed={value === item.value}
          key={item.value}
          className={value === item.value ? "selected" : ""}
          onClick={() => onChange(item.value)}
        >
          <Icon size={17} />
        </button>
      ))}
    </div>
  );
}
export function Avatar({
  name,
  small = false,
}: {
  name?: string;
  small?: boolean;
}) {
  return (
    <span className={`avatar ${small ? "small" : ""}`}>
      {(name || "?").slice(0, 1).toUpperCase()}
    </span>
  );
}
