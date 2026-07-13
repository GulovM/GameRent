export type ApiEnvelope<T> = {
  success: boolean;
  data?: T;
  error?: {
    code: string;
    message: string;
  };
};

export type Money = {
  amount: number;
  currency: string;
};

export type User = {
  id: number;
  email: string;
  first_name: string;
  last_name: string;
  role: "ADMIN" | "RENT" | string;
  email_verified?: boolean;
  is_blocked: boolean;
  trust_score?: number;
  trust_level?: string;
  balance?: number;
  created_at?: string;
  updated_at?: string;
};

export type AdminUserPatch = {
  trust_score?: number;
  is_blocked?: boolean;
  role?: string;
};

export type AdminBalanceAdjustmentInput = {
  amount: number;
  currency: "USD";
  reason_code: string;
  comment?: string;
  idempotency_key: string;
};

export type AdminBalanceAdjustmentResponse = {
  adjustment_id: number;
  user_id: number;
  previous_balance: number;
  new_balance: number;
  amount: number;
  currency: "USD";
  ledger_entry_id: number;
  idempotency_key: string;
  idempotent_replay: boolean;
  created_at: string;
};

export type Game = {
  id: number;
  steam_app_id: number;
  name: string;
  header_image: string;
};

export type AccountGame = {
  game_id: number;
  name: string;
  steam_app_id: number;
  playtime_minutes: number;
};

export type Account = {
  id: number;
  steam_id64: string;
  status: string;
  price_per_hour: Money;
  security_deposit: Money;
  games: AccountGame[];
};

export type Pagination = {
  page: number;
  page_size: number;
  total_items: number;
  total_pages: number;
};

export type Rental = {
  id: number;
  user_id: number;
  account_id: number;
  status: number;
  started_at: string;
  expires_at: string;
  payment_expires_at?: string;
  rental_price: Money;
  security_deposit: Money;
  deposit_status: "NONE" | "HELD" | "RELEASED" | "FORFEITED" | "REFUNDED" | "UNKNOWN" | string;
  total_price: Money;
  has_refund: boolean;
  refund_status: "NONE" | "REQUESTED" | "COMPLETED" | "FAILED" | string;
  refund_total_amount: Money;
  processed_at?: string;
};

export type FinancialBalance = {
  available_balance: number;
  currency: string;
};

export type LedgerEntry = {
  id: number;
  entry_type: number;
  amount: number;
  currency: string;
  rental_id?: number;
  payment_id?: number;
  created_at: string;
  display_type: string;
};

export type RefundEntry = {
  id: number;
  rental_id: number;
  payment_id: number;
  status: "REQUESTED" | "COMPLETED" | "FAILED" | string;
  principal_amount: number;
  deposit_amount: number;
  total_amount: number;
  currency: string;
  reason_code?: string;
  created_at: string;
  processed_at?: string;
};

export type AdminRentalRefundSummary = {
  id: number;
  user_id: number;
  account_id: number;
  payment_id?: number;
  status: number;
  payment_status: number | string;
  payment_provider: string;
  rental_price: Money;
  security_deposit: Money;
  deposit_status: "NONE" | "HELD" | "RELEASED" | "FORFEITED" | "REFUNDED" | "UNKNOWN" | string;
  total_price: Money;
  has_refund: boolean;
  refund_status: "NONE" | "REQUESTED" | "COMPLETED" | "FAILED" | string;
  refund_total_amount: Money;
  processed_at?: string;
};

export type RefundReasonCodeOption = {
  code: string;
  label: string;
};

export type AdminRentalFilters = {
  rental_status?: "WAITING_PAYMENT" | "ACTIVE" | "EXPIRED" | "CANCELLED" | "COMPLETED" | "";
  payment_status?: "PENDING" | "SUCCESS" | "FAILED" | "";
  payment_provider?: "balance" | "internal" | "";
  deposit_status?: "NONE" | "HELD" | "RELEASED" | "FORFEITED" | "REFUNDED" | "UNKNOWN" | "";
  refund_status?: "NONE" | "REQUESTED" | "COMPLETED" | "FAILED" | "";
  eligible_wallet_refund?: boolean | undefined;
  user_id?: number | undefined;
  rental_id?: number | undefined;
};

export type AdminRentalSummary = {
  total_count: number;
  eligible_wallet_refund_count: number;
  rental_status_counts: Record<string, number>;
  payment_status_counts: Record<string, number>;
  refund_status_counts: Record<string, number>;
};

export type AdminRentalsResponse = {
  rentals: AdminRentalRefundSummary[];
  summary: AdminRentalSummary;
  pagination: Pagination;
};

export type AdminRentalDetail = {
  rental: {
    id: number;
    user_id: number;
    account_id: number;
    status: number;
    start_at: string;
    end_at: string;
    rental_price: Money;
    deposit_amount: Money;
    payment_expires_at: string;
    created_at: string;
    updated_at: string;
  };
  payment: null | {
    id: number;
    status: number | string;
    provider: string;
    amount: number;
    currency: string;
    created_at: string;
  };
  deposit: null | {
    amount: number;
    currency: string;
    status: "NONE" | "HELD" | "RELEASED" | "FORFEITED" | "REFUNDED" | string;
    held_at?: string;
    released_at?: string;
    forfeited_at?: string;
    refunded_at?: string;
  };
  refund_summary: {
    count: number;
    latest_refund_status: "NONE" | "REQUESTED" | "COMPLETED" | "FAILED" | string;
    total_refunded_principal: Money;
    total_refunded_deposit: Money;
    latest_processed_at?: string;
  };
  ledger_summary: {
    counts_by_display_type: Record<string, number>;
    totals_by_display_type: Record<string, Money>;
    latest_entries: Array<{
      id: number;
      display_type: string;
      amount: number;
      currency: string;
      created_at: string;
    }>;
  };
  support_flags: {
    eligible_wallet_refund: boolean;
    refund_ineligible_reason: string;
    has_active_credentials_access: boolean;
    payment_window_expired: boolean;
  };
};

export type Payment = {
  id: number;
  rental_id: number;
  amount: number;
  currency: string;
  status: number | string;
  created_at?: string;
};

export type RentalCredentials = {
  login: string;
  password: string;
};

export type WalletPaymentResponse = {
  changed: boolean;
  idempotent: boolean;
  payment_id: number;
  rental_id: number;
  account_id: number;
  payment_status: number;
  rental_status: number;
  account_status: number;
  payment_provider: string;
};

export type AdminWalletRefundResponse = {
  changed: boolean;
  idempotent: boolean;
  status: string;
  principal_amount: Money;
  deposit_amount: Money;
  total_amount: Money;
  deposit_status: string;
};

export type NotificationItem = {
  id: number;
  type: number;
  title: string;
  body: string;
  read: boolean;
  created_at: string;
};

export type Review = {
  id: number;
  user_id: number;
  account_id: number;
  rating: number;
  comment: string;
  created_at: string;
};

export type AuditLog = {
  id: number;
  actor_user_id: number | null;
  entity_type: string;
  entity_id: number;
  action: string;
  created_at: string;
};

export type Tokens = {
  access_token: string;
  refresh_token: string;
};

export type RegisterPayload = {
  email: string;
  password: string;
  first_name: string;
  last_name: string;
};

export type LoginPayload = {
  email: string;
  password: string;
};

export type Query = Record<string, string | number | boolean | undefined | null>;

export class ApiError extends Error {
  status: number;
  code: string;

  constructor(message: string, status: number, code = "UNKNOWN_ERROR") {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

export function isApiError(error: unknown): error is ApiError {
  return error instanceof ApiError;
}

const API_BASE = (import.meta.env.VITE_API_BASE_URL || "").replace(/\/$/, "");
const ACCESS_KEY = "gamerent.access";
const REFRESH_KEY = "gamerent.refresh";
export const SESSION_INVALIDATED_EVENT = "gamerent:session-invalidated";
export const AUTHORIZATION_CHANGED_EVENT = "gamerent:authorization-changed";

let refreshPromise: Promise<Tokens> | null = null;
let authEpoch = 0;

export function getAccessToken() {
  return localStorage.getItem(ACCESS_KEY);
}

export function getRefreshToken() {
  return localStorage.getItem(REFRESH_KEY);
}

export function saveTokens(tokens: Tokens) {
  localStorage.setItem(ACCESS_KEY, tokens.access_token);
  localStorage.setItem(REFRESH_KEY, tokens.refresh_token);
}

export function clearTokens() {
	authEpoch += 1;
  localStorage.removeItem(ACCESS_KEY);
  localStorage.removeItem(REFRESH_KEY);
}

function url(path: string, query?: Query) {
  const params = new URLSearchParams();
  Object.entries(query ?? {}).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== "") {
      params.set(key, String(value));
    }
  });
  return `${API_BASE}${path}${params.size ? `?${params.toString()}` : ""}`;
}

async function parseResponse<T>(response: Response): Promise<{ envelope: ApiEnvelope<T>; error?: ApiError }> {
  const envelope = (await response.json().catch(() => ({}))) as ApiEnvelope<T>;
  if (!response.ok || envelope.success === false) {
    return {
      envelope,
      error: new ApiError(envelope.error?.message || `HTTP ${response.status}`, response.status, envelope.error?.code)
    };
  }
  return { envelope };
}

async function refreshTokensSingleFlight(): Promise<Tokens> {
  if (refreshPromise) return refreshPromise;

  const refreshToken = getRefreshToken();
  if (!refreshToken) throw new ApiError("Session has expired", 401, "UNAUTHORIZED");
  const epoch = authEpoch;

  refreshPromise = (async () => {
    const response = await fetch(url("/api/v1/auth/refresh"), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ refresh_token: refreshToken })
    });
    const parsed = await parseResponse<Tokens>(response);
    if (parsed.error || !parsed.envelope.data) {
      throw parsed.error ?? new ApiError("Session has expired", 401, "UNAUTHORIZED");
    }
    if (authEpoch !== epoch || getRefreshToken() !== refreshToken) {
      throw new ApiError("Session has changed", 401, "SESSION_CHANGED");
    }
    saveTokens(parsed.envelope.data);
    return parsed.envelope.data;
  })()
    .catch((error) => {
      if (authEpoch === epoch) {
        clearTokens();
        window.dispatchEvent(new Event(SESSION_INVALIDATED_EVENT));
      }
      throw error;
    })
    .finally(() => {
      refreshPromise = null;
    });

  return refreshPromise;
}

async function request<T>(path: string, options: RequestInit = {}, query?: Query, allowRefresh = true): Promise<T> {
  const headers = new Headers(options.headers);
  if (!headers.has("Content-Type") && options.body) headers.set("Content-Type", "application/json");
  const token = getAccessToken();
  if (token) headers.set("Authorization", `Bearer ${token}`);

  const response = await fetch(url(path, query), { ...options, headers });
  const parsed = await parseResponse<T>(response);

  if (parsed.error) {
    const isCredentialEndpoint =
      path === "/api/v1/auth/register" ||
      path === "/api/v1/auth/login" ||
      path === "/api/v1/auth/refresh" ||
      path === "/api/v1/auth/logout";
    if (parsed.error.status === 401 && allowRefresh && !isCredentialEndpoint && getRefreshToken()) {
      if (token && getAccessToken() && getAccessToken() !== token) {
        return request<T>(path, options, query, false);
      }
      await refreshTokensSingleFlight();
      return request<T>(path, options, query, false);
    }
    if (parsed.error.status === 401 && parsed.error.code === "SESSION_REVOKED") {
      clearTokens();
      window.dispatchEvent(new Event(SESSION_INVALIDATED_EVENT));
    } else if (parsed.error.status === 403 && path.startsWith("/api/v1/admin/")) {
      window.dispatchEvent(new Event(AUTHORIZATION_CHANGED_EVENT));
    }
    throw parsed.error;
  }

  return parsed.envelope.data as T;
}

export const api = {
  register(payload: RegisterPayload) {
    return request<Tokens & { user: User }>("/api/v1/auth/register", {
      method: "POST",
      body: JSON.stringify(payload)
    });
  },
  login(payload: LoginPayload) {
    return request<Tokens>("/api/v1/auth/login", {
      method: "POST",
      body: JSON.stringify(payload)
    });
  },
  refresh(refresh_token: string) {
    return request<Tokens>("/api/v1/auth/refresh", {
      method: "POST",
      body: JSON.stringify({ refresh_token })
    });
  },
  async logout(refresh_token?: string) {
    if (refreshPromise) {
      await refreshPromise.catch(() => undefined);
    }
    const tokenToRevoke = getRefreshToken() ?? refresh_token;
    if (!tokenToRevoke) return { message: "Logged out locally" };
    return request<{ message: string }>("/api/v1/auth/logout", {
      method: "POST",
      body: JSON.stringify({ refresh_token: tokenToRevoke })
    });
  },
  me() {
    return request<User>("/api/v1/auth/me");
  },
  myBalance() {
    return request<FinancialBalance>("/api/v1/me/balance");
  },
  myLedger(query?: Query) {
    return request<{ entries: LedgerEntry[]; pagination: Pagination }>("/api/v1/me/ledger", {}, query);
  },
  myRefunds(query?: Query) {
    return request<{ refunds: RefundEntry[]; pagination: Pagination }>("/api/v1/me/refunds", {}, query);
  },
  updateUser(id: number, payload: { first_name: string; last_name: string }) {
    return request<User>(`/api/v1/users/${id}`, {
      method: "PATCH",
      body: JSON.stringify(payload)
    });
  },
  games(query?: Query) {
    return request<{ games: Game[]; pagination: Pagination }>("/api/v1/games", {}, query);
  },
  accounts(query?: Query) {
    return request<{ accounts: Account[]; pagination: Pagination }>("/api/v1/accounts", {}, query);
  },
  account(id: number) {
    return request<Account>(`/api/v1/accounts/${id}`);
  },
  availability(id: number) {
    return request<{ account_id: number; available: boolean }>(`/api/v1/accounts/${id}/availability`);
  },
  calculateRental(payload: { account_id: number; duration_hours: number }) {
    return request<{
      price_per_hour: Money;
      security_deposit: Money;
      duration_hours: number;
      total_price: Money;
    }>("/api/v1/rentals/calculate", {
      method: "POST",
      body: JSON.stringify(payload)
    });
  },
  createRental(payload: { account_id: number; duration_hours: number }) {
    return request<Rental>("/api/v1/rentals", {
      method: "POST",
      body: JSON.stringify(payload)
    });
  },
  rentals() {
    return request<{ rentals: Rental[] }>("/api/v1/me/rentals");
  },
  rental(id: number) {
    return request<Rental>(`/api/v1/rentals/${id}`);
  },
  rentalCredentials(id: number, signal?: AbortSignal) {
    return request<RentalCredentials>(`/api/v1/me/rentals/${id}/credentials`, { signal });
  },
  payRentalWithBalance(id: number) {
    return request<WalletPaymentResponse>(`/api/v1/me/rentals/${id}/pay-with-balance`, { method: "POST" });
  },
  cancelRental(id: number) {
    return request<{ message: string }>(`/api/v1/rentals/${id}/cancel`, { method: "POST" });
  },
  createPayment(rental_id: number) {
    return request<Payment>("/api/v1/payments", {
      method: "POST",
      body: JSON.stringify({ rental_id })
    });
  },
  payments() {
    return request<{ payments: Payment[] }>("/api/v1/me/payments");
  },
  reviews(accountId: number) {
    return request<{ reviews: Review[] }>(`/api/v1/accounts/${accountId}/reviews`);
  },
  createReview(payload: { account_id: number; rental_id: number; rating: number; comment: string }) {
    return request<{ id: number }>("/api/v1/reviews", {
      method: "POST",
      body: JSON.stringify(payload)
    });
  },
  notifications() {
    return request<{ notifications: NotificationItem[] }>("/api/v1/me/notifications");
  },
  readNotification(id: number) {
    return request<{ message: string }>(`/api/v1/notifications/${id}/read`, { method: "PATCH" });
  },
  favoriteAccount(id: number) {
    return request<{ message: string }>(`/api/v1/accounts/${id}/favorite`, { method: "POST" });
  },
  unfavoriteAccount(id: number) {
    return request<{ message: string }>(`/api/v1/accounts/${id}/favorite`, { method: "DELETE" });
  },
  adminAccounts() {
    return request<{ accounts: Array<{ id: number; steam_id64: string; status: number; hourly_price: number; deposit_amount: number }> }>(
      "/api/v1/admin/accounts"
    );
  },
  adminCreateAccount(payload: {
    steam_id64: string;
    steam_login: string;
    steam_password: string;
    price_per_hour: number;
    security_deposit: number;
  }) {
    return request<{ id: number; games_count?: number; sync_error?: string }>("/api/v1/admin/accounts", {
      method: "POST",
      body: JSON.stringify(payload)
    });
  },
  adminUpdateAccount(id: number, payload: { price_per_hour?: number; security_deposit?: number }) {
    return request<{ message: string }>(`/api/v1/admin/accounts/${id}`, {
      method: "PATCH",
      body: JSON.stringify(payload)
    });
  },
  adminSyncAccount(id: number) {
    return request<{ message: string; games_count: number }>(`/api/v1/admin/accounts/${id}/sync`, { method: "POST" });
  },
  adminUsers() {
    return request<{ users: User[] }>("/api/v1/admin/users");
  },
  adminUpdateUser(id: number, payload: AdminUserPatch) {
    return request<{ message: string }>(`/api/v1/admin/users/${id}`, {
      method: "PATCH",
      body: JSON.stringify(payload)
    });
  },
  adminAdjustBalance(id: number, payload: AdminBalanceAdjustmentInput) {
    return request<AdminBalanceAdjustmentResponse>(`/api/v1/admin/users/${id}/balance-adjustments`, {
      method: "POST",
      body: JSON.stringify(payload)
    });
  },
  adminAuditLogs() {
    return request<{ audit_logs: AuditLog[] }>("/api/v1/admin/audit-logs");
  },
  adminRentals(query?: Query) {
    return request<AdminRentalsResponse>("/api/v1/admin/rentals", {}, query);
  },
  adminRentalDetail(rentalId: number) {
    return request<AdminRentalDetail>(`/api/v1/admin/rentals/${rentalId}`);
  },
  adminRefundReasonCodes() {
    return request<{ reason_codes: RefundReasonCodeOption[] }>("/api/v1/admin/refund-reason-codes");
  },
  adminWalletRefund(rentalId: number, reason_code: string) {
    return request<AdminWalletRefundResponse>(`/api/v1/admin/rentals/${rentalId}/wallet-refund`, {
      method: "POST",
      body: JSON.stringify({ reason_code })
    });
  }
};
