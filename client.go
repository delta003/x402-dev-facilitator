package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	x402types "github.com/coinbase/x402/go/pkg/types"
)

// Client represents x402 payment client
// NOTE(marko): There isn't currently an official Go implementation of the client.
type Client struct {
	httpClient *http.Client
	privateKey *ecdsa.PrivateKey
	address    common.Address
	maxValue   *big.Int
	chainID    int64
}

// NewClient creates a new x402 payment client
func NewClient(privateKey *ecdsa.PrivateKey, chainID int64) *Client {
	address := crypto.PubkeyToAddress(privateKey.PublicKey)

	// Default to 0.1 USDC (6 decimals)
	maxValue := big.NewInt(100000) // 0.1 * 10^6

	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		privateKey: privateKey,
		address:    address,
		maxValue:   maxValue,
		chainID:    chainID,
	}
}

// Helper function to create a client from private key hex string
func NewClientFromHex(privateKeyHex string, chainID int64) (*Client, error) {
	// Remove 0x prefix if present
	privateKeyHex = strings.TrimPrefix(privateKeyHex, "0x")

	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	return NewClient(privateKey, chainID), nil
}

// SetMaxValue sets the maximum payment value allowed
func (c *Client) SetMaxValue(maxValue *big.Int) {
	c.maxValue = maxValue
}

// SetHTTPClient sets a custom HTTP client
func (c *Client) SetHTTPClient(client *http.Client) {
	c.httpClient = client
}

// WrapHTTPClient returns an HTTP client that automatically handles x402 payments
func (c *Client) WrapHTTPClient() *http.Client {
	transport := &PaymentRoundTripper{
		client:    c,
		transport: http.DefaultTransport,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   c.httpClient.Timeout,
	}
}

// PaymentRoundTripper implements http.RoundTripper with payment handling
type PaymentRoundTripper struct {
	client    *Client
	transport http.RoundTripper
}

// RoundTrip implements http.RoundTripper
func (prt *PaymentRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Check if this is already a retry to avoid infinite loops
	if req.Header.Get("X-Payment-Retry") == "true" {
		return prt.transport.RoundTrip(req)
	}

	// Make the initial request
	resp, err := prt.transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	// If not a 402 Payment Required, return the response as is
	if resp.StatusCode != http.StatusPaymentRequired {
		return resp, nil
	}

	// Handle 402 Payment Required
	return prt.handlePaymentRequired(req, resp)
}

// handlePaymentRequired processes a 402 Payment Required response
func (prt *PaymentRoundTripper) handlePaymentRequired(originalReq *http.Request, resp *http.Response) (*http.Response, error) {
	defer resp.Body.Close()

	// Read the payment requirements
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read 402 response: %w", err)
	}

	var paymentRequirements x402types.PaymentRequirements
	if err := json.Unmarshal(body, &paymentRequirements); err != nil {
		return nil, fmt.Errorf("failed to parse payment response: %w", err)
	}

	// Validate amount against maximum
	maxAmount, ok := new(big.Int).SetString(paymentRequirements.MaxAmountRequired, 10)
	if !ok {
		return nil, fmt.Errorf("invalid max amount format: %s", paymentRequirements.MaxAmountRequired)
	}

	if maxAmount.Cmp(prt.client.maxValue) > 0 {
		return nil, fmt.Errorf("payment amount %s exceeds maximum allowed %s", paymentRequirements.MaxAmountRequired, prt.client.maxValue.String())
	}

	// Create payment header
	paymentHeader, err := prt.createPaymentHeader(&paymentRequirements)
	if err != nil {
		return nil, fmt.Errorf("failed to create payment header: %w", err)
	}

	// Clone the original request with payment header
	newReq := originalReq.Clone(originalReq.Context())
	newReq.Header.Set("X-PAYMENT", paymentHeader)
	newReq.Header.Set("X-Payment-Retry", "true")
	newReq.Header.Set("Access-Control-Expose-Headers", "X-PAYMENT-RESPONSE")

	// Make the request with payment
	return prt.transport.RoundTrip(newReq)
}

// createPaymentHeader creates the payment header
func (prt *PaymentRoundTripper) createPaymentHeader(requirements *x402types.PaymentRequirements) (string, error) {
	// Create authorization
	auth, err := prt.createAuthorization(requirements)
	if err != nil {
		return "", fmt.Errorf("failed to create authorization: %w", err)
	}

	// Sign the authorization
	signature, err := prt.signAuthorization(&auth)
	if err != nil {
		return "", fmt.Errorf("failed to sign authorization: %w", err)
	}

	// Create payment payload
	payload := x402types.PaymentPayload{
		X402Version: x402Version,
		Scheme:      requirements.Scheme,
		Network:     requirements.Network,
		Payload: &x402types.ExactEvmPayload{
			Signature:     signature,
			Authorization: &auth,
		},
	}

	// Encode as JSON and base64
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %w", err)
	}

	return base64.StdEncoding.EncodeToString(payloadBytes), nil
}

// createAuthorization creates the authorization for payment
func (prt *PaymentRoundTripper) createAuthorization(requirements *x402types.PaymentRequirements) (x402types.ExactEvmPayloadAuthorization, error) {
	now := time.Now().Unix()

	// Generate a random nonce
	nonce := crypto.Keccak256Hash([]byte(fmt.Sprintf("%d-%s", now, prt.client.address.Hex())))

	auth := x402types.ExactEvmPayloadAuthorization{
		From:        prt.client.address.Hex(),
		To:          requirements.PayTo,
		Value:       requirements.MaxAmountRequired,
		ValidAfter:  fmt.Sprintf("%d", now-60),   // Valid from 1 minute ago
		ValidBefore: fmt.Sprintf("%d", now+3600), // Valid for 1 hour
		Nonce:       nonce.Hex(),
	}

	return auth, nil
}

// signAuthorization signs the authorization using EIP-712
func (prt *PaymentRoundTripper) signAuthorization(auth *x402types.ExactEvmPayloadAuthorization) (string, error) {
	// This is a simplified signature - in production you'd want to use proper EIP-712
	hash := crypto.Keccak256Hash([]byte(fmt.Sprintf("%s%s%s%s%s%s",
		auth.From, auth.To, auth.Value, auth.ValidAfter, auth.ValidBefore, auth.Nonce)))

	signature, err := crypto.Sign(hash.Bytes(), prt.client.privateKey)
	if err != nil {
		return "", err
	}

	// Adjust recovery ID for Ethereum
	if signature[64] < 27 {
		signature[64] += 27
	}

	return fmt.Sprintf("0x%x", signature), nil
}

// Fetch makes an HTTP request with automatic payment handling
func (c *Client) Fetch(ctx context.Context, method, url string, body io.Reader, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	// Set headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// Use the wrapped client
	client := c.WrapHTTPClient()
	return client.Do(req)
}

// Get makes a GET request with automatic payment handling
func (c *Client) Get(ctx context.Context, url string) (*http.Response, error) {
	return c.Fetch(ctx, "GET", url, nil, nil)
}

// Post makes a POST request with automatic payment handling
func (c *Client) Post(ctx context.Context, url string, contentType string, body io.Reader) (*http.Response, error) {
	headers := map[string]string{
		"Content-Type": contentType,
	}
	return c.Fetch(ctx, "POST", url, body, headers)
}

// PostJSON makes a POST request with JSON body and automatic payment handling
func (c *Client) PostJSON(ctx context.Context, url string, data interface{}) (*http.Response, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	return c.Post(ctx, url, "application/json", bytes.NewReader(jsonData))
}
