package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	x402types "github.com/coinbase/x402/go/pkg/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

const x402Version = 1

// Facilitator handles payment verification and settlement
type Facilitator struct {
	ethClient *ethclient.Client
	chainID   *big.Int
	network   string
}

// NewFacilitator creates a new facilitator instance
func NewFacilitator(rpcURL string, network string) (*Facilitator, error) {
	ethClient, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Ethereum client: %w", err)
	}

	chainID, err := ethClient.ChainID(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get chain ID: %w", err)
	}

	return &Facilitator{
		ethClient: ethClient,
		chainID:   chainID,
		network:   network,
	}, nil
}

// VerifyPayment verifies a payment payload
func (f *Facilitator) VerifyPayment(ctx context.Context, paymentHeader string, requirements *x402types.PaymentRequirements) (*x402types.VerifyResponse, error) {
	// Decode the payment payload
	payload, err := x402types.DecodePaymentPayloadFromBase64(paymentHeader)
	if err != nil {
		return &x402types.VerifyResponse{
			IsValid:       false,
			InvalidReason: stringPtr("invalid_payment_payload_format"),
		}, nil
	}

	// Verify network matches
	if payload.Network != f.network {
		return &x402types.VerifyResponse{
			IsValid:       false,
			InvalidReason: stringPtr("invalid_network"),
		}, nil
	}

	// Verify x402 version
	if payload.X402Version != x402Version {
		return &x402types.VerifyResponse{
			IsValid:       false,
			InvalidReason: stringPtr("unsupported_x402_version"),
		}, nil
	}

	// Verify authorization details
	auth := payload.Payload.Authorization

	// Check recipient matches
	if !strings.EqualFold(auth.To, requirements.PayTo) {
		return &x402types.VerifyResponse{
			IsValid:       false,
			InvalidReason: stringPtr("invalid_exact_evm_payload_recipient_mismatch"),
		}, nil
	}

	// Check time validity
	now := time.Now().Unix()
	validAfter, err := strconv.ParseInt(auth.ValidAfter, 10, 64)
	if err != nil || now < validAfter {
		return &x402types.VerifyResponse{
			IsValid:       false,
			InvalidReason: stringPtr("invalid_exact_evm_payload_authorization_valid_after"),
		}, nil
	}

	validBefore, err := strconv.ParseInt(auth.ValidBefore, 10, 64)
	if err != nil || now > validBefore {
		return &x402types.VerifyResponse{
			IsValid:       false,
			InvalidReason: stringPtr("invalid_exact_evm_payload_authorization_valid_before"),
		}, nil
	}

	// Verify signature (simplified - in production use proper EIP-712 verification)
	if !f.verifySignature(payload.Payload) {
		return &x402types.VerifyResponse{
			IsValid:       false,
			InvalidReason: stringPtr("invalid_exact_evm_payload_signature"),
		}, nil
	}

	// Check account balance (optional, for demonstration)
	balance, err := f.getAccountBalance(ctx, auth.From, requirements.Asset)
	if err == nil {
		requiredAmount, ok := new(big.Int).SetString(auth.Value, 10)
		if ok && balance.Cmp(requiredAmount) < 0 {
			return &x402types.VerifyResponse{
				IsValid:       false,
				InvalidReason: stringPtr("insufficient_funds"),
			}, nil
		}
	}

	return &x402types.VerifyResponse{
		IsValid: true,
		Payer:   stringPtr(auth.From),
	}, nil
}

// SettlePayment processes the actual payment settlement
func (f *Facilitator) SettlePayment(ctx context.Context, paymentHeader string, requirements *x402types.PaymentRequirements) (*x402types.SettleResponse, error) {
	// First verify the payment
	verifyResp, err := f.VerifyPayment(ctx, paymentHeader, requirements)
	if err != nil {
		return nil, err
	}

	if !verifyResp.IsValid {
		return &x402types.SettleResponse{
			Success:     false,
			ErrorReason: verifyResp.InvalidReason,
			Network:     f.network,
			Transaction: "0x0000000000000000000000000000000000000000000000000000000000000000",
		}, nil
	}

	// Decode the payment payload
	payload, err := x402types.DecodePaymentPayloadFromBase64(paymentHeader)
	if err != nil {
		panic("unreachable: invalid payment payload format")
	}

	// Submit the transaction
	txHash, err := f.submitTransaction(ctx, payload)
	if err != nil {
		return nil, err
	}

	// Wait for the transaction receipt
	receipt, err := f.awaitReceipt(ctx, &txHash)
	if err != nil {
		return nil, err
	}

	if receipt.Status != ethtypes.ReceiptStatusSuccessful {
		return &x402types.SettleResponse{
			Success:     false,
			ErrorReason: stringPtr("invalid_transaction_state"),
			Network:     f.network,
			Transaction: txHash.Hex(),
		}, nil
	}

	return &x402types.SettleResponse{
		Success:     true,
		Payer:       verifyResp.Payer,
		Transaction: txHash.Hex(),
		Network:     f.network,
	}, nil
}

// HTTP handlers for facilitator endpoints

// VerifyPaymentHandler handles payment verification requests
func (f *Facilitator) VerifyPaymentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		PaymentHeader string                        `json:"paymentHeader"`
		Requirements  x402types.PaymentRequirements `json:"requirements"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	resp, err := f.VerifyPayment(r.Context(), req.PaymentHeader, &req.Requirements)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// SettlePaymentHandler handles payment settlement requests
func (f *Facilitator) SettlePaymentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		PaymentHeader string                        `json:"paymentHeader"`
		Requirements  x402types.PaymentRequirements `json:"requirements"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	resp, err := f.SettlePayment(r.Context(), req.PaymentHeader, &req.Requirements)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (f *Facilitator) verifySignature(payload *x402types.ExactEvmPayload) bool {
	// TODO(marko): Implement EIP-712 signature validation
	return true // For demonstration, assume signature is valid
}

func (f *Facilitator) getAccountBalance(ctx context.Context, address, asset string) (*big.Int, error) {
	// For ETH balance
	if strings.EqualFold(asset, "0x0000000000000000000000000000000000000000") || asset == "" {
		return f.ethClient.BalanceAt(ctx, common.HexToAddress(address), nil)
	}

	// TODO(marko): Call balanceOf for ERC-20 tokens
	return big.NewInt(1000000), nil // Assume sufficient balance
}

func (f *Facilitator) submitTransaction(ctx context.Context, payload *x402types.PaymentPayload) (common.Hash, error) {
	// TODO(marko): submit
	return common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"), nil
}

func (f *Facilitator) awaitReceipt(ctx context.Context, hash *common.Hash) (*ethtypes.Receipt, error) {
	// TODO(marko): wait for transaction receipt
	return nil, nil // Simulate receipt
}

func stringPtr(s string) *string {
	return &s
}
