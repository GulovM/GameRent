import { Clock3, Lock, RefreshCcw } from "lucide-react";
import type { Account, FinancialBalance, Payment, Rental, RentalCredentials } from "../../api";
import { gameNames } from "../../utils/accounts";
import { formatTimestamp, money, remaining } from "../../utils/format";
import { getPaymentStatusClass, paymentStatusLabel, PAYMENT_STATUS_PENDING } from "../payments/paymentStatus";
import { refundStatusClass, refundStatusLabel } from "../refunds/refundStatus";
import { depositStatusClass, depositStatusLabel } from "./depositStatus";
import {
  canRequestCredentials,
  findPaymentForRental,
  getRentalStatusClass,
  isRentalExpiredByTime,
  RENTAL_STATUS_ACTIVE,
  RENTAL_STATUS_EXPIRED,
  RENTAL_STATUS_WAITING_PAYMENT,
  rentalStatusLabels
} from "./rentalStatus";

type RentalsViewProps = {
  accounts: Account[];
  balance: FinancialBalance | null;
  balanceLoading: boolean;
  credentials: RentalCredentials | null;
  credentialsError: string | null;
  credentialsLoading: boolean;
  onCancel: (rental: Rental) => void;
  onLoadCredentials: (rental: Rental) => void;
  onPayWithBalance: (rental: Rental) => void;
  onRefreshStatus: () => void;
  onSelectRental: (rentalId: number) => void;
  payments: Payment[];
  rentals: Rental[];
  rentalsRefreshing: boolean;
  selectedRentalId: number | null;
  walletPaymentError: { rentalId: number; message: string } | null;
  walletPaymentLoadingRentalId: number | null;
};

export function RentalsView({
  accounts,
  balance,
  balanceLoading,
  credentials,
  credentialsError,
  credentialsLoading,
  onCancel,
  onLoadCredentials,
  onPayWithBalance,
  onRefreshStatus,
  onSelectRental,
  payments,
  rentals,
  rentalsRefreshing,
  selectedRentalId,
  walletPaymentError,
  walletPaymentLoadingRentalId
}: RentalsViewProps) {
  const selectedRental = rentals.find((item) => item.id === selectedRentalId) ?? rentals[0];
  const selectedPayment = selectedRental ? findPaymentForRental(payments, selectedRental.id) : undefined;
  const selectedRentalExpiredByTime = selectedRental ? isRentalExpiredByTime(selectedRental) : false;
  const selectedRentalEffectiveStatus = selectedRental
    ? selectedRentalExpiredByTime && selectedRental.status === RENTAL_STATUS_ACTIVE
      ? RENTAL_STATUS_EXPIRED
      : selectedRental.status
    : undefined;
  const selectedRentalWaitingPayment = selectedRentalEffectiveStatus === RENTAL_STATUS_WAITING_PAYMENT;
  const availableBalance = balance?.available_balance ?? 0;
  const balanceKnown = balance !== null;
  const hasEnoughBalance = Boolean(selectedRental && balanceKnown && availableBalance >= selectedRental.total_price.amount);
  const walletPaymentLoading = selectedRental ? walletPaymentLoadingRentalId === selectedRental.id : false;
  const paymentWindowExpired =
    selectedRentalWaitingPayment && selectedRental?.payment_expires_at ? remaining(selectedRental.payment_expires_at) === "00:00:00" : false;
  const walletPaymentDisabled =
    !selectedRental ||
    !selectedRentalWaitingPayment ||
    walletPaymentLoading ||
    balanceLoading ||
    !balanceKnown ||
    !selectedPayment ||
    selectedPayment.status !== PAYMENT_STATUS_PENDING ||
    paymentWindowExpired ||
    !hasEnoughBalance;
  const walletPaymentMessage =
    walletPaymentError && selectedRental && walletPaymentError.rentalId === selectedRental.id ? walletPaymentError.message : null;

  function walletPaymentHint() {
    if (!selectedRental || !selectedRentalWaitingPayment) return null;
    if (balanceLoading) return "Проверяем доступный баланс перед оплатой.";
    if (!balanceKnown) return "Баланс временно недоступен. Обновите статус и повторите.";
    if (!selectedPayment || selectedPayment.status !== PAYMENT_STATUS_PENDING) return "Платёж уже обработан или аренда уже оплачена.";
    if (paymentWindowExpired) return "Окно оплаты истекло. Обновите статус аренды.";
    if (!hasEnoughBalance) return "Недостаточно средств на балансе для оплаты этой аренды.";
    return "Steam credentials не выдаются автоматически. Получение доступа остаётся отдельным действием после активации аренды.";
  }

  return (
    <section className="workspace">
      <div className="section-heading">
        <div>
          <h2>Мои аренды</h2>
          <p>Активные сессии, история, оплата, отмена и refund summary. Paid extension is not available yet.</p>
        </div>
        <button className="secondary-button" disabled={rentalsRefreshing} onClick={onRefreshStatus} type="button">
          <RefreshCcw size={18} />
          {rentalsRefreshing ? "Refreshing..." : "Refresh status"}
        </button>
      </div>
      <div className="rental-list">
        {rentals.length > 0 ? (
          rentals.map((rental) => {
            const isExpiredByTime = isRentalExpiredByTime(rental);
            const effectiveStatus = isExpiredByTime && rental.status === RENTAL_STATUS_ACTIVE ? RENTAL_STATUS_EXPIRED : rental.status;
            const account = accounts.find((item) => item.id === rental.account_id);
            const payment = findPaymentForRental(payments, rental.id);
            const waitingForPayment = effectiveStatus === RENTAL_STATUS_WAITING_PAYMENT;
            const canLoadCredentials = canRequestCredentials(rental, payment);

            return (
              <article className={selectedRentalId === rental.id ? "rental-row selected" : "rental-row"} key={rental.id}>
                <div className="row-icon">
                  <Clock3 size={22} />
                </div>
                <div>
                  <h3>{account ? gameNames(account, 1) : `Аккаунт #${rental.account_id}`}</h3>
                  <p>
                    {rentalStatusLabels[effectiveStatus] ?? effectiveStatus} · {isExpiredByTime ? "срок истек" : `до ${remaining(rental.expires_at)}`}
                  </p>
                  <div className="rental-meta">
                    <span className={`status-pill ${getRentalStatusClass(effectiveStatus)}`}>{rentalStatusLabels[effectiveStatus] ?? effectiveStatus}</span>
                    {payment && <span className={`status-pill ${getPaymentStatusClass(payment.status)}`}>{paymentStatusLabel(payment.status)}</span>}
                    <span className={`status-pill ${depositStatusClass(rental.deposit_status)}`}>{depositStatusLabel(rental.deposit_status)}</span>
                    {rental.has_refund && <span className={`status-pill ${refundStatusClass(rental.refund_status)}`}>{refundStatusLabel(rental.refund_status)}</span>}
                    {waitingForPayment && rental.payment_expires_at && <span className="status-pill amber">Pay in {remaining(rental.payment_expires_at)}</span>}
                  </div>
                </div>
                <strong>{money(rental.total_price)}</strong>
                <div className="row-actions">
                  <button className="secondary-button" onClick={() => onSelectRental(rental.id)} type="button">
                    Details
                  </button>
                  <button className="danger-button" disabled={rental.status !== RENTAL_STATUS_WAITING_PAYMENT} onClick={() => onCancel(rental)} type="button">
                    Отменить
                  </button>
                  {canLoadCredentials && (
                    <button
                      aria-label={`Get credentials for rental ${rental.id}`}
                      className="primary-button"
                      onClick={() => onLoadCredentials(rental)}
                      type="button"
                    >
                      <Lock size={18} />
                      Credentials
                    </button>
                  )}
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
      {selectedRental && (
        <div className="rental-detail-card">
          <div className="section-heading">
            <div>
              <h2>{`Rental #${selectedRental.id}`}</h2>
              <p>Status, payment window, refund summary and controlled credential access.</p>
            </div>
          </div>
          <div className="rental-detail-grid">
            <div className="detail-item">
              <span>Rental status</span>
              <strong>{selectedRentalEffectiveStatus ? (rentalStatusLabels[selectedRentalEffectiveStatus] ?? selectedRentalEffectiveStatus) : "No data"}</strong>
            </div>
            <div className="detail-item">
              <span>Payment status</span>
              <strong>{selectedPayment ? paymentStatusLabel(selectedPayment.status) : "No payment data"}</strong>
            </div>
            <div className="detail-item">
              <span>Rental ends</span>
              <strong>{formatTimestamp(selectedRental.expires_at)}</strong>
            </div>
            <div className="detail-item">
              <span>Payment expires</span>
              <strong>{formatTimestamp(selectedRental.payment_expires_at)}</strong>
            </div>
            <div className="detail-item">
              <span>Deposit amount</span>
              <strong>{money(selectedRental.security_deposit)}</strong>
            </div>
            <div className="detail-item">
              <span>Rental price</span>
              <strong>{money(selectedRental.rental_price)}</strong>
            </div>
            <div className="detail-item">
              <span>Total amount</span>
              <strong>{money(selectedRental.total_price)}</strong>
            </div>
            <div className="detail-item">
              <span>Available balance</span>
              <strong>{balanceLoading ? "Loading..." : money(balance ? { amount: balance.available_balance, currency: balance.currency } : 0)}</strong>
            </div>
            <div className="detail-item">
              <span>Deposit status</span>
              <strong>{depositStatusLabel(selectedRental.deposit_status)}</strong>
            </div>
            <div className="detail-item">
              <span>Refund status</span>
              <strong>{selectedRental.has_refund ? refundStatusLabel(selectedRental.refund_status) : "No refund"}</strong>
            </div>
            <div className="detail-item">
              <span>Refund total</span>
              <strong>{money(selectedRental.refund_total_amount)}</strong>
            </div>
            <div className="detail-item">
              <span>Refund processed</span>
              <strong>{selectedRental.processed_at ? formatTimestamp(selectedRental.processed_at) : "No data"}</strong>
            </div>
          </div>
          {selectedRentalEffectiveStatus === RENTAL_STATUS_WAITING_PAYMENT && (
            <>
              <div className="safety-note">
                <Clock3 size={20} />
                <span>
                  Credentials stay unavailable until payment is confirmed. Remaining window:{" "}
                  {selectedRental.payment_expires_at ? remaining(selectedRental.payment_expires_at) : "00:00:00"}
                </span>
              </div>
              <div className="wallet-payment-panel">
                <div className="wallet-payment-summary">
                  <div className="detail-item">
                    <span>Total to pay</span>
                    <strong>{money(selectedRental.total_price)}</strong>
                  </div>
                  <div className="detail-item">
                    <span>Available balance</span>
                    <strong>{balanceLoading ? "Loading..." : money(balance ? { amount: balance.available_balance, currency: balance.currency } : 0)}</strong>
                  </div>
                </div>
                <div className="wallet-payment-state">
                  <span className={hasEnoughBalance ? "status-pill green" : "status-pill danger"}>
                    {hasEnoughBalance ? "Balance is sufficient" : "Balance is insufficient"}
                  </span>
                  {walletPaymentHint() ? <p className={walletPaymentMessage ? "error-text" : "wallet-payment-hint"}>{walletPaymentMessage ?? walletPaymentHint()}</p> : null}
                </div>
                <div className="detail-actions">
                  <button
                    aria-label="Pay with wallet balance"
                    className="primary-button"
                    disabled={walletPaymentDisabled}
                    onClick={() => onPayWithBalance(selectedRental)}
                    type="button"
                  >
                    {walletPaymentLoading ? "Paying..." : "Оплатить с баланса"}
                  </button>
                </div>
              </div>
            </>
          )}
          {selectedRentalEffectiveStatus !== RENTAL_STATUS_WAITING_PAYMENT && selectedRentalEffectiveStatus !== RENTAL_STATUS_ACTIVE && (
            <div className="safety-note">
              <Lock size={20} />
              <span>Credentials are not available for cancelled, expired or completed rentals.</span>
            </div>
          )}
          {credentialsError && <div className="credentials-box error-text">{credentialsError}</div>}
          {credentials && canRequestCredentials(selectedRental, selectedPayment) && (
            <div className="credentials-box">
              <div className="detail-item">
                <span>Steam login</span>
                <strong>{credentials.login}</strong>
              </div>
              <div className="detail-item">
                <span>Steam password</span>
                <strong>{credentials.password}</strong>
              </div>
            </div>
          )}
          {!credentials && selectedRental.status === RENTAL_STATUS_ACTIVE && (
            <div className="detail-actions">
              <button
                aria-label="Get rental credentials"
                className="primary-button"
                disabled={credentialsLoading || !canRequestCredentials(selectedRental, selectedPayment)}
                onClick={() => onLoadCredentials(selectedRental)}
                type="button"
              >
                <Lock size={18} />
                {credentialsLoading ? "Loading..." : "Get credentials"}
              </button>
            </div>
          )}
        </div>
      )}
    </section>
  );
}
