"""
Python implementation of x402 payment client with the same logic as the Go client.
"""

import base64
import json
import time
from dataclasses import dataclass
from typing import Any

import requests
from eth_account import Account
from web3 import Web3

# Constants from Go implementation
X402_VERSION = 1
BASE_USDC_ADDRESS = "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"
BASE_NETWORK = "base"


@dataclass
class PaymentRequirements:
    """Payment requirements from 402 response"""

    scheme: str
    network: str
    pay_to: str
    max_amount_required: str
    asset: str


@dataclass
class ExactEvmPayloadAuthorization:
    """Authorization details for EVM payment"""

    from_addr: str
    to: str
    value: str
    valid_after: str
    valid_before: str
    nonce: Any  # Can be bytes or str


@dataclass
class ExactEvmPayload:
    """EVM payment payload"""

    signature: str
    authorization: ExactEvmPayloadAuthorization


@dataclass
class PaymentPayload:
    """Complete payment payload"""

    x402_version: int
    scheme: str
    network: str
    payload: ExactEvmPayload


class Client:
    """x402 payment client for Python"""

    def __init__(self, private_key: str, chain_id: int):
        """
        Initialize client with private key and chain ID.

        Args:
            private_key: Ethereum private key (with or without 0x prefix)
            chain_id: Blockchain chain ID
        """
        # Remove 0x prefix if present
        if private_key.startswith("0x"):
            private_key = private_key[2:]

        self.private_key = private_key
        self.account = Account.from_key(private_key)
        self.address = self.account.address
        self.chain_id = chain_id

        # Default to 0.1 USDC (6 decimals)
        self.max_value = 100000  # 0.1 * 10^6

        self.session = requests.Session()
        self.session.timeout = 30

    def set_max_value(self, max_value: int):
        """Set the maximum payment value allowed"""
        self.max_value = max_value

    def set_session(self, session: requests.Session):
        """Set a custom requests session"""
        self.session = session

    def _handle_payment_required(self, response: requests.Response, original_request_args: dict) -> requests.Response:
        """Handle 402 Payment Required response"""
        try:
            req_data = response.json()
        except json.JSONDecodeError as e:
            raise Exception(f"Failed to parse 402 response: {e}")

        x402_version = req_data.get("x402Version")
        if x402_version != X402_VERSION:
            raise Exception(f"Unsupported x402 version: {x402_version}, expected: {X402_VERSION}")

        accepts = req_data.get("accepts", [])
        if not accepts:
            raise Exception("No payment requirements provided in 402 response")

        # Use the first payment requirement
        payment_req_data = accepts[0]
        payment_requirements = PaymentRequirements(
            scheme=payment_req_data["scheme"],
            network=payment_req_data["network"],
            pay_to=payment_req_data["payTo"],
            max_amount_required=payment_req_data["maxAmountRequired"],
            asset=payment_req_data.get("asset", BASE_USDC_ADDRESS),
        )

        # Validate amount against maximum
        max_amount = int(payment_requirements.max_amount_required)
        if max_amount > self.max_value:
            raise Exception(f"Payment amount {max_amount} exceeds maximum allowed {self.max_value}")

        # Create payment header
        payment_header = self._create_payment_header(payment_requirements)

        # Clone the original request with payment header
        new_headers = original_request_args.get("headers", {}).copy()
        new_headers["X-PAYMENT"] = payment_header
        new_headers["X-Payment-Retry"] = "true"
        new_headers["Access-Control-Expose-Headers"] = "X-PAYMENT-RESPONSE"

        # Make the request with payment
        new_args = original_request_args.copy()
        new_args["headers"] = new_headers

        return self.session.request(**new_args)

    def _create_payment_header(self, requirements: PaymentRequirements) -> str:
        """Create the payment header"""
        # Create authorization
        auth = self._create_authorization(requirements)

        # Sign the authorization
        signature = self._sign_authorization(auth)

        # Create payment payload
        payload = PaymentPayload(
            x402_version=X402_VERSION,
            scheme=requirements.scheme,
            network=requirements.network,
            payload=ExactEvmPayload(signature=signature, authorization=auth),
        )

        # Convert to dict for JSON serialization
        payload_dict = {
            "x402Version": payload.x402_version,
            "scheme": payload.scheme,
            "network": payload.network,
            "payload": {
                "signature": payload.payload.signature,
                "authorization": {
                    "from": payload.payload.authorization.from_addr,
                    "to": payload.payload.authorization.to,
                    "value": payload.payload.authorization.value,
                    "validAfter": payload.payload.authorization.valid_after,
                    "validBefore": payload.payload.authorization.valid_before,
                    "nonce": ("0x" + payload.payload.authorization.nonce.hex())
                    if isinstance(payload.payload.authorization.nonce, bytes)
                    else payload.payload.authorization.nonce,
                },
            },
        }

        # Encode as JSON and base64
        payload_bytes = json.dumps(payload_dict).encode("utf-8")
        return base64.b64encode(payload_bytes).decode("utf-8")

    def _create_authorization(self, requirements: PaymentRequirements) -> ExactEvmPayloadAuthorization:
        """Create the authorization for payment"""
        now = int(time.time())

        # Generate a random nonce using keccak256 hash
        nonce_input = f"{now}-{self.address}".encode()
        nonce = Web3.keccak(nonce_input)

        return ExactEvmPayloadAuthorization(
            from_addr=self.address,
            to=requirements.pay_to,
            value=requirements.max_amount_required,
            valid_after=str(now - 60),  # Valid from 1 minute ago
            valid_before=str(now + 3600),  # Valid for 1 hour
            nonce=nonce,
        )

    def _sign_authorization(self, auth: ExactEvmPayloadAuthorization) -> str:
        """Sign the authorization using EIP-712"""
        # Domain data
        domain_data = {
            "name": "USD Coin",
            "version": "2",
            "chainId": self.chain_id,
            "verifyingContract": BASE_USDC_ADDRESS,
        }

        # Message types (do not include EIP712Domain)
        message_types = {
            "TransferWithAuthorization": [
                {"name": "from", "type": "address"},
                {"name": "to", "type": "address"},
                {"name": "value", "type": "uint256"},
                {"name": "validAfter", "type": "uint256"},
                {"name": "validBefore", "type": "uint256"},
                {"name": "nonce", "type": "bytes32"},
            ],
        }

        # Message data
        message_data = {
            "from": auth.from_addr,
            "to": auth.to,
            "value": int(auth.value),
            "validAfter": int(auth.valid_after),
            "validBefore": int(auth.valid_before),
            "nonce": auth.nonce,
        }

        # Sign the typed data directly using Account.sign_typed_data
        signed_message = Account.sign_typed_data(self.account.key, domain_data, message_types, message_data)

        return "0x" + signed_message.signature.hex()

    def request(self, method: str, url: str, **kwargs) -> requests.Response:
        """Make an HTTP request with automatic payment handling"""
        # Check if this is already a retry to avoid infinite loops
        headers = kwargs.get("headers", {})
        if headers.get("X-Payment-Retry") == "true":
            return self.session.request(method, url, **kwargs)

        # Make the initial request
        response = self.session.request(method, url, **kwargs)

        # If not a 402 Payment Required, return the response as is
        if response.status_code != 402:
            return response

        # Handle 402 Payment Required
        return self._handle_payment_required(response, {"method": method, "url": url, **kwargs})

    def get(self, url: str, **kwargs) -> requests.Response:
        """Make a GET request with automatic payment handling"""
        return self.request("GET", url, **kwargs)

    def post(self, url: str, **kwargs) -> requests.Response:
        """Make a POST request with automatic payment handling"""
        return self.request("POST", url, **kwargs)

    def post_json(self, url: str, data: Any, **kwargs) -> requests.Response:
        """Make a POST request with JSON body and automatic payment handling"""
        headers = kwargs.get("headers", {})
        headers["Content-Type"] = "application/json"
        kwargs["headers"] = headers
        kwargs["data"] = json.dumps(data)
        return self.post(url, **kwargs)


def new_client_from_hex(private_key_hex: str, chain_id: int) -> Client:
    """Helper function to create a client from private key hex string"""
    return Client(private_key_hex, chain_id)
