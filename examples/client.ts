import { config } from "dotenv";
import { Hex } from "viem";
import { privateKeyToAccount } from "viem/accounts";
import { decodeXPaymentResponse, wrapFetchWithPayment } from "x402-fetch";

config();

const privateKey = process.env.WALLET_PRIVATE_KEY as Hex;
const serverURL = process.env.SERVER_URL;

if (!privateKey || !serverURL) {
  console.error("Missing required environment variables");
  process.exit(1);
}
const url = `${serverURL}/tip`;

const account = privateKeyToAccount(privateKey);
const fetchWithPayment = wrapFetchWithPayment(fetch, account, 100 * 10 ** 6);  // At most 100

async function main() {
  try {
    console.log(`Attempting to tip server at ${url}...`);
    
    const response = await fetchWithPayment(url, {
      method: "GET",
    });

    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`);
    }

    const body = await response.json();
    console.log("Success! Response:", body);

    const paymentResponseHeader = response.headers.get("X-PAYMENT-RESPONSE");
    if (paymentResponseHeader) {
      const paymentResponse = decodeXPaymentResponse(paymentResponseHeader);
      console.log("Payment response:", paymentResponse);
    }
  } catch (error) {
    console.error("Failed to tip:", error);
    process.exit(1);
  }
}

// Run the main function
main();
