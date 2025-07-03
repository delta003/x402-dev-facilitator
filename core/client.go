package core

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
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"

	"github.com/coinbase/x402/go/pkg/facilitatorclient"
	x402types "github.com/coinbase/x402/go/pkg/types"
)

// Client represents x402 payment client
// NOTE(marko): There isn't currently an official Go implementation of the client.
type Client struct {
	httpClient            *http.Client
	privateKey            *ecdsa.PrivateKey
	address               common.Address
	maxValue              *big.Int
	chainID               int64
	facilitatorClient     *facilitatorclient.FacilitatorClient
	useReceipts           bool
	enableReceiptFallback bool
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

// NewClientWithFacilitator creates a new x402 client with facilitator configured for receipts
func NewClientWithFacilitator(privateKey *ecdsa.PrivateKey, chainID int64, facilitatorConfig *x402types.FacilitatorConfig) *Client {
	client := NewClient(privateKey, chainID)
	client.SetFacilitator(facilitatorConfig)
	client.SetUseReceipts(true)
	client.SetEnableReceiptFallback(true)
	return client
}

// NewClientWithFacilitatorFromHex creates a new x402 client with facilitator from hex private key
func NewClientWithFacilitatorFromHex(privateKeyHex string, chainID int64, facilitatorConfig *x402types.FacilitatorConfig) (*Client, error) {
	client, err := NewClientFromHex(privateKeyHex, chainID)
	if err != nil {
		return nil, err
	}
	client.SetFacilitator(facilitatorConfig)
	client.SetUseReceipts(true)
	client.SetEnableReceiptFallback(true)
	return client, nil
}

// NewClientWithFacilitatorURL creates a new x402 client with facilitator URL for convenience
func NewClientWithFacilitatorURL(privateKey *ecdsa.PrivateKey, chainID int64, facilitatorURL string) *Client {
	config := &x402types.FacilitatorConfig{
		URL: facilitatorURL,
	}
	return NewClientWithFacilitator(privateKey, chainID, config)
}

// NewClientWithFacilitatorURLFromHex creates a new x402 client with facilitator URL from hex private key
func NewClientWithFacilitatorURLFromHex(privateKeyHex string, chainID int64, facilitatorURL string) (*Client, error) {
	config := &x402types.FacilitatorConfig{
		URL: facilitatorURL,
	}
	return NewClientWithFacilitatorFromHex(privateKeyHex, chainID, config)
}

// SetMaxValue sets the maximum payment value allowed
func (c *Client) SetMaxValue(maxValue *big.Int) {
	c.maxValue = maxValue
}

// SetHTTPClient sets a custom HTTP client
func (c *Client) SetHTTPClient(client *http.Client) {
	c.httpClient = client
}

// SetFacilitator sets the facilitator client for settlement and receipt generation
func (c *Client) SetFacilitator(facilitatorConfig *x402types.FacilitatorConfig) {
	c.facilitatorClient = facilitatorclient.NewFacilitatorClient(facilitatorConfig)
}


// SetUseReceipts enables or disables receipt mode
func (c *Client) SetUseReceipts(useReceipts bool) {
	c.useReceipts = useReceipts
}

// SetEnableReceiptFallback enables fallback to traditional payments if receipt settlement fails
func (c *Client) SetEnableReceiptFallback(enableFallback bool) {
	c.enableReceiptFallback = enableFallback
}

// HasFacilitator returns true if a facilitator is configured
func (c *Client) HasFacilitator() bool {
	return c.facilitatorClient != nil
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

	var req struct {
		X402Version int                             `json:"x402Version"`
		Accepts     []x402types.PaymentRequirements `json:"accepts"`
		Error       string                          `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&req); err != nil {
		return nil, fmt.Errorf("failed to parse 402 response: %w", err)
	}

	if req.X402Version != x402Version {
		return nil, fmt.Errorf("unsupported x402 version: %d, expected: %d", req.X402Version, x402Version)
	}
	// Check if we have any payment requirements
	if len(req.Accepts) == 0 {
		return nil, fmt.Errorf("no payment requirements provided in 402 response")
	}
	// Use the first payment requirement
	paymentRequirements := req.Accepts[0]

	// Validate amount against maximum
	maxAmount, ok := new(big.Int).SetString(paymentRequirements.MaxAmountRequired, 10)
	if !ok {
		return nil, fmt.Errorf("invalid max amount format: %s", paymentRequirements.MaxAmountRequired)
	}

	if maxAmount.Cmp(prt.client.maxValue) > 0 {
		return nil, fmt.Errorf("payment amount %s exceeds maximum allowed %s", paymentRequirements.MaxAmountRequired, prt.client.maxValue.String())
	}

	// Determine whether to use receipts or traditional payments
	var paymentHeader string
	var err error

	if prt.client.useReceipts && prt.client.HasFacilitator() {
		// Use receipt mode: settle via facilitator and create receipt header
		paymentHeader, err = prt.createReceiptPaymentHeader(&paymentRequirements, originalReq.Context())
		if err != nil {
			if prt.client.enableReceiptFallback {
				// Fall back to traditional payment mode
				fmt.Printf("Receipt settlement failed, falling back to payment mode: %v\n", err)
				paymentHeader, err = prt.createPaymentHeader(&paymentRequirements)
				if err != nil {
					return nil, fmt.Errorf("failed to create payment header: %w", err)
				}
			} else {
				return nil, fmt.Errorf("failed to create receipt header: %w", err)
			}
		}
	} else {
		// Use traditional payment mode
		paymentHeader, err = prt.createPaymentHeader(&paymentRequirements)
		if err != nil {
			return nil, fmt.Errorf("failed to create payment header: %w", err)
		}
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

// createReceiptPaymentHeader creates a receipt header by settling payment via facilitator
func (prt *PaymentRoundTripper) createReceiptPaymentHeader(requirements *x402types.PaymentRequirements, ctx context.Context) (string, error) {
	// First create a traditional payment payload for settlement
	paymentPayload, err := prt.createPaymentPayload(requirements)
	if err != nil {
		return "", fmt.Errorf("failed to create payment payload: %w", err)
	}

	// Settle the payment via facilitator to get transaction hash
	txHash, err := prt.client.settlePaymentWithFacilitator(ctx, paymentPayload, requirements)
	if err != nil {
		return "", fmt.Errorf("failed to settle payment: %w", err)
	}

	// Create receipt header from transaction hash
	receiptHeader, err := prt.client.createReceiptHeader(txHash, requirements)
	if err != nil {
		return "", fmt.Errorf("failed to create receipt header: %w", err)
	}

	return receiptHeader, nil
}

// createPaymentPayload creates a payment payload without encoding to base64
func (prt *PaymentRoundTripper) createPaymentPayload(requirements *x402types.PaymentRequirements) (*x402types.PaymentPayload, error) {
	// Create authorization
	auth, err := prt.createAuthorization(requirements)
	if err != nil {
		return nil, fmt.Errorf("failed to create authorization: %w", err)
	}

	// Sign the authorization
	signature, err := prt.signAuthorization(&auth)
	if err != nil {
		return nil, fmt.Errorf("failed to sign authorization: %w", err)
	}

	// Create payment payload
	payload := &x402types.PaymentPayload{
		X402Version: x402Version,
		Scheme:      requirements.Scheme,
		Network:     requirements.Network,
		Payload: &x402types.ExactEvmPayload{
			Signature:     signature,
			Authorization: &auth,
		},
	}

	return payload, nil
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
	// Parse values from authorization
	value, ok := new(big.Int).SetString(auth.Value, 10)
	if !ok {
		return "", fmt.Errorf("invalid value: %s", auth.Value)
	}

	validAfter, ok := new(big.Int).SetString(auth.ValidAfter, 10)
	if !ok {
		return "", fmt.Errorf("invalid validAfter: %s", auth.ValidAfter)
	}

	validBefore, ok := new(big.Int).SetString(auth.ValidBefore, 10)
	if !ok {
		return "", fmt.Errorf("invalid validBefore: %s", auth.ValidBefore)
	}

	// Create EIP-712 typed data
	chainIdHex := math.NewHexOrDecimal256(prt.client.chainID)
	typedData := apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": {
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"TransferWithAuthorization": {
				{Name: "from", Type: "address"},
				{Name: "to", Type: "address"},
				{Name: "value", Type: "uint256"},
				{Name: "validAfter", Type: "uint256"},
				{Name: "validBefore", Type: "uint256"},
				{Name: "nonce", Type: "bytes32"},
			},
		},
		PrimaryType: "TransferWithAuthorization",
		Domain: apitypes.TypedDataDomain{
			Name:              "USD Coin",
			Version:           "2",
			ChainId:           chainIdHex,
			VerifyingContract: baseUSDCAddress,
		},
		Message: apitypes.TypedDataMessage{
			"from":        auth.From,
			"to":          auth.To,
			"value":       value,
			"validAfter":  validAfter,
			"validBefore": validBefore,
			"nonce":       auth.Nonce,
		},
	}

	// Hash the typed data
	hash, err := typedData.HashStruct("TransferWithAuthorization", typedData.Message)
	if err != nil {
		return "", fmt.Errorf("failed to hash struct: %w", err)
	}

	domainSeparator, err := typedData.HashStruct("EIP712Domain", typedData.Domain.Map())
	if err != nil {
		return "", fmt.Errorf("failed to hash domain: %w", err)
	}

	// Create final hash with EIP-712 prefix
	finalHash := crypto.Keccak256Hash(
		[]byte("\x19\x01"),
		domainSeparator,
		hash,
	)

	// Sign the hash
	signature, err := crypto.Sign(finalHash.Bytes(), prt.client.privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign: %w", err)
	}

	// Adjust v (EIP-712 signatures require v to be 27 or 28)
	if signature[64] == 0 || signature[64] == 1 {
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

// settlePaymentWithFacilitator settles a payment using the facilitator and returns the transaction hash
func (c *Client) settlePaymentWithFacilitator(ctx context.Context, paymentPayload *x402types.PaymentPayload, requirements *x402types.PaymentRequirements) (string, error) {
	if c.facilitatorClient == nil {
		return "", fmt.Errorf("facilitator client not configured")
	}

	// Use the facilitator to settle the payment
	settleResponse, err := c.facilitatorClient.Settle(paymentPayload, requirements)
	if err != nil {
		return "", fmt.Errorf("failed to settle payment: %w", err)
	}

	if !settleResponse.Success {
		return "", fmt.Errorf("settlement failed: %s", *settleResponse.ErrorReason)
	}

	return settleResponse.Transaction, nil
}

// createReceiptHeader creates a receipt header from a transaction hash
func (c *Client) createReceiptHeader(txHash string, requirements *x402types.PaymentRequirements) (string, error) {
	// Create a signature for the transaction hash to prove ownership
	signature, err := c.signTransactionHash(txHash)
	if err != nil {
		return "", fmt.Errorf("failed to sign transaction hash: %w", err)
	}

	// Create the receipt payload
	receiptPayload := ReceiptPayload{
		X402Version: x402Version,
		Scheme:      requirements.Scheme,
		Network:     requirements.Network,
		Payload: &ExactEvmReceipt{
			Transaction: txHash,
			Signature:   signature,
		},
	}

	// Encode as JSON and base64
	payloadBytes, err := json.Marshal(receiptPayload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal receipt payload: %w", err)
	}

	return base64.StdEncoding.EncodeToString(payloadBytes), nil
}

// signTransactionHash signs a transaction hash to prove ownership
func (c *Client) signTransactionHash(txHash string) (string, error) {
	// Create a hash of the transaction hash
	msgHash := crypto.Keccak256Hash([]byte(txHash))

	// Sign the hash
	signature, err := crypto.Sign(msgHash.Bytes(), c.privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign transaction hash: %w", err)
	}

	// Adjust v for Ethereum signature format
	if signature[64] == 0 || signature[64] == 1 {
		signature[64] += 27
	}

	return fmt.Sprintf("0x%x", signature), nil
}
