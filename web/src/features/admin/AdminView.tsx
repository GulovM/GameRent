import { Activity, Check, Edit3, Gamepad2, Gauge, Plus, RefreshCcw, ShieldCheck, ToggleLeft, ToggleRight, UserCog, UsersRound, X } from "lucide-react";
import { type FormEvent, useState } from "react";
import type { ReactNode } from "react";
import type { Account, AuditLog, User } from "../../api";
import { DataTable } from "../../components/DataTable";
import { Kpi } from "../../components/Kpi";
import type { AdminAccountPatch, AdminTab } from "../../types/app";
import { accountStatusLabels, accountStatusNumbers, gameNames } from "../../utils/accounts";
import { money } from "../../utils/format";

type AdminViewProps = {
  accounts: Account[];
  auditLogs: AuditLog[];
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
  user: User | null;
  users: User[];
};

export function AdminView({
  accounts,
  auditLogs,
  onCreateAccount,
  onSync,
  onToggleAccount,
  onUpdateAccount,
  onUpdateUser,
  user,
  users
}: AdminViewProps) {
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
        {tab === "users" && <AdminUsersTable onUpdateUser={onUpdateUser} users={users} />}
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
        <Kpi icon={<Gamepad2 size={22} />} label="Всего аккаунтов" value={accounts.length} />
        <Kpi icon={<Check size={22} />} label="Доступно" value={accounts.filter((item) => item.status === "Available").length} />
        <Kpi icon={<ToggleLeft size={22} />} label="Отключено" value={accounts.filter((item) => item.status === "Disabled").length} />
        <Kpi icon={<UsersRound size={22} />} label="Пользователи" value={users.length} />
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
                <span className={disabled ? "status-pill danger" : "status-pill green"}>{accountStatusLabels[account.status] ?? account.status}</span>
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
                  <button
                    className={disabled ? "success-button icon-label" : "danger-button icon-label"}
                    disabled={busy}
                    onClick={() => runAccountAction(account, onToggle)}
                    type="button"
                  >
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
  onUpdateUser,
  users
}: {
  onUpdateUser: (targetUser: User, patch: { trust_score?: number; is_blocked?: boolean; balance?: number; role?: string }) => Promise<void>;
  users: User[];
}) {
  return (
    <DataTable empty="Пользователей нет" columns={["ID", "Пользователь", "Роль", "Доверие", "Баланс", "Действия"]}>
      {users.map((item) => (
        <AdminUserRow key={item.id} onUpdateUser={onUpdateUser} user={item} />
      ))}
    </DataTable>
  );
}

function AdminUserRow({
  onUpdateUser,
  user
}: {
  onUpdateUser: (targetUser: User, patch: { trust_score?: number; is_blocked?: boolean; balance?: number; role?: string }) => Promise<void>;
  user: User;
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
