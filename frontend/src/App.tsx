import {
  createContext,
  useContext,
  useEffect,
  useState,
  type FormEvent,
} from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Link,
  NavLink,
  Navigate,
  Route,
  Routes,
  useLocation,
} from "react-router-dom";
import {
  ArrowRight,
  Boxes,
  ChevronDown,
  Code2,
  FolderKanban,
  KeyRound,
  LayoutDashboard,
  LogOut,
  Menu,
  ShieldCheck,
  Terminal,
  UserRound,
  Users,
  X,
  History,
  Eye,
  EyeOff,
  DatabaseBackup,
  Tags,
} from "lucide-react";
import { ApiError, api, post, setCSRF, type User } from "./api";
import {
  Avatar,
  Button,
  Drawer,
  ErrorState,
  Field,
  IconButton,
  Loading,
  ToastProvider,
  useToast,
} from "./ui";
import { EngineeringPage } from "./pages/Engineering";
import { ProjectPage, ProjectDetail } from "./pages/Projects";
import { BackupsPage } from './pages/Backups';
import { TagsPage } from "./pages/Tags";
import {
  OverviewPage,
  UsersPage,
  TokensPage,
  AuditPage,
} from "./pages/Administration";

const AuthContext = createContext<{ user: User; logout: () => Promise<void> }>(
  null!,
);
export const useAuth = () => useContext(AuthContext);

export default function App() {
  return (
    <ToastProvider>
      <Authentication />
    </ToastProvider>
  );
}
function Authentication() {
  const client = useQueryClient();
  const me = useQuery({
    queryKey: ["auth"],
    queryFn: () => api<{ user: User; csrf: string }>("/api/v1/auth/me"),
    retry: false,
    staleTime: Infinity,
  });
  useEffect(() => {
    if (me.data) setCSRF(me.data.csrf);
  }, [me.data]);
  useEffect(() => {
    const expire = () => {
      void client.cancelQueries();
      client.setQueryData(["auth"], null);
      client.removeQueries({ predicate: (q) => q.queryKey[0] !== "auth" });
    };
    window.addEventListener("session-expired", expire);
    return () => window.removeEventListener("session-expired", expire);
  }, [client]);
  if (me.isPending)
    return (
      <div className="app-boot">
        <Logo />
        <Loading />
      </div>
    );
  if (me.isError && !(me.error instanceof ApiError && me.error.status === 401)) return <div className="app-boot"><Logo /><ErrorState error={me.error} retry={() => me.refetch()} /></div>;
  if (!me.data)
    return (
      <Login
        onSuccess={(data) => {
          setCSRF(data.csrf);
          client.setQueryData(["auth"], data);
        }}
      />
    );
  const logout = async () => {
    try {
      await post("/api/v1/auth/logout");
    } catch (error) {
      if (!(error instanceof ApiError && error.status === 401)) throw error;
    } finally {
      await client.cancelQueries();
      client.setQueryData(["auth"], null);
      client.removeQueries({ predicate: (q) => q.queryKey[0] !== "auth" });
      setCSRF('');
    }
  };
  return (
    <AuthContext.Provider value={{ user: me.data.user, logout }}>
      <Layout />
    </AuthContext.Provider>
  );
}
function Logo({ small = false }: { small?: boolean }) {
  return (
    <div className={`brand ${small ? "small" : ""}`}>
      <span className="brand-mark">
        <Code2 size={small ? 21 : 25} strokeWidth={2.2} />
      </span>
      <span>
        <b>工程台</b>
        <small>PROJECT HUB</small>
      </span>
    </div>
  );
}
function Login({
  onSuccess,
}: {
  onSuccess: (v: { user: User; csrf: string }) => void;
}) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [show, setShow] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  async function submit(e: FormEvent) {
    e.preventDefault();
    setLoading(true);
    setError("");
    try {
      onSuccess(await post("/api/v1/auth/login", { username, password }));
    } catch (err) {
      setError(err instanceof Error ? err.message : "登录失败");
    } finally {
      setLoading(false);
    }
  }
  return (
    <div className="login-page">
      <div className="login-top">
        <Logo />
        <span className="login-label">
          <ShieldCheck size={15} /> 团队代码资产管理
        </span>
      </div>
      <main className="login-main">
        <div className="login-side">
          <span className="login-index">01 / WORKSPACE</span>
          <h1>
            工程台
            <span>
              让代码资产
              <br />
              有序流转。
            </span>
          </h1>
          <div className="login-line" />
          <div className="login-domain">
            <Boxes size={22} />
            <span>工程池</span>
            <ArrowRight size={16} />
            <FolderKanban size={22} />
            <span>项目</span>
            <ArrowRight size={16} />
            <Users size={22} />
            <span>成员</span>
          </div>
        </div>
        <section className="login-form">
          <div className="login-form-heading">
            <span className="eyebrow">欢迎回来</span>
            <h2>登录工作空间</h2>
            <p>使用管理员为你创建的账号</p>
          </div>
          <form onSubmit={submit}>
            <Field label="用户名" required>
              <input
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                autoComplete="username"
                placeholder="输入用户名"
                required
                autoFocus
              />
            </Field>
            <Field label="密码" required>
              <div className="password-input">
                <input
                  type={show ? "text" : "password"}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  autoComplete="current-password"
                  placeholder="输入密码"
                  required
                />
                <IconButton
                  type="button"
                  icon={show ? EyeOff : Eye}
                  label={show ? "隐藏密码" : "显示密码"}
                  onClick={() => setShow(!show)}
                />
              </div>
            </Field>
            {error && (
              <div className="inline-error" role="alert">
                {error}
              </div>
            )}
            <Button
              type="submit"
              loading={loading}
              className="login-submit"
              icon={ArrowRight}
            >
              登录
            </Button>
          </form>
          <div className="login-security">
            <ShieldCheck size={15} />
            <span>账号权限由项目管理员维护</span>
          </div>
        </section>
      </main>
      <footer className="login-footer">
        <span>工程台 / Project Hub</span>
        <span>CODE · ORGANIZE · COLLABORATE</span>
      </footer>
    </div>
  );
}
function Layout() {
  const { user, logout } = useAuth();
  const toast = useToast();
  const location = useLocation();
  const [mobile, setMobile] = useState(false);
  const [account, setAccount] = useState(false);
  const [profile, setProfile] = useState(false);
  useEffect(() => {
    setMobile(false);
    setAccount(false);
  }, [location.pathname]);
  const admin = user.role === "admin";
  const nav = admin
    ? [
        { to: "/", label: "工作总览", icon: LayoutDashboard },
        { to: "/engineering", label: "工程池", icon: Boxes },
        { to: "/projects", label: "项目管理", icon: FolderKanban },
        { to: "/users", label: "用户维护", icon: Users },
      ]
    : [{ to: "/", label: "我的项目", icon: FolderKanban }];
  const secondary = admin
    ? [
        { to: "/tags", label: "标签管理", icon: Tags },
        { to: "/tokens", label: "API 与令牌", icon: KeyRound },
        { to: "/backups", label: "GitHub 灾备", icon: DatabaseBackup },
        { to: "/audit", label: "操作记录", icon: History },
      ]
    : [{ to: "/tokens", label: "API 与令牌", icon: KeyRound }];
  const current = [...nav, ...secondary].find(
    (n) => n.to === location.pathname,
  ) || { label: "项目详情" };
  return (
    <div className="app-layout">
      {mobile && (
        <div className="sidebar-backdrop" onClick={() => setMobile(false)} />
      )}
      <aside className={`sidebar ${mobile ? "open" : ""}`}>
        <Link to="/" className="brand-link">
          <Logo small />
        </Link>
        <div className="workspace-label">
          <span className="workspace-dot" />
          <span>团队工作空间</span>
          <span className="role-tag">{admin ? "管理员" : "成员"}</span>
        </div>
        <nav>
          <div className="nav-section-label">工作空间</div>
          {nav.map(({ icon: Icon, ...item }) => (
            <NavLink
              end={item.to === "/"}
              to={item.to}
              key={item.to}
              className={({ isActive }) =>
                `nav-item ${isActive ? "active" : ""}`
              }
            >
              <Icon size={18} />
              <span>{item.label}</span>
            </NavLink>
          ))}
          <div className="nav-section-label second">管理与接入</div>
          {secondary.map(({ icon: Icon, ...item }) => (
            <NavLink
              to={item.to}
              key={item.to}
              className={({ isActive }) =>
                `nav-item ${isActive ? "active" : ""}`
              }
            >
              <Icon size={18} />
              <span>{item.label}</span>
            </NavLink>
          ))}
        </nav>
        <div className="sidebar-bottom">
          <div className="workspace-status">
            <span />
            <span>代码资产工作台</span>
            <Terminal size={15} />
          </div>
          <button className="sidebar-user" onClick={() => setProfile(true)}>
            <Avatar name={user.name} />
            <span>
              <b>{user.name}</b>
              <small>@{user.username}</small>
            </span>
            <ChevronDown size={15} />
          </button>
        </div>
      </aside>
      <div className="main-shell">
        <header className="topbar">
          <div className="breadcrumb">
            <IconButton
              icon={mobile ? X : Menu}
              label="切换导航"
              className="mobile-menu"
              onClick={() => setMobile(!mobile)}
            />
            <span>工作空间</span>
            <span className="breadcrumb-sep">/</span>
            <b>{current.label}</b>
          </div>
          <div className="topbar-right">
            <span className="topbar-date">
              {new Intl.DateTimeFormat("zh-CN", {
                month: "long",
                day: "numeric",
                weekday: "long",
              }).format(new Date())}
            </span>
            <div className="account-wrap">
              <button
                className="account-trigger"
                onClick={() => setAccount(!account)}
              >
                <Avatar name={user.name} small />
                <span>{user.name}</span>
                <ChevronDown size={13} />
              </button>
              {account && (
                <div className="account-menu">
                  <button
                    onClick={() => {
                      setAccount(false);
                      setProfile(true);
                    }}
                  >
                    <UserRound size={16} />
                    账号设置
                  </button>
                  <button
                    onClick={() =>
                      logout().catch((e) => toast(e.message, "error"))
                    }
                  >
                    <LogOut size={16} />
                    退出登录
                  </button>
                </div>
              )}
            </div>
          </div>
        </header>
        <main className="page-content">
          <Routes>
            <Route
              path="/"
              element={admin ? <OverviewPage /> : <ProjectPage member />}
            />
            <Route
              path="/engineering"
              element={admin ? <EngineeringPage /> : <Navigate to="/" />}
            />
            <Route
              path="/projects"
              element={admin ? <ProjectPage /> : <ProjectPage member />}
            />
            <Route path="/projects/:id" element={<ProjectDetail />} />
            <Route
              path="/users"
              element={admin ? <UsersPage /> : <Navigate to="/" />}
            />
            <Route
              path="/tags"
              element={admin ? <TagsPage /> : <Navigate to="/" />}
            />
            <Route path="/tokens" element={<TokensPage />} />
            <Route path="/backups" element={admin ? <BackupsPage /> : <Navigate to="/" />} />
            <Route
              path="/audit"
              element={admin ? <AuditPage /> : <Navigate to="/" />}
            />
            <Route path="*" element={<Navigate to="/" />} />
          </Routes>
        </main>
        <footer className="app-footer">
          <span>工程台</span>
          <span>PROJECT HUB / 代码资产与协作</span>
        </footer>
      </div>
      {profile && <Profile onClose={() => setProfile(false)} />}
    </div>
  );
}
function Profile({ onClose }: { onClose: () => void }) {
  const { user, logout } = useAuth();
  const toast = useToast();
  const [current, setCurrent] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      await post("/api/v1/auth/password", {
        current_password: current,
        password,
      });
      toast("密码已更新，请重新登录");
      await logout();
    } catch (err) {
      toast(err instanceof Error ? err.message : "更新失败", "error");
    } finally {
      setBusy(false);
    }
  }
  return (
    <Drawer title="账号设置" onClose={onClose}>
      <div className="profile-summary">
        <Avatar name={user.name} />
        <div>
          <h3>{user.name}</h3>
          <p>
            @{user.username} · {user.role === "admin" ? "管理员" : "成员"}
          </p>
        </div>
      </div>
      <form onSubmit={submit}>
        <h3 className="form-section-title">修改密码</h3>
        <Field label="当前密码">
          <input
            type="password"
            autoComplete="current-password"
            value={current}
            onChange={(e) => setCurrent(e.target.value)}
            required
          />
        </Field>
        <Field label="新密码" hint="至少 8 个字符">
          <input
            type="password"
            minLength={8}
            autoComplete="new-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
        </Field>
        <div className="form-actions">
          <Button type="submit" loading={busy} icon={ShieldCheck}>
            更新密码
          </Button>
        </div>
      </form>
    </Drawer>
  );
}
