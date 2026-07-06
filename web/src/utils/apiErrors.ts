import { isApiError } from "../api";

export function isUnauthorized(error: unknown) {
  return isApiError(error) && error.status === 401;
}

export function messageForApiError(error: unknown, fallback: string) {
  if (isApiError(error)) {
    if (error.status === 404 || error.status === 403) return "Rental data is unavailable.";
    if (error.status === 409) return "The account is no longer available or the rental state has changed.";
    if (error.status >= 500) return fallback;
    return error.message || fallback;
  }
  return error instanceof Error ? error.message : fallback;
}

export function messageForWalletPaymentError(error: unknown, fallback: string) {
  if (isApiError(error)) {
    const message = error.message.toLowerCase();
    if (error.status === 404 || error.status === 403) {
      return "Аренда больше недоступна для оплаты с баланса.";
    }
    if (error.status === 409 && message.includes("insufficient balance")) {
      return "Недостаточно средств на балансе для оплаты этой аренды.";
    }
    if (error.status === 409) {
      return "Статус оплаты уже изменился. Мы обновили данные аренды.";
    }
    if (error.status >= 500) {
      return fallback;
    }
  }
  return fallback;
}
