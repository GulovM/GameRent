export const PAYMENT_STATUS_PENDING = 1;
export const PAYMENT_STATUS_SUCCESS = 2;
export const PAYMENT_STATUS_FAILED = 3;
export const PAYMENT_STATUS_CANCELLED = 4;

export const paymentStatusLabels: Record<number, string> = {
  1: "Pending",
  2: "Success",
  3: "Failed",
  4: "Cancelled"
};

export function paymentStatusLabel(status: number | string) {
  if (typeof status === "string") return status;
  return paymentStatusLabels[status] ?? String(status);
}

export function getPaymentStatusClass(status: number | string) {
  if (status === PAYMENT_STATUS_SUCCESS || status === "success") return "green";
  if (status === PAYMENT_STATUS_PENDING || status === "pending") return "amber";
  if (status === PAYMENT_STATUS_FAILED || status === PAYMENT_STATUS_CANCELLED) return "danger";
  return "muted";
}
