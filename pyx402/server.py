"""
Python server implementation for x402 payment middleware
"""

import base64
import json
import time
from dataclasses import dataclass
from decimal import Decimal
from typing import Any, Dict, Optional, Union

import requests
from fastapi import Request, Response
from fastapi.responses import HTMLResponse
from starlette.middleware.base import BaseHTTPMiddleware
from starlette.responses import JSONResponse

from .client import BASE_NETWORK, BASE_USDC_ADDRESS, X402_VERSION
from .extensions import ReceiptPayload


@dataclass
class PaymentRequirementsServer:
    """Payment requirements for x402 protocol server"""
    scheme: str
    network: str
    max_amount_required: str
    resource: str
    description: str = ""
    mime_type: str = ""
    pay_to: str = ""
    max_timeout_seconds: int = 60
    asset: str = BASE_USDC_ADDRESS
    output_schema: Optional[Dict[str, Any]] = None
    extra: Optional[Dict[str, Any]] = None


@dataclass
class VerifyResponse:
    """Response from payment verification"""
    is_valid: bool
    payer: Optional[str] = None
    invalid_reason: Optional[str] = None


@dataclass
class PaymentMiddlewareOptions:
    """Options for payment middleware"""
    description: str = ""
    mime_type: str = ""
    max_timeout_seconds: int = 60
    output_schema: Optional[Dict[str, Any]] = None
    facilitator_config: Optional[Dict[str, str]] = None
    testnet: bool = True
    custom_paywall_html: str = ""
    resource: str = ""
    resource_root_url: str = ""
    enable_receipts: bool = True


class PaymentMiddleware(BaseHTTPMiddleware):
    """FastAPI middleware for x402 payment processing"""
    
    def __init__(
        self,
        app,
        amount: Union[float, Decimal],
        address: str,
        **options
    ):
        super().__init__(app)
        self.amount = Decimal(str(amount))
        self.address = address
        self.options = PaymentMiddlewareOptions(**options)
        
        # Default facilitator config
        if self.options.facilitator_config is None:
            self.options.facilitator_config = {
                "url": "https://facilitator.x402.org"
            }
    
    async def dispatch(self, request: Request, call_next):
        """Process the request through payment middleware"""
        # Check if request needs payment
        if not await self._should_process_payment(request):
            return await call_next(request)
        
        # Set up network and addresses
        network = BASE_NETWORK
        usdc_address = BASE_USDC_ADDRESS
        
        if self.options.testnet:
            network = "base-sepolia"
            usdc_address = "0x036CbD53842c5426634e7929541eC2318f3dCF7e"
        
        # Calculate max amount (convert to USDC micro units)
        max_amount = int(self.amount * 1_000_000)
        
        # Determine resource
        resource = self.options.resource
        if not resource:
            resource = self.options.resource_root_url + str(request.url.path)
        
        # Create payment requirements
        payment_requirements = PaymentRequirementsServer(
            scheme="exact",
            network=network,
            max_amount_required=str(max_amount),
            resource=resource,
            description=self.options.description,
            mime_type=self.options.mime_type,
            pay_to=self.address,
            max_timeout_seconds=self.options.max_timeout_seconds,
            asset=usdc_address,
            output_schema=self.options.output_schema,
            extra=None
        )
        
        # Check for payment header
        payment_header = request.headers.get("X-PAYMENT")
        if not payment_header:
            return self._handle_no_payment(request, payment_requirements)
        
        # Decode and verify payment
        try:
            decoded_payment = base64.b64decode(payment_header)
            payment_data = json.loads(decoded_payment.decode('utf-8'))
        except Exception as e:
            return self._error_response(f"Invalid payment header: {str(e)}", payment_requirements)
        
        # Determine if this is a receipt or payment
        if self._is_receipt_payload(payment_data):
            if not self.options.enable_receipts:
                return self._error_response("Receipts are not accepted", payment_requirements)
            
            # Verify receipt
            receipt_payload = ReceiptPayload(**payment_data)
            verify_response = await self._verify_receipt(receipt_payload, payment_requirements)
            
            if not verify_response.is_valid:
                return self._error_response(verify_response.invalid_reason, payment_requirements)
            
            # For receipts, just continue with request
            return await call_next(request)
        else:
            # Verify payment
            verify_response = await self._verify_payment(payment_data, payment_requirements)
            
            if not verify_response.is_valid:
                return self._error_response(verify_response.invalid_reason, payment_requirements)
            
            # Execute request and handle settlement
            response = await call_next(request)
            
            # Settle payment after successful response
            settlement_response = await self._settle_payment(payment_data, payment_requirements)
            if settlement_response:
                response.headers["X-PAYMENT-RESPONSE"] = settlement_response
            
            return response
    
    async def _should_process_payment(self, request: Request) -> bool:
        """Determine if this request should be processed for payment"""
        # Add your logic here to determine which routes require payment
        # For now, process all requests
        return True
    
    def _handle_no_payment(self, request: Request, requirements: PaymentRequirementsServer) -> Response:
        """Handle requests without payment header"""
        user_agent = request.headers.get("User-Agent", "")
        accept_header = request.headers.get("Accept", "")
        is_web_browser = "text/html" in accept_header and "Mozilla" in user_agent
        
        if is_web_browser:
            html = self.options.custom_paywall_html
            if not html:
                html = self._get_paywall_html()
            return HTMLResponse(content=html, status_code=402)
        
        return JSONResponse(
            content={
                "error": "X-PAYMENT header is required",
                "accepts": [self._requirements_to_dict(requirements)],
                "x402Version": X402_VERSION,
            },
            status_code=402
        )
    
    def _error_response(self, error: str, requirements: PaymentRequirementsServer) -> Response:
        """Return error response"""
        return JSONResponse(
            content={
                "error": error,
                "accepts": [self._requirements_to_dict(requirements)],
                "x402Version": X402_VERSION,
            },
            status_code=402
        )
    
    def _requirements_to_dict(self, requirements: PaymentRequirementsServer) -> Dict[str, Any]:
        """Convert payment requirements to dict"""
        return {
            "scheme": requirements.scheme,
            "network": requirements.network,
            "maxAmountRequired": requirements.max_amount_required,
            "resource": requirements.resource,
            "description": requirements.description,
            "mimeType": requirements.mime_type,
            "payTo": requirements.pay_to,
            "maxTimeoutSeconds": requirements.max_timeout_seconds,
            "asset": requirements.asset,
            "outputSchema": requirements.output_schema,
            "extra": requirements.extra,
        }
    
    def _is_receipt_payload(self, payment_data: Dict[str, Any]) -> bool:
        """Determine if payload is a receipt"""
        payload = payment_data.get("payload", {})
        return "transaction" in payload and "signature" in payload
    
    async def _verify_payment(self, payment_data: Dict[str, Any], requirements: PaymentRequirementsServer) -> VerifyResponse:
        """Verify payment with facilitator"""
        facilitator_url = self.options.facilitator_config["url"]
        
        try:
            response = requests.post(
                f"{facilitator_url}/verify",
                json={
                    "x402Version": X402_VERSION,
                    "paymentPayload": payment_data,
                    "paymentRequirements": self._requirements_to_dict(requirements),
                },
                timeout=10
            )
            
            if response.status_code != 200:
                return VerifyResponse(is_valid=False, invalid_reason="verification_failed")
            
            result = response.json()
            return VerifyResponse(
                is_valid=result.get("isValid", False),
                payer=result.get("payer"),
                invalid_reason=result.get("invalidReason")
            )
        except Exception as e:
            return VerifyResponse(is_valid=False, invalid_reason=f"verification_error: {str(e)}")
    
    async def _verify_receipt(self, receipt_payload: ReceiptPayload, requirements: PaymentRequirementsServer) -> VerifyResponse:
        """Verify receipt with facilitator"""
        facilitator_url = self.options.facilitator_config["url"]
        
        try:
            response = requests.post(
                f"{facilitator_url}/verify-receipt",
                json={
                    "x402Version": X402_VERSION,
                    "receiptPayload": receipt_payload.__dict__,
                    "paymentRequirements": self._requirements_to_dict(requirements),
                },
                timeout=10
            )
            
            if response.status_code != 200:
                return VerifyResponse(is_valid=False, invalid_reason="receipt_verification_failed")
            
            result = response.json()
            return VerifyResponse(
                is_valid=result.get("isValid", False),
                payer=result.get("payer"),
                invalid_reason=result.get("invalidReason")
            )
        except Exception as e:
            return VerifyResponse(is_valid=False, invalid_reason=f"receipt_verification_error: {str(e)}")
    
    async def _settle_payment(self, payment_data: Dict[str, Any], requirements: PaymentRequirementsServer) -> Optional[str]:
        """Settle payment and return response header"""
        facilitator_url = self.options.facilitator_config["url"]
        
        try:
            response = requests.post(
                f"{facilitator_url}/settle",
                json={
                    "x402Version": X402_VERSION,
                    "paymentPayload": payment_data,
                    "paymentRequirements": self._requirements_to_dict(requirements),
                },
                timeout=30
            )
            
            if response.status_code != 200:
                return None
            
            result = response.json()
            # Encode settlement response as base64
            settlement_bytes = json.dumps(result).encode('utf-8')
            return base64.b64encode(settlement_bytes).decode('utf-8')
        except Exception:
            return None
    
    def _get_paywall_html(self) -> str:
        """Get default paywall HTML"""
        return "<html><body><h1>Payment Required</h1><p>This resource requires payment to access.</p></body></html>"


def detailed_logging_middleware():
    """Middleware for detailed request/response logging"""
    
    class DetailedLoggingMiddleware(BaseHTTPMiddleware):
        async def dispatch(self, request: Request, call_next):
            import logging
            
            logger = logging.getLogger(__name__)
            
            # Log request details
            logger.info("=== REQUEST START ===")
            logger.info(f"Method: {request.method}")
            logger.info(f"URL: {request.url}")
            logger.info("Headers:")
            for name, value in request.headers.items():
                logger.info(f"  {name}: {value}")
                
                # Special handling for X-Payment header
                if name.lower() == "x-payment":
                    try:
                        decoded = base64.b64decode(value).decode('utf-8')
                        logger.info(f"  X-Payment (decoded): {decoded}")
                    except Exception:
                        logger.info(f"  X-Payment (not base64, raw): {value}")
            
            # Log request body if present
            if request.method in ["POST", "PUT", "PATCH"]:
                try:
                    body = await request.body()
                    if body:
                        logger.info(f"Body: {body.decode('utf-8')}")
                    else:
                        logger.info("Body: (empty)")
                except Exception:
                    logger.info("Body: (unable to read)")
            
            logger.info("=== REQUEST END ===\n")
            
            # Process request
            start_time = time.time()
            response = await call_next(request)
            duration = time.time() - start_time
            
            # Log response details
            logger.info("=== RESPONSE ===")
            logger.info(f"Status: {response.status_code}")
            logger.info(f"Duration: {duration:.3f}s")
            logger.info("Response Headers:")
            for name, value in response.headers.items():
                logger.info(f"  {name}: {value}")
                
                # Special handling for X-Payment-Response header
                if name.lower() == "x-payment-response":
                    try:
                        decoded = base64.b64decode(value).decode('utf-8')
                        logger.info(f"  X-Payment-Response (decoded): {decoded}")
                    except Exception:
                        logger.info(f"  X-Payment-Response (not base64, raw): {value}")
            
            logger.info("=== RESPONSE END ===\n")
            
            return response
    
    return DetailedLoggingMiddleware


def with_description(description: str):
    """Option to set description"""
    return {"description": description}


def with_mime_type(mime_type: str):
    """Option to set mime type"""
    return {"mime_type": mime_type}


def with_max_timeout_seconds(max_timeout_seconds: int):
    """Option to set max timeout seconds"""
    return {"max_timeout_seconds": max_timeout_seconds}


def with_output_schema(output_schema: Dict[str, Any]):
    """Option to set output schema"""
    return {"output_schema": output_schema}


def with_facilitator_config(config: Dict[str, str]):
    """Option to set facilitator config"""
    return {"facilitator_config": config}


def with_testnet(testnet: bool):
    """Option to set testnet flag"""
    return {"testnet": testnet}


def with_custom_paywall_html(html: str):
    """Option to set custom paywall HTML"""
    return {"custom_paywall_html": html}


def with_resource(resource: str):
    """Option to set resource"""
    return {"resource": resource}


def with_resource_root_url(resource_root_url: str):
    """Option to set resource root URL"""
    return {"resource_root_url": resource_root_url}


def with_enable_receipts(enable_receipts: bool):
    """Option to enable receipts"""
    return {"enable_receipts": enable_receipts}