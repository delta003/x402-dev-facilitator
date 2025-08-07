import os
import random
import sys

import httpx
from dotenv import load_dotenv


def main():
    load_dotenv()

    tenderly_api_key = os.getenv("TENDERLY_API_KEY")
    if not tenderly_api_key:
        print("Error: TENDERLY_API_KEY environment variable is not set", file=sys.stderr)
        sys.exit(1)
    tenderly_org = os.getenv("TENDERLY_ORG")
    if not tenderly_org:
        print("Error: TENDERLY_ORG environment variable is not set", file=sys.stderr)
        sys.exit(1)
    tenderly_project = os.getenv("TENDERLY_PROJECT")
    if not tenderly_project:
        print("Error: TENDERLY_PROJECT environment variable is not set", file=sys.stderr)
        sys.exit(1)
    server_wallet_address = os.getenv("SERVER_WALLET_ADDRESS")
    if not server_wallet_address:
        print("Error: SERVER_WALLET_ADDRESS environment variable is not set", file=sys.stderr)
        sys.exit(1)
    client_wallet_address = os.getenv("CLIENT_WALLET_ADDRESS")
    if not client_wallet_address:
        print("Error: CLIENT_WALLET_ADDRESS environment variable is not set", file=sys.stderr)
        sys.exit(1)
    facilitator_wallet_address = os.getenv("FACILITATOR_WALLET_ADDRESS")
    if not facilitator_wallet_address:
        print("Error: FACILITATOR_WALLET_ADDRESS environment variable is not set", file=sys.stderr)
        sys.exit(1)

    client = httpx.Client(
        base_url=f"https://api.tenderly.co/api/v1/account/{tenderly_org}/project/{tenderly_project}",
        headers={"X-Access-Key": tenderly_api_key},
    )

    # Create the virtual network
    vnet_response = client.post(
        "/testnet/container",
        json={
            "slug": f"xyz-base-{random.randint(100000, 999999)}",
            "displayName": f"xyz-base-{random.randint(100000, 999999)}",
            "description": "",
            "visibility": "TEAM",
            "tags": {"purpose": "development"},
            "networkConfig": {
                "networkId": "8453",
                "blockNumber": "latest",
                "chainConfig": {"chainId": "8453"},
                "baseFeePerGas": "1",
            },
            "explorerConfig": {"enabled": False, "verificationVisibility": "bytecode"},
            "syncState": False,
        },
    )
    if vnet_response.status_code != 200:
        print(f"Error creating virtual network: {vnet_response.status_code} {vnet_response.text}", file=sys.stderr)
        sys.exit(1)
    vnet_data = vnet_response.json()
    vnet_id = vnet_data["container"]["id"]

    endpoints = vnet_data["container"]["connectivityConfig"]["endpoints"]
    rpc_uri = None
    for endpoint in endpoints:
        if str(endpoint["description"]).lower() == "admin endpoint":
            rpc_uri = endpoint["uri"]
            break
    if not rpc_uri:
        print("Error: Admin endpoint not found in virtual network response", file=sys.stderr)
        sys.exit(1)

    print(f"Virtual network created with ID: {vnet_id}")
    print(f"RPC URI: {rpc_uri}")

    # Add wallets
    resp = client.post(
        f"/testnet/{vnet_id}/accounts",
        json={
            "account_type": "wallet",
            "address": client_wallet_address,
            "display_name": "Client",
            "watched": True,
        },
    )
    if resp.status_code != 200:
        print(f"Error adding client wallet: {resp.status_code} {resp.text}", file=sys.stderr)
        sys.exit(1)
    print(f"Client wallet added: {client_wallet_address}")
    resp = client.post(
        f"/testnet/{vnet_id}/accounts",
        json={
            "account_type": "wallet",
            "address": server_wallet_address,
            "display_name": "Server",
            "watched": True,
        },
    )
    if resp.status_code != 200:
        print(f"Error adding server wallet: {resp.status_code} {resp.text}", file=sys.stderr)
        sys.exit(1)
    print(f"Server wallet added: {server_wallet_address}")
    resp = client.post(
        f"/testnet/{vnet_id}/accounts",
        json={
            "account_type": "wallet",
            "address": facilitator_wallet_address,
            "display_name": "Facilitator",
            "watched": True,
        },
    )
    if resp.status_code != 200:
        print(f"Error adding facilitator wallet: {resp.status_code} {resp.text}", file=sys.stderr)
        sys.exit(1)

    # Top up wallets

    rcp_client = httpx.Client(
        base_url=rpc_uri,
        headers={"X-Access-Key": tenderly_api_key},
        # RPC endpoints redirect...
        follow_redirects=True,
    )

    # Add ETH to facilitator wallet
    resp = rcp_client.post(
        url="/",
        json={
            "method": "tenderly_addBalance",
            "params": [[facilitator_wallet_address], "0x21e19e0c9bab2400000"],
            "id": 4,
            "jsonrpc": "2.0",
        },
    )
    if resp.status_code != 200:
        print(f"Error topping up facilitator wallet: {resp.status_code} {resp.text}", file=sys.stderr)
        sys.exit(1)

    # Add USDC to client and server wallets
    resp = rcp_client.post(
        url="/",
        json={
            "method": "tenderly_addErc20Balance",
            "params": [
                "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913",
                [client_wallet_address],
                "0x2540be400",
            ],
            "id": 3,
            "jsonrpc": "2.0",
        },
    )
    if resp.status_code != 200:
        print(f"Error topping up client wallet: {resp.status_code} {resp.text}", file=sys.stderr)
        sys.exit(1)

    print(f"Client wallet topped up: {client_wallet_address}")

    resp = rcp_client.post(
        url="/",
        json={
            "method": "tenderly_addErc20Balance",
            "params": [
                "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913",
                [server_wallet_address],
                "0x2540be400",
            ],
            "id": 3,
            "jsonrpc": "2.0",
        },
    )
    if resp.status_code != 200:
        print(f"Error topping up server wallet: {resp.status_code} {resp.text}", file=sys.stderr)
        sys.exit(1)

    print(f"Server wallet topped up: {server_wallet_address}")

    print(f"Facilitator wallet topped up: {facilitator_wallet_address}")


if __name__ == "__main__":
    main()
