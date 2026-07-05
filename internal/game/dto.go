package game

type GameResponse struct {
	ID          int64  `json:"id"`
	SteamAppID  int    `json:"steam_app_id"`
	Name        string `json:"name"`
	HeaderImage string `json:"header_image"`
}

type PaginationInfo struct {
	Page       int   `json:"page"`
	PageSize   int   `json:"page_size"`
	TotalItems int64 `json:"total_items"`
	TotalPages int   `json:"total_pages"`
}

type CatalogResponse struct {
	Games      []GameResponse `json:"games"`
	Pagination PaginationInfo `json:"pagination"`
}
