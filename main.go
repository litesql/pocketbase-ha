package main

import (
	"log"

	"github.com/litesql/pocketbase-ha/internal/config"
	"github.com/litesql/pocketbase-ha/internal/server"
)

func main() {
	cfg, err := config.FromEnv()
	if err != nil {
		log.Fatal(err)
	}

	app, err := server.New(cfg)
	if err != nil {
		log.Fatal(err)
	}

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
