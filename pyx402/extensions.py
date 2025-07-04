"""
Extensions to x402 protocol - Python implementation
"""

from dataclasses import dataclass


@dataclass
class ExactEvmReceipt:
    """Receipt for an exact EVM payment"""
    # Transaction must be successful
    transaction: str
    # Signature is used to verify the ownership of the receipt
    signature: str


@dataclass
class ReceiptPayload:
    """Receipt payload for x402 protocol extension"""
    x402Version: int
    scheme: str
    network: str
    payload: ExactEvmReceipt