import { describe, expect, it } from "vitest";
import { adminDepositStatusLabel, depositStatusClass, depositStatusLabel } from "./depositStatus";

const knownStatuses = ["NONE", "HELD", "RELEASED", "FORFEITED", "REFUNDED", "UNKNOWN"] as const;

describe("deposit status labels", () => {
  it.each(knownStatuses)("returns a clear user label for %s", (status) => {
    const label = depositStatusLabel(status);

    expect(label).not.toBe("");
    expect(label).not.toMatch(/^\?+$/);
    expect(label).not.toContain("???");
  });

  it.each(knownStatuses)("returns a clear admin label for %s", (status) => {
    const label = adminDepositStatusLabel(status);

    expect(label).not.toBe("");
    expect(label).not.toMatch(/^\?+$/);
    expect(label).not.toContain("???");
  });

  it("uses money-specific user wording for settlement states", () => {
    expect(depositStatusLabel("HELD")).toBe("Депозит удержан");
    expect(depositStatusLabel("RELEASED")).toBe("Депозит возвращён на баланс");
    expect(depositStatusLabel("FORFEITED")).toBe("Депозит удержан администратором");
    expect(depositStatusLabel("REFUNDED")).toBe("Депозит возвращён");
  });

  it("uses operational admin wording for settlement states", () => {
    expect(adminDepositStatusLabel("HELD")).toBe("Удержан, ожидает решения");
    expect(adminDepositStatusLabel("RELEASED")).toBe("Возвращён на баланс");
    expect(adminDepositStatusLabel("FORFEITED")).toBe("Удержан администратором");
    expect(adminDepositStatusLabel("REFUNDED")).toBe("Возвращён при возврате");
  });

  it("does not treat an unknown state as settled", () => {
    expect(depositStatusLabel("UNKNOWN")).toBe("Статус депозита неизвестен");
    expect(adminDepositStatusLabel("UNKNOWN")).toBe("Неизвестный статус депозита — требуется проверка");
    expect(depositStatusClass("UNKNOWN")).toBe("amber");
    expect(depositStatusLabel("SOMETHING_NEW")).toBe("Статус депозита неизвестен");
    expect(adminDepositStatusLabel("SOMETHING_NEW")).toBe("Неизвестный статус депозита — требуется проверка");
    expect(depositStatusClass("SOMETHING_NEW")).toBe("amber");
  });
});
