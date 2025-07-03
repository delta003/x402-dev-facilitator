package core

import (
	"bytes"
	"encoding/base64"
	"github.com/gin-gonic/gin"
	"io"
	"log"
	"strings"
	"time"
)

// responseBodyWriter wraps gin.ResponseWriter to capture response body
type responseBodyWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w *responseBodyWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func DetailedLoggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		startTime := time.Now()

		// Log request details
		log.Printf("=== REQUEST START ===")
		log.Printf("Method: %s", c.Request.Method)
		log.Printf("URL: %s", c.Request.URL.String())
		log.Printf("Headers:")
		for name, values := range c.Request.Header {
			for _, value := range values {
				log.Printf("  %s: %s", name, value)

				// Special handling for X-Payment header
				if strings.ToLower(name) == "x-payment" {
					if decoded, err := base64.StdEncoding.DecodeString(value); err == nil {
						log.Printf("  X-Payment (decoded): %s", string(decoded))
					} else {
						// Try without base64 decoding (might be plain JSON)
						log.Printf("  X-Payment (not base64, raw): %s", value)
					}
				}
			}
		}

		// Read and log request body
		if c.Request.Body != nil {
			bodyBytes, err := io.ReadAll(c.Request.Body)
			if err == nil {
				if len(bodyBytes) > 0 {
					log.Printf("Body: %s", string(bodyBytes))
				} else {
					log.Printf("Body: (empty)")
				}
				// Restore the body for further processing
				c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			}
		}
		log.Printf("=== REQUEST END ===\n")

		// Wrap the response writer to capture response body
		responseBody := &bytes.Buffer{}
		bodyWriter := &responseBodyWriter{
			ResponseWriter: c.Writer,
			body:           responseBody,
		}
		c.Writer = bodyWriter

		// Process request
		c.Next()

		// Log response details
		duration := time.Since(startTime)
		log.Printf("=== RESPONSE ===")
		log.Printf("Status: %d", c.Writer.Status())
		log.Printf("Duration: %v", duration)
		log.Printf("Response Headers:")
		for name, values := range c.Writer.Header() {
			for _, value := range values {
				log.Printf("  %s: %s", name, value)

				// Special handling for X-Payment-Response header
				if strings.ToLower(name) == "x-payment-response" {
					if decoded, err := base64.StdEncoding.DecodeString(value); err == nil {
						log.Printf("  X-Payment-Response (decoded): %s", string(decoded))
					} else {
						// Try without base64 decoding (might be plain JSON)
						log.Printf("  X-Payment-Response (not base64, raw): %s", value)
					}
				}
			}
		}

		// Log response body
		if responseBody.Len() > 0 {
			log.Printf("Response Body: %s", responseBody.String())
		} else {
			log.Printf("Response Body: (empty)")
		}

		log.Printf("=== RESPONSE END ===\n")
	}
}
