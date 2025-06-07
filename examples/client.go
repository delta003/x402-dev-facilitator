package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	x402types "github.com/coinbase/x402/go/pkg/types"
)

type Client struct {
	serverURL string
	client    *http.Client
}

func NewClient(serverURL string) *Client {
	return &Client{
		serverURL: serverURL,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) GetTip() error {
	url := c.serverURL + "/tip"

	// First request - expect 402 Payment Required
	resp, err := c.client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to make initial request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusPaymentRequired {
		if resp.StatusCode == http.StatusOK {
			// Payment not required, read response
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return fmt.Errorf("failed to read response: %w", err)
			}
			fmt.Printf("Success! Response: %s\n", string(body))
			return nil
		}
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Parse 402 response to get payment requirements
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read 402 response: %w", err)
	}

	var paymentRequirements x402types.PaymentRequirements
	if err := json.Unmarshal(body, &paymentRequirements); err != nil {
		return fmt.Errorf("failed to parse payment requirements response: %w", err)
	}

	fmt.Printf("Payment required. Details: %+v\n", paymentRequirements)

	// TODO(marko): Create and sign payload.
	paymentPayload := x402types.PaymentPayload{}

	paymentData, err := json.Marshal(paymentPayload)
	if err != nil {
		return fmt.Errorf("failed to marshal payment payload: %w", err)
	}

	// Make the request again with payment
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-PAYMENT", string(paymentData))

	resp2, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make paid request: %w", err)
	}
	defer func() { _ = resp2.Body.Close() }()

	body2, err := io.ReadAll(resp2.Body)
	if err != nil {
		return fmt.Errorf("failed to read paid response: %w", err)
	}

	if resp2.StatusCode == http.StatusOK {
		fmt.Printf("Payment successful! Response: %s\n", string(body2))
		return nil
	}

	return fmt.Errorf("payment failed with status %d: %s", resp2.StatusCode, string(body2))
}

func main() {
	serverPort := os.Getenv("SERVER_PORT")
	if serverPort == "" {
		serverPort = "4021" // Default port if not set
	}

	client := NewClient("http://localhost:" + serverPort)

	fmt.Printf("Attempting to tip server...")
	if err := client.GetTip(); err != nil {
		log.Fatalf("Failed to tip: %v", err)
	}
}
