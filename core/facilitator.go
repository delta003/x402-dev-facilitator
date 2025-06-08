package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"

	x402types "github.com/coinbase/x402/go/pkg/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

const x402Version = 1

// ERC-20 ABI for balanceOf function
const erc20ABI = `[{
	"constant": true,
	"inputs": [{"name": "_owner", "type": "address"}],
	"name": "balanceOf",
	"outputs": [{"name": "balance", "type": "uint256"}],
	"type": "function"
}, {
	"constant": false,
	"inputs": [{"name": "_to", "type": "address"}, {"name": "_value", "type": "uint256"}],
	"name": "transfer",
	"outputs": [{"name": "", "type": "bool"}],
	"type": "function"
}, {
	"constant": false,
	"inputs": [{"name": "_from", "type": "address"}, {"name": "_to", "type": "address"}, {"name": "_value", "type": "uint256"}],
	"name": "transferFrom",
	"outputs": [{"name": "", "type": "bool"}],
	"type": "function"
}, {
	"constant": true,
	"inputs": [{"name": "_owner", "type": "address"}, {"name": "_spender", "type": "address"}],
	"name": "allowance",
	"outputs": [{"name": "", "type": "uint256"}],
	"type": "function"
}]`

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
		log.Fatal("unreachable: invalid payment payload format")
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

// HealthHandler provides a health check endpoint
func (f *Facilitator) HealthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	health := map[string]interface{}{
		"status":  "healthy",
		"network": f.network,
		"chainId": f.chainID.String(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}

func (f *Facilitator) verifySignature(payload *x402types.ExactEvmPayload) bool {
	// Parse values from authorization
	auth := payload.Authorization

	// Convert string values to big.Int
	value, ok := new(big.Int).SetString(auth.Value, 10)
	if !ok {
		return false
	}

	validAfter, ok := new(big.Int).SetString(auth.ValidAfter, 10)
	if !ok {
		return false
	}

	validBefore, ok := new(big.Int).SetString(auth.ValidBefore, 10)
	if !ok {
		return false
	}

	// Create EIP-712 typed data
	chainIdHex := math.NewHexOrDecimal256(f.chainID.Int64())
	typedData := apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": {
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"PaymentAuthorization": {
				{Name: "from", Type: "address"},
				{Name: "to", Type: "address"},
				{Name: "value", Type: "uint256"},
				{Name: "validAfter", Type: "uint256"},
				{Name: "validBefore", Type: "uint256"},
				{Name: "nonce", Type: "bytes32"},
			},
		},
		PrimaryType: "PaymentAuthorization",
		Domain: apitypes.TypedDataDomain{
			Name:              "x402",
			Version:           "1",
			ChainId:           chainIdHex,
			VerifyingContract: "0x0000000000000000000000000000000000000000",
		},
		Message: apitypes.TypedDataMessage{
			"from":        auth.From,
			"to":          auth.To,
			"value":       (*hexutil.Big)(value),
			"validAfter":  (*hexutil.Big)(validAfter),
			"validBefore": (*hexutil.Big)(validBefore),
			"nonce":       auth.Nonce,
		},
	}

	// Hash the typed data
	hash, err := typedData.HashStruct("PaymentAuthorization", typedData.Message)
	if err != nil {
		return false
	}

	domainSeparator, err := typedData.HashStruct("EIP712Domain", typedData.Domain.Map())
	if err != nil {
		return false
	}

	// Create final hash with EIP-712 prefix
	finalHash := crypto.Keccak256Hash(
		[]byte("\x19\x01"),
		domainSeparator,
		hash,
	)

	// Decode signature
	sigBytes, err := hexutil.Decode(payload.Signature)
	if err != nil {
		return false
	}

	if len(sigBytes) != 65 {
		return false
	}

	// Adjust recovery ID for erecover
	if sigBytes[64] >= 27 {
		sigBytes[64] -= 27
	}

	// Recover public key
	pubKey, err := crypto.SigToPub(finalHash.Bytes(), sigBytes)
	if err != nil {
		return false
	}

	// Verify the recovered address matches the from address
	recoveredAddr := crypto.PubkeyToAddress(*pubKey)
	return strings.EqualFold(recoveredAddr.Hex(), auth.From)
}

func (f *Facilitator) getAccountBalance(ctx context.Context, address, asset string) (*big.Int, error) {
	// For ETH balance
	if strings.EqualFold(asset, "0x0000000000000000000000000000000000000000") || asset == "" {
		return f.ethClient.BalanceAt(ctx, common.HexToAddress(address), nil)
	}

	// For ERC-20 tokens, call balanceOf function
	contractAddress := common.HexToAddress(asset)
	userAddress := common.HexToAddress(address)

	// Parse ERC-20 ABI
	parsedABI, err := abi.JSON(strings.NewReader(erc20ABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse ERC-20 ABI: %w", err)
	}

	// Pack balanceOf function call
	data, err := parsedABI.Pack("balanceOf", userAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to pack balanceOf call: %w", err)
	}

	// Create call message
	msg := ethereum.CallMsg{
		To:   &contractAddress,
		Data: data,
	}

	// Call the contract
	result, err := f.ethClient.CallContract(ctx, msg, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to call balanceOf: %w", err)
	}

	// Unpack the result
	var balance *big.Int
	err = parsedABI.UnpackIntoInterface(&balance, "balanceOf", result)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack balanceOf result: %w", err)
	}

	return balance, nil
}

func (f *Facilitator) submitTransaction(ctx context.Context, payload *x402types.PaymentPayload) (common.Hash, error) {
	auth := payload.Payload.Authorization

	// Parse addresses and values
	fromAddr := common.HexToAddress(auth.From)
	toAddr := common.HexToAddress(auth.To)
	value, ok := new(big.Int).SetString(auth.Value, 10)
	if !ok {
		return common.Hash{}, fmt.Errorf("invalid value: %s", auth.Value)
	}

	// Get nonce for the from address (the payer)
	nonce, err := f.ethClient.PendingNonceAt(ctx, fromAddr)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to get nonce: %w", err)
	}

	// Get gas price
	gasPrice, err := f.ethClient.SuggestGasPrice(ctx)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to get gas price: %w", err)
	}

	// Create the transaction that matches the signed authorization
	legacyTx := &ethtypes.LegacyTx{
		Nonce:    nonce,
		To:       &toAddr,
		Value:    value,
		Gas:      uint64(21000), // Standard gas limit for ETH transfer
		GasPrice: gasPrice,
		Data:     nil, // No data for simple transfer
	}

	tx := ethtypes.NewTx(legacyTx)

	// Use the existing signature from the payload instead of signing with facilitator key
	// The signature in the payload is the user's authorization for this exact transaction
	sigBytes, err := hexutil.Decode(payload.Payload.Signature)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to decode signature: %w", err)
	}

	if len(sigBytes) != 65 {
		return common.Hash{}, fmt.Errorf("invalid signature length: expected 65, got %d", len(sigBytes))
	}

	// Adjust recovery ID for transaction signing (if needed)
	if sigBytes[64] < 27 {
		sigBytes[64] += 27
	}

	// Create the signed transaction using the payload signature
	// This reconstructs the transaction as it was originally signed by the payer
	signer := ethtypes.NewEIP155Signer(f.chainID)
	signedTx, err := tx.WithSignature(signer, sigBytes)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to create signed transaction: %w", err)
	}

	// Verify the transaction is properly signed by checking the sender
	sender, err := ethtypes.Sender(signer, signedTx)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to verify transaction sender: %w", err)
	}

	if !strings.EqualFold(sender.Hex(), auth.From) {
		return common.Hash{}, fmt.Errorf("transaction sender %s does not match authorization from %s", sender.Hex(), auth.From)
	}

	// Submit the transaction
	err = f.ethClient.SendTransaction(ctx, signedTx)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to send transaction: %w", err)
	}

	return signedTx.Hash(), nil
}

func (f *Facilitator) awaitReceipt(ctx context.Context, hash *common.Hash) (*ethtypes.Receipt, error) {
	// Poll for transaction receipt with timeout
	timeout := time.After(1 * time.Minute)    // 1 minute timeout
	ticker := time.NewTicker(1 * time.Second) // Check every second
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return nil, fmt.Errorf("timeout waiting for transaction receipt")
		case <-ticker.C:
			receipt, err := f.ethClient.TransactionReceipt(ctx, *hash)
			if err != nil {
				// Transaction not yet mined, continue polling
				continue
			}
			return receipt, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func stringPtr(s string) *string {
	return &s
}
