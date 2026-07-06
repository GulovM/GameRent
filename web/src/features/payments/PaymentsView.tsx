import { Bell } from "lucide-react";
import type { NotificationItem, Payment } from "../../api";
import { DataTable } from "../../components/DataTable";
import { getPaymentStatusClass, paymentStatusLabel } from "./paymentStatus";

type PaymentsViewProps = {
  notifications: NotificationItem[];
  onReadNotification: (item: NotificationItem) => void;
  payments: Payment[];
};

export function PaymentsView({ notifications, onReadNotification, payments }: PaymentsViewProps) {
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
              <td>
                <span className={`status-pill ${getPaymentStatusClass(payment.status)}`}>{paymentStatusLabel(payment.status)}</span>
              </td>
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
