const userDepositStatusLabels: Record<string, string> = {
  NONE: "Депозит не удерживается",
  HELD: "Депозит удержан",
  RELEASED: "Депозит возвращён на баланс",
  FORFEITED: "Депозит удержан администратором",
  REFUNDED: "Депозит возвращён",
  UNKNOWN: "Статус депозита неизвестен"
};

const adminDepositStatusLabels: Record<string, string> = {
  NONE: "Не удерживается",
  HELD: "Удержан, ожидает решения",
  RELEASED: "Возвращён на баланс",
  FORFEITED: "Удержан администратором",
  REFUNDED: "Возвращён при возврате",
  UNKNOWN: "Неизвестный статус депозита — требуется проверка"
};

export const depositStatusLabel = (status?: string) => userDepositStatusLabels[status ?? ""] ?? "Статус депозита неизвестен";

export const adminDepositStatusLabel = (status?: string) => adminDepositStatusLabels[status ?? ""] ?? "Неизвестный статус депозита — требуется проверка";

export function depositStatusClass(status?: string) {
  switch (status) {
    case "HELD":
      return "amber";
    case "RELEASED":
      return "green";
    case "FORFEITED":
      return "danger";
    case "REFUNDED":
      return "green";
    case "NONE":
      return "muted";
    case "UNKNOWN":
    default:
      return "amber";
  }
}
