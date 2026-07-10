import { ChevronLeft, ChevronRight, RotateCcw } from "lucide-react";
import type { Pagination, RefundEntry } from "../../api";
import { refundStatusClass, refundStatusLabel } from "../refunds/refundStatus";
import { formatTimestamp, money } from "../../utils/format";

type RefundHistoryPanelProps = {
  entries: RefundEntry[];
  error: string | null;
  loading: boolean;
  onNextPage: () => void;
  onPrevPage: () => void;
  pagination: Pagination | null;
};

export function RefundHistoryPanel({ entries, error, loading, onNextPage, onPrevPage, pagination }: RefundHistoryPanelProps) {
  const currentPage = pagination?.page ?? 1;
  const totalPages = pagination?.total_pages ?? 0;

  return (
    <section className="profile-form">
      <div className="section-heading">
        <div>
          <h2>Refund history</h2>
          <p>Read-only refund records without internal metadata or actor details.</p>
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
          <RotateCcw size={24} />
          <span>Загрузка refund history...</span>
        </div>
      ) : error ? (
        <p className="error-text">{error}</p>
      ) : entries.length === 0 ? (
        <div className="empty-inline">
          <RotateCcw size={24} />
          <strong>Refund history is empty</strong>
          <span>Completed wallet refunds will appear here.</span>
        </div>
      ) : (
        <div className="ledger-list">
          {entries.map((entry) => (
            <article className="ledger-row" key={entry.id}>
              <div>
                <strong>Rental #{entry.rental_id}</strong>
                <div className="rental-meta">
                  <span>Payment #{entry.payment_id}</span>
                  <span className={`status-pill ${refundStatusClass(entry.status)}`}>{refundStatusLabel(entry.status)}</span>
                </div>
                <div className="refund-breakdown">
                  <span>Principal: {money({ amount: entry.principal_amount, currency: entry.currency })}</span>
                  <span>Deposit: {money({ amount: entry.deposit_amount, currency: entry.currency })}</span>
                </div>
              </div>
              <div className="ledger-row-side">
                <strong>{money({ amount: entry.total_amount, currency: entry.currency })}</strong>
                <span>{formatTimestamp(entry.processed_at ?? entry.created_at)}</span>
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
          Всего возвратов <strong>{pagination?.total_items ?? 0}</strong>
        </span>
      </div>
    </section>
  );
}
