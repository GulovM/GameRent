import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import type { Payment, Rental, RentalCredentials } from "../../api";
import { PAYMENT_STATUS_PENDING, PAYMENT_STATUS_SUCCESS } from "../payments/paymentStatus";
import { RENTAL_STATUS_ACTIVE, RENTAL_STATUS_WAITING_PAYMENT } from "./rentalStatus";
import { RentalsView } from "./RentalsView";
import { makeAccount, makeBalance, makePayment, makeRental } from "../../test/factories";

const noopRentalAction = (_rental: Rental) => undefined;
const noopRefresh = () => undefined;
const noopSelect = (_rentalId: number) => undefined;

function renderRentalsView({
  rentals = [makeRental()],
  payments = [makePayment()],
  credentials = null,
  balance = makeBalance(),
  walletPaymentLoadingRentalId = null,
  walletPaymentError = null,
  onLoadCredentials = vi.fn(),
  onPayWithBalance = vi.fn()
}: {
  rentals?: Rental[];
  payments?: Payment[];
  credentials?: RentalCredentials | null;
  balance?: ReturnType<typeof makeBalance> | null;
  walletPaymentLoadingRentalId?: number | null;
  walletPaymentError?: { rentalId: number; message: string } | null;
  onLoadCredentials?: (rental: Rental) => void;
  onPayWithBalance?: (rental: Rental) => void;
} = {}) {
  render(
    <RentalsView
      accounts={[makeAccount()]}
      balance={balance}
      balanceLoading={false}
      credentials={credentials}
      credentialsError={null}
      credentialsLoading={false}
      onCancel={noopRentalAction}
      onExtend={noopRentalAction}
      onLoadCredentials={onLoadCredentials}
      onPayWithBalance={onPayWithBalance}
      onRefreshStatus={noopRefresh}
      onSelectRental={noopSelect}
      payments={payments}
      rentals={rentals}
      rentalsRefreshing={false}
      selectedRentalId={rentals[0]?.id ?? null}
      walletPaymentError={walletPaymentError}
      walletPaymentLoadingRentalId={walletPaymentLoadingRentalId}
    />
  );
}

describe("RentalsView critical flows", () => {
  it("shows an active wallet payment button when balance is sufficient", () => {
    renderRentalsView();

    expect(screen.getByText("Balance is sufficient")).toBeInTheDocument();
    expect(screen.getByLabelText("Pay with wallet balance")).toBeEnabled();
  });

  it("disables wallet payment when balance is insufficient and shows a clear message", () => {
    renderRentalsView({
      balance: makeBalance({ available_balance: 100 })
    });

    expect(screen.getByText("Balance is insufficient")).toBeInTheDocument();
    expect(screen.getByLabelText("Pay with wallet balance")).toBeDisabled();
  });

  it("blocks a second wallet payment submit after the first click", async () => {
    const onPayWithBalance = vi.fn();

    function Harness() {
      const [loadingRentalId, setLoadingRentalId] = useState<number | null>(null);
      const rental = makeRental();
      const payment = makePayment();

      return (
        <RentalsView
          accounts={[makeAccount()]}
          balance={makeBalance()}
          balanceLoading={false}
          credentials={null}
          credentialsError={null}
          credentialsLoading={false}
          onCancel={noopRentalAction}
          onExtend={noopRentalAction}
          onLoadCredentials={noopRentalAction}
          onPayWithBalance={(nextRental) => {
            onPayWithBalance(nextRental.id);
            setLoadingRentalId(nextRental.id);
          }}
          onRefreshStatus={noopRefresh}
          onSelectRental={noopSelect}
          payments={[payment]}
          rentals={[rental]}
          rentalsRefreshing={false}
          selectedRentalId={rental.id}
          walletPaymentError={null}
          walletPaymentLoadingRentalId={loadingRentalId}
        />
      );
    }

    render(<Harness />);

    const button = screen.getByLabelText("Pay with wallet balance");
    fireEvent.click(button);

    await waitFor(() => expect(screen.getByLabelText("Pay with wallet balance")).toBeDisabled());
    fireEvent.click(screen.getByLabelText("Pay with wallet balance"));

    expect(onPayWithBalance).toHaveBeenCalledTimes(1);
  });

  it("does not auto-request credentials after a successful wallet payment", async () => {
    const onLoadCredentials = vi.fn();

    function Harness() {
      const [rental, setRental] = useState(makeRental());
      const [payment, setPayment] = useState(makePayment());

      return (
        <RentalsView
          accounts={[makeAccount()]}
          balance={makeBalance()}
          balanceLoading={false}
          credentials={null}
          credentialsError={null}
          credentialsLoading={false}
          onCancel={noopRentalAction}
          onExtend={noopRentalAction}
          onLoadCredentials={onLoadCredentials}
          onPayWithBalance={() => {
            setPayment((current) => ({ ...current, status: PAYMENT_STATUS_SUCCESS }));
            setRental((current) => ({ ...current, status: RENTAL_STATUS_ACTIVE }));
          }}
          onRefreshStatus={noopRefresh}
          onSelectRental={noopSelect}
          payments={[payment]}
          rentals={[rental]}
          rentalsRefreshing={false}
          selectedRentalId={rental.id}
          walletPaymentError={null}
          walletPaymentLoadingRentalId={null}
        />
      );
    }

    render(<Harness />);

    fireEvent.click(screen.getByLabelText("Pay with wallet balance"));

    await waitFor(() => expect(screen.getByLabelText("Get rental credentials")).toBeInTheDocument());
    expect(onLoadCredentials).not.toHaveBeenCalled();
  });

  it("does not show a credentials action for WAITING_PAYMENT rentals", () => {
    renderRentalsView();

    expect(screen.queryByLabelText("Get rental credentials")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Get credentials for rental 1")).not.toBeInTheDocument();
  });

  it("shows a credentials action for ACTIVE rentals", () => {
    renderRentalsView({
      rentals: [makeRental({ status: RENTAL_STATUS_ACTIVE })],
      payments: [makePayment({ status: PAYMENT_STATUS_SUCCESS })]
    });

    expect(screen.getByLabelText("Get rental credentials")).toBeInTheDocument();
  });

  it("keeps credentials out of browser storage", async () => {
    const localStorageSpy = vi.spyOn(window.localStorage.__proto__, "setItem");
    const sessionStorageSpy = vi.spyOn(window.sessionStorage.__proto__, "setItem");

    function Harness() {
      const [credentials, setCredentials] = useState<RentalCredentials | null>(null);

      return (
        <RentalsView
          accounts={[makeAccount()]}
          balance={makeBalance()}
          balanceLoading={false}
          credentials={credentials}
          credentialsError={null}
          credentialsLoading={false}
          onCancel={noopRentalAction}
          onExtend={noopRentalAction}
          onLoadCredentials={() => setCredentials({ login: "demo_login", password: "demo_password" })}
          onPayWithBalance={noopRentalAction}
          onRefreshStatus={noopRefresh}
          onSelectRental={noopSelect}
          payments={[makePayment({ status: PAYMENT_STATUS_SUCCESS })]}
          rentals={[makeRental({ status: RENTAL_STATUS_ACTIVE })]}
          rentalsRefreshing={false}
          selectedRentalId={1}
          walletPaymentError={null}
          walletPaymentLoadingRentalId={null}
        />
      );
    }

    render(<Harness />);

    fireEvent.click(screen.getByLabelText("Get rental credentials"));

    await waitFor(() => expect(screen.getByText("demo_login")).toBeInTheDocument());
    expect(screen.getByText("demo_password")).toBeInTheDocument();
    expect(localStorageSpy).not.toHaveBeenCalled();
    expect(sessionStorageSpy).not.toHaveBeenCalled();
  });
});
