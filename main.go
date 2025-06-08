package main

import (
	"github.com/delta003/x402-dev-facilitator/core"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"log"
	"os"
)

func main() {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	// Get configuration from environment
	rpcURL := os.Getenv("RPC_URL")
	if rpcURL == "" {
		log.Fatal("RPC_URL environment variable not set")
	}
	privateKey := os.Getenv("WALLET_PRIVATE_KEY")
	if privateKey == "" {
		log.Fatal("WALLET_PRIVATE_KEY environment variable not set")
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "4020"
	}

	// Create facilitator instance
	facilitator, err := core.NewFacilitatorFromHex(privateKey, rpcURL)
	if err != nil {
		log.Fatalf("Failed to create facilitator: %v", err)
	}

	// Create Gin router
	r := gin.Default()

	// Add detailed logging middleware
	r.Use(core.DetailedLoggingMiddleware())
	r.Use(gin.Recovery())

	// Configure routes
	r.POST("/verify", facilitator.VerifyPaymentHandler)
	r.POST("/settle", facilitator.SettlePaymentHandler)
	r.GET("/health", facilitator.HealthHandler)

	err = r.Run(":" + port)
	if err != nil {
		log.Fatal(err)
	}
}
