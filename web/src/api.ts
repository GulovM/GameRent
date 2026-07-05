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
  rental_price: Money;
  security_deposit: Money;
  total_price: Money;
};

export type Payment = {
  id: number;
  rental_id: number;
  amount: number;
  currency: string;
  status: number | string;
  created_at?: string;
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

const API_BASE = (import.meta.env.VITE_API_BASE_URL || "").replace(/\/$/, "");
const ACCESS_KEY = "gamerent.access";
const REFRESH_KEY = "gamerent.refresh";

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

async function request<T>(path: string, options: RequestInit = {}, query?: Query): Promise<T> {
  const headers = new Headers(options.headers);
  if (!headers.has("Content-Type") && options.body) headers.set("Content-Type", "application/json");
  const token = getAccessToken();
  if (token) headers.set("Authorization", `Bearer ${token}`);

  const response = await fetch(url(path, query), { ...options, headers });
  const envelope = (await response.json().catch(() => ({}))) as ApiEnvelope<T>;

  if (!response.ok || envelope.success === false) {
    const message = envelope.error?.message || `HTTP ${response.status}`;
    throw new Error(message);
  }

  return envelope.data as T;
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
  logout(refresh_token: string) {
    return request<{ message: string }>("/api/v1/auth/logout", {
      method: "POST",
      body: JSON.stringify({ refresh_token })
    });
  },
  me() {
    return request<User>("/api/v1/auth/me");
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
  cancelRental(id: number) {
    return request<{ message: string }>(`/api/v1/rentals/${id}/cancel`, { method: "POST" });
  },
  extendRental(id: number, duration_hours: number) {
    return request<{ message: string }>(`/api/v1/rentals/${id}/extend`, {
      method: "POST",
      body: JSON.stringify({ duration_hours })
    });
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
    return request<{ id: number }>("/api/v1/admin/accounts", {
      method: "POST",
      body: JSON.stringify(payload)
    });
  },
  adminUpdateAccount(id: number, payload: { status?: number; price_per_hour?: number; security_deposit?: number }) {
    return request<{ message: string }>(`/api/v1/admin/accounts/${id}`, {
      method: "PATCH",
      body: JSON.stringify(payload)
    });
  },
  adminSyncAccount(id: number) {
    return request<{ message: string }>(`/api/v1/admin/accounts/${id}/sync`, { method: "POST" });
  },
  adminUsers() {
    return request<{ users: User[] }>("/api/v1/admin/users");
  },
  adminUpdateUser(id: number, payload: { trust_score?: number; is_blocked?: boolean; balance?: number; role?: string }) {
    return request<{ message: string }>(`/api/v1/admin/users/${id}`, {
      method: "PATCH",
      body: JSON.stringify(payload)
    });
  },
  adminAuditLogs() {
    return request<{ audit_logs: AuditLog[] }>("/api/v1/admin/audit-logs");
  }
};
