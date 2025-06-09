"""
pyx402: Python implementation of x402 payment client

This package provides a Python client for handling x402 payments with automatic
402 Payment Required response handling and EIP-712 signature support.
"""

from .client import (
    Client,
    PaymentRequirements,
    ExactEvmPayloadAuthorization,
    ExactEvmPayload,
    PaymentPayload,
    new_client_from_hex,
    X402_VERSION,
    BASE_USDC_ADDRESS,
    BASE_NETWORK,
)

__version__ = "0.1.0"
__all__ = [
    "Client",
    "PaymentRequirements", 
    "ExactEvmPayloadAuthorization",
    "ExactEvmPayload",
    "PaymentPayload",
    "new_client_from_hex",
    "X402_VERSION",
    "BASE_USDC_ADDRESS", 
    "BASE_NETWORK",
]