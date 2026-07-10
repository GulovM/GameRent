import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import App from "./App";
import { api } from "./api";
import { makeAccount, makeAdminRental, makeAdminSummary, makeBalance, makePagination, makeUser } from "./test/factories";

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
    adminUsers: vi.fn(),
    adminAuditLogs: vi.fn(),
    adminRefundReasonCodes: vi.fn(),
    adminWalletRefund: vi.fn(),
    logout: vi.fn(),
    adminUpdateAccount: vi.fn(),
    adminSyncAccount: vi.fn(),
    adminCreateAccount: vi.fn(),
    adminUpdateUser: vi.fn(),
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

const adminAccountsResponse = {
  accounts: [
    {
      id: 1,
      steam_id64: "76561198000000001",
      status: 2,
      hourly_price: 500,
      deposit_amount: 700
    }
  ]
};

const refundReasonCodesResponse = {
  reason_codes: [
    { code: "SERVICE_UNAVAILABLE", label: "Service unavailable" },
    { code: "ACCOUNT_INVALID", label: "Account invalid" }
  ]
};

function setBaseMocks() {
  vi.mocked(api.accounts).mockResolvedValue({ accounts: [makeAccount()], pagination: makePagination() });
  vi.mocked(api.games).mockResolvedValue({ games: [], pagination: makePagination() });
  vi.mocked(api.me).mockResolvedValue(makeUser());
  vi.mocked(api.rentals).mockResolvedValue({ rentals: [] });
  vi.mocked(api.payments).mockResolvedValue({ payments: [] });
  vi.mocked(api.notifications).mockResolvedValue({ notifications: [] });
  vi.mocked(api.myBalance).mockResolvedValue(makeBalance({ available_balance: 0 }));
  vi.mocked(api.adminAccounts).mockResolvedValue(adminAccountsResponse);
  vi.mocked(api.adminUsers).mockResolvedValue({ users: [makeUser()] });
  vi.mocked(api.adminAuditLogs).mockResolvedValue({ audit_logs: [] });
  vi.mocked(api.adminRefundReasonCodes).mockResolvedValue(refundReasonCodesResponse);
}

describe("App admin rentals filters", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    setBaseMocks();
  });

  it("calls admin rentals API with server-side filters and resets page to 1", async () => {
    vi.mocked(api.adminRentals)
      .mockResolvedValueOnce({
        rentals: [makeAdminRental()],
        summary: makeAdminSummary({ eligible_wallet_refund_count: 5 }),
        pagination: makePagination({ page: 1, total_pages: 2, total_items: 30 })
      })
      .mockResolvedValueOnce({
        rentals: [makeAdminRental({ id: 2 })],
        summary: makeAdminSummary({ eligible_wallet_refund_count: 5 }),
        pagination: makePagination({ page: 2, total_pages: 2, total_items: 30 })
      })
      .mockResolvedValueOnce({
        rentals: [makeAdminRental({ id: 3, status: 3 })],
        summary: makeAdminSummary({ eligible_wallet_refund_count: 1 }),
        pagination: makePagination({ page: 1, total_pages: 1, total_items: 4 })
      });

    render(<App />);

    await screen.findByText("Admin console");
    fireEvent.click(screen.getByRole("button", { name: "Refunds" }));
    fireEvent.click(screen.getByRole("button", { name: "Next" }));

    await waitFor(() => {
      expect(vi.mocked(api.adminRentals)).toHaveBeenLastCalledWith({ page: 2, page_size: 20 });
    });

    fireEvent.change(screen.getByLabelText("Rental status filter"), { target: { value: "EXPIRED" } });

    await waitFor(() => {
      expect(vi.mocked(api.adminRentals)).toHaveBeenLastCalledWith({
        page: 1,
        page_size: 20,
        rental_status: "EXPIRED",
        payment_status: undefined,
        payment_provider: undefined,
        deposit_status: undefined,
        refund_status: undefined,
        eligible_wallet_refund: undefined,
        user_id: undefined,
        rental_id: undefined
      });
    });
  });

  it("resets filters and updates KPI from filtered server summary", async () => {
    vi.mocked(api.adminRentals)
      .mockResolvedValueOnce({
        rentals: [makeAdminRental()],
        summary: makeAdminSummary({ eligible_wallet_refund_count: 5 }),
        pagination: makePagination({ page: 1, total_pages: 1, total_items: 10 })
      })
      .mockResolvedValueOnce({
        rentals: [makeAdminRental({ id: 4, payment_provider: "balance" })],
        summary: makeAdminSummary({ eligible_wallet_refund_count: 1 }),
        pagination: makePagination({ page: 1, total_pages: 1, total_items: 1 })
      })
      .mockResolvedValueOnce({
        rentals: [makeAdminRental()],
        summary: makeAdminSummary({ eligible_wallet_refund_count: 5 }),
        pagination: makePagination({ page: 1, total_pages: 1, total_items: 10 })
      });

    render(<App />);

    expect(await screen.findByText("Refund candidates")).toBeInTheDocument();
    expect(screen.getByText("5")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Refunds" }));
    fireEvent.change(screen.getByLabelText("Eligible wallet refund filter"), { target: { value: "true" } });

    await waitFor(() => {
      expect(vi.mocked(api.adminRentals)).toHaveBeenLastCalledWith({
        page: 1,
        page_size: 20,
        rental_status: undefined,
        payment_status: undefined,
        payment_provider: undefined,
        deposit_status: undefined,
        refund_status: undefined,
        eligible_wallet_refund: true,
        user_id: undefined,
        rental_id: undefined
      });
    });

    fireEvent.click(screen.getByRole("button", { name: "Overview" }));
    await waitFor(() => {
      expect(screen.getByText("Refund candidates").parentElement?.textContent).toContain("1");
    });

    fireEvent.click(screen.getByRole("button", { name: "Refunds" }));
    fireEvent.click(screen.getByRole("button", { name: "Reset filters" }));

    await waitFor(() => {
      expect(vi.mocked(api.adminRentals)).toHaveBeenLastCalledWith({ page: 1, page_size: 20 });
    });
  });
});
