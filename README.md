# x402-dev-facilitator

Run the x402 facilitator on a custom network. Built to be used with Tenderly's virtual networks.

To run the facilitator, use `go run main.go` and set the environment variables in the `.env` file.
Example server can be run from `examples/server` with `go run main.go`.
Example client can be run from `examples/` with either `npm run client` using the Coinbase's `x402-fetch` implementation
or `go run client.go` using the Go implementation in this repository.

NOTE: This was built to understand x402 and is heavily AI-generated. Do not use any of it in production.
