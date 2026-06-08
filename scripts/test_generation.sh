#!/bin/bash
# Helper script to test nano-banana-mcpv2 image generation

# Exit immediately if a command exits with a non-zero status
set -e

# Verify key exists
if [ -z "$GEMINI_API_KEY" ]; then
  echo "❌ Error: GEMINI_API_KEY environment variable is not set."
  echo "Please set it before running this script: export GEMINI_API_KEY='your-key'"
  exit 1
fi

echo "🚀 Compiling server binary..."
go build -o nano-banana-mcpv2 main.go

echo "🎨 Sending generate_imagen tool request over stdio..."
# Create a JSON-RPC request payload
REQUEST='{"jsonrpc": "2.0", "method": "tools/call", "params": {"name": "generate_imagen", "arguments": {"prompt": "A simple yellow banana logo, vector icon", "model": "imagen-4.0-generate-001"}}, "id": 1}'

# Execute and capture the response
RESPONSE=$(echo "$REQUEST" | ./nano-banana-mcpv2)

# Check if response contains an error
if echo "$RESPONSE" | grep -q '"error"'; then
  echo "❌ Error returned from server:"
  echo "$RESPONSE" | grep -o '"message":[^,]*' || echo "$RESPONSE"
  exit 1
fi

echo "✅ Success! Image generated successfully."
echo "Response content:"
echo "$RESPONSE" | grep -o '"text":[^,]*' || echo "$RESPONSE"
echo "📁 Check the 'generated_imgs' directory for the saved image file."
