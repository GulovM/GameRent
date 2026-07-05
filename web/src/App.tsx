import {
  Activity,
  Bell,
  Check,
  Clock3,
  CreditCard,
  DatabaseZap,
  Gamepad2,
  Heart,
  LayoutDashboard,
  Lock,
  LogIn,
  LogOut,
  Menu,
  Search,
  ShieldCheck,
  SlidersHorizontal,
  Sparkles,
  Star,
  UserRound,
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
import { mockAccounts as fallbackAccounts } from "./mock";
import { mockAuditLogs as fallbackAuditLogs } from "./mock";
import { mockNotifications as fallbackNotifications } from "./mock";
import { mockPayments as fallbackPayments } from "./mock";
import { mockRentals as fallbackRentals } from "./mock";
import { mockUser as fallbackUser } from "./mock";

type View = "catalog" | "rentals" | "payments" | "profile" | "admin";
type Toast = { type: "ok" | "error"; message: string } | null;

const statusLabels: Record<string, string> = {
  Available: "Свободен",
  Reserved: "В резерве",
  Rented: "В аренде",
  Maintenance: "Сервис",
  Disabled: "Отключен",
  Created: "Создан",
  Verifying: "Проверка"
};

const statusNumberLabels: Record<number, string> = {
  0: "Создан",
  1: "Ожидает оплаты",
  2: "Активна",
  3: "Истекла",
  4: "Завершена",
  5: "Отменена"
};

function money(value?: { amount: number; currency: string } | number, fallbackCurrency = "TJS") {
  if (typeof value === "number") return `${value} ${fallbackCurrency}`;
  if (!value) return `0 ${fallbackCurrency}`;
  return `${value.amount} ${value.currency}`;
}

function gameNames(account: Account, limit = 3) {
  const names = account.games.map((game) => game.name);
  return names.slice(0, limit).join(", ") || "Steam Library";
}

function accountAccent(account: Account) {
  const key = account.id % 4;
  return ["cyan", "violet", "pink", "green"][key];
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

function useTicker() {
  const [, setTick] = useState(0);
  useEffect(() => {
    const id = window.setInterval(() => setTick((value) => value + 1), 1000);
    return () => window.clearInterval(id);
  }, []);
}

export default function App() {
  useTicker();
  const [view, setView] = useState<View>("catalog");
  const [accounts, setAccounts] = useState<Account[]>(fallbackAccounts);
  const [rentals, setRentals] = useState<Rental[]>(fallbackRentals);
  const [payments, setPayments] = useState<Payment[]>(fallbackPayments);
  const [notifications, setNotifications] = useState<NotificationItem[]>(fallbackNotifications);
  const [auditLogs, setAuditLogs] = useState<AuditLog[]>(fallbackAuditLogs);
  const [user, setUser] = useState<User | null>(getAccessToken() ? null : fallbackUser);
  const [selectedAccount, setSelectedAccount] = useState<Account | null>(null);
  const [duration, setDuration] = useState(2);
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("Available");
  const [maxPrice, setMaxPrice] = useState(300);
  const [loading, setLoading] = useState(false);
  const [authOpen, setAuthOpen] = useState(false);
  const [toast, setToast] = useState<Toast>(null);
  const [offline, setOffline] = useState(false);

  useEffect(() => {
    void bootstrap();
  }, []);

  useEffect(() => {
    const id = window.setTimeout(() => setToast(null), 3200);
    return () => window.clearTimeout(id);
  }, [toast]);

  async function bootstrap() {
    setLoading(true);
    try {
      const [accountsRes, gamesRes] = await Promise.all([
        api.accounts({ page: 1, page_size: 20 }),
        api.games({ page: 1, page_size: 12 })
      ]);
      setAccounts(accountsRes.accounts);
      if (gamesRes.games.length === 0) {
        setOffline(true);
      }
      setOffline(false);
    } catch {
      setAccounts(fallbackAccounts);
      setOffline(true);
    }

    if (getAccessToken()) {
      await loadPrivateData();
    }
    setLoading(false);
  }

  async function loadPrivateData() {
    try {
      const [meRes, rentalsRes, paymentsRes, notificationsRes] = await Promise.all([
        api.me(),
        api.rentals(),
        api.payments(),
        api.notifications()
      ]);
      setUser(meRes);
      setRentals(rentalsRes.rentals);
      setPayments(paymentsRes.payments);
      setNotifications(notificationsRes.notifications);

      if (meRes.role === "ADMIN") {
        const [adminAccountsRes, usersRes, auditRes] = await Promise.all([
          api.adminAccounts(),
          api.adminUsers(),
          api.adminAuditLogs()
        ]);
        setAuditLogs(auditRes.audit_logs);
        if (adminAccountsRes.accounts.length > 0) {
          setAccounts((current) =>
            current.map((account) => {
              const adminAccount = adminAccountsRes.accounts.find((item) => item.id === account.id);
              return adminAccount
                ? {
                    ...account,
                    status: statusFromNumber(adminAccount.status),
                    price_per_hour: { amount: adminAccount.hourly_price, currency: "USD" },
                    security_deposit: { amount: adminAccount.deposit_amount, currency: "USD" }
                  }
                : account;
            })
          );
        }
        if (usersRes.users.length > 0 && !usersRes.users.some((item) => item.id === meRes.id)) {
          setUser(meRes);
        }
      }
    } catch {
      setUser((current) => current ?? fallbackUser);
      setRentals(fallbackRentals);
      setPayments(fallbackPayments);
      setNotifications(fallbackNotifications);
      setAuditLogs(fallbackAuditLogs);
      setOffline(true);
    }
  }

  function statusFromNumber(value: number) {
    return ["Created", "Verifying", "Available", "Reserved", "Rented", "Maintenance", "Disabled"][value] ?? "Unknown";
  }

  const filteredAccounts = useMemo(() => {
    const q = search.trim().toLowerCase();
    return accounts.filter((account) => {
      const matchesSearch =
        !q ||
        account.steam_id64.includes(q) ||
        account.games.some((game) => game.name.toLowerCase().includes(q));
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

  async function handleDisableAccount(account: Account) {
    try {
      await api.adminUpdateAccount(account.id, { status: 6 });
      setAccounts((current) => current.map((item) => (item.id === account.id ? { ...item, status: "Disabled" } : item)));
      setToast({ type: "ok", message: "Аккаунт отключен через PATCH status=6" });
    } catch (error) {
      setToast({ type: "error", message: error instanceof Error ? error.message : "Не удалось отключить аккаунт" });
    }
  }

  async function handleSyncAccount(account: Account) {
    try {
      await api.adminSyncAccount(account.id);
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
      setToast({ type: "ok", message: "Синхронизация отмечена" });
    } catch (error) {
      setToast({ type: "error", message: error instanceof Error ? error.message : "Не удалось синхронизировать" });
    }
  }

  async function handleLogout() {
    const refresh = getRefreshToken();
    clearTokens();
    setUser(null);
    if (refresh) {
      void api.logout(refresh).catch(() => undefined);
    }
    setToast({ type: "ok", message: "Сессия завершена" });
  }

  return (
    <div className="app-shell">
      <Header
        view={view}
        setView={setView}
        user={user}
        onLogin={() => setAuthOpen(true)}
        onLogout={handleLogout}
      />

      {offline && (
        <div className="offline-banner">
          <DatabaseZap size={18} />
          Backend сейчас недоступен или вернул пустой ответ. Интерфейс показывает demo-данные, но все действия привязаны к реальным `/api/v1` endpoint-ам.
        </div>
      )}

      <main>
        {view === "catalog" && (
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
          />
        )}
        {view === "rentals" && (
          <RentalsView
            accounts={accounts}
            rentals={rentals}
            onCancel={handleCancel}
            onExtend={handleExtend}
          />
        )}
        {view === "payments" && <PaymentsView notifications={notifications} payments={payments} />}
        {view === "profile" && (
          <ProfileView
            user={user}
            onLogin={() => setAuthOpen(true)}
            onUpdateUser={(next) => setUser(next)}
          />
        )}
        {view === "admin" && (
          <AdminView
            accounts={accounts}
            auditLogs={auditLogs}
            user={user}
            onDisable={handleDisableAccount}
            onSync={handleSyncAccount}
          />
        )}
      </main>

      <MobileNav view={view} setView={setView} isAdmin={user?.role === "ADMIN"} />

      {selectedAccount && (
        <CheckoutDrawer
          account={selectedAccount}
          duration={duration}
          setDuration={setDuration}
          onClose={() => setSelectedAccount(null)}
          onRent={() => handleRent(selectedAccount)}
        />
      )}

      {authOpen && (
        <AuthDialog
          onClose={() => setAuthOpen(false)}
          onAuthenticated={(nextUser) => {
            setUser(nextUser);
            setAuthOpen(false);
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
  const items: Array<{ id: View; label: string }> = [
    { id: "catalog", label: "Каталог" },
    { id: "rentals", label: "Аренды" },
    { id: "payments", label: "Платежи" },
    { id: "profile", label: "Профиль" }
  ];
  if (user?.role === "ADMIN") items.push({ id: "admin", label: "Admin" });

  return (
    <header className="topbar">
      <button className="brand" onClick={() => setView("catalog")} type="button">
        <span className="brand-mark">GR</span>
        <span>GameRent</span>
      </button>
      <nav className="desktop-nav">
        {items.map((item) => (
          <button
            className={view === item.id ? "active" : ""}
            key={item.id}
            onClick={() => setView(item.id)}
            type="button"
          >
            {item.label}
          </button>
        ))}
      </nav>
      <div className="topbar-actions">
        {user ? (
          <>
            <span className="user-chip">{user.first_name || user.email}</span>
            <button className="ghost icon-button" onClick={onLogout} title="Выйти" type="button">
              <LogOut size={18} />
            </button>
          </>
        ) : (
          <button className="secondary-button" onClick={onLogin} type="button">
            <LogIn size={18} />
            Войти
          </button>
        )}
        <button className="ghost menu-button" type="button" title="Меню">
          <Menu size={20} />
        </button>
      </div>
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
  selectAccount
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
}) {
  return (
    <>
      <section className="hero">
        <div className="hero-copy">
          <span className="eyebrow">STEAM ACCOUNT RENTAL</span>
          <h1>Арендуй игровой аккаунт на пару часов, без покупки игры</h1>
          <p>
            Выбирай аккаунт по библиотеке, цене и доступности. GameRent блокирует двойную аренду,
            показывает таймер доступа и хранит историю платежей.
          </p>
          <div className="hero-actions">
            <a className="primary-button" href="#catalog">
              Открыть каталог
            </a>
            <button className="secondary-button" type="button">
              <ShieldCheck size={18} />
              Как это работает
            </button>
          </div>
          <div className="metric-row">
            <span>Доступно {accounts.filter((item) => item.status === "Available").length} аккаунтов</span>
            <span>Старт за 90 сек</span>
            <span>JWT + RBAC</span>
          </div>
        </div>
        <LiveRentalCard rental={activeRental} />
      </section>

      <section className="catalog-section" id="catalog">
        <div className="section-heading">
          <div>
            <h2>Каталог аккаунтов</h2>
            <p>Фильтруй по игре, статусу, SteamID и бюджету.</p>
          </div>
          <div className="duration-picker" aria-label="Длительность аренды">
            {[1, 2, 4, 8].map((hours) => (
              <button
                className={duration === hours ? "selected" : ""}
                key={hours}
                onClick={() => setDuration(hours)}
                type="button"
              >
                {hours} ч
              </button>
            ))}
          </div>
        </div>

        <div className="filters">
          <label className="search-box">
            <Search size={20} />
            <input
              onChange={(event) => setSearch(event.target.value)}
              placeholder="Поиск по игре, SteamID или логину"
              value={search}
            />
          </label>
          <div className="chip-row">
            {["All", "Available", "Rented", "Maintenance"].map((item) => (
              <button
                className={status === item ? "selected" : ""}
                key={item}
                onClick={() => setStatus(item)}
                type="button"
              >
                {item === "All" ? "Все" : statusLabels[item]}
              </button>
            ))}
          </div>
          <label className="range-control">
            <SlidersHorizontal size={18} />
            до {maxPrice} TJS/ч
            <input
              max="500"
              min="50"
              onChange={(event) => setMaxPrice(Number(event.target.value))}
              step="10"
              type="range"
              value={maxPrice}
            />
          </label>
        </div>

        <div className={loading ? "account-grid loading" : "account-grid"}>
          {accounts.map((account) => (
            <AccountCard account={account} key={account.id} onSelect={selectAccount} />
          ))}
        </div>
      </section>

      <section className="flow-section">
        <h2>Поток аренды</h2>
        <div className="flow-grid">
          {[
            ["1", "Выбери аккаунт", "Фильтр по игре, цене, депозиту и доступности"],
            ["2", "Рассчитай стоимость", "Период, залог, баланс и итог до платежа"],
            ["3", "Получи доступ", "Steam login показывается только при активной аренде"],
            ["4", "Контролируй время", "Таймер, продление, отмена и история"]
          ].map(([step, title, body]) => (
            <article className="flow-card" key={step}>
              <span>{step}</span>
              <h3>{title}</h3>
              <p>{body}</p>
            </article>
          ))}
        </div>
      </section>
    </>
  );
}

function LiveRentalCard({ rental }: { rental?: Rental }) {
  return (
    <aside className="live-card">
      <div className="live-top">
        <h2>Live rental</h2>
        <span className="status-pill green">ACTIVE</span>
      </div>
      <div className="game-cover cyan">
        <span>Counter-Strike 2</span>
      </div>
      <p>До окончания доступа</p>
      <strong className="timer">{rental ? remaining(rental.expires_at) : "01:43:12"}</strong>
      <div className="live-actions">
        <button className="primary-button" type="button">
          Продлить на 1 час
        </button>
        <button className="secondary-button" type="button">
          Данные входа
        </button>
      </div>
    </aside>
  );
}

function AccountCard({ account, onSelect }: { account: Account; onSelect: (account: Account) => void }) {
  const accent = accountAccent(account);
  const available = account.status === "Available";
  return (
    <article className="account-card">
      <button className="card-button" onClick={() => onSelect(account)} type="button">
        <div className={`game-cover ${accent}`}>
          <span>{account.games[0]?.name || "Steam Account"}</span>
        </div>
        <div className="card-body">
          <div>
            <h3>{account.games[0]?.name ? `${account.games[0].name} Pack` : `Account #${account.id}`}</h3>
            <p>{gameNames(account)}</p>
          </div>
          <span className={`status-pill ${available ? "green" : account.status === "Rented" ? "pink" : "muted"}`}>
            {statusLabels[account.status] ?? account.status}
          </span>
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
  onRent
}: {
  account: Account;
  duration: number;
  setDuration: (value: number) => void;
  onClose: () => void;
  onRent: () => void;
}) {
  const total = account.price_per_hour.amount * duration + account.security_deposit.amount;
  return (
    <div className="drawer-backdrop" role="presentation">
      <aside className="checkout-drawer" aria-label="Оформление аренды">
        <button className="ghost icon-button close-button" onClick={onClose} title="Закрыть" type="button">
          <X size={20} />
        </button>
        <div className={`game-cover ${accountAccent(account)}`}>
          <span>{account.games[0]?.name || "Steam Account"}</span>
        </div>
        <div className="drawer-title">
          <div>
            <h2>Оформление аренды</h2>
            <p>Доступ будет выдан после успешной оплаты.</p>
          </div>
          <span className="status-pill green">{statusLabels[account.status] ?? account.status}</span>
        </div>
        <div className="duration-picker wide">
          {[1, 2, 4, 8].map((hours) => (
            <button
              className={duration === hours ? "selected" : ""}
              key={hours}
              onClick={() => setDuration(hours)}
              type="button"
            >
              {hours} ч
            </button>
          ))}
        </div>
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
            <dd>{total} {account.price_per_hour.currency}</dd>
          </div>
        </dl>
        <div className="safety-note">
          <Lock size={20} />
          <span>Логин и пароль отображаются только во время активной аренды.</span>
        </div>
        <button className="primary-button full" disabled={account.status !== "Available"} onClick={onRent} type="button">
          Оплатить и начать аренду
        </button>
        <button className="secondary-button full" type="button">
          <Heart size={18} />
          Добавить в избранное
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
        {rentals.map((rental) => {
          const account = accounts.find((item) => item.id === rental.account_id);
          return (
            <article className="rental-row" key={rental.id}>
              <div className="row-icon">
                <Clock3 size={22} />
              </div>
              <div>
                <h3>{account?.games[0]?.name || `Аккаунт #${rental.account_id}`}</h3>
                <p>{statusNumberLabels[rental.status] ?? rental.status} · до {remaining(rental.expires_at)}</p>
              </div>
              <strong>{money(rental.total_price)}</strong>
              <div className="row-actions">
                <button className="secondary-button" onClick={() => onExtend(rental)} type="button">
                  Продлить
                </button>
                <button className="danger-button" onClick={() => onCancel(rental)} type="button">
                  Отменить
                </button>
              </div>
            </article>
          );
        })}
      </div>
    </section>
  );
}

function PaymentsView({ notifications, payments }: { notifications: NotificationItem[]; payments: Payment[] }) {
  return (
    <section className="workspace split-workspace">
      <div>
        <div className="section-heading">
          <div>
            <h2>Платежи</h2>
            <p>История операций по арендам и депозитам.</p>
          </div>
        </div>
        <div className="table-card">
          <table>
            <thead>
              <tr>
                <th>ID</th>
                <th>Аренда</th>
                <th>Сумма</th>
                <th>Статус</th>
              </tr>
            </thead>
            <tbody>
              {payments.map((payment) => (
                <tr key={payment.id}>
                  <td>#{payment.id}</td>
                  <td>#{payment.rental_id}</td>
                  <td>{payment.amount} {payment.currency}</td>
                  <td>{String(payment.status)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
      <aside className="notification-panel">
        <h2>Уведомления</h2>
        {notifications.map((item) => (
          <article className={item.read ? "notification read" : "notification"} key={item.id}>
            <Bell size={18} />
            <div>
              <h3>{item.title}</h3>
              <p>{item.body}</p>
            </div>
          </article>
        ))}
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
          Войти
        </button>
      </section>
    );
  }

  return (
    <section className="workspace profile-grid">
      <div className="profile-card">
        <UserRound size={42} />
        <h2>{user.first_name} {user.last_name}</h2>
        <p>{user.email}</p>
        <div className="profile-stats">
          <span>Role <strong>{user.role}</strong></span>
          <span>Trust <strong>{user.trust_level ?? "Silver"}</strong></span>
          <span>Score <strong>{user.trust_score ?? 0}</strong></span>
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
  user,
  onDisable,
  onSync
}: {
  accounts: Account[];
  auditLogs: AuditLog[];
  user: User | null;
  onDisable: (account: Account) => void;
  onSync: (account: Account) => void;
}) {
  if (user?.role !== "ADMIN") {
    return (
      <section className="workspace empty-state">
        <ShieldCheck size={42} />
        <h2>Нужна роль ADMIN</h2>
        <p>Административные операции доступны только пользователю с ролью ADMIN.</p>
      </section>
    );
  }

  return (
    <section className="admin-layout">
      <aside className="admin-sidebar">
        <h2>GameRent Admin</h2>
        <span className="active">Аккаунты</span>
        <span>Аренды</span>
        <span>Платежи</span>
        <span>Пользователи</span>
        <span>Audit logs</span>
      </aside>
      <div className="admin-content">
        <div className="section-heading">
          <div>
            <h2>Управление аккаунтами</h2>
            <p>Создание, sync, отключение и контроль Steam-аккаунтов.</p>
          </div>
          <button className="primary-button" type="button">
            Добавить аккаунт
          </button>
        </div>
        <div className="kpi-grid">
          <Kpi label="Всего аккаунтов" value={accounts.length} icon={<Gamepad2 size={22} />} />
          <Kpi label="Свободно" value={accounts.filter((item) => item.status === "Available").length} icon={<Check size={22} />} />
          <Kpi label="В аренде" value={accounts.filter((item) => item.status === "Rented").length} icon={<Activity size={22} />} />
          <Kpi label="Sync errors" value="0" icon={<Zap size={22} />} />
        </div>
        <div className="table-card">
          <table>
            <thead>
              <tr>
                <th>SteamID</th>
                <th>Библиотека</th>
                <th>Цена</th>
                <th>Статус</th>
                <th>Действия</th>
              </tr>
            </thead>
            <tbody>
              {accounts.map((account) => (
                <tr key={account.id}>
                  <td>{account.steam_id64}</td>
                  <td>{gameNames(account, 2)}</td>
                  <td>{money(account.price_per_hour)}/ч</td>
                  <td>{statusLabels[account.status] ?? account.status}</td>
                  <td>
                    <div className="table-actions">
                      <button className="secondary-button" onClick={() => onSync(account)} type="button">
                        Sync
                      </button>
                      <button className="danger-button" onClick={() => onDisable(account)} type="button">
                        Отключить
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <div className="audit-strip">
          <h2>Audit logs</h2>
          {auditLogs.slice(0, 3).map((log) => (
            <span key={log.id}>{log.action} · #{log.entity_id}</span>
          ))}
        </div>
      </div>
    </section>
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

function MobileNav({ isAdmin, setView, view }: { isAdmin: boolean; setView: (view: View) => void; view: View }) {
  const items: Array<{ id: View; icon: ReactNode; label: string }> = [
    { id: "catalog", icon: <Gamepad2 size={20} />, label: "Каталог" },
    { id: "rentals", icon: <Clock3 size={20} />, label: "Аренды" },
    { id: "payments", icon: <CreditCard size={20} />, label: "Платежи" },
    { id: isAdmin ? "admin" : "profile", icon: isAdmin ? <LayoutDashboard size={20} /> : <UserRound size={20} />, label: isAdmin ? "Admin" : "Профиль" }
  ];
  return (
    <nav className="mobile-nav">
      {items.map((item) => (
        <button
          className={view === item.id ? "active" : ""}
          key={item.id}
          onClick={() => setView(item.id)}
          title={item.label}
          type="button"
        >
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
  const [email, setEmail] = useState("rent@example.com");
  const [password, setPassword] = useState("secret123");
  const [firstName, setFirstName] = useState("Ivan");
  const [lastName, setLastName] = useState("Petrov");
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
          <input onChange={(event) => setEmail(event.target.value)} type="email" value={email} />
        </label>
        <label>
          Пароль
          <input onChange={(event) => setPassword(event.target.value)} type="password" value={password} />
        </label>
        {mode === "register" && (
          <div className="two-fields">
            <label>
              Имя
              <input onChange={(event) => setFirstName(event.target.value)} value={firstName} />
            </label>
            <label>
              Фамилия
              <input onChange={(event) => setLastName(event.target.value)} value={lastName} />
            </label>
          </div>
        )}
        <button className="primary-button full" disabled={busy} type="submit">
          {busy ? "Проверка..." : mode === "login" ? "Войти" : "Создать аккаунт"}
        </button>
        <button
          className="ghost switch-auth"
          onClick={() => setMode(mode === "login" ? "register" : "login")}
          type="button"
        >
          {mode === "login" ? "Нужна регистрация" : "Уже есть аккаунт"}
        </button>
      </form>
    </div>
  );
}
