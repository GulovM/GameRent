import { LogIn, UserRound } from "lucide-react";
import { type FormEvent, useEffect, useState } from "react";
import { api, type FinancialBalance, type LedgerEntry, type Pagination, type RefundEntry, type User } from "../../api";
import { messageForApiError } from "../../utils/apiErrors";
import { BalancePanel } from "./BalancePanel";
import { FinancialHistoryPanel } from "./FinancialHistoryPanel";
import { RefundHistoryPanel } from "./RefundHistoryPanel";

type ProfileViewProps = {
  onLogin: () => void;
  onUpdateUser: (user: User) => void;
  user: User | null;
};

export function ProfileView({ onLogin, onUpdateUser, user }: ProfileViewProps) {
  const [firstName, setFirstName] = useState(user?.first_name ?? "");
  const [lastName, setLastName] = useState(user?.last_name ?? "");
  const [saving, setSaving] = useState(false);
  const [balance, setBalance] = useState<FinancialBalance | null>(null);
  const [balanceLoading, setBalanceLoading] = useState(false);
  const [balanceError, setBalanceError] = useState<string | null>(null);
  const [ledger, setLedger] = useState<LedgerEntry[]>([]);
  const [ledgerPagination, setLedgerPagination] = useState<Pagination | null>(null);
  const [ledgerPage, setLedgerPage] = useState(1);
  const [ledgerLoading, setLedgerLoading] = useState(false);
  const [ledgerError, setLedgerError] = useState<string | null>(null);
  const [refunds, setRefunds] = useState<RefundEntry[]>([]);
  const [refundsPagination, setRefundsPagination] = useState<Pagination | null>(null);
  const [refundsPage, setRefundsPage] = useState(1);
  const [refundsLoading, setRefundsLoading] = useState(false);
  const [refundsError, setRefundsError] = useState<string | null>(null);

  useEffect(() => {
    setFirstName(user?.first_name ?? "");
    setLastName(user?.last_name ?? "");
  }, [user]);

  useEffect(() => {
    if (!user) {
      setBalance(null);
      setLedger([]);
      setLedgerPagination(null);
      setLedgerPage(1);
      setBalanceError(null);
      setLedgerError(null);
      setRefunds([]);
      setRefundsPagination(null);
      setRefundsPage(1);
      setRefundsError(null);
      return;
    }

    let active = true;

    async function loadFinancialData() {
      setBalanceLoading(true);
      setLedgerLoading(true);
      setRefundsLoading(true);
      setBalanceError(null);
      setLedgerError(null);
      setRefundsError(null);
      try {
        const [balanceRes, ledgerRes, refundsRes] = await Promise.all([
          api.myBalance(),
          api.myLedger({ page: ledgerPage, page_size: 20 }),
          api.myRefunds({ page: refundsPage, page_size: 20 })
        ]);
        if (!active) return;
        setBalance(balanceRes);
        setLedger(ledgerRes.entries);
        setLedgerPagination(ledgerRes.pagination);
        setRefunds(refundsRes.refunds);
        setRefundsPagination(refundsRes.pagination);
      } catch (error) {
        if (!active) return;
        const message = messageForApiError(error, "Failed to load financial data");
        setBalanceError(message);
        setLedgerError(message);
        setRefundsError(message);
      } finally {
        if (!active) return;
        setBalanceLoading(false);
        setLedgerLoading(false);
        setRefundsLoading(false);
      }
    }

    void loadFinancialData();

    return () => {
      active = false;
    };
  }, [ledgerPage, refundsPage, user]);

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
        <p>Профиль, аренды, платежи и финансовая история доступны после авторизации.</p>
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
        </div>
      </div>
      <BalancePanel balance={balance} error={balanceError} loading={balanceLoading} />
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
      <FinancialHistoryPanel
        entries={ledger}
        error={ledgerError}
        loading={ledgerLoading}
        onNextPage={() => setLedgerPage((current) => (ledgerPagination && current < ledgerPagination.total_pages ? current + 1 : current))}
        onPrevPage={() => setLedgerPage((current) => (current > 1 ? current - 1 : current))}
        pagination={ledgerPagination}
      />
      <RefundHistoryPanel
        entries={refunds}
        error={refundsError}
        loading={refundsLoading}
        onNextPage={() => setRefundsPage((current) => (refundsPagination && current < refundsPagination.total_pages ? current + 1 : current))}
        onPrevPage={() => setRefundsPage((current) => (current > 1 ? current - 1 : current))}
        pagination={refundsPagination}
      />
    </section>
  );
}
