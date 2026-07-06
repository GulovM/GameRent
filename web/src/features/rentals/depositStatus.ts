export function depositStatusLabel(status?: string) {
  switch (status) {
    case "HELD":
      return "Удерживается";
    case "RELEASED":
      return "Возвращён на баланс";
    case "FORFEITED":
      return "Удержан";
    case "NONE":
    default:
      return "Без депозита";
  }
}

export function depositStatusClass(status?: string) {
  switch (status) {
    case "HELD":
      return "amber";
    case "RELEASED":
      return "green";
    case "FORFEITED":
      return "danger";
    case "NONE":
    default:
      return "muted";
  }
}
