"""
pyx402: Python implementation of x402 payment client

This package provides a Python client for handling x402 payments with automatic
402 Payment Required response handling and EIP-712 signature support.
"""

from .client import (
    BASE_NETWORK,
    BASE_USDC_ADDRESS,
    X402_VERSION,
    Client,
    ExactEvmPayload,
    ExactEvmPayloadAuthorization,
    PaymentPayload,
    PaymentRequirements,
    new_client_from_hex,
)

from .server import (
    PaymentMiddleware,
    PaymentMiddlewareOptions,
    VerifyResponse,
    detailed_logging_middleware,
    with_description,
    with_mime_type,
    with_max_timeout_seconds,
    with_output_schema,
    with_facilitator_config,
    with_testnet,
    with_custom_paywall_html,
    with_resource,
    with_resource_root_url,
    with_enable_receipts,
)

from .extensions import (
    ExactEvmReceipt,
    ReceiptPayload,
)

__version__ = "0.1.0"
__all__ = [
    # Client
    "Client",
    "PaymentRequirements",
    "ExactEvmPayloadAuthorization",
    "ExactEvmPayload",
    "PaymentPayload",
    "new_client_from_hex",
    # Server
    "PaymentMiddleware",
    "PaymentMiddlewareOptions",
    "VerifyResponse",
    "detailed_logging_middleware",
    "with_description",
    "with_mime_type",
    "with_max_timeout_seconds",
    "with_output_schema",
    "with_facilitator_config",
    "with_testnet",
    "with_custom_paywall_html",
    "with_resource",
    "with_resource_root_url",
    "with_enable_receipts",
    # Extensions
    "ExactEvmReceipt",
    "ReceiptPayload",
    # Constants
    "X402_VERSION",
    "BASE_USDC_ADDRESS",
    "BASE_NETWORK",
]
