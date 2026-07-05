package main

import (
	"log"
	"rent_game_accs/internal/modules"

	"github.com/subosito/gotenv"
)

func main() {
	if err := gotenv.Load(); err != nil {
		log.Println(".env file was not loaded:", err)
	}

	modules.Run()
}
