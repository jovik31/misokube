package main

import (
	"log"
	"tenant-api/server"
)

func main() {
	r := server.SetupRouter()

	log.Println("Starting tenant API on :8080")
	r.Run(":8080")
}
