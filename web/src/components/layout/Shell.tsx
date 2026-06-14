import type { ReactNode } from "react";

type Props = {
  adminToken: string;
  hasSession: boolean;
  children: ReactNode;
  onAdminTokenChange: (value: string) => void;
  onLogin: () => void;
  onLogout: () => void;
};

export function Shell({
  adminToken,
  hasSession,
  children,
  onAdminTokenChange,
  onLogin,
  onLogout,
}: Props) {
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <h1 className="brand">mini-cloud</h1>
        <nav className="nav">
          <a href="#overview">概览</a>
          <a href="#planes">cloud planes</a>
          <a href="#services">services</a>
          <a href="#detail">detail</a>
        </nav>
        <div className="auth-panel">
          <label>
            <span>Admin token</span>
            <input
              type="password"
              value={adminToken}
              onChange={(event) => onAdminTokenChange(event.target.value)}
              placeholder="Bearer token"
            />
          </label>
          <div className="button-row">
            <button className="inline-button" type="button" onClick={onLogin}>
              登录
            </button>
            <button
              className="inline-button"
              type="button"
              onClick={onLogout}
              disabled={!hasSession && adminToken.trim() === ""}
            >
              退出
            </button>
          </div>
        </div>
      </aside>

      <main className="content">
        <section className="hero">
          <p className="eyebrow">control-plane</p>
          <h1>CaaS 运维门户</h1>
          <p>
            通过 control-plane 创建 service、选择 cloud-plane、维护全局入口；
            运行态由目标 cloud-plane 自治闭环。
          </p>
        </section>
        {children}
      </main>
    </div>
  );
}
