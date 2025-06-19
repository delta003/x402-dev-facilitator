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
    tenderly_org = os.getenv("TENDERLY_ORG")
    if not tenderly_org:
        print("Error: TENDERLY_ORG environment variable is not set", file=sys.stderr)
        sys.exit(1)
    tenderly_project = os.getenv("TENDERLY_PROJECT")
    if not tenderly_project:
        print("Error: TENDERLY_PROJECT environment variable is not set", file=sys.stderr)
        sys.exit(1)

    client = httpx.Client(
        base_url=f"https://api.tenderly.co/api/v1/account/{tenderly_org}/project/{tenderly_project}",
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
