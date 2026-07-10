import { DatabaseZap } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import {
  Account,
  AdminRentalFilters,
  AdminRentalSummary,
  AdminRentalRefundSummary,
  AdminWalletRefundResponse,
  api,
  AuditLog,
  clearTokens,
  FinancialBalance,
  getAccessToken,
  getRefreshToken,
  isApiError,
  NotificationItem,
  Pagination,
  Payment,
  RefundReasonCodeOption,
  Rental,
  RentalCredentials,
  User
} from "./api";
import { AppHeader } from "./components/AppHeader";
import { AuthDialog } from "./components/AuthDialog";
import { MobileNav } from "./components/MobileNav";
import { AdminView } from "./features/admin/AdminView";
import { CatalogView, CheckoutDrawer } from "./features/catalog/CatalogView";
import { PaymentsView } from "./features/payments/PaymentsView";
import { ProfileView } from "./features/profile/ProfileView";
import { RentalsView } from "./features/rentals/RentalsView";
import {
  canRequestCredentials,
  findPaymentForRental,
  RENTAL_POLL_INTERVAL_MS,
  RENTAL_STATUS_ACTIVE,
  RENTAL_STATUS_CANCELLED,
  RENTAL_STATUS_WAITING_PAYMENT
} from "./features/rentals/rentalStatus";
import type { AdminAccountPatch, Toast, View } from "./types/app";
import { isUnauthorized, messageForApiError, messageForWalletPaymentError } from "./utils/apiErrors";
import { asList, normalizeAccount, statusFromNumber } from "./utils/accounts";

const DEFAULT_ADMIN_RENTAL_FILTERS: AdminRentalFilters = {};

function useTicker() {
  const [, setTick] = useState(0);

  useEffect(() => {
    const id = window.setInterval(() => setTick((value) => value + 1), 1000);
    return () => window.clearInterval(id);
  }, []);
}

export default function App() {
  useTicker();

  const [view, setView] = useState<View>(() => {
    const hash = window.location.hash.replace("#", "") as View;
    return ["catalog", "rentals", "payments", "profile", "admin"].includes(hash) ? hash : "catalog";
  });
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [rentals, setRentals] = useState<Rental[]>([]);
  const [payments, setPayments] = useState<Payment[]>([]);
  const [notifications, setNotifications] = useState<NotificationItem[]>([]);
  const [auditLogs, setAuditLogs] = useState<AuditLog[]>([]);
  const [adminRentals, setAdminRentals] = useState<AdminRentalRefundSummary[]>([]);
  const [adminRentalsSummary, setAdminRentalsSummary] = useState<AdminRentalSummary | null>(null);
  const [adminRentalsPagination, setAdminRentalsPagination] = useState<Pagination | null>(null);
  const [adminRentalsPage, setAdminRentalsPage] = useState(1);
  const [adminRentalFilters, setAdminRentalFilters] = useState<AdminRentalFilters>(DEFAULT_ADMIN_RENTAL_FILTERS);
  const [adminRentalsLoading, setAdminRentalsLoading] = useState(false);
  const [adminRentalsError, setAdminRentalsError] = useState<string | null>(null);
  const [adminRefundReasonCodes, setAdminRefundReasonCodes] = useState<RefundReasonCodeOption[]>([]);
  const [adminUsers, setAdminUsers] = useState<User[]>([]);
  const [user, setUser] = useState<User | null>(null);
  const [selectedAccount, setSelectedAccount] = useState<Account | null>(null);
  const [duration, setDuration] = useState(2);
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("Available");
  const [maxPrice, setMaxPrice] = useState(300);
  const [loading, setLoading] = useState(false);
  const [authOpen, setAuthOpen] = useState(false);
  const [toast, setToast] = useState<Toast>(null);
  const [backendError, setBackendError] = useState<string | null>(null);
  const [selectedRentalId, setSelectedRentalId] = useState<number | null>(null);
  const [credentials, setCredentials] = useState<RentalCredentials | null>(null);
  const [credentialsLoading, setCredentialsLoading] = useState(false);
  const [credentialsError, setCredentialsError] = useState<string | null>(null);
  const [rentalsRefreshing, setRentalsRefreshing] = useState(false);
  const [balance, setBalance] = useState<FinancialBalance | null>(null);
  const [balanceLoading, setBalanceLoading] = useState(false);
  const [walletPaymentLoadingRentalId, setWalletPaymentLoadingRentalId] = useState<number | null>(null);
  const [walletPaymentError, setWalletPaymentError] = useState<{ rentalId: number; message: string } | null>(null);
  const rentalRefreshRef = useRef<Promise<void> | null>(null);

  useEffect(() => {
    window.location.hash = view;
  }, [view]);

  useEffect(() => {
    void bootstrap();
  }, []);

  useEffect(() => {
    if (!toast) return;
    const id = window.setTimeout(() => setToast(null), 3200);
    return () => window.clearTimeout(id);
  }, [toast]);

  async function bootstrap() {
    setLoading(true);
    setBackendError(null);

    try {
      const [accountsRes] = await Promise.all([api.accounts({ page: 1, page_size: 40 }), api.games({ page: 1, page_size: 12 })]);
      setAccounts(asList(accountsRes.accounts).map(normalizeAccount));
    } catch (error) {
      if (isUnauthorized(error)) {
        setBalanceLoading(false);
        setAdminRentalsLoading(false);
        handleAuthFailure();
        return;
      }
      setAccounts([]);
      setBackendError(error instanceof Error ? error.message : "Не удалось получить публичные данные");
    } finally {
      if (getAccessToken()) {
        await loadPrivateData();
      }
      setLoading(false);
    }
  }

  function clearPrivateState() {
    setUser(null);
    setRentals([]);
    setPayments([]);
    setBalance(null);
    setNotifications([]);
    setAuditLogs([]);
    setAdminRentals([]);
    setAdminRentalsSummary(null);
    setAdminRentalsPagination(null);
    setAdminRentalsPage(1);
    setAdminRentalFilters(DEFAULT_ADMIN_RENTAL_FILTERS);
    setAdminRentalsLoading(false);
    setAdminRentalsError(null);
    setAdminRefundReasonCodes([]);
    setAdminUsers([]);
    setSelectedRentalId(null);
    setCredentials(null);
    setCredentialsError(null);
    setCredentialsLoading(false);
    setBalanceLoading(false);
    setWalletPaymentLoadingRentalId(null);
    setWalletPaymentError(null);
  }

  function handleAuthFailure() {
    clearTokens();
    clearPrivateState();
    setAuthOpen(true);
    setView("catalog");
    setToast({ type: "error", message: "Session expired. Sign in again." });
  }

  async function refreshRentalData(options?: { silent?: boolean }) {
    if (!getAccessToken()) return;
    if (rentalRefreshRef.current) {
      return rentalRefreshRef.current;
    }
    if (!options?.silent) {
      setRentalsRefreshing(true);
      if (user?.role !== "ADMIN") {
        setBalanceLoading(true);
      }
    }

    const job = (async () => {
      try {
        const [rentalsRes, paymentsRes, balanceRes] = await Promise.all([
          api.rentals(),
          api.payments(),
          user?.role === "ADMIN" ? Promise.resolve<FinancialBalance | null>(null) : api.myBalance()
        ]);
        setRentals(asList(rentalsRes.rentals));
        setPayments(asList(paymentsRes.payments));
        if (balanceRes) {
          setBalance(balanceRes);
        }
      } catch (error) {
        if (isUnauthorized(error)) {
          handleAuthFailure();
          return;
        }
        if (!options?.silent) {
          setToast({ type: "error", message: messageForApiError(error, "Failed to refresh rental status") });
        }
      } finally {
        rentalRefreshRef.current = null;
        if (!options?.silent) {
          setRentalsRefreshing(false);
          if (user?.role !== "ADMIN") {
            setBalanceLoading(false);
          }
        }
      }
    })();

    rentalRefreshRef.current = job;
    return job;
  }

  async function loadPrivateData() {
    setBackendError(null);
    setBalanceLoading(true);
    setAdminRentalsLoading(true);
    setAdminRentalsError(null);

    try {
      const [meRes, rentalsRes, paymentsRes, notificationsRes, balanceRes] = await Promise.all([
        api.me(),
        api.rentals(),
        api.payments(),
        api.notifications(),
        api.myBalance().catch(() => null)
      ]);
      setUser(meRes);
      setRentals(asList(rentalsRes.rentals));
      setPayments(asList(paymentsRes.payments));
      setNotifications(asList(notificationsRes.notifications));
      setBalance(meRes.role === "ADMIN" ? null : balanceRes);
      setWalletPaymentError(null);

      if (meRes.role === "ADMIN") {
        setView("admin");
        const [adminAccountsRes, adminRentalsRes, usersRes, auditRes, publicAccountsRes, refundReasonCodesRes] = await Promise.all([
          api.adminAccounts(),
          api.adminRentals(buildAdminRentalsQuery(adminRentalsPage, adminRentalFilters)),
          api.adminUsers(),
          api.adminAuditLogs(),
          api.accounts({ page: 1, page_size: 40 }),
          api.adminRefundReasonCodes()
        ]);

        setAdminRentals(asList(adminRentalsRes.rentals));
        setAdminRentalsSummary(adminRentalsRes.summary);
        setAdminRentalsPagination(adminRentalsRes.pagination);
        setAdminRentalsPage(adminRentalsRes.pagination.page);
        setAdminRefundReasonCodes(asList(refundReasonCodesRes.reason_codes));
        setAdminUsers(asList(usersRes.users));
        setAuditLogs(asList(auditRes.audit_logs));

        const publicAccounts = new Map(asList(publicAccountsRes.accounts).map((account) => [account.id, normalizeAccount(account)]));
        setAccounts(
          asList(adminAccountsRes.accounts).map((account) => {
            const existing = publicAccounts.get(account.id);
            return normalizeAccount({
              id: account.id,
              steam_id64: account.steam_id64,
              status: statusFromNumber(account.status),
              price_per_hour: { amount: account.hourly_price, currency: "USD" },
              security_deposit: { amount: account.deposit_amount, currency: "USD" },
              games: existing?.games ?? []
            });
          })
        );
      }
      setBalanceLoading(false);
      setAdminRentalsLoading(false);
    } catch (error) {
      if (isUnauthorized(error)) {
        setBalanceLoading(false);
        handleAuthFailure();
        return;
      }
      setRentals([]);
      setPayments([]);
      setNotifications([]);
      setAuditLogs([]);
      setAdminRentals([]);
      setAdminRentalsSummary(null);
      setAdminRentalsPagination(null);
      setAdminRentalsError(null);
      setAdminRefundReasonCodes([]);
      setAdminUsers([]);
      setBalance(null);
      setBalanceLoading(false);
      setAdminRentalsLoading(false);
      setBackendError(error instanceof Error ? error.message : "Не удалось получить приватные данные");
    }
  }

  const filteredAccounts = useMemo(() => {
    const q = search.trim().toLowerCase();

    return accounts.filter((account) => {
      const matchesSearch =
        !q || account.steam_id64.includes(q) || asList(account.games).some((game) => game.name.toLowerCase().includes(q));
      const matchesStatus = status === "All" || account.status === status;
      const matchesPrice = account.price_per_hour.amount <= maxPrice;
      return matchesSearch && matchesStatus && matchesPrice;
    });
  }, [accounts, maxPrice, search, status]);

  const activeRental =
    rentals.find((item) => item.status === RENTAL_STATUS_ACTIVE) ??
    rentals.find((item) => item.status === RENTAL_STATUS_WAITING_PAYMENT) ??
    rentals[0];

  const selectedRental =
    rentals.find((item) => item.id === selectedRentalId) ??
    rentals.find((item) => item.status === RENTAL_STATUS_WAITING_PAYMENT) ??
    rentals.find((item) => item.status === RENTAL_STATUS_ACTIVE) ??
    rentals[0];

  const selectedRentalPayment = selectedRental ? findPaymentForRental(payments, selectedRental.id) : undefined;
  const hasWaitingPaymentRental = rentals.some((item) => item.status === RENTAL_STATUS_WAITING_PAYMENT);
  const adminMode = user?.role === "ADMIN";

  useEffect(() => {
    if (!rentals.length) {
      setSelectedRentalId(null);
      return;
    }
    if (selectedRentalId && rentals.some((item) => item.id === selectedRentalId)) {
      return;
    }

    const nextRental =
      rentals.find((item) => item.status === RENTAL_STATUS_WAITING_PAYMENT) ??
      rentals.find((item) => item.status === RENTAL_STATUS_ACTIVE) ??
      rentals[0];
    setSelectedRentalId(nextRental.id);
  }, [rentals, selectedRentalId]);

  useEffect(() => {
    setCredentials(null);
    setCredentialsError(null);
    setWalletPaymentError(null);
  }, [selectedRentalId]);

  useEffect(() => {
    if (!selectedRental || !canRequestCredentials(selectedRental, selectedRentalPayment)) {
      setCredentials(null);
      setCredentialsError(null);
    }
  }, [selectedRental, selectedRentalPayment]);

  useEffect(() => {
    if (!user || adminMode || !hasWaitingPaymentRental) {
      return;
    }
    const id = window.setInterval(() => {
      void refreshRentalData({ silent: true });
    }, RENTAL_POLL_INTERVAL_MS);
    return () => window.clearInterval(id);
  }, [adminMode, hasWaitingPaymentRental, user]);

  async function handleRent(account: Account) {
    if (!getAccessToken()) {
      setSelectedAccount(account);
      setAuthOpen(true);
      setToast({ type: "error", message: "Войдите, чтобы начать аренду" });
      return;
    }

    setLoading(true);
    try {
      const rental = await api.createRental({ account_id: account.id, duration_hours: duration });
      setRentals((current) => [rental, ...current]);
      setSelectedRentalId(rental.id);
      setCredentials(null);
      setCredentialsError(null);
      setAccounts((current) => current.map((item) => (item.id === account.id ? { ...item, status: "Reserved" } : item)));
      setSelectedAccount(null);
      setView("rentals");
      await refreshRentalData({ silent: true });
      setToast({ type: "ok", message: "Аренда создана" });
    } catch (error) {
      setToast({ type: "error", message: error instanceof Error ? error.message : "Не удалось создать аренду" });
    } finally {
      setLoading(false);
    }
  }

  async function handleExtend(rental: Rental) {
    try {
      await api.extendRental(rental.id, 1);
      setRentals((current) =>
        current.map((item) =>
          item.id === rental.id ? { ...item, expires_at: new Date(new Date(item.expires_at).getTime() + 3600000).toISOString() } : item
        )
      );
      setToast({ type: "ok", message: "Аренда продлена на 1 час" });
    } catch (error) {
      setToast({ type: "error", message: error instanceof Error ? error.message : "Не удалось продлить аренду" });
    }
  }

  async function handleCancel(rental: Rental) {
    try {
      await api.cancelRental(rental.id);
      setRentals((current) => current.map((item) => (item.id === rental.id ? { ...item, status: RENTAL_STATUS_CANCELLED } : item)));
      if (selectedRentalId === rental.id) {
        setCredentials(null);
        setCredentialsError(null);
      }
      setToast({ type: "ok", message: "Аренда отменена" });
    } catch (error) {
      setToast({ type: "error", message: error instanceof Error ? error.message : "Не удалось отменить аренду" });
    }
  }

  async function handleLoadCredentials(rental: Rental) {
    const payment = findPaymentForRental(payments, rental.id);
    if (!canRequestCredentials(rental, payment)) {
      setCredentials(null);
      setCredentialsError("Credentials are unavailable until the rental is active and paid.");
      return;
    }

    setSelectedRentalId(rental.id);
    setCredentialsLoading(true);
    setCredentialsError(null);
    try {
      const nextCredentials = await api.rentalCredentials(rental.id);
      setCredentials(nextCredentials);
    } catch (error) {
      if (isUnauthorized(error)) {
        handleAuthFailure();
        return;
      }
      setCredentials(null);
      setCredentialsError(messageForApiError(error, "Failed to load credentials"));
    } finally {
      setCredentialsLoading(false);
    }
  }

  async function handlePayWithBalance(rental: Rental) {
    if (walletPaymentLoadingRentalId === rental.id) {
      return;
    }

    setSelectedRentalId(rental.id);
    setWalletPaymentLoadingRentalId(rental.id);
    setWalletPaymentError(null);

    try {
      const result = await api.payRentalWithBalance(rental.id);
      setCredentials(null);
      setCredentialsError(null);
      setAccounts((current) =>
        current.map((item) => (item.id === result.account_id ? { ...item, status: statusFromNumber(result.account_status) } : item))
      );
      await refreshRentalData({ silent: true });
      setToast({
        type: "ok",
        message: result.idempotent ? "Оплата уже подтверждена. Статус аренды обновлён." : "Аренда оплачена с внутреннего баланса."
      });
    } catch (error) {
      if (isUnauthorized(error)) {
        handleAuthFailure();
        return;
      }

      const message = messageForWalletPaymentError(error, "Не удалось оплатить с баланса. Проверьте соединение и повторите попытку.");
      setWalletPaymentError({ rentalId: rental.id, message });

      if (message.includes("Статус оплаты уже изменился") || message.includes("Недостаточно средств")) {
        await refreshRentalData({ silent: true });
      }
    } finally {
      setWalletPaymentLoadingRentalId((current) => (current === rental.id ? null : current));
    }
  }

  async function handleFavorite(account: Account) {
    if (!getAccessToken()) {
      setAuthOpen(true);
      setToast({ type: "error", message: "Войдите, чтобы добавить аккаунт в избранное" });
      return;
    }

    try {
      await api.favoriteAccount(account.id);
      setToast({ type: "ok", message: "Аккаунт добавлен в избранное" });
    } catch (error) {
      setToast({ type: "error", message: error instanceof Error ? error.message : "Не удалось добавить в избранное" });
    }
  }

  async function handleReadNotification(item: NotificationItem) {
    try {
      await api.readNotification(item.id);
      setNotifications((current) => current.map((notification) => (notification.id === item.id ? { ...notification, read: true } : notification)));
    } catch (error) {
      setToast({ type: "error", message: error instanceof Error ? error.message : "Не удалось отметить уведомление" });
    }
  }

  async function handleToggleAccount(account: Account) {
    const enable = account.status === "Disabled";
    const nextStatus = enable ? 2 : 6;
    try {
      await api.adminUpdateAccount(account.id, { status: nextStatus });
      setAccounts((current) => current.map((item) => (item.id === account.id ? { ...item, status: statusFromNumber(nextStatus) } : item)));
      setToast({ type: "ok", message: enable ? "Аккаунт включен" : "Аккаунт отключен" });
    } catch (error) {
      setToast({ type: "error", message: error instanceof Error ? error.message : "Не удалось изменить статус аккаунта" });
      throw error;
    }
  }

  async function handleSyncAccount(account: Account) {
    setLoading(true);
    try {
      const result = await api.adminSyncAccount(account.id);
      await loadPrivateData();
      setAuditLogs((current) => [
        {
          id: Date.now(),
          actor_user_id: user?.id ?? null,
          entity_type: "account",
          entity_id: account.id,
          action: "ADMIN_SYNC_ACCOUNT",
          created_at: new Date().toISOString()
        },
        ...current
      ]);
      setToast({ type: "ok", message: `Библиотека Steam синхронизирована: ${result.games_count} игр` });
    } catch (error) {
      setToast({ type: "error", message: error instanceof Error ? error.message : "Не удалось синхронизировать" });
      throw error;
    } finally {
      setLoading(false);
    }
  }

  async function handleCreateAccount(payload: {
    steam_id64: string;
    steam_login: string;
    steam_password: string;
    price_per_hour: number;
    security_deposit: number;
  }) {
    setLoading(true);
    try {
      const created = await api.adminCreateAccount(payload);
      const account: Account = {
        id: created.id,
        steam_id64: payload.steam_id64,
        status: "Available",
        price_per_hour: { amount: payload.price_per_hour, currency: "USD" },
        security_deposit: { amount: payload.security_deposit, currency: "USD" },
        games: []
      };
      setAccounts((current) => [account, ...current]);
      setToast({
        type: created.sync_error ? "error" : "ok",
        message: created.sync_error
          ? `Аккаунт добавлен, но Steam sync не завершился: ${created.sync_error}`
          : `Аккаунт добавлен, импортировано игр: ${created.games_count ?? 0}`
      });
      await loadPrivateData();
    } catch (error) {
      setToast({ type: "error", message: error instanceof Error ? error.message : "Не удалось добавить аккаунт" });
      throw error;
    } finally {
      setLoading(false);
    }
  }

  async function handleUpdateAccount(account: Account, patch: AdminAccountPatch) {
    setLoading(true);
    try {
      await api.adminUpdateAccount(account.id, patch);
      setAccounts((current) =>
        current.map((item) =>
          item.id === account.id
            ? {
                ...item,
                status: patch.status === undefined ? item.status : statusFromNumber(patch.status),
                price_per_hour:
                  patch.price_per_hour === undefined ? item.price_per_hour : { ...item.price_per_hour, amount: patch.price_per_hour },
                security_deposit:
                  patch.security_deposit === undefined ? item.security_deposit : { ...item.security_deposit, amount: patch.security_deposit }
              }
            : item
        )
      );
      setToast({ type: "ok", message: "Аккаунт обновлен" });
      await loadPrivateData();
    } catch (error) {
      setToast({ type: "error", message: error instanceof Error ? error.message : "Не удалось обновить аккаунт" });
      throw error;
    } finally {
      setLoading(false);
    }
  }

  async function handleUpdateAdminUser(targetUser: User, patch: { trust_score?: number; is_blocked?: boolean; balance?: number; role?: string }) {
    setLoading(true);
    try {
      await api.adminUpdateUser(targetUser.id, patch);
      setAdminUsers((current) => current.map((item) => (item.id === targetUser.id ? { ...item, ...patch } : item)));
      if (user?.id === targetUser.id) {
        setUser((current) => (current ? { ...current, ...patch } : current));
      }
      setToast({ type: "ok", message: "Пользователь обновлен" });
      await loadPrivateData();
    } catch (error) {
      setToast({ type: "error", message: error instanceof Error ? error.message : "Не удалось обновить пользователя" });
      throw error;
    } finally {
      setLoading(false);
    }
  }

  function buildAdminRentalsQuery(page: number, filters: AdminRentalFilters) {
    return {
      page,
      page_size: 20,
      rental_status: filters.rental_status || undefined,
      payment_status: filters.payment_status || undefined,
      payment_provider: filters.payment_provider || undefined,
      deposit_status: filters.deposit_status || undefined,
      refund_status: filters.refund_status || undefined,
      eligible_wallet_refund: filters.eligible_wallet_refund,
      user_id: filters.user_id,
      rental_id: filters.rental_id
    };
  }

  async function refreshAdminRentals(page = adminRentalsPage, filters = adminRentalFilters) {
    setAdminRentalsLoading(true);
    setAdminRentalsError(null);
    try {
      const result = await api.adminRentals(buildAdminRentalsQuery(page, filters));
      setAdminRentals(asList(result.rentals));
      setAdminRentalsSummary(result.summary);
      setAdminRentalsPagination(result.pagination);
      setAdminRentalsPage(result.pagination.page);
    } catch (error) {
      if (isUnauthorized(error)) {
        handleAuthFailure();
        return;
      }
      const message = messageForApiError(error, "Failed to refresh admin refund data");
      setAdminRentalsError(message);
      setToast({ type: "error", message });
      throw error;
    } finally {
      setAdminRentalsLoading(false);
    }
  }

  async function handleAdminRentalFiltersChange(nextFilters: AdminRentalFilters) {
    setAdminRentalFilters(nextFilters);
    setAdminRentalsPage(1);
    await refreshAdminRentals(1, nextFilters);
  }

  async function handleAdminRentalFiltersReset() {
    setAdminRentalFilters(DEFAULT_ADMIN_RENTAL_FILTERS);
    setAdminRentalsPage(1);
    await refreshAdminRentals(1, DEFAULT_ADMIN_RENTAL_FILTERS);
  }

  async function handleAdminWalletRefund(rentalId: number, reasonCode: string): Promise<AdminWalletRefundResponse> {
    try {
      const result = await api.adminWalletRefund(rentalId, reasonCode);
      await refreshAdminRentals();
      return result;
    } catch (error) {
      if (isUnauthorized(error)) {
        handleAuthFailure();
      } else if (isApiError(error) && (error.status === 404 || error.status === 409)) {
        await refreshAdminRentals().catch(() => undefined);
      }
      throw error;
    }
  }

  async function handleLogout() {
    const refresh = getRefreshToken();
    clearTokens();
    clearPrivateState();
    setView("catalog");
    if (refresh) {
      void api.logout(refresh).catch(() => undefined);
    }
    setToast({ type: "ok", message: "Сессия завершена" });
  }

  const effectiveView = adminMode ? "admin" : view;

  return (
    <div className="app-shell">
      <AppHeader adminMode={adminMode} onLogin={() => setAuthOpen(true)} onLogout={handleLogout} setView={setView} user={user} view={effectiveView} />

      {backendError && (
        <div className="offline-banner">
          <DatabaseZap size={18} />
          <span>{backendError}</span>
        </div>
      )}

      <main>
        {effectiveView === "catalog" && (
          <CatalogView
            accounts={filteredAccounts}
            activeRental={activeRental}
            duration={duration}
            loading={loading}
            maxPrice={maxPrice}
            onExtendActive={handleExtend}
            onOpenRentals={() => setView("rentals")}
            search={search}
            selectAccount={setSelectedAccount}
            setDuration={setDuration}
            setMaxPrice={setMaxPrice}
            setSearch={setSearch}
            setStatus={setStatus}
            status={status}
          />
        )}
        {effectiveView === "rentals" && (
          <RentalsView
            accounts={accounts}
            balance={balance}
            balanceLoading={balanceLoading}
            credentials={credentials}
            credentialsError={credentialsError}
            credentialsLoading={credentialsLoading}
            onCancel={handleCancel}
            onExtend={handleExtend}
            onLoadCredentials={handleLoadCredentials}
            onPayWithBalance={handlePayWithBalance}
            onRefreshStatus={() => refreshRentalData()}
            onSelectRental={setSelectedRentalId}
            payments={payments}
            rentals={rentals}
            rentalsRefreshing={rentalsRefreshing}
            selectedRentalId={selectedRental?.id ?? null}
            walletPaymentError={walletPaymentError}
            walletPaymentLoadingRentalId={walletPaymentLoadingRentalId}
          />
        )}
        {effectiveView === "payments" && (
          <PaymentsView notifications={notifications} onReadNotification={handleReadNotification} payments={payments} />
        )}
        {effectiveView === "profile" && <ProfileView onLogin={() => setAuthOpen(true)} onUpdateUser={(next) => setUser(next)} user={user} />}
        {effectiveView === "admin" && (
          <AdminView
            accounts={accounts}
            adminRentals={adminRentals}
            adminRentalFilters={adminRentalFilters}
            adminRentalsError={adminRentalsError}
            adminRentalsLoading={adminRentalsLoading}
            adminRentalsPagination={adminRentalsPagination}
            adminRentalsSummary={adminRentalsSummary}
            refundReasonOptions={adminRefundReasonCodes}
            auditLogs={auditLogs}
            onAdminRentalFiltersChange={handleAdminRentalFiltersChange}
            onAdminRentalFiltersReset={handleAdminRentalFiltersReset}
            onCreateAccount={handleCreateAccount}
            onNextRefundPage={() => {
              if (!adminRentalsPagination || adminRentalsPage >= adminRentalsPagination.total_pages) {
                return Promise.resolve();
              }
              return refreshAdminRentals(adminRentalsPage + 1);
            }}
            onPrevRefundPage={() => {
              if (adminRentalsPage <= 1) {
                return Promise.resolve();
              }
              return refreshAdminRentals(adminRentalsPage - 1);
            }}
            onWalletRefund={handleAdminWalletRefund}
            onSync={handleSyncAccount}
            onToggleAccount={handleToggleAccount}
            onUpdateAccount={handleUpdateAccount}
            onUpdateUser={handleUpdateAdminUser}
            user={user}
            users={adminUsers}
          />
        )}
      </main>

      {!adminMode && <MobileNav setView={setView} view={effectiveView} />}

      {selectedAccount && (
        <CheckoutDrawer
          account={selectedAccount}
          duration={duration}
          onClose={() => setSelectedAccount(null)}
          onFavorite={() => handleFavorite(selectedAccount)}
          onRent={() => handleRent(selectedAccount)}
          setDuration={setDuration}
        />
      )}

      {authOpen && (
        <AuthDialog
          onAuthenticated={(nextUser) => {
            setUser(nextUser);
            setAuthOpen(false);
            setView(nextUser.role === "ADMIN" ? "admin" : "rentals");
            void loadPrivateData();
          }}
          onClose={() => setAuthOpen(false)}
          setToast={setToast}
        />
      )}

      {toast && <div className={`toast ${toast.type}`}>{toast.message}</div>}
    </div>
  );
}
