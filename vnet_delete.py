import os
import sys

import httpx
from dotenv import load_dotenv


def main():
    load_dotenv()

    # Get VNET ID from args
    vnet_id = sys.argv[1] if len(sys.argv) > 1 else None
    if not vnet_id:
        print("Error: VNET ID must be provided as the first argument", file=sys.stderr)
        sys.exit(1)

    tenderly_api_key = os.getenv("TENDERLY_API_KEY")
    if not tenderly_api_key:
        print("Error: TENDERLY_API_KEY environment variable is not set", file=sys.stderr)
        sys.exit(1)
    tenderly_api_url = os.getenv("TENDERLY_API_URL")
    if not tenderly_api_url:
        print("Error: TENDERLY_API_URL environment variable is not set", file=sys.stderr)
        sys.exit(1)

    client = httpx.Client(
        base_url=f"{tenderly_api_url}",
        headers={"X-Access-Key": tenderly_api_key},
    )

    # Delete the virtual network
    response = client.delete(
        f"/testnet/container/{vnet_id}",
    )
    if response.status_code != 200 and response.status_code != 204:
        print(f"Error deleting virtual network: {response.status_code} {response.text}", file=sys.stderr)
        sys.exit(1)

    print(f"Virtual network {vnet_id} deleted successfully")


if __name__ == "__main__":
    main()
