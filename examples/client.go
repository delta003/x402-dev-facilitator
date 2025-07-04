package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	x402types "github.com/coinbase/x402/go/pkg/types"
	"github.com/delta003/x402-dev-facilitator/core"
	"github.com/joho/godotenv"
	"io"
	"log"
	"math/big"
	"net/http"
	"os"
	"strconv"
)

type Client struct {
	serverURL string
	client    *core.Client
}

func NewClient(serverURL string, privateKey string, chainId int64, facilitatorURL string) (*Client, error) {
	client, err := core.NewClientFromHex(privateKey, chainId)
	if err != nil {
		return nil, fmt.Errorf("failed to create client from private key: %w", err)
	}

	if facilitatorURL != "" {
		fmt.Printf("Using facilitator URL: %s\n", facilitatorURL)
		client.SetFacilitator(&x402types.FacilitatorConfig{
			URL: facilitatorURL,
		})
	}

	client.SetMaxValue(big.NewInt(100_000_000))

	return &Client{
		serverURL: serverURL,
		client:    client,
	}, nil
}

func (c *Client) Tip(ctx context.Context) error {
	url := c.serverURL + "/tip"

	resp, err := c.client.Get(ctx, url)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	fmt.Printf("Received tip response: %s\n", string(body))

	// Decode and print X-PAYMENT-RESPONSE header if it exists
	paymentResponseHeader := resp.Header.Get("X-PAYMENT-RESPONSE")
	if paymentResponseHeader != "" {
		fmt.Printf("X-PAYMENT-RESPONSE header found\n")

		// Try to decode base64
		if decoded, err := base64.StdEncoding.DecodeString(paymentResponseHeader); err == nil {
			fmt.Printf("Decoded payment response: %s\n", string(decoded))

			// Try to parse as JSON
			var paymentResponse map[string]interface{}
			if err := json.Unmarshal(decoded, &paymentResponse); err == nil {
				fmt.Printf("Parsed payment response:\n")
				for key, value := range paymentResponse {
					fmt.Printf("  %s: %v\n", key, value)
				}
			} else {
				fmt.Printf("Could not parse payment response as JSON: %v\n", err)
			}
		} else {
			fmt.Printf("Could not decode payment response from base64: %v\n", err)
			fmt.Printf("Raw payment response: %s\n", paymentResponseHeader)
		}
	} else {
		fmt.Printf("No X-PAYMENT-RESPONSE header found\n")
	}

	return nil
}

func main() {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	serverURL := os.Getenv("SERVER_URL")
	if serverURL == "" {
		log.Fatal("SERVER_URL environment variable is not set")
	}
	privateKey := os.Getenv("CLIENT_WALLET_PRIVATE_KEY")
	if privateKey == "" {
		log.Fatal("CLIENT_WALLET_PRIVATE_KEY environment variable is not set")
	}
	strChainID := os.Getenv("CHAIN_ID")
	if strChainID == "" {
		log.Fatal("CHAIN_ID environment variable is not set")
	}
	chainID, err := strconv.ParseInt(strChainID, 10, 64)
	if err != nil {
		log.Fatalf("Invalid CHAIN_ID: %v", err)
	}
	// An optional variable!
	facilitatorURL := os.Getenv("FACILITATOR_URL")
	if facilitatorURL != "" {
		fmt.Printf("FACILITATOR_URL environment variable is set. Using receipt-based payment.\n")
	}

	client, err := NewClient(serverURL, privateKey, chainID, facilitatorURL)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	if err := client.Tip(context.Background()); err != nil {
		log.Fatalf("Failed to tip: %v", err)
	}
}
