package account

import (
	"net/http"
	"strconv"
)

type QueryParams struct {
	Page       int
	PageSize   int
	Search     string
	Sort       string
	Order      string
	Status     string
	GameID     int64
	MinPrice   int64
	MaxPrice   int64
	TrustLevel string
}

func ParseAndValidateQueryParams(r *http.Request) QueryParams {
	q := r.URL.Query()

	page := 1
	if pStr := q.Get("page"); pStr != "" {
		if p, err := strconv.Atoi(pStr); err == nil && p > 0 {
			page = p
		}
	}

	pageSize := 20
	if psStr := q.Get("page_size"); psStr != "" {
		if ps, err := strconv.Atoi(psStr); err == nil && ps > 0 {
			pageSize = ps
			if pageSize > 100 {
				pageSize = 100
			}
		}
	}

	var gameID int64
	if gStr := q.Get("game"); gStr != "" {
		if g, err := strconv.ParseInt(gStr, 10, 64); err == nil {
			gameID = g
		}
	}

	var minPrice int64
	if minStr := q.Get("min_price"); minStr != "" {
		if min, err := strconv.ParseInt(minStr, 10, 64); err == nil {
			minPrice = min
		}
	}

	var maxPrice int64
	if maxStr := q.Get("max_price"); maxStr != "" {
		if max, err := strconv.ParseInt(maxStr, 10, 64); err == nil {
			maxPrice = max
		}
	}

	return QueryParams{
		Page:       page,
		PageSize:   pageSize,
		Search:     q.Get("search"),
		Sort:       q.Get("sort"),
		Order:      q.Get("order"),
		Status:     q.Get("status"),
		GameID:     gameID,
		MinPrice:   minPrice,
		MaxPrice:   maxPrice,
		TrustLevel: q.Get("trust_level"),
	}
}
