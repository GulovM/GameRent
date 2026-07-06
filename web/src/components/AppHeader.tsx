import { Clock3, CreditCard, Gamepad2, LayoutDashboard, LogIn, LogOut, Menu, ShieldCheck, UserRound, X } from "lucide-react";
import { useState } from "react";
import type { ReactNode } from "react";
import type { User } from "../api";
import type { View } from "../types/app";

type AppHeaderProps = {
  adminMode: boolean;
  onLogin: () => void;
  onLogout: () => void;
  setView: (view: View) => void;
  user: User | null;
  view: View;
};

export function AppHeader({ adminMode, onLogin, onLogout, setView, user, view }: AppHeaderProps) {
  const [mobileOpen, setMobileOpen] = useState(false);
  const items: Array<{ id: View; label: string; icon: ReactNode }> = adminMode
    ? [{ id: "admin", label: "Админ-панель", icon: <LayoutDashboard size={18} /> }]
    : [
        { id: "catalog", label: "Каталог", icon: <Gamepad2 size={18} /> },
        { id: "rentals", label: "Аренды", icon: <Clock3 size={18} /> },
        { id: "payments", label: "Платежи", icon: <CreditCard size={18} /> },
        { id: "profile", label: "Профиль", icon: <UserRound size={18} /> }
      ];

  function navigate(nextView: View) {
    setView(nextView);
    setMobileOpen(false);
  }

  function openLogin() {
    setMobileOpen(false);
    onLogin();
  }

  function logout() {
    setMobileOpen(false);
    onLogout();
  }

  return (
    <header className="topbar">
      <button className="brand" onClick={() => navigate(adminMode ? "admin" : "catalog")} type="button">
        <span className="brand-mark">
          <ShieldCheck size={22} />
        </span>
        <span>GameRent</span>
      </button>
      <nav className="desktop-nav">
        {items.map((item) => (
          <button className={view === item.id ? "active" : ""} key={item.id} onClick={() => navigate(item.id)} type="button">
            {item.icon}
            {item.label}
          </button>
        ))}
      </nav>
      <div className="topbar-actions">
        {user ? (
          <>
            <span className="user-chip">{user.first_name || user.email}</span>
            <button className="ghost icon-button" onClick={logout} title="Выйти" type="button">
              <LogOut size={18} />
            </button>
          </>
        ) : (
          <button className="secondary-button auth-button" onClick={openLogin} type="button">
            <LogIn size={18} />
            <span>Войти</span>
          </button>
        )}
        <button
          className="ghost icon-button menu-button"
          aria-expanded={mobileOpen}
          aria-label="Открыть меню"
          onClick={() => setMobileOpen((value) => !value)}
          title="Меню"
          type="button"
        >
          {mobileOpen ? <X size={20} /> : <Menu size={20} />}
        </button>
      </div>
      {mobileOpen && (
        <div className="mobile-menu" role="menu">
          {items.map((item) => (
            <button className={view === item.id ? "active" : ""} key={item.id} onClick={() => navigate(item.id)} type="button">
              {item.icon}
              {item.label}
            </button>
          ))}
          {user ? (
            <button onClick={logout} type="button">
              <LogOut size={18} />
              Выйти
            </button>
          ) : (
            <button className="mobile-login-action" onClick={openLogin} type="button">
              <LogIn size={18} />
              Войти в аккаунт
            </button>
          )}
        </div>
      )}
    </header>
  );
}
