# x402-dev-facilitator

Run the [x402](https://www.x402.org/) facilitator on a custom network.

Built to be used with Tenderly's virtual networks - using testnets sucks!.

To run the facilitator, use `go run main.go` and set the environment variables in the `.env` file.
Example server can be run with `go run examples/server/server.go`. Example client can be run with `go run examples/client.go` or `uv run examples/client.py`.

https://github.com/delta003/x402-dev-facilitator/pull/1 extends the x402 protocol with verifiable receipt as an acceptable
`X-PAYMENT` header. This means the client can settle payments, through a facilitator or not, while the server can use a
facilitator to abstract blockchain interaction when validating a receipt.

NOTE: This was built to understand x402 and is heavily AI-generated. Do not use any of it in production.
