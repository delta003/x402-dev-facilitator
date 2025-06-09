#!/usr/bin/env python3
"""
Python client example that implements the same functionality as the Go client example.
"""

import base64
import json
import os
import sys

from dotenv import load_dotenv

# Add the parent directory to the path to import the client module
sys.path.append(os.path.join(os.path.dirname(__file__), ".."))

from pyx402 import Client


class ExampleClient:
    """Example client that wraps the x402 client with server-specific functionality"""

    def __init__(self, server_url: str, private_key: str, chain_id: int):
        """
        Initialize the example client.

        Args:
            server_url: URL of the server to connect to
            private_key: Ethereum private key
            chain_id: Blockchain chain ID
        """
        self.server_url = server_url
        self.client = Client(private_key, chain_id)

        # Set max value to 100 USDC (6 decimals) to match Go example
        self.client.set_max_value(100_000_000)

    def tip(self) -> None:
        """Make a tip request to the server, similar to Go example"""
        url = f"{self.server_url}/tip"

        try:
            response = self.client.get(url)
        except Exception as e:
            raise Exception(f"Failed to make request: {e}")

        try:
            body = response.text
        except Exception:
            body = ""

        if response.status_code != 200:
            raise Exception(f"Unexpected status code: {response.status_code}, body: {body}")

        print(f"Received tip response: {body}")

        # Decode and print X-PAYMENT-RESPONSE header if it exists
        payment_response_header = response.headers.get("X-PAYMENT-RESPONSE")
        if payment_response_header:
            print("X-PAYMENT-RESPONSE header found")

            try:
                # Try to decode base64
                decoded = base64.b64decode(payment_response_header).decode("utf-8")
                print(f"Decoded payment response: {decoded}")

                try:
                    # Try to parse as JSON
                    payment_response = json.loads(decoded)
                    print("Parsed payment response:")
                    for key, value in payment_response.items():
                        print(f"  {key}: {value}")
                except json.JSONDecodeError as e:
                    print(f"Could not parse payment response as JSON: {e}")

            except Exception as e:
                print(f"Could not decode payment response from base64: {e}")
                print(f"Raw payment response: {payment_response_header}")
        else:
            print("No X-PAYMENT-RESPONSE header found")


def main():
    """Main function that loads environment variables and runs the client"""
    # Load environment variables
    load_dotenv()

    server_url = os.getenv("SERVER_URL")
    if not server_url:
        print("Error: SERVER_URL environment variable is not set", file=sys.stderr)
        sys.exit(1)

    private_key = os.getenv("WALLET_PRIVATE_KEY")
    if not private_key:
        print("Error: WALLET_PRIVATE_KEY environment variable is not set", file=sys.stderr)
        sys.exit(1)

    chain_id_str = os.getenv("CHAIN_ID")
    if not chain_id_str:
        print("Error: CHAIN_ID environment variable is not set", file=sys.stderr)
        sys.exit(1)

    try:
        chain_id = int(chain_id_str)
    except ValueError as e:
        print(f"Error: Invalid CHAIN_ID: {e}", file=sys.stderr)
        sys.exit(1)

    try:
        client = ExampleClient(server_url, private_key, chain_id)
        client.tip()
    except Exception as e:
        print(f"Error: Failed to tip: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
