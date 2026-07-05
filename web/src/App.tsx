import {
  Activity,
  Bell,
  Check,
  ChevronRight,
  Clock3,
  CreditCard,
  DatabaseZap,
  Edit3,
  Gamepad2,
  Gauge,
  LayoutDashboard,
  LibraryBig,
  Lock,
  LogIn,
  LogOut,
  Menu,
  Plus,
  RefreshCcw,
  Search,
  ShieldCheck,
  SlidersHorizontal,
  Sparkles,
  ToggleLeft,
  ToggleRight,
  UserCog,
  UserRound,
  UsersRound,
  X,
  Zap
} from "lucide-react";
import { FormEvent, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import {
  Account,
  api,
  AuditLog,
  clearTokens,
  getAccessToken,
  getRefreshToken,
  NotificationItem,
  Payment,
  Rental,
  saveTokens,
  User
} from "./api";

type View = "catalog" | "rentals" | "payments" | "profile" | "admin";
type AdminTab = "overview" | "accounts" | "users" | "audit";
type Toast = { type: "ok" | "error"; message: string } | null;

type AdminAccountPatch = {
  status?: number;
  price_per_hour?: number;
  security_deposit?: number;
};

const accountStatusLabels: Record<string, string> = {
  Created: "Создан",
  Verifying: "Проверка",
  Available: "Доступен",
  Reserved: "Резерв",
  Rented: "В аренде",
  Maintenance: "Обслуживание",
  Disabled: "Отключен"
};

const accountStatusNumbers: Record<string, number> = {
  Created: 0,
  Verifying: 1,
  Available: 2,
  Reserved: 3,
  Rented: 4,
  Maintenance: 5,
  Disabled: 6
};

const rentalStatusLabels: Record<number, string> = {
  0: "Создана",
  1: "Ожидает оплаты",
  2: "Активна",
  3: "Истекла",
  4: "Завершена",
  5: "Отменена"
};

function statusFromNumber(value: number) {
  return ["Created", "Verifying", "Available", "Reserved", "Rented", "Maintenance", "Disabled"][value] ?? "Unknown";
}

function money(value?: { amount: number; currency: string } | number, fallbackCurrency = "USD") {
  if (typeof value === "number") return `${value} ${fallbackCurrency}`;
  if (!value) return `0 ${fallbackCurrency}`;
  return `${value.amount} ${value.currency}`;
}

function asList<T>(value: T[] | null | undefined): T[] {
  return Array.isArray(value) ? value : [];
}

function normalizeAccount(account: Account): Account {
  return {
    ...account,
    games: asList(account.games)
  };
}

function gameNames(account: Account, limit = 3) {
  const names = asList(account.games).map((game) => game.name);
  return names.slice(0, limit).join(", ") || "Библиотека Steam";
}

function remaining(expiresAt: string) {
  const ms = new Date(expiresAt).getTime() - Date.now();
  if (Number.isNaN(ms) || ms <= 0) return "00:00:00";
  const totalSeconds = Math.floor(ms / 1000);
  const h = Math.floor(totalSeconds / 3600).toString().padStart(2, "0");
  const m = Math.floor((totalSeconds % 3600) / 60).toString().padStart(2, "0");
  const s = Math.floor(totalSeconds % 60).toString().padStart(2, "0");
  return `${h}:${m}:${s}`;
}

function isAdmin(user: User | null) {
  return user?.role === "ADMIN";
}

function useTicker() {
  const [, setTick] = useState(0);
  useEffect(() => {
    const id = window.setInterval(() => setTick((value) => value + 1), 1000);
    return () => window.clearInterval(id);
  }, []);
}

export default function App() {
  useTicker();
  const [view, setView] = useState<View>(() => {
    const hash = window.location.hash.replace("#", "") as View;
    return ["catalog", "rentals", "payments", "profile", "admin"].includes(hash) ? hash : "catalog";
  });
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [rentals, setRentals] = useState<Rental[]>([]);
  const [payments, setPayments] = useState<Payment[]>([]);
  const [notifications, setNotifications] = useState<NotificationItem[]>([]);
  const [auditLogs, setAuditLogs] = useState<AuditLog[]>([]);
  const [adminUsers, setAdminUsers] = useState<User[]>([]);
  const [user, setUser] = useState<User | null>(null);
  const [selectedAccount, setSelectedAccount] = useState<Account | null>(null);
  const [duration, setDuration] = useState(2);
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("Available");
  const [maxPrice, setMaxPrice] = useState(300);
  const [loading, setLoading] = useState(false);
  const [authOpen, setAuthOpen] = useState(false);
  const [toast, setToast] = useState<Toast>(null);
  const [backendError, setBackendError] = useState<string | null>(null);

  useEffect(() => {
    window.location.hash = view;
  }, [view]);

  useEffect(() => {
    void bootstrap();
  }, []);

  useEffect(() => {
    if (!toast) return;
    const id = window.setTimeout(() => setToast(null), 3200);
    return () => window.clearTimeout(id);
  }, [toast]);

  async function bootstrap() {
    setLoading(true);
    setBackendError(null);
    try {
      const [accountsRes] = await Promise.all([api.accounts({ page: 1, page_size: 40 }), api.games({ page: 1, page_size: 12 })]);
      setAccounts(asList(accountsRes.accounts).map(normalizeAccount));
    } catch (error) {
      setAccounts([]);
      setBackendError(error instanceof Error ? error.message : "Не удалось получить публичные данные");
    } finally {
      if (getAccessToken()) {
        await loadPrivateData();
      }
      setLoading(false);
    }
  }

  async function loadPrivateData() {
    setBackendError(null);
    try {
      const [meRes, rentalsRes, paymentsRes, notificationsRes] = await Promise.all([
        api.me(),
        api.rentals(),
        api.payments(),
        api.notifications()
      ]);
      setUser(meRes);
      setRentals(asList(rentalsRes.rentals));
      setPayments(asList(paymentsRes.payments));
      setNotifications(asList(notificationsRes.notifications));

      if (meRes.role === "ADMIN") {
        setView("admin");
        const [adminAccountsRes, usersRes, auditRes, publicAccountsRes] = await Promise.all([
          api.adminAccounts(),
          api.adminUsers(),
          api.adminAuditLogs(),
          api.accounts({ page: 1, page_size: 40 })
        ]);
        setAdminUsers(asList(usersRes.users));
        setAuditLogs(asList(auditRes.audit_logs));
        const publicAccounts = new Map(asList(publicAccountsRes.accounts).map((account) => [account.id, normalizeAccount(account)]));
        setAccounts(
          asList(adminAccountsRes.accounts).map((account) => {
            const existing = publicAccounts.get(account.id);
            return normalizeAccount({
              id: account.id,
              steam_id64: account.steam_id64,
              status: statusFromNumber(account.status),
              price_per_hour: { amount: account.hourly_price, currency: "USD" },
              security_deposit: { amount: account.deposit_amount, currency: "USD" },
              games: existing?.games ?? []
            });
          })
        );
      }
    } catch (error) {
      setRentals([]);
      setPayments([]);
      setNotifications([]);
      setAuditLogs([]);
      setAdminUsers([]);
      setBackendError(error instanceof Error ? error.message : "Не удалось получить приватные данные");
    }
  }

  const filteredAccounts = useMemo(() => {
    const q = search.trim().toLowerCase();
    return accounts.filter((account) => {
      const matchesSearch =
        !q ||
        account.steam_id64.includes(q) ||
        asList(account.games).some((game) => game.name.toLowerCase().includes(q));
      const matchesStatus = status === "All" || account.status === status;
      const matchesPrice = account.price_per_hour.amount <= maxPrice;
      return matchesSearch && matchesStatus && matchesPrice;
    });
  }, [accounts, maxPrice, search, status]);

  const activeRental = rentals.find((item) => item.status === 2) ?? rentals[0];

  async function handleRent(account: Account) {
    if (!getAccessToken()) {
      setSelectedAccount(account);
      setAuthOpen(true);
      setToast({ type: "error", message: "Войдите, чтобы начать аренду" });
      return;
    }

    setLoading(true);
    try {
      const rental = await api.createRental({ account_id: account.id, duration_hours: duration });
      setRentals((current) => [rental, ...current]);
      setAccounts((current) => current.map((item) => (item.id === account.id ? { ...item, status: "Rented" } : item)));
      setSelectedAccount(null);
      setView("rentals");
      setToast({ type: "ok", message: "Аренда создана" });
    } catch (error) {
      setToast({ type: "error", message: error instanceof Error ? error.message : "Не удалось создать аренду" });
    } finally {
      setLoading(false);
    }
  }

  async function handleExtend(rental: Rental) {
    try {
      await api.extendRental(rental.id, 1);
      setRentals((current) =>
        current.map((item) =>
          item.id === rental.id
            ? { ...item, expires_at: new Date(new Date(item.expires_at).getTime() + 3600000).toISOString() }
            : item
        )
      );
      setToast({ type: "ok", message: "Аренда продлена на 1 час" });
    } catch (error) {
      setToast({ type: "error", message: error instanceof Error ? error.message : "Не удалось продлить аренду" });
    }
  }

  async function handleCancel(rental: Rental) {
    try {
      await api.cancelRental(rental.id);
      setRentals((current) => current.map((item) => (item.id === rental.id ? { ...item, status: 5 } : item)));
      setToast({ type: "ok", message: "Аренда отменена" });
    } catch (error) {
      setToast({ type: "error", message: error instanceof Error ? error.message : "Не удалось отменить аренду" });
    }
  }

  async function handleFavorite(account: Account) {
    if (!getAccessToken()) {
      setAuthOpen(true);
      setToast({ type: "error", message: "Войдите, чтобы добавить аккаунт в избранное" });
      return;
    }
    try {
      await api.favoriteAccount(account.id);
      setToast({ type: "ok", message: "Аккаунт добавлен в избранное" });
    } catch (error) {
      setToast({ type: "error", message: error instanceof Error ? error.message : "Не удалось добавить в избранное" });
    }
  }

  async function handleReadNotification(item: NotificationItem) {
    try {
      await api.readNotification(item.id);
      setNotifications((current) => current.map((notification) => (notification.id === item.id ? { ...notification, read: true } : notification)));
    } catch (error) {
      setToast({ type: "error", message: error instanceof Error ? error.message : "Не удалось отметить уведомление" });
    }
  }

  async function handleToggleAccount(account: Account) {
    const enable = account.status === "Disabled";
    const nextStatus = enable ? 2 : 6;
    try {
      await api.adminUpdateAccount(account.id, { status: nextStatus });
      setAccounts((current) => current.map((item) => (item.id === account.id ? { ...item, status: statusFromNumber(nextStatus) } : item)));
      setToast({ type: "ok", message: enable ? "Аккаунт включен" : "Аккаунт отключен" });
    } catch (error) {
      setToast({ type: "error", message: error instanceof Error ? error.message : "Не удалось изменить статус аккаунта" });
      throw error;
    }
  }

  async function handleSyncAccount(account: Account) {
    setLoading(true);
    try {
      const result = await api.adminSyncAccount(account.id);
      await loadPrivateData();
      setAuditLogs((current) => [
        {
          id: Date.now(),
          actor_user_id: user?.id ?? null,
          entity_type: "account",
          entity_id: account.id,
          action: "ADMIN_SYNC_ACCOUNT",
          created_at: new Date().toISOString()
        },
        ...current
      ]);
      setToast({ type: "ok", message: `Библиотека Steam синхронизирована: ${result.games_count} игр` });
    } catch (error) {
      setToast({ type: "error", message: error instanceof Error ? error.message : "Не удалось синхронизировать" });
      throw error;
    } finally {
      setLoading(false);
    }
  }

  async function handleCreateAccount(payload: {
    steam_id64: string;
    steam_login: string;
    steam_password: string;
    price_per_hour: number;
    security_deposit: number;
  }) {
    setLoading(true);
    try {
      const created = await api.adminCreateAccount(payload);
      const account: Account = {
        id: created.id,
        steam_id64: payload.steam_id64,
        status: "Available",
        price_per_hour: { amount: payload.price_per_hour, currency: "USD" },
        security_deposit: { amount: payload.security_deposit, currency: "USD" },
        games: []
      };
      setAccounts((current) => [account, ...current]);
      setToast({
        type: created.sync_error ? "error" : "ok",
        message: created.sync_error
          ? `Аккаунт добавлен, но Steam sync не завершился: ${created.sync_error}`
          : `Аккаунт добавлен, импортировано игр: ${created.games_count ?? 0}`
      });
      await loadPrivateData();
    } catch (error) {
      setToast({ type: "error", message: error instanceof Error ? error.message : "Не удалось добавить аккаунт" });
      throw error;
    } finally {
      setLoading(false);
    }
  }

  async function handleUpdateAccount(account: Account, patch: AdminAccountPatch) {
    setLoading(true);
    try {
      await api.adminUpdateAccount(account.id, patch);
      setAccounts((current) =>
        current.map((item) =>
          item.id === account.id
            ? {
                ...item,
                status: patch.status === undefined ? item.status : statusFromNumber(patch.status),
                price_per_hour:
                  patch.price_per_hour === undefined ? item.price_per_hour : { ...item.price_per_hour, amount: patch.price_per_hour },
                security_deposit:
                  patch.security_deposit === undefined ? item.security_deposit : { ...item.security_deposit, amount: patch.security_deposit }
              }
            : item
        )
      );
      setToast({ type: "ok", message: "Аккаунт обновлен" });
      await loadPrivateData();
    } catch (error) {
      setToast({ type: "error", message: error instanceof Error ? error.message : "Не удалось обновить аккаунт" });
      throw error;
    } finally {
      setLoading(false);
    }
  }

  async function handleUpdateAdminUser(targetUser: User, patch: { trust_score?: number; is_blocked?: boolean; balance?: number; role?: string }) {
    setLoading(true);
    try {
      await api.adminUpdateUser(targetUser.id, patch);
      setAdminUsers((current) => current.map((item) => (item.id === targetUser.id ? { ...item, ...patch } : item)));
      if (user?.id === targetUser.id) {
        setUser((current) => (current ? { ...current, ...patch } : current));
      }
      setToast({ type: "ok", message: "Пользователь обновлен" });
      await loadPrivateData();
    } catch (error) {
      setToast({ type: "error", message: error instanceof Error ? error.message : "Не удалось обновить пользователя" });
      throw error;
    } finally {
      setLoading(false);
    }
  }

  async function handleLogout() {
    const refresh = getRefreshToken();
    clearTokens();
    setUser(null);
    setRentals([]);
    setPayments([]);
    setNotifications([]);
    setAuditLogs([]);
    setAdminUsers([]);
    setView("catalog");
    if (refresh) {
      void api.logout(refresh).catch(() => undefined);
    }
    setToast({ type: "ok", message: "Сессия завершена" });
  }

  const effectiveView = isAdmin(user) ? "admin" : view;

  return (
    <div className="app-shell">
      <Header view={effectiveView} setView={setView} user={user} onLogin={() => setAuthOpen(true)} onLogout={handleLogout} />

      {backendError && (
        <div className="offline-banner">
          <DatabaseZap size={18} />
          <span>{backendError}</span>
        </div>
      )}

      <main>
        {effectiveView === "catalog" && (
          <CatalogView
            accounts={filteredAccounts}
            activeRental={activeRental}
            duration={duration}
            loading={loading}
            maxPrice={maxPrice}
            search={search}
            status={status}
            setDuration={setDuration}
            setMaxPrice={setMaxPrice}
            setSearch={setSearch}
            setStatus={setStatus}
            selectAccount={setSelectedAccount}
            onExtendActive={handleExtend}
            onOpenRentals={() => setView("rentals")}
          />
        )}
        {effectiveView === "rentals" && <RentalsView accounts={accounts} rentals={rentals} onCancel={handleCancel} onExtend={handleExtend} />}
        {effectiveView === "payments" && (
          <PaymentsView notifications={notifications} payments={payments} onReadNotification={handleReadNotification} />
        )}
        {effectiveView === "profile" && <ProfileView user={user} onLogin={() => setAuthOpen(true)} onUpdateUser={(next) => setUser(next)} />}
        {effectiveView === "admin" && (
          <AdminView
            accounts={accounts}
            auditLogs={auditLogs}
            users={adminUsers}
            user={user}
            onCreateAccount={handleCreateAccount}
            onSync={handleSyncAccount}
            onToggleAccount={handleToggleAccount}
            onUpdateAccount={handleUpdateAccount}
            onUpdateUser={handleUpdateAdminUser}
          />
        )}
      </main>

      {!isAdmin(user) && <MobileNav view={effectiveView} setView={setView} />}

      {selectedAccount && (
        <CheckoutDrawer
          account={selectedAccount}
          duration={duration}
          setDuration={setDuration}
          onClose={() => setSelectedAccount(null)}
          onFavorite={() => handleFavorite(selectedAccount)}
          onRent={() => handleRent(selectedAccount)}
        />
      )}

      {authOpen && (
        <AuthDialog
          onClose={() => setAuthOpen(false)}
          onAuthenticated={(nextUser) => {
            setUser(nextUser);
            setAuthOpen(false);
            setView(nextUser.role === "ADMIN" ? "admin" : "profile");
            void loadPrivateData();
          }}
          setToast={setToast}
        />
      )}

      {toast && <div className={`toast ${toast.type}`}>{toast.message}</div>}
    </div>
  );
}

function Header({
  view,
  setView,
  user,
  onLogin,
  onLogout
}: {
  view: View;
  setView: (view: View) => void;
  user: User | null;
  onLogin: () => void;
  onLogout: () => void;
}) {
  const [mobileOpen, setMobileOpen] = useState(false);
  const items: Array<{ id: View; label: string; icon: ReactNode }> = isAdmin(user)
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
      <button className="brand" onClick={() => navigate(isAdmin(user) ? "admin" : "catalog")} type="button">
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

function CatalogView({
  accounts,
  activeRental,
  duration,
  loading,
  maxPrice,
  search,
  status,
  setDuration,
  setMaxPrice,
  setSearch,
  setStatus,
  selectAccount,
  onExtendActive,
  onOpenRentals
}: {
  accounts: Account[];
  activeRental?: Rental;
  duration: number;
  loading: boolean;
  maxPrice: number;
  search: string;
  status: string;
  setDuration: (value: number) => void;
  setMaxPrice: (value: number) => void;
  setSearch: (value: string) => void;
  setStatus: (value: string) => void;
  selectAccount: (account: Account) => void;
  onExtendActive: (rental: Rental) => void;
  onOpenRentals: () => void;
}) {
  return (
    <>
      <section className="catalog-hero">
        <div className="hero-copy">
          <span className="eyebrow">Steam rental platform</span>
          <h1>Каталог игровых аккаунтов с быстрым доступом и контролем аренды</h1>
          <p>
            Выбирайте аккаунт по библиотеке, стоимости и статусу. Активная аренда, платежи и уведомления остаются в
            личном кабинете.
          </p>
          <div className="hero-actions">
            <a className="primary-button" href="#catalog">
              Открыть каталог
              <ChevronRight size={18} />
            </a>
            <button className="secondary-button" onClick={onOpenRentals} type="button">
              <Clock3 size={18} />
              Мои аренды
            </button>
          </div>
        </div>
        <LiveRentalCard rental={activeRental} onExtend={onExtendActive} onOpenRentals={onOpenRentals} />
      </section>

      <section className="catalog-section" id="catalog">
        <div className="section-heading">
          <div>
            <h2>Каталог аккаунтов</h2>
            <p>Поиск по SteamID и библиотеке, фильтр по статусу и бюджету.</p>
          </div>
          <DurationPicker duration={duration} setDuration={setDuration} />
        </div>

        <div className="filters">
          <label className="search-box">
            <Search size={20} />
            <input onChange={(event) => setSearch(event.target.value)} placeholder="Игра или SteamID" value={search} />
          </label>
          <div className="chip-row">
            {["All", "Available", "Rented", "Maintenance", "Disabled"].map((item) => (
              <button className={status === item ? "selected" : ""} key={item} onClick={() => setStatus(item)} type="button">
                {item === "All" ? "Все" : accountStatusLabels[item]}
              </button>
            ))}
          </div>
          <label className="range-control">
            <SlidersHorizontal size={18} />
            до {maxPrice} USD/ч
            <input max="500" min="10" onChange={(event) => setMaxPrice(Number(event.target.value))} step="10" type="range" value={maxPrice} />
          </label>
        </div>

        <div className={loading ? "account-grid loading" : "account-grid"}>
          {accounts.length > 0 ? (
            accounts.map((account) => <AccountCard account={account} key={account.id} onSelect={selectAccount} />)
          ) : (
            <div className="empty-inline">
              <LibraryBig size={28} />
              <strong>Аккаунтов по фильтрам нет</strong>
              <span>Измените запрос, статус или максимальную цену.</span>
            </div>
          )}
        </div>
      </section>
    </>
  );
}

function DurationPicker({ duration, setDuration }: { duration: number; setDuration: (value: number) => void }) {
  return (
    <div className="duration-picker" aria-label="Длительность аренды">
      {[1, 2, 4, 8].map((hours) => (
        <button className={duration === hours ? "selected" : ""} key={hours} onClick={() => setDuration(hours)} type="button">
          {hours} ч
        </button>
      ))}
    </div>
  );
}

function LiveRentalCard({
  rental,
  onExtend,
  onOpenRentals
}: {
  rental?: Rental;
  onExtend: (rental: Rental) => void;
  onOpenRentals: () => void;
}) {
  return (
    <aside className="live-card">
      <div className="live-top">
        <div>
          <span className="eyebrow">Активная сессия</span>
          <h2>{rental ? `Аренда #${rental.id}` : "Нет активной аренды"}</h2>
        </div>
        <span className={rental?.status === 2 ? "status-pill green" : "status-pill muted"}>
          {rental ? rentalStatusLabels[rental.status] ?? rental.status : "Пусто"}
        </span>
      </div>
      <strong className="timer">{rental ? remaining(rental.expires_at) : "00:00:00"}</strong>
      <div className="live-actions">
        <button className="primary-button" disabled={!rental || rental.status !== 2} onClick={() => rental && onExtend(rental)} type="button">
          <Zap size={18} />
          Продлить
        </button>
        <button className="secondary-button" onClick={onOpenRentals} type="button">
          <ShieldCheck size={18} />
          Детали
        </button>
      </div>
    </aside>
  );
}

function AccountCard({ account, onSelect }: { account: Account; onSelect: (account: Account) => void }) {
  const available = account.status === "Available";
  const games = asList(account.games);
  const firstGame = games[0];
  const visibleGames = games.slice(0, 8);
  const hiddenGamesCount = Math.max(games.length - visibleGames.length, 0);
  const statusClass = available ? "green" : account.status === "Rented" ? "amber" : account.status === "Disabled" ? "danger" : "muted";
  return (
    <article className="account-card">
      <button className="card-button" onClick={() => onSelect(account)} type="button">
        <div className="account-cover">
          <span>{firstGame?.name || "Steam Account"}</span>
          <Gamepad2 size={34} />
        </div>
        <div className="card-body">
          <div>
            <h3>{firstGame?.name ? `${firstGame.name} Pack` : `Account #${account.id}`}</h3>
            <p>{gameNames(account)}</p>
          </div>
          <div className="account-library" aria-label="Список игр аккаунта">
            <span className="account-library-title">Игры аккаунта</span>
            {visibleGames.length > 0 ? (
              <ul className="account-game-list">
                {visibleGames.map((game) => (
                  <li key={`${account.id}-${game.game_id}`}>{game.name}</li>
                ))}
                {hiddenGamesCount > 0 && <li className="more-games">+{hiddenGamesCount}</li>}
              </ul>
            ) : (
              <span className="empty-library">Библиотека не синхронизирована</span>
            )}
          </div>
          <span className={`status-pill ${statusClass}`}>{accountStatusLabels[account.status] ?? account.status}</span>
          <div className="price-row">
            <strong>{money(account.price_per_hour)}/ч</strong>
            <span>залог {money(account.security_deposit)}</span>
          </div>
        </div>
      </button>
    </article>
  );
}

function CheckoutDrawer({
  account,
  duration,
  setDuration,
  onClose,
  onFavorite,
  onRent
}: {
  account: Account;
  duration: number;
  setDuration: (value: number) => void;
  onClose: () => void;
  onFavorite: () => void;
  onRent: () => void;
}) {
  const total = account.price_per_hour.amount * duration + account.security_deposit.amount;
  return (
    <div className="drawer-backdrop" role="presentation">
      <aside className="checkout-drawer" aria-label="Оформление аренды">
        <button className="ghost icon-button close-button" onClick={onClose} title="Закрыть" type="button">
          <X size={20} />
        </button>
        <div className="drawer-title">
          <div>
            <span className="eyebrow">Checkout</span>
            <h2>Оформление аренды</h2>
            <p>{gameNames(account, 5)}</p>
          </div>
          <span className={account.status === "Available" ? "status-pill green" : "status-pill muted"}>
            {accountStatusLabels[account.status] ?? account.status}
          </span>
        </div>
        <DurationPicker duration={duration} setDuration={setDuration} />
        <dl className="summary-list">
          <div>
            <dt>Цена за час</dt>
            <dd>{money(account.price_per_hour)}</dd>
          </div>
          <div>
            <dt>Период</dt>
            <dd>{duration} ч</dd>
          </div>
          <div>
            <dt>Залог</dt>
            <dd>{money(account.security_deposit)}</dd>
          </div>
          <div>
            <dt>Итого</dt>
            <dd>
              {total} {account.price_per_hour.currency}
            </dd>
          </div>
        </dl>
        <div className="safety-note">
          <Lock size={20} />
          <span>Доступ к Steam-данным выдается только на время активной аренды.</span>
        </div>
        <button className="primary-button full" disabled={account.status !== "Available"} onClick={onRent} type="button">
          Оплатить и начать
        </button>
        <button className="secondary-button full" onClick={onFavorite} type="button">
          <Check size={18} />
          В избранное
        </button>
      </aside>
    </div>
  );
}

function RentalsView({
  accounts,
  rentals,
  onCancel,
  onExtend
}: {
  accounts: Account[];
  rentals: Rental[];
  onCancel: (rental: Rental) => void;
  onExtend: (rental: Rental) => void;
}) {
  return (
    <section className="workspace">
      <div className="section-heading">
        <div>
          <h2>Мои аренды</h2>
          <p>Активные сессии, история, продление и отмена.</p>
        </div>
      </div>
      <div className="rental-list">
        {rentals.length > 0 ? (
          rentals.map((rental) => {
            const account = accounts.find((item) => item.id === rental.account_id);
            return (
              <article className="rental-row" key={rental.id}>
                <div className="row-icon">
                  <Clock3 size={22} />
                </div>
                <div>
                  <h3>{account ? gameNames(account, 1) : `Аккаунт #${rental.account_id}`}</h3>
                  <p>
                    {rentalStatusLabels[rental.status] ?? rental.status} · до {remaining(rental.expires_at)}
                  </p>
                </div>
                <strong>{money(rental.total_price)}</strong>
                <div className="row-actions">
                  <button className="secondary-button" disabled={rental.status !== 2} onClick={() => onExtend(rental)} type="button">
                    Продлить
                  </button>
                  <button className="danger-button" disabled={rental.status > 2} onClick={() => onCancel(rental)} type="button">
                    Отменить
                  </button>
                </div>
              </article>
            );
          })
        ) : (
          <div className="empty-inline">
            <Clock3 size={28} />
            <strong>Аренд нет</strong>
            <span>После оформления аренда появится в этом списке.</span>
          </div>
        )}
      </div>
    </section>
  );
}

function PaymentsView({
  notifications,
  payments,
  onReadNotification
}: {
  notifications: NotificationItem[];
  payments: Payment[];
  onReadNotification: (item: NotificationItem) => void;
}) {
  return (
    <section className="workspace split-workspace">
      <div>
        <div className="section-heading">
          <div>
            <h2>Платежи</h2>
            <p>История операций по арендам и депозитам.</p>
          </div>
        </div>
        <DataTable empty="Платежей нет" columns={["ID", "Аренда", "Сумма", "Статус"]}>
          {payments.map((payment) => (
            <tr key={payment.id}>
              <td>#{payment.id}</td>
              <td>#{payment.rental_id}</td>
              <td>
                {payment.amount} {payment.currency}
              </td>
              <td>{String(payment.status)}</td>
            </tr>
          ))}
        </DataTable>
      </div>
      <aside className="notification-panel">
        <h2>Уведомления</h2>
        {notifications.length > 0 ? (
          notifications.map((item) => (
            <button className={item.read ? "notification read" : "notification"} key={item.id} onClick={() => onReadNotification(item)} type="button">
              <Bell size={18} />
              <span>
                <strong>{item.title}</strong>
                <small>{item.body}</small>
              </span>
            </button>
          ))
        ) : (
          <div className="empty-inline compact">
            <Bell size={24} />
            <strong>Уведомлений нет</strong>
          </div>
        )}
      </aside>
    </section>
  );
}

function ProfileView({
  user,
  onLogin,
  onUpdateUser
}: {
  user: User | null;
  onLogin: () => void;
  onUpdateUser: (user: User) => void;
}) {
  const [firstName, setFirstName] = useState(user?.first_name ?? "");
  const [lastName, setLastName] = useState(user?.last_name ?? "");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setFirstName(user?.first_name ?? "");
    setLastName(user?.last_name ?? "");
  }, [user]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!user) return;
    setSaving(true);
    try {
      const updated = await api.updateUser(user.id, { first_name: firstName, last_name: lastName });
      onUpdateUser(updated);
    } finally {
      setSaving(false);
    }
  }

  if (!user) {
    return (
      <section className="workspace empty-state">
        <UserRound size={42} />
        <h2>Войдите в аккаунт</h2>
        <p>Профиль, аренды, платежи и уведомления доступны после авторизации.</p>
        <button className="primary-button" onClick={onLogin} type="button">
          <LogIn size={18} />
          Войти
        </button>
      </section>
    );
  }

  return (
    <section className="workspace profile-grid">
      <div className="profile-card">
        <UserRound size={42} />
        <h2>
          {user.first_name} {user.last_name}
        </h2>
        <p>{user.email}</p>
        <div className="profile-stats">
          <span>
            Роль <strong>{user.role}</strong>
          </span>
          <span>
            Уровень <strong>{user.trust_level ?? "Silver"}</strong>
          </span>
          <span>
            Баланс <strong>{user.balance ?? 0} USD</strong>
          </span>
        </div>
      </div>
      <form className="profile-form" onSubmit={submit}>
        <h2>Личные данные</h2>
        <label>
          Имя
          <input onChange={(event) => setFirstName(event.target.value)} value={firstName} />
        </label>
        <label>
          Фамилия
          <input onChange={(event) => setLastName(event.target.value)} value={lastName} />
        </label>
        <button className="primary-button" disabled={saving} type="submit">
          {saving ? "Сохранение..." : "Сохранить"}
        </button>
      </form>
    </section>
  );
}

function AdminView({
  accounts,
  auditLogs,
  users,
  user,
  onCreateAccount,
  onSync,
  onToggleAccount,
  onUpdateAccount,
  onUpdateUser
}: {
  accounts: Account[];
  auditLogs: AuditLog[];
  users: User[];
  user: User | null;
  onCreateAccount: (payload: {
    steam_id64: string;
    steam_login: string;
    steam_password: string;
    price_per_hour: number;
    security_deposit: number;
  }) => Promise<void>;
  onSync: (account: Account) => Promise<void>;
  onToggleAccount: (account: Account) => Promise<void>;
  onUpdateAccount: (account: Account, patch: AdminAccountPatch) => Promise<void>;
  onUpdateUser: (targetUser: User, patch: { trust_score?: number; is_blocked?: boolean; balance?: number; role?: string }) => Promise<void>;
}) {
  const [tab, setTab] = useState<AdminTab>("overview");
  const [createOpen, setCreateOpen] = useState(false);
  const [editingAccount, setEditingAccount] = useState<Account | null>(null);

  if (user?.role !== "ADMIN") {
    return (
      <section className="workspace empty-state">
        <ShieldCheck size={42} />
        <h2>Нужна роль ADMIN</h2>
        <p>Административные операции доступны только пользователю с ролью ADMIN.</p>
      </section>
    );
  }

  const tabs: Array<{ id: AdminTab; label: string; icon: ReactNode }> = [
    { id: "overview", label: "Обзор", icon: <Gauge size={18} /> },
    { id: "accounts", label: "Аккаунты", icon: <Gamepad2 size={18} /> },
    { id: "users", label: "Пользователи", icon: <UsersRound size={18} /> },
    { id: "audit", label: "Аудит", icon: <Activity size={18} /> }
  ];

  return (
    <section className="admin-layout">
      <aside className="admin-sidebar">
        <h2>Админ-панель</h2>
        {tabs.map((item) => (
          <button className={tab === item.id ? "active" : ""} key={item.id} onClick={() => setTab(item.id)} type="button">
            {item.icon}
            {item.label}
          </button>
        ))}
      </aside>

      <div className="admin-content">
        <div className="section-heading">
          <div>
            <span className="eyebrow">Operations</span>
            <h2>{tabs.find((item) => item.id === tab)?.label}</h2>
            <p>Отдельный рабочий интерфейс администратора: аккаунты, пользователи и журнал действий.</p>
          </div>
          {tab === "accounts" && (
            <button className="primary-button" onClick={() => setCreateOpen(true)} type="button">
              <Plus size={18} />
              Добавить аккаунт
            </button>
          )}
        </div>

        {tab === "overview" && <AdminOverview accounts={accounts} auditLogs={auditLogs} users={users} />}
        {tab === "accounts" && (
          <AdminAccountsTable
            accounts={accounts}
            onCreate={() => setCreateOpen(true)}
            onEdit={setEditingAccount}
            onSync={onSync}
            onToggle={onToggleAccount}
          />
        )}
        {tab === "users" && <AdminUsersTable users={users} onUpdateUser={onUpdateUser} />}
        {tab === "audit" && <AuditLogList auditLogs={auditLogs} />}
      </div>

      {createOpen && (
        <AccountCreateDialog
          onClose={() => setCreateOpen(false)}
          onCreate={async (payload) => {
            await onCreateAccount(payload);
            setCreateOpen(false);
          }}
        />
      )}
      {editingAccount && (
        <AccountEditDialog
          account={editingAccount}
          onClose={() => setEditingAccount(null)}
          onSave={async (patch) => {
            await onUpdateAccount(editingAccount, patch);
            setEditingAccount(null);
          }}
        />
      )}
    </section>
  );
}

function AdminOverview({ accounts, auditLogs, users }: { accounts: Account[]; auditLogs: AuditLog[]; users: User[] }) {
  return (
    <>
      <div className="kpi-grid">
        <Kpi label="Всего аккаунтов" value={accounts.length} icon={<Gamepad2 size={22} />} />
        <Kpi label="Доступно" value={accounts.filter((item) => item.status === "Available").length} icon={<Check size={22} />} />
        <Kpi label="Отключено" value={accounts.filter((item) => item.status === "Disabled").length} icon={<ToggleLeft size={22} />} />
        <Kpi label="Пользователи" value={users.length} icon={<UsersRound size={22} />} />
      </div>
      <AuditLogList auditLogs={auditLogs.slice(0, 8)} compact />
    </>
  );
}

function AdminAccountsTable({
  accounts,
  onCreate,
  onEdit,
  onSync,
  onToggle
}: {
  accounts: Account[];
  onCreate: () => void;
  onEdit: (account: Account) => void;
  onSync: (account: Account) => Promise<void>;
  onToggle: (account: Account) => Promise<void>;
}) {
  const [busyAccountId, setBusyAccountId] = useState<number | null>(null);

  async function runAccountAction(account: Account, action: (account: Account) => Promise<void>) {
    setBusyAccountId(account.id);
    try {
      await action(account);
    } catch {
      // The parent handler already shows a toast. Keep the row interactive after the request settles.
    } finally {
      setBusyAccountId(null);
    }
  }

  return (
    <DataTable empty="Аккаунтов нет" columns={["SteamID", "Библиотека", "Цена", "Статус", "Действия"]}>
      {accounts.length > 0 ? (
        accounts.map((account) => {
          const disabled = account.status === "Disabled";
          const busy = busyAccountId === account.id;
          return (
            <tr key={account.id}>
              <td>{account.steam_id64}</td>
              <td>{gameNames(account, 2)}</td>
              <td>{money(account.price_per_hour)}/ч</td>
              <td>
                <span className={disabled ? "status-pill danger" : "status-pill green"}>
                  {accountStatusLabels[account.status] ?? account.status}
                </span>
              </td>
              <td>
                <div className="table-actions">
                  <button className="secondary-button icon-label" disabled={busy} onClick={() => onEdit(account)} type="button">
                    <Edit3 size={16} />
                    Изменить
                  </button>
                  <button className="secondary-button icon-label" disabled={busy} onClick={() => runAccountAction(account, onSync)} type="button">
                    <RefreshCcw size={16} />
                    {busy ? "Обновление..." : "Sync"}
                  </button>
                  <button className={disabled ? "success-button icon-label" : "danger-button icon-label"} disabled={busy} onClick={() => runAccountAction(account, onToggle)} type="button">
                    {disabled ? <ToggleRight size={16} /> : <ToggleLeft size={16} />}
                    {disabled ? "Включить" : "Отключить"}
                  </button>
                </div>
              </td>
            </tr>
          );
        })
      ) : (
        <tr>
          <td colSpan={5}>
            <button className="primary-button" onClick={onCreate} type="button">
              <Plus size={18} />
              Добавить первый аккаунт
            </button>
          </td>
        </tr>
      )}
    </DataTable>
  );
}

function AdminUsersTable({
  users,
  onUpdateUser
}: {
  users: User[];
  onUpdateUser: (targetUser: User, patch: { trust_score?: number; is_blocked?: boolean; balance?: number; role?: string }) => Promise<void>;
}) {
  return (
    <DataTable empty="Пользователей нет" columns={["ID", "Пользователь", "Роль", "Доверие", "Баланс", "Действия"]}>
      {users.map((item) => (
        <AdminUserRow key={item.id} user={item} onUpdateUser={onUpdateUser} />
      ))}
    </DataTable>
  );
}

function AdminUserRow({
  user,
  onUpdateUser
}: {
  user: User;
  onUpdateUser: (targetUser: User, patch: { trust_score?: number; is_blocked?: boolean; balance?: number; role?: string }) => Promise<void>;
}) {
  const [trust, setTrust] = useState(String(user.trust_score ?? 0));
  const [balance, setBalance] = useState(String(user.balance ?? 0));
  const [role, setRole] = useState(user.role === "ADMIN" ? "ADMIN" : "RENT");
  const [busy, setBusy] = useState(false);

  async function save() {
    setBusy(true);
    try {
      await onUpdateUser(user, { trust_score: Number(trust), balance: Number(balance), role });
    } catch {
      // Toast is emitted by the parent handler.
    } finally {
      setBusy(false);
    }
  }

  async function toggleBlock() {
    setBusy(true);
    try {
      await onUpdateUser(user, { is_blocked: !user.is_blocked });
    } catch {
      // Toast is emitted by the parent handler.
    } finally {
      setBusy(false);
    }
  }

  return (
    <tr>
      <td>#{user.id}</td>
      <td>
        <strong>{user.email}</strong>
        <small>
          {user.first_name} {user.last_name}
        </small>
      </td>
      <td>
        <select value={role} onChange={(event) => setRole(event.target.value)}>
          <option value="RENT">Клиент</option>
          <option value="ADMIN">Админ</option>
        </select>
      </td>
      <td>
        <input className="table-input" min="0" onChange={(event) => setTrust(event.target.value)} type="number" value={trust} />
      </td>
      <td>
        <input className="table-input" min="0" onChange={(event) => setBalance(event.target.value)} type="number" value={balance} />
      </td>
      <td>
        <div className="table-actions">
          <button className="secondary-button icon-label" disabled={busy} onClick={save} type="button">
            <Check size={16} />
            Сохранить
          </button>
          <button className={user.is_blocked ? "success-button icon-label" : "danger-button icon-label"} disabled={busy} onClick={toggleBlock} type="button">
            <UserCog size={16} />
            {user.is_blocked ? "Разблокировать" : "Заблокировать"}
          </button>
        </div>
      </td>
    </tr>
  );
}

function AuditLogList({ auditLogs, compact = false }: { auditLogs: AuditLog[]; compact?: boolean }) {
  return (
    <div className={compact ? "audit-strip compact" : "audit-strip"}>
      <h2>Audit log</h2>
      {auditLogs.length > 0 ? (
        auditLogs.map((log) => (
          <article className="audit-item" key={log.id}>
            <Activity size={16} />
            <span>{log.action}</span>
            <small>
              {log.entity_type} #{log.entity_id}
            </small>
          </article>
        ))
      ) : (
        <div className="empty-inline compact">
          <Activity size={24} />
          <strong>Событий пока нет</strong>
        </div>
      )}
    </div>
  );
}

function AccountCreateDialog({
  onClose,
  onCreate
}: {
  onClose: () => void;
  onCreate: (payload: {
    steam_id64: string;
    steam_login: string;
    steam_password: string;
    price_per_hour: number;
    security_deposit: number;
  }) => Promise<void>;
}) {
  const [steamId, setSteamId] = useState("");
  const [login, setLogin] = useState("");
  const [password, setPassword] = useState("");
  const [price, setPrice] = useState("50");
  const [deposit, setDeposit] = useState("100");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await onCreate({
        steam_id64: steamId,
        steam_login: login,
        steam_password: password,
        price_per_hour: Number(price),
        security_deposit: Number(deposit)
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Не удалось создать аккаунт");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="dialog-backdrop">
      <form className="auth-dialog wide-dialog" onSubmit={submit}>
        <button className="ghost icon-button close-button" onClick={onClose} title="Закрыть" type="button">
          <X size={20} />
        </button>
        <Plus size={28} />
        <h2>Добавить аккаунт</h2>
        <label>
          SteamID64
          <input onChange={(event) => setSteamId(event.target.value)} required value={steamId} />
        </label>
        <div className="two-fields">
          <label>
            Логин Steam
            <input onChange={(event) => setLogin(event.target.value)} required value={login} />
          </label>
          <label>
            Пароль Steam
            <input onChange={(event) => setPassword(event.target.value)} required type="password" value={password} />
          </label>
        </div>
        <div className="two-fields">
          <label>
            Цена за час
            <input min="1" onChange={(event) => setPrice(event.target.value)} required type="number" value={price} />
          </label>
          <label>
            Депозит
            <input min="0" onChange={(event) => setDeposit(event.target.value)} required type="number" value={deposit} />
          </label>
        </div>
        {error && <span className="form-error">{error}</span>}
        <button className="primary-button full" disabled={busy} type="submit">
          {busy ? "Создание..." : "Создать аккаунт"}
        </button>
      </form>
    </div>
  );
}

function AccountEditDialog({
  account,
  onClose,
  onSave
}: {
  account: Account;
  onClose: () => void;
  onSave: (patch: AdminAccountPatch) => Promise<void>;
}) {
  const [price, setPrice] = useState(String(account.price_per_hour.amount));
  const [deposit, setDeposit] = useState(String(account.security_deposit.amount));
  const [status, setStatus] = useState(account.status);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await onSave({
        price_per_hour: Number(price),
        security_deposit: Number(deposit),
        status: accountStatusNumbers[status]
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Не удалось обновить аккаунт");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="dialog-backdrop">
      <form className="auth-dialog" onSubmit={submit}>
        <button className="ghost icon-button close-button" onClick={onClose} title="Закрыть" type="button">
          <X size={20} />
        </button>
        <Edit3 size={28} />
        <h2>Редактировать аккаунт</h2>
        <p className="dialog-subtitle">{account.steam_id64}</p>
        <label>
          Статус
          <select value={status} onChange={(event) => setStatus(event.target.value)}>
            {Object.keys(accountStatusNumbers).map((item) => (
              <option key={item} value={item}>
                {accountStatusLabels[item] ?? item}
              </option>
            ))}
          </select>
        </label>
        <label>
          Цена за час
          <input min="1" onChange={(event) => setPrice(event.target.value)} required type="number" value={price} />
        </label>
        <label>
          Депозит
          <input min="0" onChange={(event) => setDeposit(event.target.value)} required type="number" value={deposit} />
        </label>
        {error && <span className="form-error">{error}</span>}
        <button className="primary-button full" disabled={busy} type="submit">
          {busy ? "Сохранение..." : "Сохранить"}
        </button>
      </form>
    </div>
  );
}

function Kpi({ icon, label, value }: { icon: ReactNode; label: string; value: string | number }) {
  return (
    <article className="kpi-card">
      {icon}
      <span>{label}</span>
      <strong>{value}</strong>
    </article>
  );
}

function DataTable({
  children,
  columns,
  empty
}: {
  children: ReactNode;
  columns: string[];
  empty: string;
}) {
  const hasRows = Array.isArray(children) ? children.length > 0 : Boolean(children);
  return (
    <div className="table-card">
      <table>
        <thead>
          <tr>
            {columns.map((column) => (
              <th key={column}>{column}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {hasRows ? (
            children
          ) : (
            <tr>
              <td colSpan={columns.length}>{empty}</td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}

function MobileNav({ setView, view }: { setView: (view: View) => void; view: View }) {
  const items: Array<{ id: View; icon: ReactNode; label: string }> = [
    { id: "catalog", icon: <Gamepad2 size={20} />, label: "Каталог" },
    { id: "rentals", icon: <Clock3 size={20} />, label: "Аренды" },
    { id: "payments", icon: <CreditCard size={20} />, label: "Платежи" },
    { id: "profile", icon: <UserRound size={20} />, label: "Профиль" }
  ];
  return (
    <nav className="mobile-nav">
      {items.map((item) => (
        <button className={view === item.id ? "active" : ""} key={item.id} onClick={() => setView(item.id)} title={item.label} type="button">
          {item.icon}
        </button>
      ))}
    </nav>
  );
}

function AuthDialog({
  onAuthenticated,
  onClose,
  setToast
}: {
  onAuthenticated: (user: User) => void;
  onClose: () => void;
  setToast: (toast: Toast) => void;
}) {
  const [mode, setMode] = useState<"login" | "register">("login");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [firstName, setFirstName] = useState("");
  const [lastName, setLastName] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    try {
      if (mode === "login") {
        const tokens = await api.login({ email, password });
        saveTokens(tokens);
      } else {
        const res = await api.register({ email, password, first_name: firstName, last_name: lastName });
        saveTokens({ access_token: res.access_token, refresh_token: res.refresh_token });
      }
      const me = await api.me();
      onAuthenticated(me);
      setToast({ type: "ok", message: "Вы вошли в систему" });
    } catch (error) {
      setToast({ type: "error", message: error instanceof Error ? error.message : "Ошибка авторизации" });
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="dialog-backdrop">
      <form className="auth-dialog" onSubmit={submit}>
        <button className="ghost icon-button close-button" onClick={onClose} title="Закрыть" type="button">
          <X size={20} />
        </button>
        <Sparkles size={28} />
        <h2>{mode === "login" ? "Вход" : "Регистрация"}</h2>
        <label>
          Email
          <input onChange={(event) => setEmail(event.target.value)} required type="email" value={email} />
        </label>
        <label>
          Пароль
          <input onChange={(event) => setPassword(event.target.value)} required type="password" value={password} />
        </label>
        {mode === "register" && (
          <div className="two-fields">
            <label>
              Имя
              <input onChange={(event) => setFirstName(event.target.value)} required value={firstName} />
            </label>
            <label>
              Фамилия
              <input onChange={(event) => setLastName(event.target.value)} required value={lastName} />
            </label>
          </div>
        )}
        <button className="primary-button full" disabled={busy} type="submit">
          {busy ? "Проверка..." : mode === "login" ? "Войти" : "Создать аккаунт"}
        </button>
        <button className="ghost switch-auth" onClick={() => setMode(mode === "login" ? "register" : "login")} type="button">
          {mode === "login" ? "Нужна регистрация" : "Уже есть аккаунт"}
        </button>
      </form>
    </div>
  );
}
