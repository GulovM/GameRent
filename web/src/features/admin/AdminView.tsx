import {
  Activity,
  AlertTriangle,
  Check,
  ChevronLeft,
  ChevronRight,
  Edit3,
  Gamepad2,
  Gauge,
  Plus,
  RefreshCcw,
  RotateCcw,
  ShieldCheck,
  UserCog,
  UsersRound,
  Wallet,
  X
} from "lucide-react";
import { type FormEvent, useEffect, useState } from "react";
import type { ReactNode } from "react";
import {
  isApiError,
  type Account,
  type AdminBalanceAdjustmentInput,
  type AdminBalanceAdjustmentResponse,
  type AdminRentalDetail,
  type AdminRentalFilters,
  type AdminRentalRefundSummary,
  type AdminRentalSummary,
  type AdminWalletRefundResponse,
  type AdminUserPatch,
  type AuditLog,
  type Pagination,
  type RefundReasonCodeOption,
  type User
} from "../../api";
import { DataTable } from "../../components/DataTable";
import { Kpi } from "../../components/Kpi";
import { refundStatusClass, refundStatusLabel } from "../refunds/refundStatus";
import { adminDepositStatusLabel, depositStatusClass } from "../rentals/depositStatus";
import { getRentalStatusClass, rentalStatusLabels, RENTAL_STATUS_COMPLETED, RENTAL_STATUS_EXPIRED } from "../rentals/rentalStatus";
import { getPaymentStatusClass, paymentStatusLabel, PAYMENT_STATUS_SUCCESS } from "../payments/paymentStatus";
import type { AdminAccountPatch, AdminTab } from "../../types/app";
import { accountStatusLabels, gameNames } from "../../utils/accounts";
import { formatTimestamp, money } from "../../utils/format";

type AdminViewProps = {
  accounts: Account[];
  adminRentalDetail: AdminRentalDetail | null;
  adminRentalDetailError: string | null;
  adminRentalDetailLoading: boolean;
  adminRentalFilters: AdminRentalFilters;
  adminRentals: AdminRentalRefundSummary[];
  adminRentalsError: string | null;
  adminRentalsLoading: boolean;
  adminRentalsPagination: Pagination | null;
  adminRentalsSummary: AdminRentalSummary | null;
  auditLogs: AuditLog[];
  onCloseAdminRentalDetail: () => void;
  onAdminRentalFiltersChange: (filters: AdminRentalFilters) => Promise<void>;
  onAdminRentalFiltersReset: () => Promise<void>;
  onCreateAccount: (payload: {
    steam_id64: string;
    steam_login: string;
    steam_password: string;
    price_per_hour: number;
    security_deposit: number;
  }) => Promise<void>;
  onNextRefundPage: () => Promise<void>;
  onOpenAdminRentalDetail: (rentalId: number) => Promise<void>;
  onPrevRefundPage: () => Promise<void>;
  onSync: (account: Account) => Promise<void>;
  onUpdateAccount: (account: Account, patch: AdminAccountPatch) => Promise<void>;
  onAdjustBalance: (targetUser: User, input: AdminBalanceAdjustmentInput) => Promise<AdminBalanceAdjustmentResponse>;
  onUpdateUser: (targetUser: User, patch: AdminUserPatch) => Promise<void>;
  onWalletRefund: (rentalId: number, reasonCode: string) => Promise<AdminWalletRefundResponse>;
  refundReasonOptions: RefundReasonCodeOption[];
  user: User | null;
  users: User[];
};

export function AdminView({
  accounts,
  adminRentalDetail,
  adminRentalDetailError,
  adminRentalDetailLoading,
  adminRentalFilters,
  adminRentals,
  adminRentalsError,
  adminRentalsLoading,
  adminRentalsPagination,
  adminRentalsSummary,
  auditLogs,
  onCloseAdminRentalDetail,
  onAdminRentalFiltersChange,
  onAdminRentalFiltersReset,
  onCreateAccount,
  onNextRefundPage,
  onOpenAdminRentalDetail,
  onPrevRefundPage,
  onSync,
  onUpdateAccount,
  onAdjustBalance,
  onUpdateUser,
  onWalletRefund,
  refundReasonOptions,
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
        <h2>ADMIN role required</h2>
        <p>Administrative tools are available only to users with ADMIN role.</p>
      </section>
    );
  }

  const tabs: Array<{ id: AdminTab; label: string; icon: ReactNode }> = [
    { id: "overview", label: "Overview", icon: <Gauge size={18} /> },
    { id: "accounts", label: "Accounts", icon: <Gamepad2 size={18} /> },
    { id: "refunds", label: "Refunds", icon: <Wallet size={18} /> },
    { id: "users", label: "Users", icon: <UsersRound size={18} /> },
    { id: "audit", label: "Audit", icon: <Activity size={18} /> }
  ];

  return (
    <section className="admin-layout">
      <aside className="admin-sidebar">
        <h2>Admin console</h2>
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
            <p>Dedicated admin workflows for accounts, users, wallet refunds and audit review.</p>
          </div>
          {tab === "accounts" && (
            <button className="primary-button" onClick={() => setCreateOpen(true)} type="button">
              <Plus size={18} />
              Add account
            </button>
          )}
        </div>

        {tab === "overview" && <AdminOverview accounts={accounts} auditLogs={auditLogs} refundCandidates={adminRentalsSummary?.eligible_wallet_refund_count ?? 0} users={users} />}
        {tab === "accounts" && (
          <AdminAccountsTable
            accounts={accounts}
            onCreate={() => setCreateOpen(true)}
            onEdit={setEditingAccount}
            onSync={onSync}
          />
        )}
        {tab === "refunds" && (
          <AdminRefundsSection
            detail={adminRentalDetail}
            detailError={adminRentalDetailError}
            detailLoading={adminRentalDetailLoading}
            error={adminRentalsError}
            filters={adminRentalFilters}
            loading={adminRentalsLoading}
            onCloseDetail={onCloseAdminRentalDetail}
            onFiltersChange={onAdminRentalFiltersChange}
            onNextPage={onNextRefundPage}
            onOpenDetail={onOpenAdminRentalDetail}
            onPrevPage={onPrevRefundPage}
            onResetFilters={onAdminRentalFiltersReset}
            onWalletRefund={onWalletRefund}
            pagination={adminRentalsPagination}
            refundReasonOptions={refundReasonOptions}
            rentals={adminRentals}
          />
        )}
        {tab === "users" && <AdminUsersTable onAdjustBalance={onAdjustBalance} onUpdateUser={onUpdateUser} users={users} />}
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
function canRenderRefundAction(rental: AdminRentalRefundSummary) {
  return (
    rental.payment_provider === "balance" &&
    rental.payment_status === PAYMENT_STATUS_SUCCESS &&
    (rental.status === RENTAL_STATUS_EXPIRED || rental.status === RENTAL_STATUS_COMPLETED) &&
    rental.refund_status !== "COMPLETED"
  );
}

function AdminOverview({
  accounts,
  auditLogs,
  refundCandidates,
  users
}: {
  accounts: Account[];
  auditLogs: AuditLog[];
  refundCandidates: number;
  users: User[];
}) {
  return (
    <>
      <div className="kpi-grid">
        <Kpi icon={<Gamepad2 size={22} />} label="Total accounts" value={accounts.length} />
        <Kpi icon={<Check size={22} />} label="Available" value={accounts.filter((item) => item.status === "Available").length} />
        <Kpi icon={<Wallet size={22} />} label="Refund candidates" value={refundCandidates} />
        <Kpi icon={<UsersRound size={22} />} label="Users" value={users.length} />
      </div>
      <AuditLogList auditLogs={auditLogs.slice(0, 8)} compact />
    </>
  );
}

const rentalStatusFilterOptions = ["WAITING_PAYMENT", "ACTIVE", "EXPIRED", "CANCELLED", "COMPLETED"] as const;
const paymentStatusFilterOptions = ["PENDING", "SUCCESS", "FAILED"] as const;
const paymentProviderFilterOptions = ["balance", "internal"] as const;
const depositStatusFilterOptions = ["NONE", "HELD", "RELEASED", "FORFEITED", "REFUNDED", "UNKNOWN"] as const;
const depositStatusFilterLabels: Record<(typeof depositStatusFilterOptions)[number], string> = {
  NONE: "Не удерживается",
  HELD: "Удержан, ожидает решения",
  RELEASED: "Возвращён на баланс",
  FORFEITED: "Удержан администратором",
  REFUNDED: "Возвращён при возврате",
  UNKNOWN: "Неизвестен — требуется проверка"
};
const refundStatusFilterOptions = ["NONE", "REQUESTED", "COMPLETED", "FAILED"] as const;

function AdminRefundsSection({
  detail,
  detailError,
  detailLoading,
  error,
  filters,
  loading,
  onCloseDetail,
  onFiltersChange,
  onNextPage,
  onOpenDetail,
  onPrevPage,
  onResetFilters,
  onWalletRefund,
  pagination,
  refundReasonOptions,
  rentals
}: {
  detail: AdminRentalDetail | null;
  detailError: string | null;
  detailLoading: boolean;
  error: string | null;
  filters: AdminRentalFilters;
  loading: boolean;
  onCloseDetail: () => void;
  onFiltersChange: (filters: AdminRentalFilters) => Promise<void>;
  onNextPage: () => Promise<void>;
  onOpenDetail: (rentalId: number) => Promise<void>;
  onPrevPage: () => Promise<void>;
  onResetFilters: () => Promise<void>;
  onWalletRefund: (rentalId: number, reasonCode: string) => Promise<AdminWalletRefundResponse>;
  pagination: Pagination | null;
  refundReasonOptions: RefundReasonCodeOption[];
  rentals: AdminRentalRefundSummary[];
}) {
  const [confirmRentalId, setConfirmRentalId] = useState<number | null>(null);
  const [reasonByRental, setReasonByRental] = useState<Record<number, string>>({});
  const [submittingRentalId, setSubmittingRentalId] = useState<number | null>(null);
  const [feedback, setFeedback] = useState<{ type: "ok" | "error"; message: string } | null>(null);

  const currentPage = pagination?.page ?? 1;
  const totalPages = pagination?.total_pages ?? 0;

  function selectedReason(rentalId: number) {
    return reasonByRental[rentalId] ?? refundReasonOptions[0]?.code ?? "";
  }

  function canRefund(rental: AdminRentalRefundSummary) {
    return (
      rental.payment_provider === "balance" &&
      rental.payment_status === PAYMENT_STATUS_SUCCESS &&
      (rental.status === RENTAL_STATUS_EXPIRED || rental.status === RENTAL_STATUS_COMPLETED) &&
      rental.refund_status !== "COMPLETED"
    );
  }

  function refundableDepositAmount(rental: AdminRentalRefundSummary) {
    return rental.deposit_status === "HELD" ? rental.security_deposit.amount : 0;
  }

  function refundBlockedReason(rental: AdminRentalRefundSummary) {
    if (rental.refund_status === "COMPLETED") {
      return "Refund already completed.";
    }
    if (rental.payment_provider !== "balance") {
      return "Provider-paid rentals are excluded from this flow.";
    }
    if (rental.payment_status !== PAYMENT_STATUS_SUCCESS) {
      return "Payment must be SUCCESS before refund.";
    }
    if (rental.status !== RENTAL_STATUS_EXPIRED && rental.status !== RENTAL_STATUS_COMPLETED) {
      return "Rental must be EXPIRED or COMPLETED.";
    }
    return "Refund is unavailable for this rental.";
  }

  function updateFilters(patch: Partial<AdminRentalFilters>) {
    void onFiltersChange({
      ...filters,
      ...patch
    });
  }

  async function submitRefund(rental: AdminRentalRefundSummary) {
    if (submittingRentalId === rental.id || !canRefund(rental) || !selectedReason(rental.id)) {
      return;
    }

    setSubmittingRentalId(rental.id);
    setFeedback(null);

    try {
      const result = await onWalletRefund(rental.id, selectedReason(rental.id));
      setConfirmRentalId((current) => (current === rental.id ? null : current));
      setFeedback({
        type: "ok",
        message: result.idempotent
          ? `Refund for rental #${rental.id} was already completed.`
          : `Refund completed for rental #${rental.id}: ${money(result.total_amount)} credited.`
      });
    } catch (error) {
      if (isApiError(error)) {
        if (error.status === 401 || error.status === 403) {
          setFeedback({ type: "error", message: "Admin session is unavailable or permission was denied." });
        } else if (error.status === 404 || error.status === 409) {
          setFeedback({ type: "error", message: "Rental is no longer eligible. Data was refreshed from backend state." });
        } else {
          setFeedback({ type: "error", message: "Refund request failed. Retry after the network recovers." });
        }
        return;
      }
      setFeedback({ type: "error", message: "Refund request failed. Retry after the network recovers." });
    } finally {
      setSubmittingRentalId((current) => (current === rental.id ? null : current));
    }
  }

  return (
    <section className="profile-form admin-refund-panel">
      <div className="section-heading">
        <div>
          <h2>Wallet refunds</h2>
          <p>Read-only rental summary plus one controlled refund action for eligible balance-paid rentals.</p>
        </div>
        <div className="row-actions">
          <button className="secondary-button" disabled={loading || currentPage <= 1} onClick={() => void onPrevPage()} type="button">
            <ChevronLeft size={18} />
            Prev
          </button>
          <button className="secondary-button" disabled={loading || totalPages === 0 || currentPage >= totalPages} onClick={() => void onNextPage()} type="button">
            Next
            <ChevronRight size={18} />
          </button>
        </div>
      </div>

      <div className="admin-filter-panel" data-testid="admin-rentals-filters">
        <label>
          <span>Rental status</span>
          <select aria-label="Rental status filter" onChange={(event) => updateFilters({ rental_status: event.target.value as AdminRentalFilters["rental_status"] })} value={filters.rental_status ?? ""}>
            <option value="">All</option>
            {rentalStatusFilterOptions.map((option) => (
              <option key={option} value={option}>
                {option}
              </option>
            ))}
          </select>
        </label>
        <label>
          <span>Payment status</span>
          <select aria-label="Payment status filter" onChange={(event) => updateFilters({ payment_status: event.target.value as AdminRentalFilters["payment_status"] })} value={filters.payment_status ?? ""}>
            <option value="">All</option>
            {paymentStatusFilterOptions.map((option) => (
              <option key={option} value={option}>
                {option}
              </option>
            ))}
          </select>
        </label>
        <label>
          <span>Provider</span>
          <select aria-label="Payment provider filter" onChange={(event) => updateFilters({ payment_provider: event.target.value as AdminRentalFilters["payment_provider"] })} value={filters.payment_provider ?? ""}>
            <option value="">All</option>
            {paymentProviderFilterOptions.map((option) => (
              <option key={option} value={option}>
                {option}
              </option>
            ))}
          </select>
        </label>
        <label>
          <span>Deposit</span>
          <select aria-label="Deposit status filter" onChange={(event) => updateFilters({ deposit_status: event.target.value as AdminRentalFilters["deposit_status"] })} value={filters.deposit_status ?? ""}>
            <option value="">All</option>
            {depositStatusFilterOptions.map((option) => (
              <option key={option} value={option}>
                {depositStatusFilterLabels[option]}
              </option>
            ))}
          </select>
        </label>
        <label>
          <span>Refund</span>
          <select aria-label="Refund status filter" onChange={(event) => updateFilters({ refund_status: event.target.value as AdminRentalFilters["refund_status"] })} value={filters.refund_status ?? ""}>
            <option value="">All</option>
            {refundStatusFilterOptions.map((option) => (
              <option key={option} value={option}>
                {option}
              </option>
            ))}
          </select>
        </label>
        <label>
          <span>Eligible</span>
          <select
            aria-label="Eligible wallet refund filter"
            onChange={(event) =>
              updateFilters({
                eligible_wallet_refund: event.target.value === "" ? undefined : event.target.value === "true"
              })
            }
            value={filters.eligible_wallet_refund === undefined ? "" : String(filters.eligible_wallet_refund)}
          >
            <option value="">All</option>
            <option value="true">true</option>
            <option value="false">false</option>
          </select>
        </label>
        <label>
          <span>User ID</span>
          <input
            aria-label="User ID filter"
            inputMode="numeric"
            min={1}
            onChange={(event) => updateFilters({ user_id: event.target.value ? Number(event.target.value) : undefined })}
            type="number"
            value={filters.user_id ?? ""}
          />
        </label>
        <label>
          <span>Rental ID</span>
          <input
            aria-label="Rental ID filter"
            inputMode="numeric"
            min={1}
            onChange={(event) => updateFilters({ rental_id: event.target.value ? Number(event.target.value) : undefined })}
            type="number"
            value={filters.rental_id ?? ""}
          />
        </label>
        <button className="secondary-button" onClick={() => void onResetFilters()} type="button">
          Reset filters
        </button>
      </div>

      {feedback && <div className={`admin-refund-feedback ${feedback.type}`}>{feedback.message}</div>}
      {error && <div className="admin-refund-feedback error">{error}</div>}

      {loading ? (
        <div className="empty-inline">
          <RotateCcw size={24} />
          <span>Loading admin refund data...</span>
        </div>
      ) : rentals.length === 0 ? (
        <div className="empty-inline">
          <Wallet size={24} />
          <strong>No rentals match the current filters</strong>
          <span>Adjust the filters or reset them to inspect more rentals.</span>
        </div>
      ) : (
        <div className="admin-refund-list">
          {rentals.map((rental) => {
            const eligible = canRefund(rental);
            const confirming = confirmRentalId === rental.id;
            const submitting = submittingRentalId === rental.id;
            const depositRefund = refundableDepositAmount(rental);
            const totalRefund = rental.rental_price.amount + depositRefund;

            return (
              <article className="admin-refund-card" key={rental.id}>
                <div className="admin-refund-card-top">
                  <div>
                    <strong>Rental #{rental.id}</strong>
                    <div className="rental-meta">
                      <span>User #{rental.user_id}</span>
                      <span>Account #{rental.account_id}</span>
                      {rental.payment_id && <span>Payment #{rental.payment_id}</span>}
                    </div>
                  </div>
                  <div className="rental-meta">
                    <span className={`status-pill ${getRentalStatusClass(rental.status)}`}>{rentalStatusLabels[rental.status] ?? rental.status}</span>
                    <span className={`status-pill ${getPaymentStatusClass(rental.payment_status)}`}>{paymentStatusLabel(rental.payment_status)}</span>
                    <span className={`status-pill ${refundStatusClass(rental.refund_status)}`}>{refundStatusLabel(rental.refund_status)}</span>
                  </div>
                </div>

                <div className="admin-refund-grid">
                  <div className="detail-item">
                    <span>Payment method</span>
                    <strong>{rental.payment_provider || "unknown"}</strong>
                  </div>
                  <div className="detail-item">
                    <span>Rental price</span>
                    <strong>{money(rental.rental_price)}</strong>
                  </div>
                  <div className="detail-item">
                    <span>Deposit amount</span>
                    <strong>{money(rental.security_deposit)}</strong>
                  </div>
                  <div className="detail-item">
                    <span>Deposit status</span>
                    <strong className={`status-pill ${depositStatusClass(rental.deposit_status)}`}>{adminDepositStatusLabel(rental.deposit_status)}</strong>
                  </div>
                  <div className="detail-item">
                    <span>Refund summary</span>
                    <strong>{rental.has_refund ? money(rental.refund_total_amount) : "No refund"}</strong>
                  </div>
                  <div className="detail-item">
                    <span>Processed at</span>
                    <strong>{rental.processed_at ? new Date(rental.processed_at).toLocaleString() : "Not processed"}</strong>
                  </div>
                </div>

                {eligible ? (
                  <>
                    <div className="admin-refund-actions">
                      <button className="secondary-button" onClick={() => void onOpenDetail(rental.id)} type="button">
                        Details
                      </button>
                      <label className="admin-refund-reason">
                        <span>Reason code</span>
                        <select
                          disabled={submitting || refundReasonOptions.length === 0}
                          onChange={(event) => {
                            setReasonByRental((current) => ({ ...current, [rental.id]: event.target.value }));
                          }}
                          value={selectedReason(rental.id)}
                        >
                          {refundReasonOptions.map((option) => (
                            <option key={option.code} value={option.code}>
                              {option.label}
                            </option>
                          ))}
                        </select>
                      </label>
                      <button
                        className="secondary-button"
                        disabled={submitting || refundReasonOptions.length === 0}
                        onClick={() => {
                          setFeedback(null);
                          setConfirmRentalId((current) => (current === rental.id ? null : rental.id));
                        }}
                        type="button"
                      >
                        <AlertTriangle size={16} />
                        Review refund
                      </button>
                    </div>

                    {confirming && (
                      <div className="admin-refund-confirm">
                        <div className="admin-refund-confirm-grid">
                          <span>
                            Principal <strong>{money(rental.rental_price)}</strong>
                          </span>
                          <span>
                            Deposit <strong>{money({ amount: depositRefund, currency: rental.security_deposit.currency })}</strong>
                          </span>
                          <span>
                            Total <strong>{money({ amount: totalRefund, currency: rental.total_price.currency })}</strong>
                          </span>
                          <span>
                            Reason <strong>{selectedReason(rental.id)}</strong>
                          </span>
                        </div>
                        <div className="row-actions">
                          <button className="secondary-button" disabled={submitting} onClick={() => setConfirmRentalId(null)} type="button">
                            Cancel
                          </button>
                          <button className="danger-button" disabled={submitting} onClick={() => void submitRefund(rental)} type="button">
                            <Wallet size={16} />
                            {submitting ? "Refunding..." : "Confirm refund"}
                          </button>
                        </div>
                      </div>
                    )}
                    {refundReasonOptions.length === 0 && <div className="admin-refund-hint">Refund reason codes are unavailable. Refresh admin data.</div>}
                  </>
                ) : (
                  <div className="admin-refund-hint">
                    <div className="row-actions">
                      <button className="secondary-button" onClick={() => void onOpenDetail(rental.id)} type="button">
                        Details
                      </button>
                    </div>
                    {refundBlockedReason(rental)}
                  </div>
                )}
              </article>
            );
          })}
        </div>
      )}

      {(detailLoading || detail || detailError) && (
        <div className="drawer-backdrop" role="presentation">
          <aside aria-label="Admin rental detail" className="checkout-drawer admin-detail-drawer">
            <div className="drawer-title">
              <div>
                <h2>{detail ? `Rental #${detail.rental.id}` : "Rental detail"}</h2>
                <p>Read-only admin support detail with safe payment, deposit, refund and ledger context.</p>
              </div>
              <button className="ghost icon-button close-button" onClick={onCloseDetail} title="Close detail" type="button">
                <X size={20} />
              </button>
            </div>

            {detailLoading ? (
              <div className="empty-inline">
                <RotateCcw size={24} />
                <span>Loading admin rental detail...</span>
              </div>
            ) : detailError ? (
              <div className="admin-refund-feedback error">{detailError}</div>
            ) : detail ? (
              <AdminRentalDetailPanel detail={detail} />
            ) : (
              <div className="empty-inline">
                <AlertTriangle size={24} />
                <span>No detail data</span>
              </div>
            )}
          </aside>
        </div>
      )}

      <div className="profile-stats">
        <span>
          Page <strong>{currentPage}</strong>
        </span>
        <span>
          Rentals <strong>{pagination?.total_items ?? 0}</strong>
        </span>
      </div>
    </section>
  );
}

function AdminRentalDetailPanel({ detail }: { detail: AdminRentalDetail }) {
  return (
    <div className="admin-detail-body">
      <div className="admin-detail-section">
        <h3>Rental</h3>
        <div className="admin-refund-grid">
          <div className="detail-item">
            <span>Status</span>
            <strong className={`status-pill ${getRentalStatusClass(detail.rental.status)}`}>{rentalStatusLabels[detail.rental.status] ?? detail.rental.status}</strong>
          </div>
          <div className="detail-item">
            <span>User</span>
            <strong>User #{detail.rental.user_id}</strong>
          </div>
          <div className="detail-item">
            <span>Account</span>
            <strong>Account #{detail.rental.account_id}</strong>
          </div>
          <div className="detail-item">
            <span>Rental price</span>
            <strong>{money(detail.rental.rental_price)}</strong>
          </div>
          <div className="detail-item">
            <span>Deposit amount</span>
            <strong>{money(detail.rental.deposit_amount)}</strong>
          </div>
          <div className="detail-item">
            <span>Payment window</span>
            <strong>{formatTimestamp(detail.rental.payment_expires_at)}</strong>
          </div>
          <div className="detail-item">
            <span>Start</span>
            <strong>{formatTimestamp(detail.rental.start_at)}</strong>
          </div>
          <div className="detail-item">
            <span>End</span>
            <strong>{formatTimestamp(detail.rental.end_at)}</strong>
          </div>
          <div className="detail-item">
            <span>Created</span>
            <strong>{formatTimestamp(detail.rental.created_at)}</strong>
          </div>
          <div className="detail-item">
            <span>Updated</span>
            <strong>{formatTimestamp(detail.rental.updated_at)}</strong>
          </div>
        </div>
      </div>

      <div className="admin-detail-section">
        <h3>Payment</h3>
        {detail.payment ? (
          <div className="admin-refund-grid">
            <div className="detail-item">
              <span>Payment ID</span>
              <strong>Payment #{detail.payment.id}</strong>
            </div>
            <div className="detail-item">
              <span>Status</span>
              <strong className={`status-pill ${getPaymentStatusClass(detail.payment.status)}`}>{paymentStatusLabel(detail.payment.status)}</strong>
            </div>
            <div className="detail-item">
              <span>Provider</span>
              <strong>{detail.payment.provider}</strong>
            </div>
            <div className="detail-item">
              <span>Amount</span>
              <strong>{money({ amount: detail.payment.amount, currency: detail.payment.currency })}</strong>
            </div>
            <div className="detail-item">
              <span>Created</span>
              <strong>{formatTimestamp(detail.payment.created_at)}</strong>
            </div>
          </div>
        ) : (
          <div className="admin-refund-hint">No payment data</div>
        )}
      </div>

      <div className="admin-detail-section">
        <h3>Deposit</h3>
        {detail.deposit ? (
          <div className="admin-refund-grid">
            <div className="detail-item">
              <span>Status</span>
              <strong className={`status-pill ${depositStatusClass(detail.deposit.status)}`}>{adminDepositStatusLabel(detail.deposit.status)}</strong>
            </div>
            <div className="detail-item">
              <span>Amount</span>
              <strong>{money({ amount: detail.deposit.amount, currency: detail.deposit.currency })}</strong>
            </div>
            <div className="detail-item">
              <span>Held at</span>
              <strong>{formatTimestamp(detail.deposit.held_at)}</strong>
            </div>
            <div className="detail-item">
              <span>Released at</span>
              <strong>{formatTimestamp(detail.deposit.released_at)}</strong>
            </div>
            <div className="detail-item">
              <span>Forfeited at</span>
              <strong>{formatTimestamp(detail.deposit.forfeited_at)}</strong>
            </div>
            <div className="detail-item">
              <span>Refunded at</span>
              <strong>{formatTimestamp(detail.deposit.refunded_at)}</strong>
            </div>
          </div>
        ) : (
          <div className="admin-refund-hint">No deposit data</div>
        )}
      </div>

      <div className="admin-detail-section">
        <h3>Refund summary</h3>
        <div className="admin-refund-grid">
          <div className="detail-item">
            <span>Refund count</span>
            <strong>{detail.refund_summary.count}</strong>
          </div>
          <div className="detail-item">
            <span>Latest status</span>
            <strong className={`status-pill ${refundStatusClass(detail.refund_summary.latest_refund_status)}`}>{refundStatusLabel(detail.refund_summary.latest_refund_status)}</strong>
          </div>
          <div className="detail-item">
            <span>Total principal</span>
            <strong>{money(detail.refund_summary.total_refunded_principal)}</strong>
          </div>
          <div className="detail-item">
            <span>Total deposit</span>
            <strong>{money(detail.refund_summary.total_refunded_deposit)}</strong>
          </div>
          <div className="detail-item">
            <span>Latest processed</span>
            <strong>{formatTimestamp(detail.refund_summary.latest_processed_at)}</strong>
          </div>
        </div>
      </div>

      <div className="admin-detail-section">
        <h3>Ledger summary</h3>
        <div className="admin-ledger-summary">
          {Object.keys(detail.ledger_summary.counts_by_display_type).length === 0 ? (
            <div className="admin-refund-hint">No ledger entries</div>
          ) : (
            Object.entries(detail.ledger_summary.counts_by_display_type).map(([displayType, count]) => (
              <article className="admin-ledger-summary-card" key={displayType}>
                <strong>{displayType}</strong>
                <span>Count: {count}</span>
                <span>Total: {money(detail.ledger_summary.totals_by_display_type[displayType] ?? 0)}</span>
              </article>
            ))
          )}
        </div>
        <div className="ledger-list">
          {detail.ledger_summary.latest_entries.map((entry) => (
            <article className="ledger-row" key={entry.id}>
              <div>
                <strong>{entry.display_type}</strong>
              </div>
              <div className="ledger-row-side">
                <strong>{money({ amount: entry.amount, currency: entry.currency })}</strong>
                <span>{formatTimestamp(entry.created_at)}</span>
              </div>
            </article>
          ))}
        </div>
      </div>

      <div className="admin-detail-section">
        <h3>Support flags</h3>
        <div className="admin-refund-grid">
          <div className="detail-item">
            <span>Eligible wallet refund</span>
            <strong className={`status-pill ${detail.support_flags.eligible_wallet_refund ? "green" : "danger"}`}>
              {detail.support_flags.eligible_wallet_refund ? "Eligible" : "Not eligible"}
            </strong>
          </div>
          <div className="detail-item">
            <span>Ineligible reason</span>
            <strong>{detail.support_flags.refund_ineligible_reason || "ELIGIBLE"}</strong>
          </div>
          <div className="detail-item">
            <span>Credentials access</span>
            <strong>{detail.support_flags.has_active_credentials_access ? "Active" : "Inactive"}</strong>
          </div>
          <div className="detail-item">
            <span>Payment window expired</span>
            <strong>{detail.support_flags.payment_window_expired ? "Yes" : "No"}</strong>
          </div>
        </div>
      </div>
    </div>
  );
}

function AdminAccountsTable({
  accounts,
  onCreate,
  onEdit,
  onSync
}: {
  accounts: Account[];
  onCreate: () => void;
  onEdit: (account: Account) => void;
  onSync: (account: Account) => Promise<void>;
}) {
  const [busyAccountId, setBusyAccountId] = useState<number | null>(null);

  async function runAccountAction(account: Account, action: (account: Account) => Promise<void>) {
    setBusyAccountId(account.id);
    try {
      await action(account);
    } catch {
      // Parent handler already reports the failure.
    } finally {
      setBusyAccountId(null);
    }
  }

  return (
    <DataTable empty="No accounts" columns={["SteamID", "Library", "Price", "Status", "Actions"]}>
      {accounts.length > 0 ? (
        accounts.map((account) => {
          const disabled = account.status === "Disabled";
          const busy = busyAccountId === account.id;

          return (
            <tr key={account.id}>
              <td>{account.steam_id64}</td>
              <td>{gameNames(account, 2)}</td>
              <td>{money(account.price_per_hour)}/h</td>
              <td>
                <span className={disabled ? "status-pill danger" : "status-pill green"}>{accountStatusLabels[account.status] ?? account.status}</span>
              </td>
              <td>
                <div className="table-actions">
                  <button className="secondary-button icon-label" disabled={busy} onClick={() => onEdit(account)} type="button">
                    <Edit3 size={16} />
                    Edit
                  </button>
                  <button className="secondary-button icon-label" disabled={busy} onClick={() => runAccountAction(account, onSync)} type="button">
                    <RefreshCcw size={16} />
                    {busy ? "Syncing..." : "Sync"}
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
              Add first account
            </button>
          </td>
        </tr>
      )}
    </DataTable>
  );
}

function createAdminBalanceAdjustmentKey() {
  const randomPart = typeof crypto !== "undefined" && typeof crypto.randomUUID === "function"
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  return `admin-balance-adjustment-${randomPart}`;
}

function AdminUsersTable({
  onAdjustBalance,
  onUpdateUser,
  users
}: {
  onAdjustBalance: (targetUser: User, input: AdminBalanceAdjustmentInput) => Promise<AdminBalanceAdjustmentResponse>;
  onUpdateUser: (targetUser: User, patch: AdminUserPatch) => Promise<void>;
  users: User[];
}) {
  return (
    <DataTable empty="No users" columns={["ID", "User", "Role", "Trust", "Balance", "Actions"]}>
      {users.map((item) => (
        <AdminUserRow key={item.id} onAdjustBalance={onAdjustBalance} onUpdateUser={onUpdateUser} user={item} />
      ))}
    </DataTable>
  );
}

function AdminUserRow({
  onAdjustBalance,
  onUpdateUser,
  user
}: {
  onAdjustBalance: (targetUser: User, input: AdminBalanceAdjustmentInput) => Promise<AdminBalanceAdjustmentResponse>;
  onUpdateUser: (targetUser: User, patch: AdminUserPatch) => Promise<void>;
  user: User;
}) {
  const [trust, setTrust] = useState(String(user.trust_score ?? 0));
  const [role, setRole] = useState(user.role === "ADMIN" ? "ADMIN" : "RENT");
  const [busy, setBusy] = useState(false);
  const [displayBalance, setDisplayBalance] = useState(user.balance ?? 0);
  const [adjustmentOpen, setAdjustmentOpen] = useState(false);
  const [adjustmentAmount, setAdjustmentAmount] = useState("");
  const [adjustmentReason, setAdjustmentReason] = useState("");
  const [adjustmentComment, setAdjustmentComment] = useState("");
  const [adjustmentKey, setAdjustmentKey] = useState("");
  const [confirmingAdjustment, setConfirmingAdjustment] = useState(false);
  const [adjusting, setAdjusting] = useState(false);
  const [adjustmentError, setAdjustmentError] = useState<string | null>(null);
  const [adjustmentSuccess, setAdjustmentSuccess] = useState<string | null>(null);

  useEffect(() => {
    setDisplayBalance(user.balance ?? 0);
  }, [user.balance]);

  async function save() {
    setBusy(true);
    try {
      await onUpdateUser(user, { trust_score: Number(trust), role });
    } catch {
      // Parent handler already reports the failure.
    } finally {
      setBusy(false);
    }
  }

  async function toggleBlock() {
    setBusy(true);
    try {
      await onUpdateUser(user, { is_blocked: !user.is_blocked });
    } catch {
      // Parent handler already reports the failure.
    } finally {
      setBusy(false);
    }
  }

  function reviewAdjustment() {
    const amount = Number(adjustmentAmount);
    setAdjustmentError(null);
    setAdjustmentSuccess(null);
    if (!Number.isSafeInteger(amount) || amount === 0) {
      setAdjustmentError("Amount must be a non-zero integer in minor units.");
      return;
    }
    if (!/^[A-Za-z0-9_-]{1,64}$/.test(adjustmentReason.trim())) {
      setAdjustmentError("Reason code may contain only letters, numbers, underscores, and hyphens.");
      return;
    }
    const previewBalance = displayBalance + amount;
    if (!Number.isSafeInteger(previewBalance)) {
      setAdjustmentError("The resulting balance is outside the supported integer range.");
      return;
    }
    if (previewBalance < 0) {
      setAdjustmentError("This debit would make the balance negative.");
      return;
    }
    if (!adjustmentKey) {
      setAdjustmentKey(createAdminBalanceAdjustmentKey());
    }
    setConfirmingAdjustment(true);
  }

  async function confirmAdjustment() {
    if (adjusting) return;
    const amount = Number(adjustmentAmount);
    setAdjusting(true);
    setAdjustmentError(null);
    setAdjustmentSuccess(null);
    try {
      const result = await onAdjustBalance(user, {
        amount,
        currency: "USD",
        reason_code: adjustmentReason.trim(),
        comment: adjustmentComment.trim() || undefined,
        idempotency_key: adjustmentKey
      });
      setDisplayBalance(result.new_balance);
      setAdjustmentSuccess(`Balance adjusted to ${money({ amount: result.new_balance, currency: result.currency })}.`);
      setAdjustmentAmount("");
      setAdjustmentReason("");
      setAdjustmentComment("");
      setAdjustmentKey("");
      setConfirmingAdjustment(false);
    } catch (error) {
      setAdjustmentError(error instanceof Error ? error.message : "Balance adjustment failed.");
    } finally {
      setAdjusting(false);
    }
  }

  return (
    <>
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
            <option value="RENT">Customer</option>
            <option value="ADMIN">Admin</option>
          </select>
        </td>
        <td>
          <input aria-label={`Trust score for ${user.email}`} className="table-input" min="0" onChange={(event) => setTrust(event.target.value)} type="number" value={trust} />
        </td>
        <td>
          <strong data-testid={`admin-user-balance-${user.id}`}>{money({ amount: displayBalance, currency: "USD" })}</strong>
        </td>
        <td>
          <div className="table-actions">
            <button className="secondary-button icon-label" disabled={busy} onClick={save} type="button">
              <Check size={16} />
              Save
            </button>
            <button className={user.is_blocked ? "success-button icon-label" : "danger-button icon-label"} disabled={busy} onClick={toggleBlock} type="button">
              <UserCog size={16} />
              {user.is_blocked ? "Unblock" : "Block"}
            </button>
            <button className="secondary-button icon-label" disabled={busy || adjusting} onClick={() => setAdjustmentOpen((current) => !current)} type="button">
              <Wallet size={16} />
              Adjust balance
            </button>
          </div>
        </td>
      </tr>
      {adjustmentOpen && (
        <tr>
          <td colSpan={6}>
            <section className="admin-balance-adjustment" aria-label={`Balance adjustment for ${user.email}`}>
              <div className="form-grid">
                <label>
                  Signed amount in minor units
                  <input
                    aria-label={`Balance adjustment amount for ${user.email}`}
                    onChange={(event) => {
                      setAdjustmentAmount(event.target.value);
                      setConfirmingAdjustment(false);
                    }}
                    step="1"
                    type="number"
                    value={adjustmentAmount}
                  />
                  <small>Positive credits; negative debits.</small>
                </label>
                <label>
                  Reason code
                  <input
                    aria-label={`Balance adjustment reason for ${user.email}`}
                    maxLength={64}
                    onChange={(event) => {
                      setAdjustmentReason(event.target.value);
                      setConfirmingAdjustment(false);
                    }}
                    placeholder="MANUAL_COMPENSATION"
                    value={adjustmentReason}
                  />
                </label>
                <label>
                  Comment (optional)
                  <input
                    aria-label={`Balance adjustment comment for ${user.email}`}
                    maxLength={500}
                    onChange={(event) => {
                      setAdjustmentComment(event.target.value);
                      setConfirmingAdjustment(false);
                    }}
                    value={adjustmentComment}
                  />
                </label>
              </div>
              {adjustmentError && <p className="error-text">{adjustmentError}</p>}
              {adjustmentSuccess && <p className="success-text">{adjustmentSuccess}</p>}
              {confirmingAdjustment ? (
                <div className="admin-refund-confirm">
                  <strong>
                    Confirm {Number(adjustmentAmount) < 0 ? "debit" : "credit"} of {money({ amount: Math.abs(Number(adjustmentAmount)), currency: "USD" })}
                  </strong>
                  <span>
                    Balance: {money({ amount: displayBalance, currency: "USD" })} to {money({ amount: displayBalance + Number(adjustmentAmount), currency: "USD" })}
                  </span>
                  <div className="row-actions">
                    <button className="secondary-button" disabled={adjusting} onClick={() => setConfirmingAdjustment(false)} type="button">
                      Back
                    </button>
                    <button className="danger-button" disabled={adjusting} onClick={confirmAdjustment} type="button">
                      {adjusting ? "Adjusting..." : "Confirm adjustment"}
                    </button>
                  </div>
                </div>
              ) : (
                <button className="primary-button" disabled={adjusting} onClick={reviewAdjustment} type="button">
                  Review adjustment
                </button>
              )}
            </section>
          </td>
        </tr>
      )}
    </>
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
          <strong>No audit events yet</strong>
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
      setError(err instanceof Error ? err.message : "Failed to create account");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="dialog-backdrop">
      <form className="auth-dialog wide-dialog" onSubmit={submit}>
        <button className="ghost icon-button close-button" onClick={onClose} title="Close" type="button">
          <X size={20} />
        </button>
        <Plus size={28} />
        <h2>Add account</h2>
        <label>
          SteamID64
          <input onChange={(event) => setSteamId(event.target.value)} required value={steamId} />
        </label>
        <div className="two-fields">
          <label>
            Steam login
            <input onChange={(event) => setLogin(event.target.value)} required value={login} />
          </label>
          <label>
            Steam password
            <input onChange={(event) => setPassword(event.target.value)} required type="password" value={password} />
          </label>
        </div>
        <div className="two-fields">
          <label>
            Hourly price
            <input min="1" onChange={(event) => setPrice(event.target.value)} required type="number" value={price} />
          </label>
          <label>
            Deposit
            <input min="0" onChange={(event) => setDeposit(event.target.value)} required type="number" value={deposit} />
          </label>
        </div>
        {error && <span className="form-error">{error}</span>}
        <button className="primary-button full" disabled={busy} type="submit">
          {busy ? "Creating..." : "Create account"}
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
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await onSave({
        price_per_hour: Number(price),
        security_deposit: Number(deposit)
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to update account");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="dialog-backdrop">
      <form className="auth-dialog" onSubmit={submit}>
        <button className="ghost icon-button close-button" onClick={onClose} title="Close" type="button">
          <X size={20} />
        </button>
        <Edit3 size={28} />
        <h2>Edit account</h2>
        <p className="dialog-subtitle">{account.steam_id64}</p>
        <p className="dialog-subtitle">Lifecycle status is read-only here: {accountStatusLabels[account.status] ?? account.status}.</p>
        <label>
          Hourly price
          <input min="1" onChange={(event) => setPrice(event.target.value)} required type="number" value={price} />
        </label>
        <label>
          Deposit
          <input min="0" onChange={(event) => setDeposit(event.target.value)} required type="number" value={deposit} />
        </label>
        {error && <span className="form-error">{error}</span>}
        <button className="primary-button full" disabled={busy} type="submit">
          {busy ? "Saving..." : "Save"}
        </button>
      </form>
    </div>
  );
}
