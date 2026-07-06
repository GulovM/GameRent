import type { FinancialBalance } from "../../api";
import { money } from "../../utils/format";

type BalancePanelProps = {
  balance: FinancialBalance | null;
  error: string | null;
  loading: boolean;
};

export function BalancePanel({ balance, error, loading }: BalancePanelProps) {
  return (
    <section className="profile-card">
      <h2>Доступный баланс</h2>
      {loading ? (
        <p>Загрузка баланса...</p>
      ) : error ? (
        <p className="error-text">{error}</p>
      ) : (
        <div className="balance-panel-value">
          <strong>{money(balance ? { amount: balance.available_balance, currency: balance.currency } : 0)}</strong>
        </div>
      )}
    </section>
  );
}
