import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import App from "./App";
import { api } from "./api";
import { makeAccount, makeAdminRental, makeAdminRentalDetail, makeAdminSummary, makeBalance, makePagination, makeUser } from "./test/factories";

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
  vi.mocked(api.adminRentalDetail).mockResolvedValue(makeAdminRentalDetail());
}

describe("App admin rentals filters", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    setBaseMocks();
  });

  it("keeps a committed balance adjustment successful when the follow-up refresh fails", async () => {
    vi.mocked(api.me).mockReset().mockResolvedValueOnce(makeUser()).mockRejectedValueOnce(new Error("refresh failed"));
    vi.mocked(api.adminRentals).mockResolvedValue({
      rentals: [],
      summary: makeAdminSummary(),
      pagination: makePagination()
    });
    vi.mocked(api.adminAdjustBalance).mockResolvedValue({
      adjustment_id: 71,
      ledger_entry_id: 71,
      user_id: 900,
      previous_balance: 0,
      new_balance: 100,
      amount: 100,
      currency: "USD",
      idempotency_key: "admin-balance-adjustment-test-001",
      idempotent_replay: false,
      created_at: "2026-07-11T10:00:00Z"
    });

    render(<App />);

    await screen.findByText("Admin console");
    fireEvent.click(screen.getByRole("button", { name: "Users" }));
    fireEvent.click(screen.getByRole("button", { name: "Adjust balance" }));
    fireEvent.change(screen.getByLabelText("Balance adjustment amount for admin@example.com"), { target: { value: "100" } });
    fireEvent.change(screen.getByLabelText("Balance adjustment reason for admin@example.com"), { target: { value: "MANUAL_COMPENSATION" } });
    fireEvent.click(screen.getByRole("button", { name: "Review adjustment" }));
    fireEvent.click(screen.getByRole("button", { name: "Confirm adjustment" }));

    expect(await screen.findByText("Balance adjusted to 100 USD")).toBeInTheDocument();
    expect(vi.mocked(api.adminAdjustBalance)).toHaveBeenCalledTimes(1);
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

  it("opens detail without resetting filters and refreshes detail after refund success", async () => {
    vi.mocked(api.adminRentals)
      .mockResolvedValueOnce({
        rentals: [makeAdminRental()],
        summary: makeAdminSummary({ eligible_wallet_refund_count: 1 }),
        pagination: makePagination({ page: 1, total_pages: 1, total_items: 1 })
      })
      .mockResolvedValueOnce({
        rentals: [makeAdminRental()],
        summary: makeAdminSummary({ eligible_wallet_refund_count: 1 }),
        pagination: makePagination({ page: 1, total_pages: 1, total_items: 1 })
      })
      .mockResolvedValueOnce({
        rentals: [makeAdminRental({ refund_status: "COMPLETED", has_refund: true })],
        summary: makeAdminSummary({ eligible_wallet_refund_count: 0 }),
        pagination: makePagination({ page: 1, total_pages: 1, total_items: 1 })
      });
    vi.mocked(api.adminRentalDetail)
      .mockResolvedValueOnce(makeAdminRentalDetail({ support_flags: { eligible_wallet_refund: true, refund_ineligible_reason: "", has_active_credentials_access: false, payment_window_expired: true } }))
      .mockResolvedValueOnce(makeAdminRentalDetail({ refund_summary: { count: 1, latest_refund_status: "COMPLETED", total_refunded_principal: { amount: 500, currency: "USD" }, total_refunded_deposit: { amount: 700, currency: "USD" }, latest_processed_at: "2026-07-10T13:00:00Z" }, support_flags: { eligible_wallet_refund: false, refund_ineligible_reason: "REFUND_ALREADY_COMPLETED", has_active_credentials_access: false, payment_window_expired: true } }));
    vi.mocked(api.adminWalletRefund).mockResolvedValue({
      changed: true,
      idempotent: false,
      status: "COMPLETED",
      principal_amount: { amount: 500, currency: "USD" },
      deposit_amount: { amount: 700, currency: "USD" },
      total_amount: { amount: 1200, currency: "USD" },
      deposit_status: "REFUNDED"
    });

    render(<App />);

    await screen.findByText("Admin console");
    fireEvent.click(screen.getByRole("button", { name: "Refunds" }));
    fireEvent.change(screen.getByLabelText("Rental status filter"), { target: { value: "EXPIRED" } });

    await waitFor(() => expect(vi.mocked(api.adminRentals)).toHaveBeenLastCalledWith(expect.objectContaining({ rental_status: "EXPIRED" })));

    fireEvent.click(screen.getByRole("button", { name: "Details" }));
    await waitFor(() => expect(vi.mocked(api.adminRentalDetail)).toHaveBeenCalledWith(1));
    expect(screen.getByLabelText("Admin rental detail")).toBeInTheDocument();
    expect(screen.getByLabelText("Rental status filter")).toHaveValue("EXPIRED");

    fireEvent.click(screen.getByText("Review refund"));
    fireEvent.click(screen.getByText("Confirm refund"));

    await waitFor(() => expect(vi.mocked(api.adminWalletRefund)).toHaveBeenCalledWith(1, "SERVICE_UNAVAILABLE"));
    await waitFor(() => expect(vi.mocked(api.adminRentalDetail)).toHaveBeenCalledTimes(2));

    fireEvent.click(screen.getByTitle("Close detail"));
    await waitFor(() => expect(screen.queryByLabelText("Admin rental detail")).not.toBeInTheDocument());
    expect(screen.getByLabelText("Rental status filter")).toHaveValue("EXPIRED");
  });
});
