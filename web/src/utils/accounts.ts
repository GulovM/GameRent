import type { Account } from "../api";

export const accountStatusLabels: Record<string, string> = {
  Created: "Создан",
  Verifying: "Проверка",
  Available: "Доступен",
  Reserved: "Резерв",
  Rented: "В аренде",
  Maintenance: "Обслуживание",
  Disabled: "Отключен"
};

export const accountStatusNumbers: Record<string, number> = {
  Created: 0,
  Verifying: 1,
  Available: 2,
  Reserved: 3,
  Rented: 4,
  Maintenance: 5,
  Disabled: 6
};

export function asList<T>(value: T[] | null | undefined): T[] {
  return Array.isArray(value) ? value : [];
}

export function statusFromNumber(value: number) {
  return ["Created", "Verifying", "Available", "Reserved", "Rented", "Maintenance", "Disabled"][value] ?? "Unknown";
}

export function normalizeAccount(account: Account): Account {
  return {
    ...account,
    games: asList(account.games)
  };
}

export function gameNames(account: Account, limit = 3) {
  const names = asList(account.games).map((game) => game.name);
  return names.slice(0, limit).join(", ") || "Библиотека Steam";
}
