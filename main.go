package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const (
	ServerName    = "nano-banana-mcpv2"
	ServerVersion = "0.1.1-beta.0"
)

type Config struct {
	GeminiAPIKey string `json:"geminiApiKey"`
}

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      interface{}     `json:"id,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
	ID      interface{} `json:"id"`
}

type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

type InputSchema struct {
	Type                 string                 `json:"type"`
	Properties           map[string]interface{} `json:"properties"`
	Required             []string               `json:"required,omitempty"`
	AdditionalProperties bool                   `json:"additionalProperties,omitempty"`
}

// Global State
var (
	lastImagePath string
	httpClient    = &http.Client{Timeout: 60 * time.Second}
	logFile       *os.File
	globalCtx     context.Context
)

func main() {
	// Parse CLI flags first
	if len(os.Args) > 1 && (os.Args[1] == "--setup" || os.Args[1] == "-setup") {
		runSetupWizard()
		return
	}

	rand.Seed(time.Now().UnixNano())

	// Initialize log file if requested
	logPath := os.Getenv("NANO_BANANA_LOG_FILE")
	if logPath != "" {
		var err error
		// #nosec - log path is user-configured from environment variable, permission restricted to owner-only
		logFile, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error opening log file:", err)
		} else {
			logMessage("Server started, logging enabled (version: %s)", ServerVersion)
		}
	}

	// Set up global context and signal handling for graceful shutdown
	var cancel context.CancelFunc
	globalCtx, cancel = context.WithCancel(context.Background())
	defer cancel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		logMessage("Received signal %v, shutting down...", sig)
		cancel()
		if logFile != nil {
			_ = logFile.Close()
		}
		os.Exit(0)
	}()

	scanner := bufio.NewScanner(os.Stdin)

	for scanner.Scan() {
		line := scanner.Bytes()
		logMessage("Received request raw: %s", string(line))
		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			logMessage("JSON Parse error: %v", err)
			sendError(nil, -32700, "Parse error", err.Error())
			continue
		}

		handleRequest(&req)
	}

	if err := scanner.Err(); err != nil {
		logMessage("Error reading stdin: %v", err)
		fmt.Fprintln(os.Stderr, "Error reading stdin:", err)
		os.Exit(1)
	}
}

func logMessage(format string, args ...interface{}) {
	if logFile != nil {
		timestamp := time.Now().Format("2006-01-02 15:04:05")
		msg := fmt.Sprintf("[%s] %s\n", timestamp, fmt.Sprintf(format, args...))
		_, _ = logFile.WriteString(msg)
	}
}

func sendResponse(id interface{}, result interface{}) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		Result:  result,
		ID:      id,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		logMessage("Error marshaling response: %v", err)
		fmt.Fprintln(os.Stderr, "Error marshaling response:", err)
		return
	}
	logMessage("Sending response: %s", string(data))
	_, _ = os.Stdout.Write(data)
	_, _ = os.Stdout.Write([]byte("\n"))
}

func sendError(id interface{}, code int, message string, data interface{}) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		Error: &RPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
		ID: id,
	}
	respData, err := json.Marshal(resp)
	if err != nil {
		logMessage("Error marshaling error response: %v", err)
		fmt.Fprintln(os.Stderr, "Error marshaling error response:", err)
		return
	}
	logMessage("Sending error response: %s", string(respData))
	_, _ = os.Stdout.Write(respData)
	_, _ = os.Stdout.Write([]byte("\n"))
}

func handleRequest(req *JSONRPCRequest) {
	logMessage("Handling method: %s", req.Method)
	switch req.Method {
	case "initialize":
		result := map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]string{
				"name":    ServerName,
				"version": ServerVersion,
			},
		}
		sendResponse(req.ID, result)

	case "notifications/initialized":
		// No response required for notifications

	case "tools/list":
		tools := getToolsList()
		sendResponse(req.ID, map[string]interface{}{
			"tools": tools,
		})

	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendError(req.ID, -32602, "Invalid params", err.Error())
			return
		}
		handleToolCall(req.ID, params.Name, params.Arguments)

	default:
		if req.ID != nil {
			sendError(req.ID, -32601, fmt.Sprintf("Method not found: %s", req.Method), nil)
		}
	}
}

func getToolsList() []Tool {
	return []Tool{
		{
			Name:        "configure_gemini_token",
			Description: "Configure your Gemini API token for nano-banana image generation",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"apiKey": map[string]interface{}{
						"type":        "string",
						"description": "Your Gemini API key from Google AI Studio",
					},
				},
				Required: []string{"apiKey"},
			},
		},
		{
			Name:        "generate_image",
			Description: "Generate a NEW image from text prompt using Gemini multimodal native generation.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": "Text prompt describing the NEW image to create from scratch",
					},
					"model": map[string]interface{}{
						"type":        "string",
						"description": "Optional model name to use (e.g., 'gemini-3.1-flash-image'). Defaults to GEMINI_IMAGE_MODEL environment variable or 'gemini-3.1-flash-image'.",
					},
					"aspectRatio": map[string]interface{}{
						"type":        "string",
						"description": "Optional aspect ratio for the generated image. Defaults to '1:1'.",
						"enum":        []string{"1:1", "16:9", "9:16", "4:3", "3:4"},
					},
				},
				Required: []string{"prompt"},
			},
		},
		{
			Name:        "generate_imagen",
			Description: "Generate a NEW image from text prompt using Google's dedicated Imagen model. Optimized for high-fidelity text-to-image.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": "Text prompt describing the NEW image to generate",
					},
					"model": map[string]interface{}{
						"type":        "string",
						"description": "Optional model name to use. Defaults to 'imagen-4.0-generate-001'.",
						"enum":        []string{"imagen-4.0-generate-001", "imagen-4.0-ultra-generate-001", "imagen-4.0-fast-generate-001"},
					},
					"aspectRatio": map[string]interface{}{
						"type":        "string",
						"description": "Optional aspect ratio for the generated image. Defaults to '1:1'.",
						"enum":        []string{"1:1", "3:4", "4:3", "9:16", "16:9"},
					},
					"numberOfImages": map[string]interface{}{
						"type":        "integer",
						"description": "Optional number of images to generate (1-4). Defaults to 1.",
						"minimum":     1,
						"maximum":     4,
					},
					"negativePrompt": map[string]interface{}{
						"type":        "string",
						"description": "Optional description of elements to avoid in the generated image.",
					},
				},
				Required: []string{"prompt"},
			},
		},
		{
			Name:        "edit_image",
			Description: "Edit a SPECIFIC existing image file, optionally using additional reference images. Use this when you have the exact file path of an image to modify.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"imagePath": map[string]interface{}{
						"type":        "string",
						"description": "Full file path to the main image file to edit",
					},
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": "Text describing the modifications to make to the existing image",
					},
					"referenceImages": map[string]interface{}{
						"type":        "array",
						"description": "Optional array of file paths to additional reference images to use during editing",
						"items": map[string]interface{}{
							"type": "string",
						},
					},
					"model": map[string]interface{}{
						"type":        "string",
						"description": "Optional model name to use. Defaults to GEMINI_IMAGE_MODEL environment variable or 'gemini-3.1-flash-image'.",
					},
				},
				Required: []string{"imagePath", "prompt"},
			},
		},
		{
			Name:        "continue_editing",
			Description: "Continue editing the LAST image that was generated or edited in this session, optionally using additional reference images. This automatically uses the previous image without needing a file path.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": "Text describing the modifications/changes/improvements to make to the last image",
					},
					"referenceImages": map[string]interface{}{
						"type":        "array",
						"description": "Optional array of file paths to additional reference images to use during editing",
						"items": map[string]interface{}{
							"type": "string",
						},
					},
					"model": map[string]interface{}{
						"type":        "string",
						"description": "Optional model name to use. Defaults to GEMINI_IMAGE_MODEL environment variable or 'gemini-3.1-flash-image'.",
					},
				},
				Required: []string{"prompt"},
			},
		},
		{
			Name:        "get_configuration_status",
			Description: "Check if Gemini API token is configured",
			InputSchema: InputSchema{
				Type:                 "object",
				Properties:           map[string]interface{}{},
				AdditionalProperties: false,
			},
		},
		{
			Name:        "get_last_image_info",
			Description: "Get information about the last generated/edited image in this session (file path, size, etc.)",
			InputSchema: InputSchema{
				Type:                 "object",
				Properties:           map[string]interface{}{},
				AdditionalProperties: false,
			},
		},
	}
}

func handleToolCall(id interface{}, toolName string, arguments json.RawMessage) {
	apiKey, _ := loadConfig()

	// Intercept configure token because it doesn't require an active API key
	if toolName == "configure_gemini_token" {
		var args struct {
			APIKey string `json:"apiKey"`
		}
		if err := json.Unmarshal(arguments, &args); err != nil {
			sendError(id, -32602, "Invalid arguments", err.Error())
			return
		}
		if args.APIKey == "" {
			sendError(id, -32602, "API key is required", nil)
			return
		}
		if err := saveConfig(args.APIKey); err != nil {
			sendError(id, -32603, "Failed to save configuration", err.Error())
			return
		}
		sendResponse(id, map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": "✅ Gemini API token configured successfully! You can now use nano-banana image generation features.",
				},
			},
		})
		return
	}

	if toolName == "get_configuration_status" {
		isConfigured := apiKey != ""
		statusText := "❌ Gemini API token is not configured"
		sourceInfo := "\n\n📝 Configuration options:\n1. Environment variable: GEMINI_API_KEY\n2. Use configure_gemini_token tool"
		if isConfigured {
			_, source := loadConfig()
			statusText = "✅ Gemini API token is configured and ready to use"
			if source == "environment" {
				sourceInfo = "\n📍 Source: Environment variable (GEMINI_API_KEY)"
			} else {
				sourceInfo = "\n📍 Source: Configuration file (~/.nano-banana-config.json)"
			}
		}
		sendResponse(id, map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": statusText + sourceInfo,
				},
			},
		})
		return
	}

	// For all other tools, make sure API key is loaded
	if apiKey == "" {
		sendError(id, -32603, "Gemini API token not configured. Use configure_gemini_token first.", nil)
		return
	}

	switch toolName {
	case "generate_image":
		var args struct {
			Prompt      string  `json:"prompt"`
			Model       *string `json:"model"`
			AspectRatio *string `json:"aspectRatio"`
		}
		if err := json.Unmarshal(arguments, &args); err != nil {
			sendError(id, -32602, "Invalid arguments", err.Error())
			return
		}
		handleGenerateImage(id, apiKey, args.Prompt, args.Model, args.AspectRatio)

	case "generate_imagen":
		var args struct {
			Prompt         string  `json:"prompt"`
			Model          *string `json:"model"`
			AspectRatio    *string `json:"aspectRatio"`
			NumberOfImages *int    `json:"numberOfImages"`
			NegativePrompt *string `json:"negativePrompt"`
		}
		if err := json.Unmarshal(arguments, &args); err != nil {
			sendError(id, -32602, "Invalid arguments", err.Error())
			return
		}
		handleGenerateImagen(id, apiKey, args.Prompt, args.Model, args.AspectRatio, args.NumberOfImages, args.NegativePrompt)

	case "edit_image":
		var args struct {
			ImagePath       string    `json:"imagePath"`
			Prompt          string    `json:"prompt"`
			ReferenceImages []string  `json:"referenceImages"`
			Model           *string   `json:"model"`
			AspectRatio     *string   `json:"aspectRatio"`
		}
		if err := json.Unmarshal(arguments, &args); err != nil {
			sendError(id, -32602, "Invalid arguments", err.Error())
			return
		}
		handleEditImage(id, apiKey, args.ImagePath, args.Prompt, args.ReferenceImages, args.Model, args.AspectRatio)

	case "continue_editing":
		var args struct {
			Prompt          string   `json:"prompt"`
			ReferenceImages []string `json:"referenceImages"`
			Model           *string  `json:"model"`
		}
		if err := json.Unmarshal(arguments, &args); err != nil {
			sendError(id, -32602, "Invalid arguments", err.Error())
			return
		}
		if lastImagePath == "" {
			sendError(id, -32603, "No previous image found. Please generate or edit an image first.", nil)
			return
		}
		if _, err := os.Stat(lastImagePath); os.IsNotExist(err) {
			sendError(id, -32603, fmt.Sprintf("Last image file not found at: %s. Please generate a new image.", lastImagePath), nil)
			return
		}
		handleEditImage(id, apiKey, lastImagePath, args.Prompt, args.ReferenceImages, args.Model, nil)

	case "get_last_image_info":
		if lastImagePath == "" {
			sendResponse(id, map[string]interface{}{
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": "📷 No previous image found.",
					},
				},
			})
			return
		}
		info, err := os.Stat(lastImagePath)
		if err != nil {
			sendResponse(id, map[string]interface{}{
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": fmt.Sprintf("📷 Last Image Path: %s\nStatus: ❌ File not found", lastImagePath),
					},
				},
			})
			return
		}
		sendResponse(id, map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": fmt.Sprintf("📷 Last Image Information:\n\nPath: %s\nFile Size: %d KB\nLast Modified: %s\n\n💡 Use continue_editing to modify this image.", lastImagePath, info.Size()/1024, info.ModTime().Format(time.RFC1123)),
				},
			},
		})

	default:
		sendError(id, -32601, fmt.Sprintf("Unknown tool: %s", toolName), nil)
	}
}

func loadConfig() (string, string) {
	if key := os.Getenv("GEMINI_API_KEY"); key != "" {
		return key, "environment"
	}

	// Fallback to local
	var config Config
	// #nosec G304 - local config file read is intentional
	if data, err := os.ReadFile(".nano-banana-config.json"); err == nil {
		if err := json.Unmarshal(data, &config); err == nil && config.GeminiAPIKey != "" {
			// Auto migrate
			home, _ := os.UserHomeDir()
			globalPath := filepath.Join(home, ".nano-banana-config.json")
			if _, err := os.Stat(globalPath); os.IsNotExist(err) {
				// #nosec - global path is home-dir relative, migration is safe and intentional
				_ = os.WriteFile(globalPath, data, 0600)
				fmt.Fprintln(os.Stderr, "[nano-banana-mcpv2] Automatically migrated local configuration to global:", globalPath)
			}
			return config.GeminiAPIKey, "config_file"
		}
	}

	// Fallback to global
	home, _ := os.UserHomeDir()
	globalPath := filepath.Join(home, ".nano-banana-config.json")
	// #nosec G304 - global config file read is intentional
	if data, err := os.ReadFile(globalPath); err == nil {
		if err := json.Unmarshal(data, &config); err == nil && config.GeminiAPIKey != "" {
			return config.GeminiAPIKey, "config_file"
		}
	}

	return "", "not_configured"
}

func saveConfig(key string) error {
	config := Config{GeminiAPIKey: key}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	globalPath := filepath.Join(home, ".nano-banana-config.json")
	
	// Ensure folder exists with restrictive permissions
	// #nosec G301 - global config dir must be owner-only (0700)
	_ = os.MkdirAll(filepath.Dir(globalPath), 0700)
	// #nosec G306 - config file must be owner-only (0600)
	return os.WriteFile(globalPath, data, 0600)
}

func resolveModel(customModel *string) string {
	if customModel != nil && strings.TrimSpace(*customModel) != "" {
		return strings.TrimSpace(*customModel)
	}
	if envModel := os.Getenv("GEMINI_IMAGE_MODEL"); envModel != "" {
		return strings.TrimSpace(envModel)
	}
	return "gemini-3.1-flash-image"
}

func getImagesDirectory() string {
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		return filepath.Join(home, "Documents", "nano-banana-images")
	}

	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	if strings.HasPrefix(cwd, "/usr/") || strings.HasPrefix(cwd, "/opt/") || strings.HasPrefix(cwd, "/var/") {
		return filepath.Join(home, "nano-banana-images")
	}
	return filepath.Join(cwd, "generated_imgs")
}

func getMimeType(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return "image/jpeg"
	}
}

// REST call payloads
type GeminiPart struct {
	Text       string      `json:"text,omitempty"`
	InlineData *InlineData `json:"inlineData,omitempty"`
}

type InlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type GeminiContent struct {
	Parts []GeminiPart `json:"parts"`
}

type ImageConfig struct {
	AspectRatio string `json:"aspectRatio,omitempty"`
}

type GeminiGenerationConfig struct {
	ResponseModalities []string     `json:"responseModalities,omitempty"`
	ImageConfig        *ImageConfig `json:"imageConfig,omitempty"`
}

type GeminiRequest struct {
	Contents         GeminiContent           `json:"contents"`
	GenerationConfig GeminiGenerationConfig  `json:"generationConfig"`
}

type GeminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text       string `json:"text"`
				InlineData *struct {
					MimeType string `json:"mimeType"`
					Data     string `json:"data"`
				} `json:"inlineData"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

func handleGenerateImage(id interface{}, apiKey, prompt string, customModel, aspectRatio *string) {
	model := resolveModel(customModel)
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, apiKey)

	reqPayload := GeminiRequest{
		Contents: GeminiContent{
			Parts: []GeminiPart{
				{Text: prompt},
			},
		},
		GenerationConfig: GeminiGenerationConfig{
			ResponseModalities: []string{"IMAGE"},
		},
	}

	if aspectRatio != nil && *aspectRatio != "" {
		reqPayload.GenerationConfig.ImageConfig = &ImageConfig{
			AspectRatio: *aspectRatio,
		}
	}

	payloadData, err := json.Marshal(reqPayload)
	if err != nil {
		logMessage("Internal payload formatting error: %v", err)
		sendError(id, -32603, "Internal payload formatting error", err.Error())
		return
	}

	logMessage("Sending generate_image request to URL: %s, model: %s", url, model)
	ctx := globalCtx
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(payloadData))
	if err != nil {
		logMessage("Internal request creation error: %v", err)
		sendError(id, -32603, "Internal request creation error", err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		logMessage("HTTP request failed: %v", err)
		sendError(id, -32603, "HTTP request failed", err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		logMessage("Gemini API call failed with status %d: %s", resp.StatusCode, string(bodyBytes))
		sendError(id, -32603, fmt.Sprintf("Gemini API call failed with status %d", resp.StatusCode), string(bodyBytes))
		return
	}
	logMessage("Gemini API call succeeded with status %d", resp.StatusCode)

	var geminiResp GeminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		sendError(id, -32603, "Failed to parse API response", err.Error())
		return
	}

	content := []map[string]interface{}{}
	savedFiles := []string{}
	textContent := ""
	imagesDir := getImagesDirectory()
	// #nosec G301 - generated images folder should be user-browsable (0755)
	_ = os.MkdirAll(imagesDir, 0755)

	if len(geminiResp.Candidates) > 0 && len(geminiResp.Candidates[0].Content.Parts) > 0 {
		for _, part := range geminiResp.Candidates[0].Content.Parts {
			if part.Text != "" {
				textContent += part.Text
			}
			if part.InlineData != nil && part.InlineData.Data != "" {
				timestamp := time.Now().Format("20060102-150405")
				// #nosec G404 - weak random is sufficient for filename random identifier
				randomId := fmt.Sprintf("%06d", rand.Intn(1000000))
				fileName := fmt.Sprintf("generated-%s-%s.png", timestamp, randomId)
				filePath := filepath.Join(imagesDir, fileName)

				imageBytes, err := json.Marshal(part.InlineData.Data)
				if err != nil {
					continue
				}
				var decodedBytes []byte
				// Decode JSON string to raw bytes
				if err := json.Unmarshal(imageBytes, &decodedBytes); err == nil {
					// #nosec G306 - generated image files must be user-viewable (0644)
					if err := os.WriteFile(filePath, decodedBytes, 0644); err == nil {
						savedFiles = append(savedFiles, filePath)
						lastImagePath = filePath
					}
				}

				content = append(content, map[string]interface{}{
					"type":     "image",
					"data":     part.InlineData.Data,
					"mimeType": part.InlineData.MimeType,
				})
			}
		}
	}

	statusText := fmt.Sprintf("🎨 Image generated with nano-banana (%s)!\n\nPrompt: \"%s\"", model, prompt)
	if textContent != "" {
		statusText += fmt.Sprintf("\n\nDescription: %s", textContent)
	}
	if len(savedFiles) > 0 {
		statusText += "\n\n📁 Image saved to:\n"
		for _, f := range savedFiles {
			statusText += fmt.Sprintf("- %s\n", f)
		}
		statusText += "\n🔄 To modify this image, use: continue_editing"
	} else {
		statusText += "\n\nNote: No image was generated. The model may have returned only text."
	}

	// Insert status text at start
	textPart := map[string]interface{}{
		"type": "text",
		"text": statusText,
	}
	content = append([]map[string]interface{}{textPart}, content...)

	sendResponse(id, map[string]interface{}{
		"content": content,
	})
}

type ImagenInstance struct {
	Prompt string `json:"prompt"`
}

type ImagenParameters struct {
	SampleCount    int    `json:"sampleCount,omitempty"`
	AspectRatio    string `json:"aspectRatio,omitempty"`
	NegativePrompt string `json:"negativePrompt,omitempty"`
}

type ImagenRequest struct {
	Instances  []ImagenInstance `json:"instances"`
	Parameters ImagenParameters `json:"parameters"`
}

type ImagenResponse struct {
	Predictions []struct {
		MimeType           string `json:"mimeType"`
		BytesBase64Encoded string `json:"bytesBase64Encoded"`
	} `json:"predictions"`
}

func handleGenerateImagen(id interface{}, apiKey, prompt string, customModel, aspectRatio *string, numberOfImages *int, negativePrompt *string) {
	model := "imagen-4.0-generate-001"
	if customModel != nil && *customModel != "" {
		model = *customModel
	}
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:predict?key=%s", model, apiKey)

	reqPayload := ImagenRequest{
		Instances: []ImagenInstance{
			{Prompt: prompt},
		},
		Parameters: ImagenParameters{},
	}

	if aspectRatio != nil && *aspectRatio != "" {
		reqPayload.Parameters.AspectRatio = *aspectRatio
	}
	if numberOfImages != nil {
		reqPayload.Parameters.SampleCount = *numberOfImages
	}
	if negativePrompt != nil && *negativePrompt != "" {
		reqPayload.Parameters.NegativePrompt = *negativePrompt
	}

	payloadData, err := json.Marshal(reqPayload)
	if err != nil {
		logMessage("Internal payload formatting error: %v", err)
		sendError(id, -32603, "Internal payload formatting error", err.Error())
		return
	}

	logMessage("Sending generate_imagen request to URL: %s, model: %s", url, model)
	ctx := globalCtx
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(payloadData))
	if err != nil {
		logMessage("Internal request creation error: %v", err)
		sendError(id, -32603, "Internal request creation error", err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		logMessage("HTTP request failed: %v", err)
		sendError(id, -32603, "HTTP request failed", err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		logMessage("Imagen API call failed with status %d: %s", resp.StatusCode, string(bodyBytes))
		sendError(id, -32603, fmt.Sprintf("Imagen API call failed with status %d", resp.StatusCode), string(bodyBytes))
		return
	}
	logMessage("Imagen API call succeeded with status %d", resp.StatusCode)

	var imagenResp ImagenResponse
	if err := json.NewDecoder(resp.Body).Decode(&imagenResp); err != nil {
		sendError(id, -32603, "Failed to parse API response", err.Error())
		return
	}

	content := []map[string]interface{}{}
	savedFiles := []string{}
	imagesDir := getImagesDirectory()
	// #nosec G301 - generated images folder should be user-browsable (0755)
	_ = os.MkdirAll(imagesDir, 0755)

	for _, pred := range imagenResp.Predictions {
		if pred.BytesBase64Encoded != "" {
			timestamp := time.Now().Format("20060102-150405")
			// #nosec G404 - weak random is sufficient for filename random identifier
			randomId := fmt.Sprintf("%06d", rand.Intn(1000000))
			fileName := fmt.Sprintf("imagen-%s-%s.png", timestamp, randomId)
			filePath := filepath.Join(imagesDir, fileName)

			imageBytes, err := json.Marshal(pred.BytesBase64Encoded)
			if err != nil {
				continue
			}
			var decodedBytes []byte
			if err := json.Unmarshal(imageBytes, &decodedBytes); err == nil {
				// #nosec G306 - generated image files must be user-viewable (0644)
				if err := os.WriteFile(filePath, decodedBytes, 0644); err == nil {
					savedFiles = append(savedFiles, filePath)
					lastImagePath = filePath
				}
			}

			content = append(content, map[string]interface{}{
				"type":     "image",
				"data":     pred.BytesBase64Encoded,
				"mimeType": pred.MimeType,
			})
		}
	}

	statusText := fmt.Sprintf("🎨 Image(s) generated using Google Imagen (%s)!\n\nPrompt: \"%s\"", model, prompt)
	if aspectRatio != nil && *aspectRatio != "" {
		statusText += fmt.Sprintf("\nAspect Ratio: %s", *aspectRatio)
	}
	if negativePrompt != nil && *negativePrompt != "" {
		statusText += fmt.Sprintf("\nNegative Prompt: \"%s\"", *negativePrompt)
	}

	if len(savedFiles) > 0 {
		statusText += "\n\n📁 Image(s) saved to:\n"
		for _, f := range savedFiles {
			statusText += fmt.Sprintf("- %s\n", f)
		}
		statusText += "\n🔄 To modify the last image, use: continue_editing"
	} else {
		statusText += "\n\nNote: No image was returned by the Imagen API."
	}

	textPart := map[string]interface{}{
		"type": "text",
		"text": statusText,
	}
	content = append([]map[string]interface{}{textPart}, content...)

	sendResponse(id, map[string]interface{}{
		"content": content,
	})
}

func handleEditImage(id interface{}, apiKey, imagePath, prompt string, referenceImages []string, customModel, aspectRatio *string) {
	model := resolveModel(customModel)
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, apiKey)

	// Read and base64 encode the main image
	// #nosec G304 - reading base image path is intentional and requested by user/client
	imgData, err := os.ReadFile(imagePath)
	if err != nil {
		sendError(id, -32603, fmt.Sprintf("Failed to read image at %s", imagePath), err.Error())
		return
	}
	mainMimeType := getMimeType(imagePath)

	var mainB64 string
	mainB64Bytes, _ := json.Marshal(imgData)
	_ = json.Unmarshal(mainB64Bytes, &mainB64)

	parts := []GeminiPart{
		{
			InlineData: &InlineData{
				MimeType: mainMimeType,
				Data:     mainB64,
			},
		},
	}

	// Add reference images if provided
	for _, refPath := range referenceImages {
		// #nosec G304 - reading reference image path is intentional and requested by user/client
		if refBytes, err := os.ReadFile(refPath); err == nil {
			refMimeType := getMimeType(refPath)
			var refB64 string
			refB64Bytes, _ := json.Marshal(refBytes)
			_ = json.Unmarshal(refB64Bytes, &refB64)

			parts = append(parts, GeminiPart{
				InlineData: &InlineData{
					MimeType: refMimeType,
					Data:     refB64,
				},
			})
		}
	}

	// Append text prompt last
	parts = append(parts, GeminiPart{Text: prompt})

	reqPayload := GeminiRequest{
		Contents: GeminiContent{
			Parts: parts,
		},
		GenerationConfig: GeminiGenerationConfig{
			ResponseModalities: []string{"IMAGE"},
		},
	}

	if aspectRatio != nil && *aspectRatio != "" {
		reqPayload.GenerationConfig.ImageConfig = &ImageConfig{
			AspectRatio: *aspectRatio,
		}
	}

	payloadData, err := json.Marshal(reqPayload)
	if err != nil {
		logMessage("Internal payload formatting error: %v", err)
		sendError(id, -32603, "Internal payload formatting error", err.Error())
		return
	}

	logMessage("Sending edit_image request to URL: %s, model: %s", url, model)
	ctx := globalCtx
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(payloadData))
	if err != nil {
		logMessage("Internal request creation error: %v", err)
		sendError(id, -32603, "Internal request creation error", err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		logMessage("HTTP request failed: %v", err)
		sendError(id, -32603, "HTTP request failed", err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		logMessage("Gemini API call failed with status %d: %s", resp.StatusCode, string(bodyBytes))
		sendError(id, -32603, fmt.Sprintf("Gemini API call failed with status %d", resp.StatusCode), string(bodyBytes))
		return
	}
	logMessage("Gemini API call succeeded with status %d", resp.StatusCode)

	var geminiResp GeminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		sendError(id, -32603, "Failed to parse API response", err.Error())
		return
	}

	content := []map[string]interface{}{}
	savedFiles := []string{}
	textContent := ""
	imagesDir := getImagesDirectory()
	// #nosec G301 - generated images folder should be user-browsable (0755)
	_ = os.MkdirAll(imagesDir, 0755)

	if len(geminiResp.Candidates) > 0 && len(geminiResp.Candidates[0].Content.Parts) > 0 {
		for _, part := range geminiResp.Candidates[0].Content.Parts {
			if part.Text != "" {
				textContent += part.Text
			}
			if part.InlineData != nil && part.InlineData.Data != "" {
				timestamp := time.Now().Format("20060102-150405")
				// #nosec G404 - weak random is sufficient for filename random identifier
				randomId := fmt.Sprintf("%06d", rand.Intn(1000000))
				fileName := fmt.Sprintf("edited-%s-%s.png", timestamp, randomId)
				filePath := filepath.Join(imagesDir, fileName)

				imageBytes, err := json.Marshal(part.InlineData.Data)
				if err != nil {
					continue
				}
				var decodedBytes []byte
				if err := json.Unmarshal(imageBytes, &decodedBytes); err == nil {
					// #nosec G306 - generated image files must be user-viewable (0644)
					if err := os.WriteFile(filePath, decodedBytes, 0644); err == nil {
						savedFiles = append(savedFiles, filePath)
						lastImagePath = filePath
					}
				}

				content = append(content, map[string]interface{}{
					"type":     "image",
					"data":     part.InlineData.Data,
					"mimeType": part.InlineData.MimeType,
				})
			}
		}
	}

	statusText := fmt.Sprintf("🎨 Image edited with nano-banana (%s)!\n\nOriginal: %s\nEdit prompt: \"%s\"", model, imagePath, prompt)
	if textContent != "" {
		statusText += fmt.Sprintf("\n\nDescription: %s", textContent)
	}
	if len(savedFiles) > 0 {
		statusText += "\n\n📁 Edited image saved to:\n"
		for _, f := range savedFiles {
			statusText += fmt.Sprintf("- %s\n", f)
		}
		statusText += "\n🔄 To modify this image, use: continue_editing"
	} else {
		statusText += "\n\nNote: No edited image was generated."
	}

	textPart := map[string]interface{}{
		"type": "text",
		"text": statusText,
	}
	content = append([]map[string]interface{}{textPart}, content...)

	sendResponse(id, map[string]interface{}{
		"content": content,
	})
}

func runSetupWizard() {
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("🍌 Nano Banana MCP v2 - Interactive Setup Wizard 🍌")
	fmt.Println("==================================================")
	fmt.Println("This wizard will help you configure your Google AI Studio API key.")
	fmt.Println()

	var apiKey string
	for {
		fmt.Print("Enter your Gemini API key (should start with AIza...): ")
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("❌ Error reading input: %v\n", err)
			os.Exit(1)
		}
		apiKey = strings.TrimSpace(input)
		if apiKey == "" {
			fmt.Println("❌ API key cannot be empty. Please try again.")
			continue
		}
		break
	}

	fmt.Println("\n🔍 Validating your API key against Google AI Studio API...")
	
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models?key=%s", apiKey)
	resp, err := httpClient.Get(url)
	if err != nil {
		fmt.Printf("❌ Connection error while validating key: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		var errResp struct {
			Error struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
				Status  string `json:"status"`
			} `json:"error"`
		}
		_ = json.Unmarshal(bodyBytes, &errResp)
		if errResp.Error.Message != "" {
			fmt.Printf("❌ API key validation failed (Status %d): %s\n", resp.StatusCode, errResp.Error.Message)
		} else {
			fmt.Printf("❌ API key validation failed with HTTP status %d\n", resp.StatusCode)
		}
		os.Exit(1)
	}

	fmt.Println("✅ API key is valid!")

	fmt.Println("\n💾 Saving configuration...")
	if err := saveConfig(apiKey); err != nil {
		fmt.Printf("❌ Failed to save configuration: %v\n", err)
		os.Exit(1)
	}

	home, _ := os.UserHomeDir()
	globalPath := filepath.Join(home, ".nano-banana-config.json")
	fmt.Printf("🎉 Setup completed successfully! Key saved to: %s\n", globalPath)
}
