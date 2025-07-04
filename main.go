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
	privateKey := os.Getenv("FACILITATOR_WALLET_PRIVATE_KEY")
	if privateKey == "" {
		log.Fatal("FACILITATOR_WALLET_PRIVATE_KEY environment variable not set")
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
	// This is an extension to the x402 protocol. See core/extensions.go for details.
	r.POST("/verify-receipt", facilitator.VerifyReceiptHandler)
	r.POST("/settle", facilitator.SettlePaymentHandler)
	r.GET("/health", facilitator.HealthHandler)

	err = r.Run(":4020")
	if err != nil {
		log.Fatal(err)
	}
}
