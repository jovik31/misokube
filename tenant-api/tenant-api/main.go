package main

import (
	"log"
	"os"
	"tenant-api/server"
)

func main() {
	token := os.Getenv("TENANT_API_TOKEN")
	if token == "" {
		log.Fatal("TENANT_API_TOKEN must be set")
	}
	r := server.SetupRouter(token)

	log.Println("Starting tenant API on :8080")
	if err := r.Run(":8080"); err != nil {
		log.Fatal(err)
	}
}
