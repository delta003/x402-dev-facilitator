package main

import (
	"context"
	"fmt"
	"github.com/x40/x402-tenderly/core"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
)

type Client struct {
	serverURL string
	client    *core.Client
}

func NewClient(serverURL string, privateKey string, chainId int64) (*Client, error) {
	client, err := core.NewClientFromHex(privateKey, chainId)
	if err != nil {
		return nil, fmt.Errorf("failed to create client from private key: %w", err)
	}
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
	return nil
}

func main() {
	serverPort := os.Getenv("SERVER_PORT")
	if serverPort == "" {
		serverPort = "4021" // Default port if not set
	}
	privateKey := os.Getenv("PRIVATE_KEY")
	if privateKey == "" {
		log.Fatal("PRIVATE_KEY environment variable is not set")
	}
	strChainID := os.Getenv("CHAIN_ID")
	if strChainID == "" {
		log.Fatal("CHAIN_ID environment variable is not set")
	}
	chainID, err := strconv.ParseInt(strChainID, 10, 64)
	if err != nil {
		log.Fatalf("Invalid CHAIN_ID: %v", err)
	}

	client, err := NewClient("http://localhost:"+serverPort, privateKey, chainID)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	fmt.Printf("Attempting to tip server...")
	if err := client.Tip(context.Background()); err != nil {
		log.Fatalf("Failed to tip: %v", err)
	}
}
