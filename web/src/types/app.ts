export type View = "catalog" | "rentals" | "payments" | "profile" | "admin";

export type AdminTab = "overview" | "accounts" | "refunds" | "users" | "audit";

export type Toast = { type: "ok" | "error"; message: string } | null;

export type AdminAccountPatch = {
  status?: number;
  price_per_hour?: number;
  security_deposit?: number;
};
