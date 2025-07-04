#!/usr/bin/env python3
"""
FastAPI server example using pyx402 payment middleware

This is the Python equivalent of the Go server example.
"""

import logging
import os
from decimal import Decimal

import uvicorn
from dotenv import load_dotenv
from fastapi import FastAPI

from pyx402 import (
    PaymentMiddleware,
    detailed_logging_middleware,
    with_facilitator_config,
    with_resource,
    with_testnet,
)


def main():
    # Load environment variables from .env file
    load_dotenv()
    
    # Configure logging
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s - %(name)s - %(levelname)s - %(message)s"
    )
    
    # Get configuration from environment variables
    facilitator_url = os.getenv("FACILITATOR_URL")
    if not facilitator_url:
        raise ValueError("FACILITATOR_URL environment variable is not set")
    
    port = int(os.getenv("SERVER_PORT", "4021"))
    
    wallet_address = os.getenv("SERVER_WALLET_ADDRESS")
    if not wallet_address:
        raise ValueError("SERVER_WALLET_ADDRESS environment variable is not set")
    
    # Create FastAPI app
    app = FastAPI(title="x402 Payment Server", version="1.0.0")
    
    # Add detailed logging middleware
    app.add_middleware(detailed_logging_middleware())
    
    # Configure facilitator
    facilitator_config = {"url": facilitator_url}
    
    # Add payment middleware to the app
    # This creates a payment-protected endpoint that requires 42.0 USDC
    app.add_middleware(
        PaymentMiddleware,
        amount=Decimal("42.0"),
        address=wallet_address,
        **with_facilitator_config(facilitator_config),
        **with_testnet(False),
        **with_resource(f"http://localhost:{port}/tip"),
    )
    
    @app.get("/tip")
    async def tip():
        """
        Tip endpoint - requires payment of 42.0 USDC
        
        This endpoint is protected by the payment middleware and will only
        execute after successful payment verification and settlement.
        """
        return {"msg": "Thanks!"}
    
    @app.get("/health")
    async def health():
        """Health check endpoint - no payment required"""
        return {"status": "healthy", "service": "x402-payment-server"}
    
    # Run the server
    print(f"Starting server on port {port}")
    print(f"Facilitator URL: {facilitator_url}")
    print(f"Wallet address: {wallet_address}")
    print(f"Payment required: 42.0 USDC")
    print(f"Tip endpoint: http://localhost:{port}/tip")
    
    uvicorn.run(app, host="0.0.0.0", port=port)


if __name__ == "__main__":
    main()