package main

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

func main() {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	// Get configuration from environment
	rpcURL := os.Getenv("RPC_URL")
	if rpcURL == "" {
		panic("RPC_URL environment variable not set")
	}
	network := os.Getenv("NETWORK")
	if network == "" {
		network = "localhost"
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "4020"
	}
}
