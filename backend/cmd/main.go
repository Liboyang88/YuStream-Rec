package main

import (
	"log"

	"litstream/bootstrap"
)

func main() {
	app := bootstrap.NewApp(bootstrap.NewEnv())
	defer app.CloseAllDBConnection()

	if err := app.StartServer(); err != nil {
		log.Fatalf("Server can't be started: %v", err)
	}
}
