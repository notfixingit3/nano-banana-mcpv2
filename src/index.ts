#!/usr/bin/env node

import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
  Tool,
  CallToolRequest,
  CallToolResult,
  ErrorCode,
  McpError,
} from "@modelcontextprotocol/sdk/types.js";
import { GoogleGenAI } from "@google/genai";
import { z } from "zod";
import fs from "fs/promises";
import path from "path";
import { config as dotenvConfig } from "dotenv";
import os from "os";

// Load environment variables
dotenvConfig();

const ConfigSchema = z.object({
  geminiApiKey: z.string().min(1, "Gemini API key is required"),
});

type Config = z.infer<typeof ConfigSchema>;

class NanoBananaMCP {
  private server: Server;
  private genAI: GoogleGenAI | null = null;
  private config: Config | null = null;
  private lastImagePath: string | null = null;
  private configSource: 'environment' | 'config_file' | 'not_configured' = 'not_configured';

  constructor() {
    this.server = new Server(
      {
        name: "nano-banana-mcp",
        version: "1.0.0",
      },
      {
        capabilities: {
          tools: {},
        },
      }
    );

    this.setupHandlers();
  }

  private setupHandlers() {
    this.server.setRequestHandler(ListToolsRequestSchema, async () => {
      return {
        tools: [
          {
            name: "configure_gemini_token",
            description: "Configure your Gemini API token for nano-banana image generation",
            inputSchema: {
              type: "object",
              properties: {
                apiKey: {
                  type: "string",
                  description: "Your Gemini API key from Google AI Studio",
                },
              },
              required: ["apiKey"],
            },
          },
          {
            name: "generate_image",
            description: "Generate a NEW image from text prompt. Use this ONLY when creating a completely new image, not when modifying an existing one.",
            inputSchema: {
              type: "object",
              properties: {
                prompt: {
                  type: "string",
                  description: "Text prompt describing the NEW image to create from scratch",
                },
                model: {
                  type: "string",
                  description: "Optional model name to use for image generation (e.g., 'gemini-3.1-flash-image' or 'gemini-3-pro-image'). Defaults to the value of environment variable GEMINI_IMAGE_MODEL, or 'gemini-3.1-flash-image' if unset.",
                },
                aspectRatio: {
                  type: "string",
                  enum: ["1:1", "16:9", "9:16", "4:3", "3:4"],
                  description: "Optional aspect ratio for the generated image. Defaults to '1:1'.",
                },
              },
              required: ["prompt"],
            },
          },
          {
            name: "generate_imagen",
            description: "Generate a NEW image from text prompt using Google's dedicated Imagen model (e.g. 'imagen-3.0-generate-002'). Optimized for high-fidelity text-to-image.",
            inputSchema: {
              type: "object",
              properties: {
                prompt: {
                  type: "string",
                  description: "Text prompt describing the NEW image to generate",
                },
                model: {
                  type: "string",
                  description: "Optional model name to use (e.g., 'imagen-3.0-generate-002'). Defaults to 'imagen-3.0-generate-002'.",
                },
                aspectRatio: {
                  type: "string",
                  enum: ["1:1", "3:4", "4:3", "9:16", "16:9"],
                  description: "Optional aspect ratio for the generated image. Defaults to '1:1'.",
                },
                numberOfImages: {
                  type: "integer",
                  minimum: 1,
                  maximum: 4,
                  description: "Optional number of images to generate (1-4). Defaults to 1.",
                },
                negativePrompt: {
                  type: "string",
                  description: "Optional description of elements to avoid in the generated image.",
                },
              },
              required: ["prompt"],
            },
          },
          {
            name: "edit_image",
            description: "Edit a SPECIFIC existing image file, optionally using additional reference images. Use this when you have the exact file path of an image to modify.",
            inputSchema: {
              type: "object",
              properties: {
                imagePath: {
                  type: "string",
                  description: "Full file path to the main image file to edit",
                },
                prompt: {
                  type: "string",
                  description: "Text describing the modifications to make to the existing image",
                },
                referenceImages: {
                  type: "array",
                  items: {
                    type: "string"
                  },
                  description: "Optional array of file paths to additional reference images to use during editing (e.g., for style transfer, adding elements, etc.)",
                },
                model: {
                  type: "string",
                  description: "Optional model name to use for image editing. Defaults to environment variable GEMINI_IMAGE_MODEL, or 'gemini-3.1-flash-image' if unset.",
                },
              },
              required: ["imagePath", "prompt"],
            },
          },
          {
            name: "get_configuration_status",
            description: "Check if Gemini API token is configured",
            inputSchema: {
              type: "object",
              properties: {},
              additionalProperties: false,
            },
          },
          {
            name: "continue_editing",
            description: "Continue editing the LAST image that was generated or edited in this session, optionally using additional reference images. Use this for iterative improvements, modifications, or changes to the most recent image. This automatically uses the previous image without needing a file path.",
            inputSchema: {
              type: "object",
              properties: {
                prompt: {
                  type: "string",
                  description: "Text describing the modifications/changes/improvements to make to the last image (e.g., 'change the hat color to red', 'remove the background', 'add flowers')",
                },
                referenceImages: {
                  type: "array",
                  items: {
                    type: "string"
                  },
                  description: "Optional array of file paths to additional reference images to use during editing (e.g., for style transfer, adding elements from other images, etc.)",
                },
                model: {
                  type: "string",
                  description: "Optional model name to use for image editing. Defaults to environment variable GEMINI_IMAGE_MODEL, or 'gemini-3.1-flash-image' if unset.",
                },
              },
              required: ["prompt"],
            },
          },
          {
            name: "get_last_image_info",
            description: "Get information about the last generated/edited image in this session (file path, size, etc.). Use this to check what image is currently available for continue_editing.",
            inputSchema: {
              type: "object",
              properties: {},
              additionalProperties: false,
            },
          },
        ] as Tool[],
      };
    });

    this.server.setRequestHandler(CallToolRequestSchema, async (request: CallToolRequest): Promise<CallToolResult> => {
      try {
        switch (request.params.name) {
          case "configure_gemini_token":
            return await this.configureGeminiToken(request);
          
          case "generate_image":
            return await this.generateImage(request);
          
          case "generate_imagen":
            return await this.generateImagen(request);
          
          case "edit_image":
            return await this.editImage(request);
          
          case "get_configuration_status":
            return await this.getConfigurationStatus();
          
          case "continue_editing":
            return await this.continueEditing(request);
          
          case "get_last_image_info":
            return await this.getLastImageInfo();
          
          default:
            throw new McpError(ErrorCode.MethodNotFound, `Unknown tool: ${request.params.name}`);
        }
      } catch (error) {
        if (error instanceof McpError) {
          throw error;
        }
        throw new McpError(ErrorCode.InternalError, `Tool execution failed: ${error instanceof Error ? error.message : String(error)}`);
      }
    });
  }

  private async configureGeminiToken(request: CallToolRequest): Promise<CallToolResult> {
    const { apiKey } = request.params.arguments as { apiKey: string };
    
    try {
      ConfigSchema.parse({ geminiApiKey: apiKey });
      
      this.config = { geminiApiKey: apiKey };
      this.genAI = new GoogleGenAI({ apiKey });
      this.configSource = 'config_file'; // Manual configuration via tool
      
      await this.saveConfig();
      
      return {
        content: [
          {
            type: "text",
            text: "✅ Gemini API token configured successfully! You can now use nano-banana image generation features.",
          },
        ],
      };
    } catch (error) {
      if (error instanceof z.ZodError) {
        throw new McpError(ErrorCode.InvalidParams, `Invalid API key: ${error.errors[0]?.message}`);
      }
      throw error;
    }
  }

  private async generateImage(request: CallToolRequest): Promise<CallToolResult> {
    if (!this.ensureConfigured()) {
      throw new McpError(ErrorCode.InvalidRequest, "Gemini API token not configured. Use configure_gemini_token first.");
    }

    const { prompt, model: customModel, aspectRatio } = request.params.arguments as { 
      prompt: string; 
      model?: string;
      aspectRatio?: string;
    };
    const model = this.resolveModel(customModel);
    
    try {
      const config: any = {};
      if (aspectRatio) {
        config.imageConfig = { aspectRatio };
      }

      const response = await this.genAI!.models.generateContent({
        model: model,
        contents: prompt,
        config: config,
      });
      
      // Process response to extract image data
      const content: any[] = [];
      const savedFiles: string[] = [];
      let textContent = "";
      
      // Get appropriate save directory based on OS
      const imagesDir = this.getImagesDirectory();
      
      // Create directory
      await fs.mkdir(imagesDir, { recursive: true, mode: 0o755 });
      
      if (response.candidates && response.candidates[0]?.content?.parts) {
        for (const part of response.candidates[0].content.parts) {
          // Process text content
          if (part.text) {
            textContent += part.text;
          }
          
          // Process image data
          if (part.inlineData?.data) {
            const timestamp = new Date().toISOString().replace(/[:.]/g, '-');
            const randomId = Math.random().toString(36).substring(2, 8);
            const fileName = `generated-${timestamp}-${randomId}.png`;
            const filePath = path.join(imagesDir, fileName);
            
            const imageBuffer = Buffer.from(part.inlineData.data, 'base64');
            await fs.writeFile(filePath, imageBuffer);
            savedFiles.push(filePath);
            this.lastImagePath = filePath;
            
            // Add image to MCP response
            content.push({
              type: "image",
              data: part.inlineData.data,
              mimeType: part.inlineData.mimeType || "image/png",
            });
          }
        }
      }
      
      // Build response content
      let statusText = `🎨 Image generated with nano-banana (${model})!\n\nPrompt: "${prompt}"`;
      
      if (textContent) {
        statusText += `\n\nDescription: ${textContent}`;
      }
      
      if (savedFiles.length > 0) {
        statusText += `\n\n📁 Image saved to:\n${savedFiles.map(f => `- ${f}`).join('\n')}`;
        statusText += `\n\n💡 View the image by:`;
        statusText += `\n1. Opening the file at the path above`;
        statusText += `\n2. Clicking on "Called generate_image" in Cursor to expand the MCP call details`;
        statusText += `\n\n🔄 To modify this image, use: continue_editing`;
        statusText += `\n📋 To check current image info, use: get_last_image_info`;
      } else {
        statusText += `\n\nNote: No image was generated. The model may have returned only text.`;
        statusText += `\n\n💡 Tip: Try running the command again - sometimes the first call needs to warm up the model.`;
      }
      
      // Add text content first
      content.unshift({
        type: "text",
        text: statusText,
      });
      
      return { content };
      
    } catch (error) {
      console.error("Error generating image:", error);
      throw new McpError(
        ErrorCode.InternalError,
        `Failed to generate image: ${error instanceof Error ? error.message : String(error)}`
      );
    }
  }

  private async generateImagen(request: CallToolRequest): Promise<CallToolResult> {
    if (!this.ensureConfigured()) {
      throw new McpError(ErrorCode.InvalidRequest, "Gemini API token not configured. Use configure_gemini_token first.");
    }

    const { prompt, model: customModel, aspectRatio, numberOfImages, negativePrompt } = request.params.arguments as { 
      prompt: string; 
      model?: string;
      aspectRatio?: "1:1" | "3:4" | "4:3" | "9:16" | "16:9";
      numberOfImages?: number;
      negativePrompt?: string;
    };
    
    const model = customModel || "imagen-3.0-generate-002";
    
    try {
      const config: any = {};
      if (aspectRatio) config.aspectRatio = aspectRatio;
      if (numberOfImages !== undefined) config.numberOfImages = numberOfImages;
      if (negativePrompt) config.negativePrompt = negativePrompt;

      const response = await this.genAI!.models.generateImages({
        model: model,
        prompt: prompt,
        config: config
      });
      
      const content: any[] = [];
      const savedFiles: string[] = [];
      
      // Get appropriate save directory based on OS
      const imagesDir = this.getImagesDirectory();
      await fs.mkdir(imagesDir, { recursive: true, mode: 0o755 });
      
      if (response.generatedImages && response.generatedImages.length > 0) {
        for (const generatedImage of response.generatedImages) {
          const imgBytes = generatedImage.image?.imageBytes;
          if (imgBytes) {
            const timestamp = new Date().toISOString().replace(/[:.]/g, '-');
            const randomId = Math.random().toString(36).substring(2, 8);
            const fileName = `imagen-${timestamp}-${randomId}.png`;
            const filePath = path.join(imagesDir, fileName);
            
            const imageBuffer = Buffer.from(imgBytes, 'base64');
            await fs.writeFile(filePath, imageBuffer);
            savedFiles.push(filePath);
            this.lastImagePath = filePath;
            
            // Add image to MCP response
            content.push({
              type: "image",
              data: imgBytes,
              mimeType: "image/png",
            });
          }
        }
      }
      
      // Build response content
      let statusText = `🎨 Image(s) generated using Google Imagen (${model})!\n\nPrompt: "${prompt}"`;
      if (aspectRatio) {
        statusText += `\nAspect Ratio: ${aspectRatio}`;
      }
      if (negativePrompt) {
        statusText += `\nNegative Prompt: "${negativePrompt}"`;
      }
      
      if (savedFiles.length > 0) {
        statusText += `\n\n📁 Image(s) saved to:\n${savedFiles.map(f => `- ${f}`).join('\n')}`;
        statusText += `\n\n💡 View the image by:`;
        statusText += `\n1. Opening the file at the path above`;
        statusText += `\n2. Clicking on "Called generate_imagen" in Cursor/Claude to expand details`;
        statusText += `\n\n🔄 To modify the last image, use: continue_editing`;
        statusText += `\n📋 To check current image info, use: get_last_image_info`;
      } else {
        statusText += `\n\nNote: No image was returned by the Imagen API.`;
      }
      
      // Add text content first
      content.unshift({
        type: "text",
        text: statusText,
      });
      
      return { content };
      
    } catch (error) {
      console.error("Error generating Imagen image:", error);
      throw new McpError(
        ErrorCode.InternalError,
        `Failed to generate Imagen image: ${error instanceof Error ? error.message : String(error)}`
      );
    }
  }

  private async editImage(request: CallToolRequest): Promise<CallToolResult> {
    if (!this.ensureConfigured()) {
      throw new McpError(ErrorCode.InvalidRequest, "Gemini API token not configured. Use configure_gemini_token first.");
    }

    const { imagePath, prompt, referenceImages, model: customModel } = request.params.arguments as { 
      imagePath: string; 
      prompt: string; 
      referenceImages?: string[];
      model?: string;
    };
    const model = this.resolveModel(customModel);
    
    try {
      // Prepare the main image
      const imageBuffer = await fs.readFile(imagePath);
      const mimeType = this.getMimeType(imagePath);
      const imageBase64 = imageBuffer.toString('base64');
      
      // Prepare all image parts
      const imageParts: any[] = [
        { 
          inlineData: {
            data: imageBase64,
            mimeType: mimeType,
          }
        }
      ];
      
      // Add reference images if provided
      if (referenceImages && referenceImages.length > 0) {
        for (const refPath of referenceImages) {
          try {
            const refBuffer = await fs.readFile(refPath);
            const refMimeType = this.getMimeType(refPath);
            const refBase64 = refBuffer.toString('base64');
            
            imageParts.push({
              inlineData: {
                data: refBase64,
                mimeType: refMimeType,
              }
            });
          } catch (error) {
            // Continue with other images, don't fail the entire operation
            continue;
          }
        }
      }
      
      // Add the text prompt
      imageParts.push({ text: prompt });
      
      // Use new API format with multiple images and text
      const response = await this.genAI!.models.generateContent({
        model: model,
        contents: [
          {
            parts: imageParts
          }
        ],
      });
      
      // Process response
      const content: any[] = [];
      const savedFiles: string[] = [];
      let textContent = "";
      
      // Get appropriate save directory
      const imagesDir = this.getImagesDirectory();
      await fs.mkdir(imagesDir, { recursive: true, mode: 0o755 });
      
      // Extract image from response
      if (response.candidates && response.candidates[0]?.content?.parts) {
        for (const part of response.candidates[0].content.parts) {
          if (part.text) {
            textContent += part.text;
          }
          
          if (part.inlineData) {
            // Save edited image
            const timestamp = new Date().toISOString().replace(/[:.]/g, '-');
            const randomId = Math.random().toString(36).substring(2, 8);
            const fileName = `edited-${timestamp}-${randomId}.png`;
            const filePath = path.join(imagesDir, fileName);
            
            if (part.inlineData.data) {
              const imageBuffer = Buffer.from(part.inlineData.data, 'base64');
              await fs.writeFile(filePath, imageBuffer);
              savedFiles.push(filePath);
              this.lastImagePath = filePath;
            }
            
            // Add to MCP response
            if (part.inlineData.data) {
              content.push({
                type: "image",
                data: part.inlineData.data,
                mimeType: part.inlineData.mimeType || "image/png",
              });
            }
          }
        }
      }
      
      // Build response
      let statusText = `🎨 Image edited with nano-banana (${model})!\n\nOriginal: ${imagePath}\nEdit prompt: "${prompt}"`;
      
      if (referenceImages && referenceImages.length > 0) {
        statusText += `\n\nReference images used:\n${referenceImages.map(f => `- ${f}`).join('\n')}`;
      }
      
      if (textContent) {
        statusText += `\n\nDescription: ${textContent}`;
      }
      
      if (savedFiles.length > 0) {
        statusText += `\n\n📁 Edited image saved to:\n${savedFiles.map(f => `- ${f}`).join('\n')}`;
        statusText += `\n\n💡 View the edited image by:`;
        statusText += `\n1. Opening the file at the path above`;
        statusText += `\n2. Clicking on "Called edit_image" in Cursor to expand the MCP call details`;
        statusText += `\n\n🔄 To continue editing, use: continue_editing`;
        statusText += `\n📋 To check current image info, use: get_last_image_info`;
      } else {
        statusText += `\n\nNote: No edited image was generated.`;
        statusText += `\n\n💡 Tip: Try running the command again - sometimes the first call needs to warm up the model.`;
      }
      
      content.unshift({
        type: "text",
        text: statusText,
      });
      
      return { content };
      
    } catch (error) {
      throw new McpError(
        ErrorCode.InternalError,
        `Failed to edit image: ${error instanceof Error ? error.message : String(error)}`
      );
    }
  }

  private async getConfigurationStatus(): Promise<CallToolResult> {
    const isConfigured = this.config !== null && this.genAI !== null;
    
    let statusText: string;
    let sourceInfo = "";
    
    if (isConfigured) {
      statusText = "✅ Gemini API token is configured and ready to use";
      
      switch (this.configSource) {
        case 'environment':
          sourceInfo = "\n📍 Source: Environment variable (GEMINI_API_KEY)\n💡 This is the most secure configuration method.";
          break;
        case 'config_file':
          sourceInfo = "\n📍 Source: Configuration file (~/.nano-banana-config.json)\n💡 Consider using environment variables for better security.";
          break;
      }
    } else {
      statusText = "❌ Gemini API token is not configured";
      sourceInfo = `

📝 Configuration options (in priority order):
1. 🥇 MCP client environment variables (Recommended)
2. 🥈 System environment variable: GEMINI_API_KEY  
3. 🥉 Use configure_gemini_token tool

💡 For the most secure setup, add this to your MCP configuration:
"env": { "GEMINI_API_KEY": "your-api-key-here" }`;
    }
    
    return {
      content: [
        {
          type: "text",
          text: statusText + sourceInfo,
        },
      ],
    };
  }

  private async continueEditing(request: CallToolRequest): Promise<CallToolResult> {
    if (!this.ensureConfigured()) {
      throw new McpError(ErrorCode.InvalidRequest, "Gemini API token not configured. Use configure_gemini_token first.");
    }

    if (!this.lastImagePath) {
      throw new McpError(ErrorCode.InvalidRequest, "No previous image found. Please generate or edit an image first, then use continue_editing for subsequent edits.");
    }

    const { prompt, referenceImages, model } = request.params.arguments as { 
      prompt: string; 
      referenceImages?: string[];
      model?: string;
    };

    // 检查最后的图片文件是否存在
    try {
      await fs.access(this.lastImagePath);
    } catch {
      throw new McpError(ErrorCode.InvalidRequest, `Last image file not found at: ${this.lastImagePath}. Please generate a new image first.`);
    }

    // Use editImage logic with lastImagePath
    
    return await this.editImage({
      method: "tools/call",
      params: {
        name: "edit_image",
        arguments: {
          imagePath: this.lastImagePath,
          prompt: prompt,
          referenceImages: referenceImages,
          model: model
        }
      }
    } as CallToolRequest);
  }

  private async getLastImageInfo(): Promise<CallToolResult> {
    if (!this.lastImagePath) {
      return {
        content: [
          {
            type: "text",
            text: "📷 No previous image found.\n\nPlease generate or edit an image first, then this command will show information about your last image.",
          },
        ],
      };
    }

    // 检查文件是否存在
    try {
      await fs.access(this.lastImagePath);
      const stats = await fs.stat(this.lastImagePath);
      
      return {
        content: [
          {
            type: "text",
            text: `📷 Last Image Information:\n\nPath: ${this.lastImagePath}\nFile Size: ${Math.round(stats.size / 1024)} KB\nLast Modified: ${stats.mtime.toLocaleString()}\n\n💡 Use continue_editing to make further changes to this image.`,
          },
        ],
      };
    } catch {
      return {
        content: [
          {
            type: "text",
            text: `📷 Last Image Information:\n\nPath: ${this.lastImagePath}\nStatus: ❌ File not found\n\n💡 The image file may have been moved or deleted. Please generate a new image.`,
          },
        ],
      };
    }
  }

  private ensureConfigured(): boolean {
    return this.config !== null && this.genAI !== null;
  }

  private resolveModel(customModel?: string): string {
    if (customModel && customModel.trim().length > 0) {
      return customModel.trim();
    }
    const envModel = process.env.GEMINI_IMAGE_MODEL;
    if (envModel && envModel.trim().length > 0) {
      return envModel.trim();
    }
    return "gemini-3.1-flash-image";
  }

  private getMimeType(filePath: string): string {
    const ext = path.extname(filePath).toLowerCase();
    switch (ext) {
      case '.jpg':
      case '.jpeg':
        return 'image/jpeg';
      case '.png':
        return 'image/png';
      case '.webp':
        return 'image/webp';
      default:
        return 'image/jpeg';
    }
  }

  private getImagesDirectory(): string {
    const platform = os.platform();
    
    if (platform === 'win32') {
      // Windows: Use Documents folder
      const homeDir = os.homedir();
      return path.join(homeDir, 'Documents', 'nano-banana-images');
    } else {
      // macOS/Linux: Use current directory or home directory if in system paths
      const cwd = process.cwd();
      const homeDir = os.homedir();
      
      // If in system directories, use home directory instead
      if (cwd.startsWith('/usr/') || cwd.startsWith('/opt/') || cwd.startsWith('/var/')) {
        return path.join(homeDir, 'nano-banana-images');
      }
      
      return path.join(cwd, 'generated_imgs');
    }
  }

  private async saveConfig(): Promise<void> {
    if (this.config) {
      // Save globally in home directory so it's accessible across different folders
      const configPath = path.join(os.homedir(), '.nano-banana-config.json');
      await fs.writeFile(configPath, JSON.stringify(this.config, null, 2));
    }
  }

  private async loadConfig(): Promise<void> {
    // Try to load from environment variable first
    const envApiKey = process.env.GEMINI_API_KEY;
    if (envApiKey) {
      try {
        this.config = ConfigSchema.parse({ geminiApiKey: envApiKey });
        this.genAI = new GoogleGenAI({ apiKey: this.config.geminiApiKey });
        this.configSource = 'environment';
        return;
      } catch (error) {
        // Invalid API key in environment
      }
    }
    
    // Fallback to local config first, then global config in home directory
    const localConfigPath = path.join(process.cwd(), '.nano-banana-config.json');
    const globalConfigPath = path.join(os.homedir(), '.nano-banana-config.json');
    
    for (const configPath of [localConfigPath, globalConfigPath]) {
      try {
        const configData = await fs.readFile(configPath, 'utf-8');
        const parsedConfig = JSON.parse(configData);
        
        this.config = ConfigSchema.parse(parsedConfig);
        this.genAI = new GoogleGenAI({ apiKey: this.config.geminiApiKey });
        this.configSource = 'config_file';
        
        // Auto-migrate: if we loaded from local config and global config doesn't exist, write it globally!
        if (configPath === localConfigPath) {
          try {
            await fs.access(globalConfigPath);
          } catch {
            // Global config doesn't exist, save it globally
            await fs.writeFile(globalConfigPath, JSON.stringify(this.config, null, 2));
            console.error(`[nano-banana-mcpv2] Automatically migrated local configuration to global: ${globalConfigPath}`);
          }
        }
        
        return;
      } catch {
        // Config file doesn't exist or is invalid, try next one
      }
    }
    
    this.configSource = 'not_configured';
  }

  public async run(): Promise<void> {
    await this.loadConfig();
    
    const transport = new StdioServerTransport();
    await this.server.connect(transport);
  }
}

const server = new NanoBananaMCP();
server.run().catch(console.error);