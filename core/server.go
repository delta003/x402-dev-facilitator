package core

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/coinbase/x402/go/pkg/facilitatorclient"
	"github.com/coinbase/x402/go/pkg/types"
)

// PaymentMiddlewareOptions is the options for the PaymentMiddleware.
type PaymentMiddlewareOptions struct {
	Description       string
	MimeType          string
	MaxTimeoutSeconds int
	OutputSchema      *json.RawMessage
	FacilitatorConfig *types.FacilitatorConfig
	Testnet           bool
	CustomPaywallHTML string
	Resource          string
	ResourceRootURL   string
	// Receipt-specific options
	EnableReceipts bool
}

// Options is the type for the options for the PaymentMiddleware.
type Options func(*PaymentMiddlewareOptions)

// WithDescription is an option for the PaymentMiddleware to set the description.
func WithDescription(description string) Options {
	return func(options *PaymentMiddlewareOptions) {
		options.Description = description
	}
}

// WithMimeType is an option for the PaymentMiddleware to set the mime type.
func WithMimeType(mimeType string) Options {
	return func(options *PaymentMiddlewareOptions) {
		options.MimeType = mimeType
	}
}

// WithMaxDeadlineSeconds is an option for the PaymentMiddleware to set the max timeout seconds.
func WithMaxTimeoutSeconds(maxTimeoutSeconds int) Options {
	return func(options *PaymentMiddlewareOptions) {
		options.MaxTimeoutSeconds = maxTimeoutSeconds
	}
}

// WithOutputSchema is an option for the PaymentMiddleware to set the output schema.
func WithOutputSchema(outputSchema *json.RawMessage) Options {
	return func(options *PaymentMiddlewareOptions) {
		options.OutputSchema = outputSchema
	}
}

// WithFacilitatorConfig is an option for the PaymentMiddleware to set the facilitator config.
func WithFacilitatorConfig(config *types.FacilitatorConfig) Options {
	return func(options *PaymentMiddlewareOptions) {
		options.FacilitatorConfig = config
	}
}

// WithTestnet is an option for the PaymentMiddleware to set the testnet flag.
func WithTestnet(testnet bool) Options {
	return func(options *PaymentMiddlewareOptions) {
		options.Testnet = testnet
	}
}

// WithCustomPaywallHTML is an option for the PaymentMiddleware to set the custom paywall HTML.
func WithCustomPaywallHTML(customPaywallHTML string) Options {
	return func(options *PaymentMiddlewareOptions) {
		options.CustomPaywallHTML = customPaywallHTML
	}
}

// WithResource is an option for the PaymentMiddleware to set the resource.
func WithResource(resource string) Options {
	return func(options *PaymentMiddlewareOptions) {
		options.Resource = resource
	}
}

func WithResourceRootURL(resourceRootURL string) Options {
	return func(options *PaymentMiddlewareOptions) {
		options.ResourceRootURL = resourceRootURL
	}
}

// WithEnableReceipts is an option for the PaymentMiddleware to enable receipt verification.
func WithEnableReceipts(enableReceipts bool) Options {
	return func(options *PaymentMiddlewareOptions) {
		options.EnableReceipts = enableReceipts
	}
}

// PaymentMiddleware is the Gin middleware for the resource server using the x402payment protocol.
// Amount: the decimal denominated amount to charge (ex: 0.01 for 1 cent)
func PaymentMiddleware(amount *big.Float, address string, opts ...Options) gin.HandlerFunc {
	options := &PaymentMiddlewareOptions{
		FacilitatorConfig: &types.FacilitatorConfig{
			URL: facilitatorclient.DefaultFacilitatorURL,
		},
		MaxTimeoutSeconds: 60,
		Testnet:           true,
		EnableReceipts:    true, // Enable receipts by default
	}

	for _, opt := range opts {
		opt(options)
	}

	return func(c *gin.Context) {
		var (
			network              = "base"
			usdcAddress          = "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"
			facilitatorClient    = facilitatorclient.NewFacilitatorClient(options.FacilitatorConfig)
			maxAmountRequired, _ = new(big.Float).Mul(amount, big.NewFloat(1e6)).Int(nil)
		)

		if options.Testnet {
			network = "base-sepolia"
			usdcAddress = "0x036CbD53842c5426634e7929541eC2318f3dCF7e"
		}

		fmt.Println("Payment middleware checking request:", c.Request.URL)

		userAgent := c.GetHeader("User-Agent")
		acceptHeader := c.GetHeader("Accept")
		isWebBrowser := strings.Contains(acceptHeader, "text/html") && strings.Contains(userAgent, "Mozilla")
		var resource string
		if options.Resource == "" {
			resource = options.ResourceRootURL + c.Request.URL.Path
		} else {
			resource = options.Resource
		}

		paymentRequirements := &types.PaymentRequirements{
			Scheme:            "exact",
			Network:           network,
			MaxAmountRequired: maxAmountRequired.String(),
			Resource:          resource,
			Description:       options.Description,
			MimeType:          options.MimeType,
			PayTo:             address,
			MaxTimeoutSeconds: options.MaxTimeoutSeconds,
			Asset:             usdcAddress,
			OutputSchema:      options.OutputSchema,
			Extra:             nil,
		}

		if err := paymentRequirements.SetUSDCInfo(options.Testnet); err != nil {
			fmt.Println("failed to set USDC info:", err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error":       err.Error(),
				"x402Version": x402Version,
			})
			return
		}

		payment := c.GetHeader("X-PAYMENT")
		if payment == "" {
			if isWebBrowser {
				html := options.CustomPaywallHTML
				if html == "" {
					html = getPaywallHtml(options)
				}
				c.Abort()
				c.Data(http.StatusPaymentRequired, "text/html", []byte(html))
				return
			}

			c.AbortWithStatusJSON(http.StatusPaymentRequired, gin.H{
				"error":       "X-PAYMENT header is required",
				"accepts":     []*types.PaymentRequirements{paymentRequirements},
				"x402Version": x402Version,
			})
			return
		}

		// Decode the base64 payment to determine if it's a receipt or payment
		decoded, err := base64.StdEncoding.DecodeString(payment)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error":       "Invalid base64 encoding in X-PAYMENT header",
				"x402Version": x402Version,
			})
			return
		}

		var response *types.VerifyResponse
		var paymentPayload *types.PaymentPayload
		isReceipt := false

		// Determine if this is a receipt or payment payload
		if isReceiptPayload(decoded) {
			// Check if receipts are allowed
			if !options.EnableReceipts {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
					"error":       "Receipts are not accepted",
					"x402Version": x402Version,
				})
				return
			}

			fmt.Println("Receipt payload detected, verifying receipt")
			isReceipt = true

			// Decode as receipt payload
			receiptPayload, err := DecodeReceiptPayloadFromBase64(payment)
			if err != nil {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
					"error":       "Invalid receipt payload",
					"x402Version": x402Version,
				})
				return
			}
			receiptPayload.X402Version = x402Version

			// Verify receipt with facilitator
			response, err = verifyReceiptWithFacilitator(options.FacilitatorConfig.URL, receiptPayload, paymentRequirements)
			if err != nil {
				fmt.Println("failed to verify receipt", err)
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error":       err.Error(),
					"x402Version": x402Version,
				})
				return
			}

			if !response.IsValid {
				fmt.Println("Invalid receipt: ", response.InvalidReason)
				c.AbortWithStatusJSON(http.StatusPaymentRequired, gin.H{
					"error":       response.InvalidReason,
					"accepts":     []*types.PaymentRequirements{paymentRequirements},
					"x402Version": x402Version,
				})
				return
			}

			fmt.Println("Receipt verified, proceeding")
		} else {
			fmt.Println("Payment payload detected, verifying payment")

			// Decode as payment payload
			var err error
			paymentPayload, err = types.DecodePaymentPayloadFromBase64(payment)
			if err != nil {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
					"error":       "Invalid payment payload",
					"x402Version": x402Version,
				})
				return
			}
			paymentPayload.X402Version = x402Version

			// Verify payment
			response, err = facilitatorClient.Verify(paymentPayload, paymentRequirements)
			if err != nil {
				fmt.Println("failed to verify payment", err)
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error":       err.Error(),
					"x402Version": x402Version,
				})
				return
			}

			if !response.IsValid {
				fmt.Println("Invalid payment: ", response.InvalidReason)
				c.AbortWithStatusJSON(http.StatusPaymentRequired, gin.H{
					"error":       response.InvalidReason,
					"accepts":     []*types.PaymentRequirements{paymentRequirements},
					"x402Version": x402Version,
				})
				return
			}

			fmt.Println("Payment verified, proceeding")
		}

		// For receipts, just let the request through
		if isReceipt {
			c.Next()
			return
		}

		// For payments, we need to handle settlement after the request completes
		// Create a custom response writer to intercept the response
		writer := &responseWriter{
			ResponseWriter: c.Writer,
			body:           &strings.Builder{},
			statusCode:     http.StatusOK,
		}
		c.Writer = writer

		// Execute the handler
		c.Next()

		// Check if the handler was aborted
		if c.IsAborted() {
			return
		}

		// Settle payment
		settleResponse, err := facilitatorClient.Settle(paymentPayload, paymentRequirements)
		if err != nil {
			fmt.Println("Settlement failed:", err)
			// Reset the response writer
			c.Writer = writer.ResponseWriter
			c.AbortWithStatusJSON(http.StatusPaymentRequired, gin.H{
				"error":       err.Error(),
				"accepts":     []*types.PaymentRequirements{paymentRequirements},
				"x402Version": x402Version,
			})
			return
		}

		settleResponseHeader, err := settleResponse.EncodeToBase64String()
		if err != nil {
			fmt.Println("Settle Header Encoding failed:", err)
			// Reset the response writer
			c.Writer = writer.ResponseWriter
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error":       err.Error(),
				"x402Version": x402Version,
			})
			return
		}

		// Write the original response with the settlement header
		c.Header("X-PAYMENT-RESPONSE", settleResponseHeader)
		// Reset the response writer to the original
		c.Writer = writer.ResponseWriter
		c.Writer.WriteHeader(writer.statusCode)
		c.Writer.Write([]byte(writer.body.String()))
	}
}

// responseWriter is a custom response writer that captures the response
type responseWriter struct {
	gin.ResponseWriter
	body       *strings.Builder
	statusCode int
	written    bool
}

func (w *responseWriter) WriteHeader(code int) {
	if !w.written {
		w.statusCode = code
		w.written = true
	}
}

func (w *responseWriter) Write(b []byte) (int, error) {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}
	w.body.Write(b)
	return len(b), nil
}

func (w *responseWriter) WriteString(s string) (int, error) {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}
	return w.body.WriteString(s)
}

// getPaywallHtml is the default paywall HTML for the PaymentMiddleware.
func getPaywallHtml(_ *PaymentMiddlewareOptions) string {
	return "<html><body>Payment Required</body></html>"
}

// DecodeReceiptPayloadFromBase64 decodes a base64 encoded receipt payload
func DecodeReceiptPayloadFromBase64(encoded string) (*ReceiptPayload, error) {
	if encoded == "" {
		return nil, fmt.Errorf("encoded receipt payload is empty")
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64: %w", err)
	}

	var payload ReceiptPayload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal receipt payload: %w", err)
	}

	return &payload, nil
}

// isReceiptPayload determines if the payload is a receipt payload based on its structure
func isReceiptPayload(payloadBytes []byte) bool {
	var payload map[string]interface{}
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return false
	}

	// Check if the payload has receipt-specific fields
	if payloadObj, ok := payload["payload"].(map[string]interface{}); ok {
		// Receipt payloads have "transaction" and "signature" fields
		_, hasTransaction := payloadObj["transaction"]
		_, hasSignature := payloadObj["signature"]
		return hasTransaction && hasSignature
	}

	return false
}

// verifyReceiptWithFacilitator calls the facilitator's verify receipt endpoint
func verifyReceiptWithFacilitator(facilitatorURL string, receiptPayload *ReceiptPayload, paymentRequirements *types.PaymentRequirements) (*types.VerifyResponse, error) {
	// Create a request similar to the facilitator's VerifyReceiptHandler
	reqBody := struct {
		X402Version         int                        `json:"x402Version"`
		ReceiptPayload      *ReceiptPayload            `json:"receiptPayload"`
		PaymentRequirements *types.PaymentRequirements `json:"paymentRequirements"`
	}{
		X402Version:         x402Version,
		ReceiptPayload:      receiptPayload,
		PaymentRequirements: paymentRequirements,
	}

	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make HTTP request to the facilitator's verify-receipt endpoint
	url := strings.TrimSuffix(facilitatorURL, "/") + "/verify-receipt"
	resp, err := http.Post(url, "application/json", bytes.NewReader(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to call verify receipt endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("verify receipt failed with status %d", resp.StatusCode)
	}

	var verifyResponse types.VerifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&verifyResponse); err != nil {
		return nil, fmt.Errorf("failed to decode verify response: %w", err)
	}

	return &verifyResponse, nil
}

// getReceiptStringOrDefault returns the receipt value if not empty, otherwise returns the default value
func getReceiptStringOrDefault(receiptValue, defaultValue string) string {
	if receiptValue != "" {
		return receiptValue
	}
	return defaultValue
}

// getReceiptSchemaOrDefault returns the receipt schema if not nil, otherwise returns the default schema
func getReceiptSchemaOrDefault(receiptSchema, defaultSchema *json.RawMessage) *json.RawMessage {
	if receiptSchema != nil {
		return receiptSchema
	}
	return defaultSchema
}
