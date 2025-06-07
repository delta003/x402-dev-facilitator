import { config } from "dotenv";
import { Hex } from "viem";
import { privateKeyToAccount } from "viem/accounts";
import { decodeXPaymentResponse, wrapFetchWithPayment } from "x402-fetch";

config();

const privateKey = process.env.WALLET_PRIVATE_KEY as Hex;
const serverPort = process.env.SERVER_PORT || "4021";
const url = `http://localhost:${serverPort}/tip`;

if (!privateKey) {
  console.error("Missing required environment variables");
  process.exit(1);
}

const account = privateKeyToAccount(privateKey);
const fetchWithPayment = wrapFetchWithPayment(fetch, account);

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
    
    console.log("Tip completed successfully!");
  } catch (error) {
    console.error("Failed to tip:", error);
    process.exit(1);
  }
}

// Run the main function
main();
