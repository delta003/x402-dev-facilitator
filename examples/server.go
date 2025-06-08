package main

import (
	"bytes"
	"encoding/base64"
	"io"
	"log"
	"math/big"
	"os"
	"strings"
	"time"

	x402gin "github.com/coinbase/x402/go/pkg/gin"
	"github.com/coinbase/x402/go/pkg/types"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	r := gin.Default()

	// Add detailed logging middleware
	r.Use(detailedLoggingMiddleware())
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
	walletAddress := os.Getenv("WALLET_ADDRESS")
	if walletAddress == "" {
		log.Fatal("WALLET_ADDRESS environment variable is not set")
	}

	facilitatorConfig := &types.FacilitatorConfig{
		URL: facilitatorURL,
	}

	r.GET(
		"/tip",
		x402gin.PaymentMiddleware(
			big.NewFloat(0.0001),
			walletAddress,
			x402gin.WithFacilitatorConfig(facilitatorConfig),
			x402gin.WithResource("http://localhost:"+port+"/tip"),
		),
		func(c *gin.Context) {
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

func detailedLoggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		startTime := time.Now()

		// Log request details
		log.Printf("=== REQUEST START ===")
		log.Printf("Method: %s", c.Request.Method)
		log.Printf("URL: %s", c.Request.URL.String())
		log.Printf("Headers:")
		for name, values := range c.Request.Header {
			for _, value := range values {
				log.Printf("  %s: %s", name, value)

				// Special handling for X-Payment header
				if strings.ToLower(name) == "x-payment" {
					if decoded, err := base64.StdEncoding.DecodeString(value); err == nil {
						log.Printf("  X-Payment (decoded): %s", string(decoded))
					} else {
						// Try without base64 decoding (might be plain JSON)
						log.Printf("  X-Payment (not base64, raw): %s", value)
					}
				}
			}
		}

		// Read and log request body
		if c.Request.Body != nil {
			bodyBytes, err := io.ReadAll(c.Request.Body)
			if err == nil {
				if len(bodyBytes) > 0 {
					log.Printf("Body: %s", string(bodyBytes))
				} else {
					log.Printf("Body: (empty)")
				}
				// Restore the body for further processing
				c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			}
		}
		log.Printf("=== REQUEST END ===\n")

		// Process request
		c.Next()

		// Log response details
		duration := time.Since(startTime)
		log.Printf("=== RESPONSE ===")
		log.Printf("Status: %d", c.Writer.Status())
		log.Printf("Duration: %v", duration)
		log.Printf("Response Headers:")
		for name, values := range c.Writer.Header() {
			for _, value := range values {
				log.Printf("  %s: %s", name, value)

				// Special handling for X-Payment-Response header
				if strings.ToLower(name) == "x-payment-response" {
					if decoded, err := base64.StdEncoding.DecodeString(value); err == nil {
						log.Printf("  X-Payment-Response (decoded): %s", string(decoded))
					} else {
						// Try without base64 decoding (might be plain JSON)
						log.Printf("  X-Payment-Response (not base64, raw): %s", value)
					}
				}
			}
		}
		log.Printf("=== RESPONSE END ===\n")
	}
}
