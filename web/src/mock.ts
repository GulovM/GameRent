import type { Account, AuditLog, Game, NotificationItem, Payment, Rental, User } from "./api";

export const mockGames: Game[] = [
  { id: 1, steam_app_id: 730, name: "Counter-Strike 2", header_image: "" },
  { id: 2, steam_app_id: 570, name: "Dota 2", header_image: "" },
  { id: 3, steam_app_id: 1245620, name: "Elden Ring", header_image: "" },
  { id: 4, steam_app_id: 367520, name: "Hollow Knight", header_image: "" }
];

export const mockAccounts: Account[] = [
  {
    id: 1,
    steam_id64: "76561198000000001",
    status: "Available",
    price_per_hour: { amount: 150, currency: "TJS" },
    security_deposit: { amount: 500, currency: "TJS" },
    games: [
      { game_id: 1, name: "Counter-Strike 2", steam_app_id: 730, playtime_minutes: 1045 },
      { game_id: 2, name: "Dota 2", steam_app_id: 570, playtime_minutes: 320 },
      { game_id: 4, name: "Wallpaper Engine", steam_app_id: 431960, playtime_minutes: 44 }
    ]
  },
  {
    id: 2,
    steam_id64: "76561198000000147",
    status: "Available",
    price_per_hour: { amount: 120, currency: "TJS" },
    security_deposit: { amount: 500, currency: "TJS" },
    games: [
      { game_id: 2, name: "Dota 2", steam_app_id: 570, playtime_minutes: 1880 },
      { game_id: 1, name: "Counter-Strike 2", steam_app_id: 730, playtime_minutes: 740 },
      { game_id: 5, name: "Portal 2", steam_app_id: 620, playtime_minutes: 160 }
    ]
  },
  {
    id: 3,
    steam_id64: "76561198000000883",
    status: "Rented",
    price_per_hour: { amount: 240, currency: "TJS" },
    security_deposit: { amount: 900, currency: "TJS" },
    games: [
      { game_id: 3, name: "Elden Ring", steam_app_id: 1245620, playtime_minutes: 260 },
      { game_id: 6, name: "Cyberpunk 2077", steam_app_id: 1091500, playtime_minutes: 540 },
      { game_id: 7, name: "Skyrim", steam_app_id: 489830, playtime_minutes: 1160 }
    ]
  },
  {
    id: 4,
    steam_id64: "76561198000000512",
    status: "Available",
    price_per_hour: { amount: 90, currency: "TJS" },
    security_deposit: { amount: 350, currency: "TJS" },
    games: [
      { game_id: 8, name: "Hades", steam_app_id: 1145360, playtime_minutes: 340 },
      { game_id: 4, name: "Hollow Knight", steam_app_id: 367520, playtime_minutes: 620 },
      { game_id: 9, name: "Stardew Valley", steam_app_id: 413150, playtime_minutes: 1400 }
    ]
  }
];

export const mockUser: User = {
  id: 1,
  email: "rent@example.com",
  first_name: "Ivan",
  last_name: "Petrov",
  role: "ADMIN",
  email_verified: true,
  is_blocked: false,
  trust_score: 420,
  trust_level: "Silver",
  balance: 2500
};

export const mockRentals: Rental[] = [
  {
    id: 45,
    user_id: 1,
    account_id: 1,
    status: 2,
    started_at: new Date(Date.now() - 22 * 60 * 1000).toISOString(),
    expires_at: new Date(Date.now() + 103 * 60 * 1000).toISOString(),
    rental_price: { amount: 300, currency: "TJS" },
    security_deposit: { amount: 500, currency: "TJS" },
    total_price: { amount: 800, currency: "TJS" }
  }
];

export const mockPayments: Payment[] = [
  { id: 88, rental_id: 45, amount: 800, currency: "TJS", status: "Waiting", created_at: new Date().toISOString() },
  { id: 72, rental_id: 31, amount: 620, currency: "TJS", status: 2, created_at: new Date(Date.now() - 86400000).toISOString() }
];

export const mockNotifications: NotificationItem[] = [
  { id: 1, type: 1, title: "Аренда активна", body: "Steam credentials доступны до окончания таймера.", read: false, created_at: new Date().toISOString() },
  { id: 2, type: 2, title: "Steam Sync", body: "Библиотека аккаунта обновлена.", read: true, created_at: new Date(Date.now() - 3600000).toISOString() }
];

export const mockAuditLogs: AuditLog[] = [
  { id: 1, actor_user_id: 1, entity_type: "account", entity_id: 1, action: "ADMIN_SYNC_ACCOUNT", created_at: new Date().toISOString() },
  { id: 2, actor_user_id: 1, entity_type: "user", entity_id: 2, action: "ADMIN_UPDATE_USER", created_at: new Date(Date.now() - 7200000).toISOString() }
];
