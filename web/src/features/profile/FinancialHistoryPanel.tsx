import { ChevronLeft, ChevronRight, Wallet } from "lucide-react";
import type { LedgerEntry, Pagination } from "../../api";
import { formatTimestamp, money } from "../../utils/format";

type FinancialHistoryPanelProps = {
  entries: LedgerEntry[];
  error: string | null;
  loading: boolean;
  onNextPage: () => void;
  onPrevPage: () => void;
  pagination: Pagination | null;
};

export function FinancialHistoryPanel({
  entries,
  error,
  loading,
  onNextPage,
  onPrevPage,
  pagination
}: FinancialHistoryPanelProps) {
  const hasEntries = entries.length > 0;
  const currentPage = pagination?.page ?? 1;
  const totalPages = pagination?.total_pages ?? 0;

  return (
    <section className="profile-form">
      <div className="section-heading">
        <div>
          <h2>Финансовая история</h2>
          <p>Только безопасные read-only записи без технической metadata.</p>
        </div>
        <div className="row-actions">
          <button className="secondary-button" disabled={currentPage <= 1 || loading} onClick={onPrevPage} type="button">
            <ChevronLeft size={18} />
            Назад
          </button>
          <button className="secondary-button" disabled={loading || totalPages === 0 || currentPage >= totalPages} onClick={onNextPage} type="button">
            Вперёд
            <ChevronRight size={18} />
          </button>
        </div>
      </div>
      {loading ? (
        <div className="empty-inline">
          <Wallet size={24} />
          <span>Загрузка ledger history...</span>
        </div>
      ) : error ? (
        <p className="error-text">{error}</p>
      ) : !hasEntries ? (
        <div className="empty-inline">
          <Wallet size={24} />
          <strong>История пуста</strong>
          <span>Финансовые записи появятся после оплаты или settlement депозита.</span>
        </div>
      ) : (
        <div className="ledger-list">
          {entries.map((entry) => (
            <article className="ledger-row" key={entry.id}>
              <div>
                <strong>{entry.display_type}</strong>
                <div className="rental-meta">
                  {entry.rental_id ? <span>Rental #{entry.rental_id}</span> : null}
                  {entry.payment_id ? <span>Payment #{entry.payment_id}</span> : null}
                </div>
              </div>
              <div className="ledger-row-side">
                <strong>{money({ amount: entry.amount, currency: entry.currency })}</strong>
                <span>{formatTimestamp(entry.created_at)}</span>
              </div>
            </article>
          ))}
        </div>
      )}
      <div className="profile-stats">
        <span>
          Страница <strong>{currentPage}</strong>
        </span>
        <span>
          Всего записей <strong>{pagination?.total_items ?? 0}</strong>
        </span>
      </div>
    </section>
  );
}
