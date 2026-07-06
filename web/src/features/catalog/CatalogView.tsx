import { Check, ChevronRight, Clock3, Gamepad2, LibraryBig, Lock, Search, ShieldCheck, SlidersHorizontal, X, Zap } from "lucide-react";
import type { Account, Rental } from "../../api";
import { accountStatusLabels, asList, gameNames } from "../../utils/accounts";
import { money, remaining } from "../../utils/format";
import { rentalStatusLabels } from "../rentals/rentalStatus";

type CatalogViewProps = {
  accounts: Account[];
  activeRental?: Rental;
  duration: number;
  loading: boolean;
  maxPrice: number;
  onExtendActive: (rental: Rental) => void;
  onOpenRentals: () => void;
  search: string;
  selectAccount: (account: Account) => void;
  setDuration: (value: number) => void;
  setMaxPrice: (value: number) => void;
  setSearch: (value: string) => void;
  setStatus: (value: string) => void;
  status: string;
};

type CheckoutDrawerProps = {
  account: Account;
  duration: number;
  onClose: () => void;
  onFavorite: () => void;
  onRent: () => void;
  setDuration: (value: number) => void;
};

export function CatalogView({
  accounts,
  activeRental,
  duration,
  loading,
  maxPrice,
  onExtendActive,
  onOpenRentals,
  search,
  selectAccount,
  setDuration,
  setMaxPrice,
  setSearch,
  setStatus,
  status
}: CatalogViewProps) {
  return (
    <>
      <section className="catalog-hero">
        <div className="hero-copy">
          <span className="eyebrow">Steam rental platform</span>
          <h1>Каталог игровых аккаунтов с быстрым доступом и контролем аренды</h1>
          <p>
            Выбирайте аккаунт по библиотеке, стоимости и статусу. Активная аренда, платежи и уведомления остаются в
            личном кабинете.
          </p>
          <div className="hero-actions">
            <a className="primary-button" href="#catalog">
              Открыть каталог
              <ChevronRight size={18} />
            </a>
            <button className="secondary-button" onClick={onOpenRentals} type="button">
              <Clock3 size={18} />
              Мои аренды
            </button>
          </div>
        </div>
        <LiveRentalCard rental={activeRental} onExtend={onExtendActive} onOpenRentals={onOpenRentals} />
      </section>

      <section className="catalog-section" id="catalog">
        <div className="section-heading">
          <div>
            <h2>Каталог аккаунтов</h2>
            <p>Поиск по SteamID и библиотеке, фильтр по статусу и бюджету.</p>
          </div>
          <DurationPicker duration={duration} setDuration={setDuration} />
        </div>

        <div className="filters">
          <label className="search-box">
            <Search size={20} />
            <input onChange={(event) => setSearch(event.target.value)} placeholder="Игра или SteamID" value={search} />
          </label>
          <div className="chip-row">
            {["All", "Available", "Reserved", "Rented", "Maintenance", "Disabled"].map((item) => (
              <button className={status === item ? "selected" : ""} key={item} onClick={() => setStatus(item)} type="button">
                {item === "All" ? "Все" : accountStatusLabels[item]}
              </button>
            ))}
          </div>
          <label className="range-control">
            <SlidersHorizontal size={18} />
            до {maxPrice} USD/ч
            <input max="500" min="10" onChange={(event) => setMaxPrice(Number(event.target.value))} step="10" type="range" value={maxPrice} />
          </label>
        </div>

        <div className={loading ? "account-grid loading" : "account-grid"}>
          {accounts.length > 0 ? (
            accounts.map((account) => <AccountCard account={account} key={account.id} onSelect={selectAccount} />)
          ) : (
            <div className="empty-inline">
              <LibraryBig size={28} />
              <strong>Аккаунтов по фильтрам нет</strong>
              <span>Измените запрос, статус или максимальную цену.</span>
            </div>
          )}
        </div>
      </section>
    </>
  );
}

export function CheckoutDrawer({ account, duration, onClose, onFavorite, onRent, setDuration }: CheckoutDrawerProps) {
  const total = account.price_per_hour.amount * duration + account.security_deposit.amount;
  return (
    <div className="drawer-backdrop" role="presentation">
      <aside className="checkout-drawer" aria-label="Оформление аренды">
        <button className="ghost icon-button close-button" onClick={onClose} title="Закрыть" type="button">
          <X size={20} />
        </button>
        <div className="drawer-title">
          <div>
            <span className="eyebrow">Checkout</span>
            <h2>Оформление аренды</h2>
            <p>{gameNames(account, 5)}</p>
          </div>
          <span className={account.status === "Available" ? "status-pill green" : "status-pill muted"}>
            {accountStatusLabels[account.status] ?? account.status}
          </span>
        </div>
        <DurationPicker duration={duration} setDuration={setDuration} />
        <dl className="summary-list">
          <div>
            <dt>Цена за час</dt>
            <dd>{money(account.price_per_hour)}</dd>
          </div>
          <div>
            <dt>Период</dt>
            <dd>{duration} ч</dd>
          </div>
          <div>
            <dt>Залог</dt>
            <dd>{money(account.security_deposit)}</dd>
          </div>
          <div>
            <dt>Итого</dt>
            <dd>
              {total} {account.price_per_hour.currency}
            </dd>
          </div>
        </dl>
        <div className="safety-note">
          <Lock size={20} />
          <span>Доступ к Steam-данным выдается только на время активной аренды.</span>
        </div>
        <button className="primary-button full" disabled={account.status !== "Available"} onClick={onRent} type="button">
          Оплатить и начать
        </button>
        <button className="secondary-button full" onClick={onFavorite} type="button">
          <Check size={18} />В избранное
        </button>
      </aside>
    </div>
  );
}

function DurationPicker({ duration, setDuration }: { duration: number; setDuration: (value: number) => void }) {
  return (
    <div className="duration-picker" aria-label="Длительность аренды">
      {[1, 2, 4, 8].map((hours) => (
        <button className={duration === hours ? "selected" : ""} key={hours} onClick={() => setDuration(hours)} type="button">
          {hours} ч
        </button>
      ))}
    </div>
  );
}

function LiveRentalCard({
  onExtend,
  onOpenRentals,
  rental
}: {
  onExtend: (rental: Rental) => void;
  onOpenRentals: () => void;
  rental?: Rental;
}) {
  return (
    <aside className="live-card">
      <div className="live-top">
        <div>
          <span className="eyebrow">Активная сессия</span>
          <h2>{rental ? `Аренда #${rental.id}` : "Нет активной аренды"}</h2>
        </div>
        <span className={rental?.status === 2 ? "status-pill green" : "status-pill muted"}>
          {rental ? rentalStatusLabels[rental.status] ?? rental.status : "Пусто"}
        </span>
      </div>
      <strong className="timer">{rental ? remaining(rental.expires_at) : "00:00:00"}</strong>
      <div className="live-actions">
        <button className="primary-button" disabled={!rental || rental.status !== 2} onClick={() => rental && onExtend(rental)} type="button">
          <Zap size={18} />
          Продлить
        </button>
        <button className="secondary-button" onClick={onOpenRentals} type="button">
          <ShieldCheck size={18} />
          Детали
        </button>
      </div>
    </aside>
  );
}

function AccountCard({ account, onSelect }: { account: Account; onSelect: (account: Account) => void }) {
  const available = account.status === "Available";
  const games = asList(account.games);
  const firstGame = games[0];
  const visibleGames = games.slice(0, 8);
  const hiddenGamesCount = Math.max(games.length - visibleGames.length, 0);
  const statusClass = available ? "green" : account.status === "Rented" ? "amber" : account.status === "Disabled" ? "danger" : "muted";

  return (
    <article className="account-card">
      <button className="card-button" onClick={() => onSelect(account)} type="button">
        <div className="account-cover">
          <span>{firstGame?.name || "Steam Account"}</span>
          <Gamepad2 size={34} />
        </div>
        <div className="card-body">
          <div>
            <h3>{firstGame?.name ? `${firstGame.name} Pack` : `Account #${account.id}`}</h3>
            <p>{gameNames(account)}</p>
          </div>
          <div className="account-library" aria-label="Список игр аккаунта">
            <span className="account-library-title">Игры аккаунта</span>
            {visibleGames.length > 0 ? (
              <ul className="account-game-list">
                {visibleGames.map((game) => (
                  <li key={`${account.id}-${game.game_id}`}>{game.name}</li>
                ))}
                {hiddenGamesCount > 0 && <li className="more-games">+{hiddenGamesCount}</li>}
              </ul>
            ) : (
              <span className="empty-library">Библиотека не синхронизирована</span>
            )}
          </div>
          <span className={`status-pill ${statusClass}`}>{accountStatusLabels[account.status] ?? account.status}</span>
          <div className="price-row">
            <strong>{money(account.price_per_hour)}/ч</strong>
            <span>залог {money(account.security_deposit)}</span>
          </div>
        </div>
      </button>
    </article>
  );
}
