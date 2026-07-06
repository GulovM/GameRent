import type { Payment, Rental } from "../../api";
import { PAYMENT_STATUS_SUCCESS } from "../payments/paymentStatus";

export const RENTAL_STATUS_WAITING_PAYMENT = 1;
export const RENTAL_STATUS_ACTIVE = 2;
export const RENTAL_STATUS_EXPIRED = 3;
export const RENTAL_STATUS_COMPLETED = 4;
export const RENTAL_STATUS_CANCELLED = 5;

export const RENTAL_POLL_INTERVAL_MS = 5000;

export const rentalStatusLabels: Record<number, string> = {
  0: "Создана",
  1: "Ожидает оплаты",
  2: "Активна",
  3: "Истекла",
  4: "Завершена",
  5: "Отменена"
};

export function getRentalStatusClass(status: number) {
  if (status === RENTAL_STATUS_ACTIVE) return "green";
  if (status === RENTAL_STATUS_WAITING_PAYMENT) return "amber";
  if (status === RENTAL_STATUS_EXPIRED || status === RENTAL_STATUS_CANCELLED) return "danger";
  return "muted";
}

export function isRentalExpiredByTime(rental: Rental) {
  return new Date(rental.expires_at).getTime() <= Date.now();
}

export function canRequestCredentials(rental: Rental, payment?: Payment) {
  return rental.status === RENTAL_STATUS_ACTIVE && !isRentalExpiredByTime(rental) && payment?.status === PAYMENT_STATUS_SUCCESS;
}

export function findPaymentForRental(payments: Payment[], rentalId: number) {
  return payments.find((payment) => payment.rental_id === rentalId);
}
