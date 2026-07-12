import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import { useState } from "react";
import App from "./App";
import { api, getRefreshToken, isApiError } from "./api";
import { makeAccount, makeBalance, makeRental, makePayment, makeUser, makePagination } from "./test/factories";
import type { RentalCredentials } from "./api";

vi.mock("./api", () => {
  const api = {
    accounts: vi.fn(),
    games: vi.fn(),
    me: vi.fn(),
    rentals: vi.fn(),
    payments: vi.fn(),
    notifications: vi.fn(),
    myBalance: vi.fn(),
    adminAccounts: vi.fn(),
    adminRentals: vi.fn(),
    adminRentalDetail: vi.fn(),
    adminUsers: vi.fn(),
    adminAuditLogs: vi.fn(),
    adminRefundReasonCodes: vi.fn(),
    adminWalletRefund: vi.fn(),
    logout: vi.fn(),
    adminUpdateAccount: vi.fn(),
    adminSyncAccount: vi.fn(),
    adminCreateAccount: vi.fn(),
    adminUpdateUser: vi.fn(),
    adminAdjustBalance: vi.fn(),
    cancelRental: vi.fn(),
    extendRental: vi.fn(),
    readNotification: vi.fn(),
    favoriteAccount: vi.fn(),
    payRentalWithBalance: vi.fn(),
    rentalCredentials: vi.fn(),
    createRental: vi.fn()
  };

  return {
    api,
    clearTokens: vi.fn(),
    getAccessToken: vi.fn(() => "token"),
    getRefreshToken: vi.fn(() => null),
    isApiError: vi.fn(() => false)
  };
});

async function waitForFakeTimers(assertion: () => void, timeoutMs = 2000, stepMs = 50) {
  const startTime = Date.now();
  while (true) {
    try {
      assertion();
      return;
    } catch (err) {
      if (Date.now() - startTime > timeoutMs) {
        throw err;
      }
      await vi.advanceTimersByTimeAsync(stepMs);
    }
  }
}

describe("App credentials safety and auto-clearing", () => {
  beforeEach(() => {
    vi.resetAllMocks();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("clears credentials after 60 seconds, on window blur, and visibilitychange", async () => {
    const userObj = makeUser({ role: "RENT" });
    const rentalObj = makeRental({ id: 10, status: 2 }); // Active status = 2
    const paymentObj = makePayment({ id: 100, rental_id: 10, status: 2 }); // Success status = 2

    vi.mocked(api.accounts).mockResolvedValue({ accounts: [makeAccount()], pagination: makePagination() });
    vi.mocked(api.games).mockResolvedValue({ games: [], pagination: makePagination() });
    vi.mocked(api.me).mockResolvedValue(userObj);
    vi.mocked(api.rentals).mockResolvedValue({ rentals: [rentalObj] });
    vi.mocked(api.payments).mockResolvedValue({ payments: [paymentObj] });
    vi.mocked(api.notifications).mockResolvedValue({ notifications: [] });
    vi.mocked(api.myBalance).mockResolvedValue(makeBalance());
    vi.mocked(api.rentalCredentials).mockResolvedValue({ login: "secure_login", password: "secure_password" });

    render(<App />);

    await screen.findByText("Каталог аккаунтов");

    fireEvent.click(screen.getByRole("button", { name: "Мои аренды" }));

    const getCredsBtn = await screen.findByLabelText("Get rental credentials");

    // Enable fake timers specifically for the 60-second auto-clear check
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });

    fireEvent.click(getCredsBtn);
    await Promise.resolve();
    await Promise.resolve();
    await vi.advanceTimersByTimeAsync(50);

    expect(screen.getByText("secure_login")).toBeInTheDocument();
    expect(screen.getByText("secure_password")).toBeInTheDocument();

    await vi.advanceTimersByTimeAsync(60000);

    await vi.advanceTimersByTimeAsync(10);
    expect(screen.queryByText("secure_login")).not.toBeInTheDocument();
    expect(screen.queryByText("secure_password")).not.toBeInTheDocument();

    // Restore real timers for the rest of the test
    vi.useRealTimers();

    const btn = await screen.findByLabelText("Get rental credentials");
    fireEvent.click(btn);
    await waitFor(() => {
      expect(screen.getByText("secure_login")).toBeInTheDocument();
    });

    window.dispatchEvent(new Event("blur"));
    await waitFor(() => {
      expect(screen.queryByText("secure_login")).not.toBeInTheDocument();
    });

    fireEvent.click(await screen.findByLabelText("Get rental credentials"));
    await waitFor(() => {
      expect(screen.getByText("secure_login")).toBeInTheDocument();
    });

    document.dispatchEvent(new Event("visibilitychange"));
    await waitFor(() => {
      expect(screen.queryByText("secure_login")).not.toBeInTheDocument();
    });
  });

  function deferredCredentials() {
    let resolve!: (value: RentalCredentials) => void;
    let reject!: (reason: unknown) => void;
    const promise = new Promise<RentalCredentials>((resolvePromise, rejectPromise) => {
      resolve = resolvePromise;
      reject = rejectPromise;
    });
    return { promise, reject, resolve };
  }

  async function renderActiveRentals(rentals = [makeRental({ id: 10, status: 2 })]) {
    const userObj = makeUser({ id: 500, role: "RENT", first_name: "Current" });
    const payments = rentals.map((rental, index) => makePayment({ id: 100 + index, rental_id: rental.id, status: 2 }));
    vi.mocked(api.accounts).mockResolvedValue({ accounts: [makeAccount()], pagination: makePagination() });
    vi.mocked(api.games).mockResolvedValue({ games: [], pagination: makePagination() });
    vi.mocked(api.me).mockResolvedValue(userObj);
    vi.mocked(api.rentals).mockResolvedValue({ rentals });
    vi.mocked(api.payments).mockResolvedValue({ payments });
    vi.mocked(api.notifications).mockResolvedValue({ notifications: [] });
    vi.mocked(api.myBalance).mockResolvedValue(makeBalance());

    const rendered = render(<App />);
    await screen.findByText("Current");
    fireEvent.click(desktopNavButton("lucide-clock3"));
    await screen.findByLabelText("Get rental credentials");
    return rendered;
  }

  function desktopNavButton(iconClass: string) {
    const button = document.querySelector(`.desktop-nav .${iconClass}`)?.closest("button");
    if (!button) throw new Error(`desktop navigation button ${iconClass} not found`);
    return button;
  }

  it("does not repopulate credentials when a deferred request resolves after logout starts", async () => {
    const pending = deferredCredentials();
    vi.mocked(api.rentalCredentials).mockReturnValue(pending.promise);
    vi.mocked(getRefreshToken).mockReturnValue("refresh-token");
    let finishLogout!: () => void;
    vi.mocked(api.logout).mockReturnValue(new Promise((resolve) => {
      finishLogout = () => resolve({ message: "ok" });
    }));
    await renderActiveRentals();

    fireEvent.click(screen.getByLabelText("Get rental credentials"));
    fireEvent.click(document.querySelector(".topbar .lucide-log-out")!.closest("button")!);
    pending.resolve({ login: "stale-after-logout", password: "secret" });

    await waitFor(() => expect(screen.queryByText("stale-after-logout")).not.toBeInTheDocument());
    finishLogout();
  });

  it("does not repopulate credentials after navigation away from rentals", async () => {
    const pending = deferredCredentials();
    vi.mocked(api.rentalCredentials).mockReturnValue(pending.promise);
    await renderActiveRentals();

    fireEvent.click(screen.getByLabelText("Get rental credentials"));
    fireEvent.click(desktopNavButton("lucide-credit-card"));
    pending.resolve({ login: "stale-after-navigation", password: "secret" });

    await waitFor(() => expect(screen.queryByText("stale-after-navigation")).not.toBeInTheDocument());
  });

  it("does not show credentials from a previous rental after switching rentals", async () => {
    const first = deferredCredentials();
    const second = deferredCredentials();
    vi.mocked(api.rentalCredentials).mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    await renderActiveRentals([
      makeRental({ id: 10, account_id: 1, status: 2 }),
      makeRental({ id: 11, account_id: 2, status: 2 })
    ]);

    fireEvent.click(screen.getByLabelText("Get credentials for rental 10"));
    fireEvent.click(screen.getByLabelText("Get credentials for rental 11"));
    first.resolve({ login: "wrong-rental-login", password: "wrong-rental-password" });
    await waitFor(() => expect(screen.queryByText("wrong-rental-login")).not.toBeInTheDocument());

    second.resolve({ login: "current-rental-login", password: "current-rental-password" });
    await waitFor(() => expect(screen.getByText("current-rental-login")).toBeInTheDocument());
  });

  it("does not repopulate credentials when the rental expires while the request is pending", async () => {
    const pending = deferredCredentials();
    vi.mocked(api.rentalCredentials).mockReturnValue(pending.promise);
    await renderActiveRentals([makeRental({ id: 10, status: 2, expires_at: new Date(Date.now() + 50).toISOString() })]);

    fireEvent.click(screen.getByLabelText("Get rental credentials"));
    await new Promise((resolve) => window.setTimeout(resolve, 80));
    pending.resolve({ login: "stale-after-expiry", password: "secret" });

    await waitFor(() => expect(screen.queryByText("stale-after-expiry")).not.toBeInTheDocument());
  });

  it("aborts a pending credential request on component unmount", async () => {
    const pending = deferredCredentials();
    let signal: AbortSignal | undefined;
    vi.mocked(api.rentalCredentials).mockImplementation((_id, requestSignal) => {
      signal = requestSignal;
      return pending.promise;
    });
    const rendered = await renderActiveRentals();

    fireEvent.click(screen.getByLabelText("Get rental credentials"));
    rendered.unmount();

    expect(signal?.aborted).toBe(true);
    pending.resolve({ login: "stale-after-unmount", password: "secret" });
  });

  it("does not repopulate credentials after the authenticated user changes", async () => {
    const pending = deferredCredentials();
    vi.mocked(api.rentalCredentials).mockReturnValue(pending.promise);
    await renderActiveRentals();
    fireEvent.click(screen.getByLabelText("Get rental credentials"));

    vi.mocked(api.me).mockResolvedValueOnce(makeUser({ id: 501, role: "RENT", first_name: "NextUser" }));
    window.dispatchEvent(new Event("gamerent:authorization-changed"));
    await screen.findByText("NextUser");
    pending.resolve({ login: "stale-after-user-change", password: "secret" });

    await waitFor(() => expect(screen.queryByText("stale-after-user-change")).not.toBeInTheDocument());
  });

  it("clears displayed credentials when a later credential fetch is forbidden", async () => {
    const forbidden = Object.assign(new Error("forbidden"), { status: 403, code: "FORBIDDEN" });
    vi.mocked(isApiError).mockImplementation((error) => error === forbidden);
    vi.mocked(api.rentalCredentials)
      .mockResolvedValueOnce({ login: "visible-login", password: "visible-password" })
      .mockRejectedValueOnce(forbidden);
    await renderActiveRentals();

    fireEvent.click(screen.getByLabelText("Get rental credentials"));
    await screen.findByText("visible-login");
    fireEvent.click(screen.getByLabelText("Get credentials for rental 10"));

    await waitFor(() => expect(screen.queryByText("visible-login")).not.toBeInTheDocument());
    expect(screen.getByText("Rental data is unavailable.")).toBeInTheDocument();
  });

  it("never writes fetched credentials to browser storage", async () => {
    const localStorageSpy = vi.spyOn(window.localStorage.__proto__, "setItem");
    const sessionStorageSpy = vi.spyOn(window.sessionStorage.__proto__, "setItem");
    vi.mocked(api.rentalCredentials).mockResolvedValue({ login: "memory-only-login", password: "memory-only-password" });
    await renderActiveRentals();

    fireEvent.click(screen.getByLabelText("Get rental credentials"));
    await screen.findByText("memory-only-login");

    expect(localStorageSpy).not.toHaveBeenCalled();
    expect(sessionStorageSpy).not.toHaveBeenCalled();
  });
});
