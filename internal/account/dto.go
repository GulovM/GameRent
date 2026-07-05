package account

type MoneyResponse struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

type AccountGameResponse struct {
	GameID     int64  `json:"game_id"`
	Name       string `json:"name"`
	SteamAppID int    `json:"steam_app_id"`
	Playtime   int    `json:"playtime_minutes"`
}

type AccountResponse struct {
	ID              int64                 `json:"id"`
	SteamID64       string                `json:"steam_id64"`
	Status          string                `json:"status"`
	PricePerHour    MoneyResponse         `json:"price_per_hour"`
	SecurityDeposit MoneyResponse         `json:"security_deposit"`
	Games           []AccountGameResponse `json:"games"`
}

type PaginationInfo struct {
	Page       int   `json:"page"`
	PageSize   int   `json:"page_size"`
	TotalItems int64 `json:"total_items"`
	TotalPages int   `json:"total_pages"`
}

type PaginatedAccountsResponse struct {
	Accounts   []AccountResponse `json:"accounts"`
	Pagination PaginationInfo    `json:"pagination"`
}
