import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import type { AdminBalanceAdjustmentInput, AdminBalanceAdjustmentResponse, AdminRentalDetail, AdminRentalFilters, AdminRentalRefundSummary } from "../../api";
import { PAYMENT_STATUS_PENDING } from "../payments/paymentStatus";
import { RENTAL_STATUS_ACTIVE } from "../rentals/rentalStatus";
import { AdminView } from "./AdminView";
import { makeAccount, makeAdminRental, makeAdminRentalDetail, makeAdminSummary, makePagination, makeUser } from "../../test/factories";

const noopPromise = async () => undefined;
const noopUserUpdate = async () => undefined;
const successfulBalanceAdjustment = (input?: Partial<AdminBalanceAdjustmentResponse>): AdminBalanceAdjustmentResponse => ({
  adjustment_id: 1001,
  user_id: 900,
  previous_balance: 1000,
  new_balance: 1500,
  amount: 500,
  currency: "USD",
  ledger_entry_id: 1001,
  idempotency_key: "admin-balance-adjustment-test",
  idempotent_replay: false,
  created_at: "2026-07-11T09:00:00Z",
  ...input
});
const noopBalanceAdjustment = async (_user: ReturnType<typeof makeUser>, _input: AdminBalanceAdjustmentInput) => successfulBalanceAdjustment();

const refundReasonOptions = [
  { code: "SERVICE_UNAVAILABLE", label: "Service unavailable" },
  { code: "ACCOUNT_INVALID", label: "Account invalid" },
  { code: "ADMIN_CORRECTION", label: "Admin correction" }
];

function renderAdminView({
  rentals = [makeAdminRental()],
  onWalletRefund = async () => ({
    changed: true,
    idempotent: false,
    status: "COMPLETED",
    principal_amount: { amount: 500, currency: "USD" },
    deposit_amount: { amount: 700, currency: "USD" },
    total_amount: { amount: 1200, currency: "USD" },
    deposit_status: "REFUNDED"
  }),
  adminRentalDetail = null,
  adminRentalDetailLoading = false,
  adminRentalDetailError = null,
  onCloseAdminRentalDetail = () => undefined,
  onOpenAdminRentalDetail = async (_rentalId: number) => undefined,
  onAdjustBalance = noopBalanceAdjustment
}: {
  rentals?: AdminRentalRefundSummary[];
  adminRentalDetail?: AdminRentalDetail | null;
  adminRentalDetailLoading?: boolean;
  adminRentalDetailError?: string | null;
  onCloseAdminRentalDetail?: () => void;
  onOpenAdminRentalDetail?: (rentalId: number) => Promise<void>;
  onAdjustBalance?: (user: ReturnType<typeof makeUser>, input: AdminBalanceAdjustmentInput) => Promise<AdminBalanceAdjustmentResponse>;
  onWalletRefund?: (rentalId: number, reasonCode: string) => Promise<{
    changed: boolean;
    idempotent: boolean;
    status: string;
    principal_amount: { amount: number; currency: string };
    deposit_amount: { amount: number; currency: string };
    total_amount: { amount: number; currency: string };
    deposit_status: string;
  }>;
} = {}) {
  render(
    <AdminView
      accounts={[makeAccount()]}
      adminRentalDetail={adminRentalDetail}
      adminRentalDetailError={adminRentalDetailError}
      adminRentalDetailLoading={adminRentalDetailLoading}
      adminRentalFilters={{}}
      adminRentals={rentals}
      adminRentalsError={null}
      adminRentalsLoading={false}
      adminRentalsPagination={makePagination()}
      adminRentalsSummary={makeAdminSummary()}
      auditLogs={[]}
      onCloseAdminRentalDetail={onCloseAdminRentalDetail}
      onAdminRentalFiltersChange={async (_filters: AdminRentalFilters) => undefined}
      onAdminRentalFiltersReset={async () => undefined}
      onAdjustBalance={onAdjustBalance}
      onCreateAccount={noopPromise}
      onNextRefundPage={noopPromise}
      onOpenAdminRentalDetail={onOpenAdminRentalDetail}
      onPrevRefundPage={noopPromise}
      onSync={noopPromise}
      onUpdateAccount={noopPromise}
      onUpdateUser={noopUserUpdate}
      onWalletRefund={onWalletRefund}
      refundReasonOptions={refundReasonOptions}
      user={makeUser()}
      users={[makeUser()]}
    />
  );
}

describe("AdminView wallet refunds", () => {
  it("renders the admin deposit label instead of the raw status code", () => {
    renderAdminView({ rentals: [makeAdminRental({ deposit_status: "HELD" })] });

    fireEvent.click(screen.getByRole("button", { name: "Refunds" }));

    expect(screen.getByText("Удержан, ожидает решения", { selector: "strong" })).toBeInTheDocument();
    expect(screen.queryByText("HELD")).not.toBeInTheDocument();
  });

  it("renders an unknown deposit as requiring review", () => {
    renderAdminView({ rentals: [makeAdminRental({ deposit_status: "UNKNOWN" })] });

    fireEvent.click(screen.getByRole("button", { name: "Refunds" }));

    expect(screen.getByText("Неизвестный статус депозита — требуется проверка", { selector: "strong" })).toHaveClass("amber");
    expect(screen.queryByText("Не удерживается", { selector: "strong" })).not.toBeInTheDocument();
  });

  it("does not expose generic account lifecycle controls", () => {
    renderAdminView();

    fireEvent.click(screen.getByRole("button", { name: "Accounts" }));
    expect(screen.queryByRole("button", { name: "Enable" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Disable" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
    expect(screen.getByText(/Lifecycle status is read-only here/)).toBeInTheDocument();
  });

  it("shows refund controls for an eligible rental", () => {
    renderAdminView();

    fireEvent.click(screen.getByRole("button", { name: "Refunds" }));
    expect(screen.getByText("Review refund")).toBeInTheDocument();
  });

  it("renders the server-side filters panel", () => {
    renderAdminView();

    fireEvent.click(screen.getByRole("button", { name: "Refunds" }));
    expect(screen.getByTestId("admin-rentals-filters")).toBeInTheDocument();
    expect(screen.getByLabelText("Rental status filter")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Reset filters" })).toBeInTheDocument();
  });

  it("opens detail via Details action and renders safe sections", async () => {
    const onOpenAdminRentalDetail = vi.fn().mockResolvedValue(undefined);

    renderAdminView({
      adminRentalDetail: makeAdminRentalDetail(),
      onOpenAdminRentalDetail
    });

    fireEvent.click(screen.getByRole("button", { name: "Refunds" }));
    fireEvent.click(screen.getByRole("button", { name: "Details" }));

    await waitFor(() => expect(onOpenAdminRentalDetail).toHaveBeenCalledWith(1));
    expect(screen.getByLabelText("Admin rental detail")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Refund summary" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Ledger summary" })).toBeInTheDocument();
    expect(screen.queryByText(/idempotency_key/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/metadata/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/password/i)).not.toBeInTheDocument();
  });

  it("shows detail loading state and clears panel on close", async () => {
    function Harness() {
      const [detail, setDetail] = useState<AdminRentalDetail | null>(makeAdminRentalDetail());
      const [loading, setLoading] = useState(true);

      return (
        <AdminView
          accounts={[makeAccount()]}
          adminRentalDetail={detail}
          adminRentalDetailError={null}
          adminRentalDetailLoading={loading}
          adminRentalFilters={{ rental_status: "EXPIRED" }}
          adminRentals={[makeAdminRental()]}
          adminRentalsError={null}
          adminRentalsLoading={false}
          adminRentalsPagination={makePagination()}
          adminRentalsSummary={makeAdminSummary()}
          auditLogs={[]}
          onCloseAdminRentalDetail={() => {
            setDetail(null);
            setLoading(false);
          }}
          onAdminRentalFiltersChange={async (_filters: AdminRentalFilters) => undefined}
          onAdminRentalFiltersReset={async () => undefined}
          onAdjustBalance={noopBalanceAdjustment}
          onCreateAccount={noopPromise}
          onNextRefundPage={noopPromise}
          onOpenAdminRentalDetail={async (_rentalId: number) => undefined}
          onPrevRefundPage={noopPromise}
          onSync={noopPromise}
          onUpdateAccount={noopPromise}
          onUpdateUser={noopUserUpdate}
          onWalletRefund={async () => ({
            changed: true,
            idempotent: false,
            status: "COMPLETED",
            principal_amount: { amount: 500, currency: "USD" },
            deposit_amount: { amount: 700, currency: "USD" },
            total_amount: { amount: 1200, currency: "USD" },
            deposit_status: "REFUNDED"
          })}
          refundReasonOptions={refundReasonOptions}
          user={makeUser()}
          users={[makeUser()]}
        />
      );
    }

    render(<Harness />);

    fireEvent.click(screen.getByRole("button", { name: "Refunds" }));
    expect(screen.getByText("Loading admin rental detail...")).toBeInTheDocument();
    fireEvent.click(screen.getByTitle("Close detail"));
    await waitFor(() => expect(screen.queryByLabelText("Admin rental detail")).not.toBeInTheDocument());
    expect(screen.getByLabelText("Rental status filter")).toHaveValue("EXPIRED");
  });

  it("does not show a refund action for an ineligible rental", () => {
    renderAdminView({
      rentals: [makeAdminRental({ payment_status: PAYMENT_STATUS_PENDING, status: RENTAL_STATUS_ACTIVE })]
    });

    fireEvent.click(screen.getByRole("button", { name: "Refunds" }));
    expect(screen.queryByText("Review refund")).not.toBeInTheDocument();
  });

  it("requires a confirm step before submitting a wallet refund", async () => {
    const onWalletRefund = vi.fn().mockResolvedValue({
      changed: true,
      idempotent: false,
      status: "COMPLETED",
      principal_amount: { amount: 500, currency: "USD" },
      deposit_amount: { amount: 700, currency: "USD" },
      total_amount: { amount: 1200, currency: "USD" },
      deposit_status: "REFUNDED"
    });

    renderAdminView({ onWalletRefund });

    fireEvent.click(screen.getByRole("button", { name: "Refunds" }));
    fireEvent.click(screen.getByText("Review refund"));
    expect(onWalletRefund).not.toHaveBeenCalled();

    fireEvent.click(screen.getByText("Confirm refund"));

    await waitFor(() => expect(onWalletRefund).toHaveBeenCalledTimes(1));
  });

  it("blocks a second refund submit while the first one is in flight", async () => {
    let resolveRefund: (() => void) | undefined;
    const onWalletRefund = vi.fn().mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveRefund = () =>
            resolve({
              changed: true,
              idempotent: false,
              status: "COMPLETED",
              principal_amount: { amount: 500, currency: "USD" },
              deposit_amount: { amount: 700, currency: "USD" },
              total_amount: { amount: 1200, currency: "USD" },
              deposit_status: "REFUNDED"
            });
        })
    );

    renderAdminView({ onWalletRefund });

    fireEvent.click(screen.getByRole("button", { name: "Refunds" }));
    fireEvent.click(screen.getByText("Review refund"));
    fireEvent.click(screen.getByText("Confirm refund"));

    await waitFor(() => expect(screen.getByText("Refunding...")).toBeInTheDocument());
    fireEvent.click(screen.getByText("Refunding..."));

    expect(onWalletRefund).toHaveBeenCalledTimes(1);
    resolveRefund?.();
  });

  it("does not show the refund button again after a successful refresh", async () => {
    function Harness() {
      const [rentals, setRentals] = useState([makeAdminRental()]);

      return (
        <AdminView
          accounts={[makeAccount()]}
          adminRentalDetail={null}
          adminRentalDetailError={null}
          adminRentalDetailLoading={false}
          adminRentalFilters={{}}
          adminRentals={rentals}
          adminRentalsError={null}
          adminRentalsLoading={false}
          adminRentalsPagination={makePagination()}
          adminRentalsSummary={makeAdminSummary()}
          auditLogs={[]}
          onCloseAdminRentalDetail={() => undefined}
          onAdminRentalFiltersChange={async (_filters: AdminRentalFilters) => undefined}
          onAdminRentalFiltersReset={async () => undefined}
          onAdjustBalance={noopBalanceAdjustment}
          onCreateAccount={noopPromise}
          onNextRefundPage={noopPromise}
          onOpenAdminRentalDetail={async (_rentalId: number) => undefined}
          onPrevRefundPage={noopPromise}
          onSync={noopPromise}
          onUpdateAccount={noopPromise}
          onUpdateUser={noopUserUpdate}
          onWalletRefund={async (rentalId, _reasonCode) => {
            setRentals((current) =>
              current.map((item) => (item.id === rentalId ? { ...item, refund_status: "COMPLETED", has_refund: true } : item))
            );
            return {
              changed: true,
              idempotent: false,
              status: "COMPLETED",
              principal_amount: { amount: 500, currency: "USD" },
              deposit_amount: { amount: 700, currency: "USD" },
              total_amount: { amount: 1200, currency: "USD" },
              deposit_status: "REFUNDED"
            };
          }}
          refundReasonOptions={refundReasonOptions}
          user={makeUser()}
          users={[makeUser()]}
        />
      );
    }

    render(<Harness />);

    fireEvent.click(screen.getByRole("button", { name: "Refunds" }));
    fireEvent.click(screen.getByText("Review refund"));
    fireEvent.click(screen.getByText("Confirm refund"));

    await waitFor(() => expect(screen.queryByText("Review refund")).not.toBeInTheDocument());
  });
});

describe("AdminView balance adjustments", () => {
  function openAdjustmentForm() {
    fireEvent.click(screen.getByRole("button", { name: "Users" }));
    fireEvent.click(screen.getByRole("button", { name: "Adjust balance" }));
  }

  it("keeps balance read-only in the generic user editor", () => {
    renderAdminView();
    fireEvent.click(screen.getByRole("button", { name: "Users" }));

    const balance = screen.getByTestId("admin-user-balance-900");
    expect(balance).toHaveTextContent("0 USD");
    expect(within(balance.parentElement as HTMLElement).queryByRole("spinbutton")).not.toBeInTheDocument();
  });

  it("requires a reason before showing confirmation", () => {
    const onAdjustBalance = vi.fn().mockResolvedValue(successfulBalanceAdjustment());
    renderAdminView({ onAdjustBalance });
    openAdjustmentForm();

    fireEvent.change(screen.getByLabelText("Balance adjustment amount for admin@example.com"), { target: { value: "500" } });
    fireEvent.click(screen.getByRole("button", { name: "Review adjustment" }));

    expect(screen.getByText("Reason code may contain only letters, numbers, underscores, and hyphens.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Confirm adjustment" })).not.toBeInTheDocument();
    expect(onAdjustBalance).not.toHaveBeenCalled();
  });

  it("prevents double-submit and updates the displayed balance only after API success", async () => {
    let resolveAdjustment: ((value: AdminBalanceAdjustmentResponse) => void) | undefined;
    const onAdjustBalance = vi.fn().mockImplementation(
      () => new Promise<AdminBalanceAdjustmentResponse>((resolve) => {
        resolveAdjustment = resolve;
      })
    );
    renderAdminView({ onAdjustBalance });
    openAdjustmentForm();

    fireEvent.change(screen.getByLabelText("Balance adjustment amount for admin@example.com"), { target: { value: "500" } });
    fireEvent.change(screen.getByLabelText("Balance adjustment reason for admin@example.com"), { target: { value: "MANUAL_COMPENSATION" } });
    fireEvent.click(screen.getByRole("button", { name: "Review adjustment" }));
    fireEvent.click(screen.getByRole("button", { name: "Confirm adjustment" }));

    await waitFor(() => expect(screen.getByRole("button", { name: "Adjusting..." })).toBeDisabled());
    fireEvent.click(screen.getByRole("button", { name: "Adjusting..." }));
    expect(onAdjustBalance).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("admin-user-balance-900")).toHaveTextContent("0 USD");

    resolveAdjustment?.(successfulBalanceAdjustment());
    await waitFor(() => expect(screen.getByTestId("admin-user-balance-900")).toHaveTextContent("1500 USD"));
    expect(screen.getByText("Balance adjusted to 1500 USD.")).toBeInTheDocument();
  });

  it("shows API errors without changing the displayed balance", async () => {
    const onAdjustBalance = vi.fn().mockRejectedValue(new Error("Adjustment was rejected"));
    renderAdminView({ onAdjustBalance });
    openAdjustmentForm();

    fireEvent.change(screen.getByLabelText("Balance adjustment amount for admin@example.com"), { target: { value: "100" } });
    fireEvent.change(screen.getByLabelText("Balance adjustment reason for admin@example.com"), { target: { value: "MANUAL_COMPENSATION" } });
    fireEvent.click(screen.getByRole("button", { name: "Review adjustment" }));
    fireEvent.click(screen.getByRole("button", { name: "Confirm adjustment" }));

    await waitFor(() => expect(screen.getByText("Adjustment was rejected")).toBeInTheDocument());
    expect(screen.getByTestId("admin-user-balance-900")).toHaveTextContent("0 USD");
  });
});

describe("AdminView deposit release and forfeit", () => {
  it("renders release and forfeit buttons for HELD deposit status in detail view", async () => {
    const onReleaseDeposit = vi.fn().mockResolvedValue({ changed: true });
    const onForfeitDeposit = vi.fn().mockResolvedValue({ changed: true });

    const detailWithHeldDeposit: AdminRentalDetail = makeAdminRentalDetail({
      deposit: {
        amount: 700,
        currency: "USD",
        status: "HELD",
        held_at: "2026-07-10T12:00:00Z"
      }
    });

    render(
      <AdminView
        accounts={[makeAccount()]}
        adminRentalDetail={detailWithHeldDeposit}
        adminRentalDetailError={null}
        adminRentalDetailLoading={false}
        adminRentalFilters={{}}
        adminRentals={[makeAdminRental()]}
        adminRentalsError={null}
        adminRentalsLoading={false}
        adminRentalsPagination={makePagination()}
        adminRentalsSummary={makeAdminSummary()}
        auditLogs={[]}
        onCloseAdminRentalDetail={() => undefined}
        onAdminRentalFiltersChange={async (_filters: AdminRentalFilters) => undefined}
        onAdminRentalFiltersReset={async () => undefined}
        onAdjustBalance={noopBalanceAdjustment}
        onCreateAccount={noopPromise}
        onNextRefundPage={noopPromise}
        onOpenAdminRentalDetail={async (_rentalId: number) => undefined}
        onPrevRefundPage={noopPromise}
        onSync={noopPromise}
        onUpdateAccount={noopPromise}
        onUpdateUser={noopUserUpdate}
        onWalletRefund={async () => ({} as any)}
        onReleaseDeposit={onReleaseDeposit}
        onForfeitDeposit={onForfeitDeposit}
        refundReasonOptions={refundReasonOptions}
        user={makeUser()}
        users={[makeUser()]}
      />
    );

    fireEvent.click(screen.getByRole("button", { name: "Refunds" }));

    // Assert detail is rendered and shows release/forfeit buttons
    expect(screen.getByRole("button", { name: "Release deposit" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Forfeit deposit" })).toBeInTheDocument();

    // Test release confirm & submit
    fireEvent.click(screen.getByRole("button", { name: "Release deposit" }));
    expect(screen.getByText(/Are you sure you want to release the deposit/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Confirm release" }));
    await waitFor(() => expect(onReleaseDeposit).toHaveBeenCalledWith(1));

    // Test forfeit form & submit
    fireEvent.click(screen.getByRole("button", { name: "Forfeit deposit" }));
    expect(screen.getByRole("heading", { name: "Forfeit Deposit" })).toBeInTheDocument();
    
    // Fill forfeit evidence reference
    fireEvent.change(screen.getByPlaceholderText("SECURITY_EVENT:123"), { target: { value: "SECURITY_EVENT:123" } });
    fireEvent.click(screen.getByRole("button", { name: "Confirm forfeit" }));
    await waitFor(() => expect(onForfeitDeposit).toHaveBeenCalledWith(1, {
      reason_code: "ACCOUNT_SECURITY_VIOLATION",
      evidence_reference: "SECURITY_EVENT:123"
    }));
  });
});
