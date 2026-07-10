import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import type { AdminRentalFilters, AdminRentalRefundSummary } from "../../api";
import { PAYMENT_STATUS_PENDING } from "../payments/paymentStatus";
import { RENTAL_STATUS_ACTIVE } from "../rentals/rentalStatus";
import { AdminView } from "./AdminView";
import { makeAccount, makeAdminRental, makeAdminSummary, makePagination, makeUser } from "../../test/factories";

const noopPromise = async () => undefined;
const noopUserUpdate = async () => undefined;

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
  })
}: {
  rentals?: AdminRentalRefundSummary[];
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
      adminRentalFilters={{}}
      adminRentals={rentals}
      adminRentalsError={null}
      adminRentalsLoading={false}
      adminRentalsPagination={makePagination()}
      adminRentalsSummary={makeAdminSummary()}
      auditLogs={[]}
      onAdminRentalFiltersChange={async (_filters: AdminRentalFilters) => undefined}
      onAdminRentalFiltersReset={async () => undefined}
      onCreateAccount={noopPromise}
      onNextRefundPage={noopPromise}
      onPrevRefundPage={noopPromise}
      onSync={noopPromise}
      onToggleAccount={noopPromise}
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
          adminRentalFilters={{}}
          adminRentals={rentals}
          adminRentalsError={null}
          adminRentalsLoading={false}
          adminRentalsPagination={makePagination()}
          adminRentalsSummary={makeAdminSummary()}
          auditLogs={[]}
          onAdminRentalFiltersChange={async (_filters: AdminRentalFilters) => undefined}
          onAdminRentalFiltersReset={async () => undefined}
          onCreateAccount={noopPromise}
          onNextRefundPage={noopPromise}
          onPrevRefundPage={noopPromise}
          onSync={noopPromise}
          onToggleAccount={noopPromise}
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
