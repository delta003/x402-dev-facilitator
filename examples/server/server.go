package main

import (
	x402types "github.com/coinbase/x402/go/pkg/types"
	"github.com/delta003/x402-dev-facilitator/core"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"log"
	"math/big"
	"os"
)

func main() {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	r := gin.Default()

	// Add detailed logging middleware
	r.Use(core.DetailedLoggingMiddleware())
	r.Use(gin.Recovery())

	// Get URL and port from environment variables.
	facilitatorURL := os.Getenv("FACILITATOR_URL")
	if facilitatorURL == "" {
		log.Fatal("FACILITATOR_URL environment variable is not set")
	}
	port := os.Getenv("SERVER_PORT")
	if port == "" {
		port = "4021" // Default port if not set
	}
	walletAddress := os.Getenv("SERVER_WALLET_ADDRESS")
	if walletAddress == "" {
		log.Fatal("SERVER_WALLET_ADDRESS environment variable is not set")
	}

	facilitatorConfig := &x402types.FacilitatorConfig{
		URL: facilitatorURL,
	}

	r.GET(
		"/tip",
		core.PaymentMiddleware(
			big.NewFloat(42.0),
			walletAddress,
			core.WithFacilitatorConfig(facilitatorConfig),
			core.WithTestnet(false),
			// NOTE(marko): This is weird. How is this used?
			core.WithResource("http://localhost:"+port+"/tip"),
		),
		func(c *gin.Context) {
			// NOTE(marko): This is executed even if settlement fails.
			c.JSON(200, gin.H{
				"msg": "Thanks!",
			})
		},
	)

	err := r.Run(":" + port)
	if err != nil {
		log.Fatal(err)
	}
}
