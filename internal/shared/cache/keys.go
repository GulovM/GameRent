package cache

import "fmt"

func GameKey(gameID string) string {
	return fmt.Sprintf("game:%s", gameID)
}

func PopularGamesKey() string {
	return "games:popular"
}

func AccountKey(accountID string) string {
	return fmt.Sprintf("account:%s", accountID)
}

func AccountProfileKey(accountID string) string {
	return fmt.Sprintf("account:profile:%s", accountID)
}

func UserProfileKey(userID string) string {
	return fmt.Sprintf("user:profile:%s", userID)
}
