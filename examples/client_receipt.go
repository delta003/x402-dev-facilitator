package main

import (
	"context"
	"fmt"
	"log"
	"os"

	x402types "github.com/coinbase/x402/go/pkg/types"
	"github.com/delta003/x402-dev-facilitator/core"
	"github.com/joho/godotenv"
)

func main() {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	serverURL := os.Getenv("SERVER_URL")
	if serverURL == "" {
		log.Fatal("SERVER_URL environment variable is not set")
	}
	privateKey := os.Getenv("WALLET_PRIVATE_KEY")
	if privateKey == "" {
		log.Fatal("WALLET_PRIVATE_KEY environment variable is not set")
	}
	facilitatorURL := os.Getenv("FACILITATOR_URL")
	if facilitatorURL == "" {
		log.Fatal("FACILITATOR_URL environment variable is not set")
	}

	chainID := int64(8453) // Base mainnet

	fmt.Println("=== Receipt-based Payment Example ===")

	// Example 1: Create client with FacilitatorConfig
	facilitatorConfig := &x402types.FacilitatorConfig{
		URL: facilitatorURL,
	}

	client, err := core.NewClientWithFacilitatorFromHex(privateKey, chainID, facilitatorConfig)
	if err != nil {
		log.Fatalf("Failed to create receipt client: %v", err)
	}

	fmt.Printf("Created receipt client with facilitator: %s\n", facilitatorConfig.URL)

	// Example 2: Create client with URL convenience method
	clientURL, err := core.NewClientWithFacilitatorURLFromHex(privateKey, chainID, facilitatorURL)
	if err != nil {
		log.Fatalf("Failed to create receipt client with URL: %v", err)
	}

	fmt.Printf("Created receipt client with URL convenience method\n")

	// Example 3: Manual configuration
	manualClient, err := core.NewClientFromHex(privateKey, chainID)
	if err != nil {
		log.Fatalf("Failed to create manual client: %v", err)
	}

	// Configure facilitator and receipt mode manually
	manualClient.SetFacilitator(facilitatorConfig)
	manualClient.SetUseReceipts(true)
	manualClient.SetEnableReceiptFallback(true)

	fmt.Printf("Configured manual client for receipts\n")

	// Make a request using receipt-based payment
	ctx := context.Background()
	resp, err := client.Get(ctx, serverURL+"/tip")
	if err != nil {
		log.Fatalf("Receipt request failed: %v", err)
	}
	defer resp.Body.Close()

	fmt.Printf("Receipt-based request completed with status: %d\n", resp.StatusCode)

	// Example 4: Compare with traditional payment client
	fmt.Println("\n=== Traditional Payment Example ===")

	traditionalClient, err := core.NewClientFromHex(privateKey, chainID)
	if err != nil {
		log.Fatalf("Failed to create traditional client: %v", err)
	}

	resp2, err := traditionalClient.Get(ctx, serverURL+"/tip")
	if err != nil {
		log.Fatalf("Traditional request failed: %v", err)
	}
	defer resp2.Body.Close()

	fmt.Printf("Traditional request completed with status: %d\n", resp2.StatusCode)

	fmt.Println("\n=== Configuration Options ===")
	fmt.Printf("Receipt client has facilitator: %t\n", client.HasFacilitator())
	fmt.Printf("Traditional client has facilitator: %t\n", traditionalClient.HasFacilitator())
	fmt.Printf("URL convenience client has facilitator: %t\n", clientURL.HasFacilitator())
	fmt.Printf("Manual client has facilitator: %t\n", manualClient.HasFacilitator())
}
