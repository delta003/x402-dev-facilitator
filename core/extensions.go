package core

// ReceiptPayload is an extension to x402 protocol.
//
// In the protocol, the server handles the payment. This works well for most cases,
// and especially for micropayments but falls short when both the client and server
// need to agree on the payment being processed. x402 protocol leaves no room for
// dispute as the client blindly trusts the server to provide the service on successful
// settlement and the client might not even have a necessary reference to being a dispute.
//
// Receipt protocol enables the client to settle the payment on its own, with or without
// the facilitator, and provide a server with a verifiable receipt of the payment.
// Similarly to the payment, to abstract an interaction with the blockchain, facilitator
// provides a verify-receipt endpoint. It's server's responsibility to ensure that the
// verified receipt isn't reused for another payment request.
type ReceiptPayload struct {
	X402Version int              `json:"x402Version"`
	Scheme      string           `json:"scheme"`
	Network     string           `json:"network"`
	Payload     *ExactEvmReceipt `json:"payload"`
}

// ExactEvmReceipt represents the receipt for an exact EVM payment
type ExactEvmReceipt struct {
	// Transaction must be successful.
	Transaction string `json:"transaction"`
	// Signature is used to verify the ownership of the receipt.
	//
	// A client must have been able to sign the given transaction,
	// in order to be considered an owner of the receipt.
	Signature string `json:"signature"`
}
