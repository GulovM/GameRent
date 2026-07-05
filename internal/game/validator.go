package game

import "net/http"

type QueryParams struct {
	Page     int
	PageSize int
	Search   string
}

func ParseAndValidateQueryParams(r *http.Request) QueryParams {
	q := r.URL.Query()

	page := 1
	if pStr := q.Get("page"); pStr != "" {
		if p, err := parsePositiveInt(pStr); err == nil && p > 0 {
			page = p
		}
	}

	pageSize := 20
	if psStr := q.Get("page_size"); psStr != "" {
		if ps, err := parsePositiveInt(psStr); err == nil && ps > 0 {
			pageSize = ps
			if pageSize > 100 {
				pageSize = 100
			}
		}
	}

	return QueryParams{
		Page:     page,
		PageSize: pageSize,
		Search:   q.Get("search"),
	}
}

func parsePositiveInt(s string) (int, error) {
	var res int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, http.ErrLineTooLong
		}
		res = res*10 + int(c-'0')
	}
	return res, nil
}
