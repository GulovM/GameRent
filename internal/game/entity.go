package game

import "time"

type Game struct {
	ID               int64      `json:"id"`
	SteamAppID       int        `json:"steam_app_id"`
	Name             string     `json:"name"`
	ShortDescription string     `json:"short_description"`
	HeaderImage      string     `json:"header_image"`
	ReleaseDate      *time.Time `json:"release_date"`
	Developers       []string   `json:"developers"`
	Publishers       []string   `json:"publishers"`
	Genres           []string   `json:"genres"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}
