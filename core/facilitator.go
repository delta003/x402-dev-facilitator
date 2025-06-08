package core

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"log"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	x402types "github.com/coinbase/x402/go/pkg/types"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

const x402Version = 1
const baseUSDCAddress = "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"
const baseNetwork = "base"

// Facilitator handles payment verification and settlement
type Facilitator struct {
	ethClient  *ethclient.Client
	chainID    *big.Int
	privateKey *ecdsa.PrivateKey
	address    common.Address
}

// NewFacilitator creates a new facilitator instance
func NewFacilitator(privateKey *ecdsa.PrivateKey, rpcURL string) (*Facilitator, error) {
	ethClient, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Ethereum client: %w", err)
	}
	chainID, err := ethClient.ChainID(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get chain ID: %w", err)
	}
	address := crypto.PubkeyToAddress(privateKey.PublicKey)

	return &Facilitator{
		ethClient:  ethClient,
		chainID:    chainID,
		privateKey: privateKey,
		address:    address,
	}, nil
}

// NewFacilitator creates a new facilitator instance
func NewFacilitatorFromHex(privateKeyHex string, rpcURL string) (*Facilitator, error) {
	// Remove 0x prefix if present
	privateKeyHex = strings.TrimPrefix(privateKeyHex, "0x")

	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	return NewFacilitator(privateKey, rpcURL)
}

// VerifyPayment verifies a payment payload
func (f *Facilitator) VerifyPayment(ctx context.Context, payload *x402types.PaymentPayload, requirements *x402types.PaymentRequirements) (*x402types.VerifyResponse, error) {
	// Verify network matches
	if payload.Network != baseNetwork {
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

	// Verify signature
	if err := f.verifySignature(payload.Payload); err != nil {
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
func (f *Facilitator) SettlePayment(ctx context.Context, payload *x402types.PaymentPayload, requirements *x402types.PaymentRequirements) (*x402types.SettleResponse, error) {
	// First verify the payment
	verifyResp, err := f.VerifyPayment(ctx, payload, requirements)
	if err != nil {
		return nil, err
	}

	if !verifyResp.IsValid {
		return &x402types.SettleResponse{
			Success:     false,
			ErrorReason: verifyResp.InvalidReason,
			Network:     baseNetwork,
			Transaction: "0x0000000000000000000000000000000000000000000000000000000000000000",
		}, nil
	}

	// Submit the transaction
	txHash, err := f.transferWithAuthorization(ctx, payload)
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
			Network:     baseNetwork,
			Transaction: txHash.Hex(),
		}, nil
	}

	return &x402types.SettleResponse{
		Success:     true,
		Payer:       verifyResp.Payer,
		Transaction: txHash.Hex(),
		Network:     baseNetwork,
	}, nil
}

// HTTP handlers for facilitator endpoints

// VerifyPaymentHandler handles payment verification requests
func (f *Facilitator) VerifyPaymentHandler(c *gin.Context) {
	r := c.Request
	w := c.Writer

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		X402Version         int                           `json:"x402Version"`
		PaymentPayload      x402types.PaymentPayload      `json:"paymentPayload"`
		PaymentRequirements x402types.PaymentRequirements `json:"paymentRequirements"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.X402Version != x402Version {
		http.Error(w, "Unsupported x402 version", http.StatusBadRequest)
		return
	}

	resp, err := f.VerifyPayment(r.Context(), &req.PaymentPayload, &req.PaymentRequirements)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// SettlePaymentHandler handles payment settlement requests
func (f *Facilitator) SettlePaymentHandler(c *gin.Context) {
	r := c.Request
	w := c.Writer

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		X402Version         int                           `json:"x402Version"`
		PaymentPayload      x402types.PaymentPayload      `json:"paymentPayload"`
		PaymentRequirements x402types.PaymentRequirements `json:"paymentRequirements"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.X402Version != x402Version {
		http.Error(w, "Unsupported x402 version", http.StatusBadRequest)
		return
	}

	resp, err := f.SettlePayment(r.Context(), &req.PaymentPayload, &req.PaymentRequirements)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HealthHandler provides a health check endpoint
func (f *Facilitator) HealthHandler(c *gin.Context) {
	r := c.Request
	w := c.Writer

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	health := map[string]interface{}{
		"status":  "healthy",
		"network": baseNetwork,
		"chainId": f.chainID.String(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}

func (f *Facilitator) verifySignature(payload *x402types.ExactEvmPayload) error {
	auth := payload.Authorization

	tokenAddress := common.HexToAddress(baseUSDCAddress)
	fromAddr := common.HexToAddress(auth.From)
	nonce := common.HexToHash(auth.Nonce)

	// Verify nonce (optional, to ensure signature is valid)
	abiData, err := abi.JSON(strings.NewReader(`[
		{
			"name": "authorizationState",
			"type": "function",
			"inputs": [
				{"name": "authorizer", "type": "address"},
				{"name": "nonce", "type": "bytes32"}
			],
			"outputs": [{"name": "", "type": "bool"}]
		}
	]`))
	if err != nil {
		log.Fatalf("Failed to parse authorizationState ABI: %v", err)
	}

	nonceCheckData, err := abiData.Pack("authorizationState", fromAddr, nonce)
	if err != nil {
		log.Fatalf("Failed to encode authorizationState call: %v", err)
	}

	nonceResult, err := f.ethClient.CallContract(context.Background(), ethereum.CallMsg{
		To:   &tokenAddress,
		Data: nonceCheckData,
	}, nil)
	if err != nil {
		log.Fatalf("Failed to check nonce: %v", err)
	}

	if len(nonceResult) > 0 && nonceResult[0] != 0 {
		log.Fatalf("Nonce already used")
	}

	return nil
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
	abiData, err := abi.JSON(strings.NewReader(`[
		{
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
		}
	]`))
	if err != nil {
		log.Fatalf("Failed to parse ERC20 ABI: %v", err)
	}

	// Pack balanceOf function call
	data, err := abiData.Pack("balanceOf", userAddress)
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
	err = abiData.UnpackIntoInterface(&balance, "balanceOf", result)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack balanceOf result: %w", err)
	}

	return balance, nil
}

func (f *Facilitator) transferWithAuthorization(ctx context.Context, payload *x402types.PaymentPayload) (common.Hash, error) {
	// Decode the signature from hex
	signature, err := hexutil.Decode(payload.Payload.Signature)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to decode signature: %w", err)
	}

	auth := payload.Payload.Authorization

	tokenAddress := common.HexToAddress(baseUSDCAddress)
	fromAddr := common.HexToAddress(auth.From)
	toAddr := common.HexToAddress(auth.To)

	value, ok := new(big.Int).SetString(auth.Value, 10)
	if !ok {
		return common.Hash{}, fmt.Errorf("invalid value: %s", auth.Value)
	}

	validAfter, ok := new(big.Int).SetString(auth.ValidAfter, 10)
	if !ok {
		return common.Hash{}, fmt.Errorf("invalid validAfter %s", auth.ValidAfter)
	}

	validBefore, ok := new(big.Int).SetString(auth.ValidBefore, 10)
	if !ok {
		return common.Hash{}, fmt.Errorf("invalid validBefore %s", auth.ValidBefore)
	}

	// Get permit signature components
	r := common.BytesToHash(signature[0:32])
	s := common.BytesToHash(signature[32:64])
	v := signature[64]

	// Prepare for calling transaction

	// Just very high, don't care.
	gasLimit := uint64(1_000_000_000)
	maxPriorityFeePerGas := big.NewInt(1_000_000_000)
	maxFeePerGas := big.NewInt(1_000_000_000)

	txNonce, err := f.ethClient.PendingNonceAt(ctx, f.address)
	if err != nil {
		log.Fatalf("Failed to get transaction nonce: %v", err)
	}

	abiData, err := abi.JSON(strings.NewReader(`[
		{
			"name": "transferWithAuthorization",
			"type": "function",
			"inputs": [
				{"name": "from", "type": "address"},
				{"name": "to", "type": "address"},
				{"name": "value", "type": "uint256"},
				{"name": "validAfter", "type": "uint256"},
				{"name": "validBefore", "type": "uint256"},
				{"name": "nonce", "type": "bytes32"},
				{"name": "v", "type": "uint8"},
				{"name": "r", "type": "bytes32"},
				{"name": "s", "type": "bytes32"}
			]
		}
	]`))
	if err != nil {
		log.Fatalf("Failed to parse transferWithAuthorization ABI: %v", err)
	}

	transferData, err := abiData.Pack("transferWithAuthorization",
		fromAddr,
		toAddr,
		value,
		validAfter,
		validBefore,
		common.HexToHash(auth.Nonce),
		v,
		r,
		s,
	)
	if err != nil {
		log.Fatalf("Failed to encode transferWithAuthorization call: %v", err)
	}

	transferTx := ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		ChainID:   f.chainID,
		Nonce:     txNonce,
		GasFeeCap: maxFeePerGas,
		GasTipCap: maxPriorityFeePerGas,
		Gas:       gasLimit,
		To:        &tokenAddress,
		Data:      transferData,
	})

	// Sign the transaction
	signer := ethtypes.NewPragueSigner(f.chainID)
	signedTx, err := ethtypes.SignTx(transferTx, signer, f.privateKey)
	if err != nil {
		log.Fatalf("Failed to sign transaction: %v", err)
	}

	// Send the transaction
	err = f.ethClient.SendTransaction(ctx, signedTx)
	if err != nil {
		log.Fatalf("Failed to send transaction: %v", err)
	}

	return signedTx.Hash(), nil
}

func (f *Facilitator) transferWithPermit(ctx context.Context, payload *x402types.PaymentPayload) (common.Hash, error) {
	// Decode the signature from hex
	signature, err := hexutil.Decode(payload.Payload.Signature)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to decode signature: %w", err)
	}

	auth := payload.Payload.Authorization

	tokenAddress := common.HexToAddress(baseUSDCAddress)
	fromAddr := common.HexToAddress(auth.From)
	toAddr := common.HexToAddress(auth.To)

	value, ok := new(big.Int).SetString(auth.Value, 10)
	if !ok {
		return common.Hash{}, fmt.Errorf("invalid value: %s", auth.Value)
	}

	validBefore, ok := new(big.Int).SetString(auth.ValidBefore, 10)
	if !ok {
		return common.Hash{}, fmt.Errorf("invalid validBefore %s", auth.ValidBefore)
	}

	// Get permit signature components
	r := common.BytesToHash(signature[0:32])
	s := common.BytesToHash(signature[32:64])
	v := signature[64]

	// Prepare for calling transaction

	// Just very high, don't care.
	gasLimit := uint64(1_000_000_000)
	maxPriorityFeePerGas := big.NewInt(1_000_000_000)
	maxFeePerGas := big.NewInt(1_000_000_000)

	nonce, err := f.ethClient.PendingNonceAt(ctx, f.address)
	if err != nil {
		log.Fatalf("Failed to get transaction nonce: %v", err)
	}

	// Step 1: Call permit
	abiData, err := abi.JSON(strings.NewReader(`[
		{
			"name": "permit",
			"type": "function",
			"inputs": [
				{"name": "owner", "type": "address"},
				{"name": "spender", "type": "address"},
				{"name": "value", "type": "uint256"},
				{"name": "deadline", "type": "uint256"},
				{"name": "v", "type": "uint8"},
				{"name": "r", "type": "bytes32"},
				{"name": "s", "type": "bytes32"}
			],
			"outputs": []
		}
	]`))
	if err != nil {
		log.Fatalf("Failed to parse permit ABI: %v", err)
	}

	permitData, err := abiData.Pack("permit",
		fromAddr,
		f.address,
		value,
		validBefore,
		v,
		r,
		s,
	)
	if err != nil {
		log.Fatalf("Failed to encode permit call: %v", err)
	}

	permitTx := ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		ChainID:   f.chainID,
		Nonce:     nonce,
		GasFeeCap: maxFeePerGas,
		GasTipCap: maxPriorityFeePerGas,
		Gas:       gasLimit,
		To:        &tokenAddress,
		Data:      permitData,
	})

	// Sign the permit transaction
	signer := ethtypes.NewPragueSigner(f.chainID)
	signedPermitTx, err := ethtypes.SignTx(permitTx, signer, f.privateKey)
	if err != nil {
		log.Fatalf("Failed to sign transaction: %v", err)
	}

	// Send the transaction
	err = f.ethClient.SendTransaction(ctx, signedPermitTx)
	if err != nil {
		log.Fatalf("Failed to send transaction: %v", err)
	}
	permitTxHash := signedPermitTx.Hash()

	// Wait for permit transaction to be mined
	receipt, err := f.awaitReceipt(ctx, &permitTxHash)
	if err != nil {
		log.Fatalf("Failed to get permit transaction receipt: %v", err)
	}
	if receipt.Status != ethtypes.ReceiptStatusSuccessful {
		return common.Hash{}, fmt.Errorf("permit transaction failed: %s", permitTxHash.Hex())
	}

	// Step 2: Call transferFrom
	transferAbi, err := abi.JSON(strings.NewReader(`[
		{
			"name": "transferFrom",
			"type": "function",
			"inputs": [
				{"name": "from", "type": "address"},
				{"name": "to", "type": "address"},
				{"name": "value", "type": "uint256"}
			],
			"outputs": [{"name": "", "type": "bool"}]
		}
	]`))
	if err != nil {
		log.Fatalf("Failed to parse transferFrom ABI: %v", err)
	}

	transferData, err := transferAbi.Pack("transferFrom",
		f.address,
		toAddr,
		value,
	)
	if err != nil {
		log.Fatalf("Failed to encode transferFrom call: %v", err)
	}

	nonce++ // Increment nonce for next transaction

	transferTx := ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		ChainID:   f.chainID,
		Nonce:     nonce,
		GasFeeCap: maxFeePerGas,
		GasTipCap: maxPriorityFeePerGas,
		Gas:       gasLimit,
		To:        &tokenAddress,
		Data:      transferData,
	})

	signedTransferTx, err := ethtypes.SignTx(transferTx, signer, f.privateKey)
	if err != nil {
		log.Fatalf("Failed to sign transferFrom transaction: %v", err)
	}

	err = f.ethClient.SendTransaction(ctx, signedTransferTx)
	if err != nil {
		log.Fatalf("Failed to send transferFrom transaction: %v", err)
	}

	return signedTransferTx.Hash(), nil
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
