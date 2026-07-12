import type {
  Account,
  AdminRentalDetail,
  AdminRentalRefundSummary,
  AdminRentalSummary,
  FinancialBalance,
  Pagination,
  Payment,
  Rental,
  User
} from "../api";

export function makeAccount(overrides: Partial<Account> = {}): Account {
  return {
    id: 1,
    steam_id64: "76561198000000001",
    status: "Available",
    price_per_hour: { amount: 500, currency: "USD" },
    security_deposit: { amount: 700, currency: "USD" },
    games: [],
    ...overrides
  };
}

export function makeRental(overrides: Partial<Rental> = {}): Rental {
  return {
    id: 1,
    user_id: 101,
    account_id: 1,
    status: 1,
    started_at: "2026-07-10T10:00:00Z",
    expires_at: "2099-07-10T12:00:00Z",
    payment_expires_at: "2099-07-10T10:15:00Z",
    rental_price: { amount: 500, currency: "USD" },
    security_deposit: { amount: 700, currency: "USD" },
    deposit_status: "HELD",
    total_price: { amount: 1200, currency: "USD" },
    has_refund: false,
    refund_status: "NONE",
    refund_total_amount: { amount: 0, currency: "USD" },
    ...overrides
  };
}

export function makePayment(overrides: Partial<Payment> = {}): Payment {
  return {
    id: 11,
    rental_id: 1,
    amount: 1200,
    currency: "USD",
    status: 1,
    created_at: "2026-07-10T10:00:00Z",
    ...overrides
  };
}

export function makeBalance(overrides: Partial<FinancialBalance> = {}): FinancialBalance {
  return {
    available_balance: 1500,
    currency: "USD",
    ...overrides
  };
}

export function makeAdminRental(overrides: Partial<AdminRentalRefundSummary> = {}): AdminRentalRefundSummary {
  return {
    id: 1,
    user_id: 101,
    account_id: 1,
    payment_id: 11,
    status: 3,
    payment_status: 2,
    payment_provider: "balance",
    rental_price: { amount: 500, currency: "USD" },
    security_deposit: { amount: 700, currency: "USD" },
    deposit_status: "HELD",
    total_price: { amount: 1200, currency: "USD" },
    has_refund: false,
    refund_status: "NONE",
    refund_total_amount: { amount: 0, currency: "USD" },
    processed_at: undefined,
    ...overrides
  };
}

export function makeAdminSummary(overrides: Partial<AdminRentalSummary> = {}): AdminRentalSummary {
  return {
    total_count: 10,
    eligible_wallet_refund_count: 2,
    rental_status_counts: {
      WAITING_PAYMENT: 2,
      ACTIVE: 3,
      EXPIRED: 2,
      COMPLETED: 2,
      CANCELLED: 1
    },
    payment_status_counts: {
      PENDING: 2,
      SUCCESS: 6,
      FAILED: 1,
      CANCELLED: 1
    },
    refund_status_counts: {
      NONE: 8,
      REQUESTED: 0,
      COMPLETED: 2,
      FAILED: 0
    },
    ...overrides
  };
}

export function makeAdminRentalDetail(overrides: Partial<AdminRentalDetail> = {}): AdminRentalDetail {
  return {
    rental: {
      id: 1,
      user_id: 101,
      account_id: 1,
      status: 3,
      start_at: "2026-07-10T10:00:00Z",
      end_at: "2026-07-10T12:00:00Z",
      rental_price: { amount: 500, currency: "USD" },
      deposit_amount: { amount: 700, currency: "USD" },
      payment_expires_at: "2026-07-10T10:15:00Z",
      created_at: "2026-07-10T09:59:00Z",
      updated_at: "2026-07-10T12:00:00Z"
    },
    payment: {
      id: 11,
      status: 2,
      provider: "balance",
      amount: 1200,
      currency: "USD",
      created_at: "2026-07-10T10:00:00Z"
    },
    deposit: {
      amount: 700,
      currency: "USD",
      status: "HELD",
      held_at: "2026-07-10T10:00:00Z"
    },
    refund_summary: {
      count: 1,
      latest_refund_status: "COMPLETED",
      total_refunded_principal: { amount: 500, currency: "USD" },
      total_refunded_deposit: { amount: 700, currency: "USD" },
      latest_processed_at: "2026-07-10T13:00:00Z"
    },
    ledger_summary: {
      counts_by_display_type: {
        BALANCE_REFUND_CREDIT: 1
      },
      totals_by_display_type: {
        BALANCE_REFUND_CREDIT: { amount: 1200, currency: "USD" }
      },
      latest_entries: [
        {
          id: 1001,
          display_type: "BALANCE_REFUND_CREDIT",
          amount: 1200,
          currency: "USD",
          created_at: "2026-07-10T13:00:00Z"
        }
      ]
    },
    support_flags: {
      eligible_wallet_refund: false,
      refund_ineligible_reason: "REFUND_ALREADY_COMPLETED",
      has_active_credentials_access: false,
      payment_window_expired: true
    },
    ...overrides
  };
}

export function makePagination(overrides: Partial<Pagination> = {}): Pagination {
  return {
    page: 1,
    page_size: 20,
    total_items: 10,
    total_pages: 1,
    ...overrides
  };
}

export function makeUser(overrides: Partial<User> = {}): User {
  return {
    id: 900,
    email: "admin@example.com",
    first_name: "Admin",
    last_name: "User",
    role: "ADMIN",
    is_blocked: false,
    balance: 0,
    trust_score: 100,
    trust_level: "Gold",
    ...overrides
  };
}
