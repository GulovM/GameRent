import type { Money } from "../api";

export function money(value?: Money | number, fallbackCurrency = "USD") {
  if (typeof value === "number") return `${value} ${fallbackCurrency}`;
  if (!value) return `0 ${fallbackCurrency}`;
  return `${value.amount} ${value.currency}`;
}

export function remaining(expiresAt: string) {
  const ms = new Date(expiresAt).getTime() - Date.now();
  if (Number.isNaN(ms) || ms <= 0) return "00:00:00";
  const totalSeconds = Math.floor(ms / 1000);
  const h = Math.floor(totalSeconds / 3600)
    .toString()
    .padStart(2, "0");
  const m = Math.floor((totalSeconds % 3600) / 60)
    .toString()
    .padStart(2, "0");
  const s = Math.floor(totalSeconds % 60)
    .toString()
    .padStart(2, "0");
  return `${h}:${m}:${s}`;
}

export function formatTimestamp(value?: string) {
  if (!value) return "No data";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "No data";
  return date.toLocaleString();
}
