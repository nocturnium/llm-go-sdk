// Example: Vision / Multi-modal
//
// This example demonstrates how to send images to vision-capable models
// for analysis, description, or question answering about visual content.
//
// Run with: go run ./examples/vision
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v2"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/providers/anthropic"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/providers/gemini"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/providers/openai"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := createClient()
	if client == nil {
		cancel()
		log.Fatal("No API key found. Set OPENAI_API_KEY, ANTHROPIC_API_KEY, or GEMINI_API_KEY")
	}

	fmt.Printf("Using: %s (%s)\n\n", client.Provider(), client.Model())

	// Example 1: Analyze an image from URL
	fmt.Println("=== Image from URL ===")
	imageURL := "https://upload.wikimedia.org/wikipedia/commons/thumb/4/47/PNG_transparency_demonstration_1.png/280px-PNG_transparency_demonstration_1.png"

	msg := llms.NewImageMessage("What do you see in this image? Describe it briefly.", imageURL)

	resp, err := client.GenerateContent(ctx, []llms.Message{msg})
	if err != nil {
		log.Fatalf("Vision request failed: %v", err)
	}
	fmt.Printf("Response: %s\n\n", resp.Content)

	// Example 2: Multi-part message with text and image
	fmt.Println("=== Multi-part Message ===")
	multiPartMsg := llms.NewMultiPartMessage(llms.RoleUser,
		llms.NewTextPart("Look at this image and tell me:"),
		llms.NewTextPart("1. What colors do you see?"),
		llms.NewTextPart("2. What shapes are present?"),
		llms.NewImageURLPart(imageURL),
	)

	resp, err = client.GenerateContent(ctx, []llms.Message{multiPartMsg})
	if err != nil {
		log.Fatalf("Multi-part request failed: %v", err)
	}
	fmt.Printf("Response: %s\n\n", resp.Content)

	// Example 3: Multiple images comparison
	fmt.Println("=== Multiple Images ===")
	// Using the same image twice for demo (in practice, use different images)
	comparisonMsg := llms.NewMultiPartMessage(llms.RoleUser,
		llms.NewTextPart("Compare these two images. Are they the same or different?"),
		llms.NewImageURLPart(imageURL),
		llms.NewImageURLPart(imageURL),
	)

	resp, err = client.GenerateContent(ctx, []llms.Message{comparisonMsg})
	if err != nil {
		log.Fatalf("Comparison request failed: %v", err)
	}
	fmt.Printf("Response: %s\n\n", resp.Content)

	// Example 4: Image from local file (if exists)
	fmt.Println("=== Image from File (demo) ===")
	testImagePath := "test_image.png"

	// Check if a test image exists
	if _, err := os.Stat(testImagePath); err == nil {
		msg, err := llms.NewImageFileMessage("Describe this image.", testImagePath)
		if err != nil {
			log.Printf("Failed to load image file: %v", err)
		} else {
			resp, err = client.GenerateContent(ctx, []llms.Message{msg})
			if err != nil {
				log.Printf("File image request failed: %v", err)
			} else {
				fmt.Printf("Response: %s\n", resp.Content)
			}
		}
	} else {
		fmt.Println("No test_image.png found. To test file loading:")
		fmt.Println("  1. Place an image named 'test_image.png' in the current directory")
		fmt.Println("  2. Run this example again")
		fmt.Println()
		fmt.Println("Supported formats: PNG, JPEG, GIF, WebP (max 20MB)")
	}

	// Example 5: Using vision with conversation context
	fmt.Println("\n=== Vision in Conversation ===")
	conversation := []llms.Message{
		{Role: llms.RoleSystem, Content: "You are an art critic. Analyze images with an artistic perspective."},
		llms.NewImageMessage("What artistic elements do you notice in this image?", imageURL),
	}

	resp, err = client.GenerateContent(ctx, conversation)
	if err != nil {
		log.Fatalf("Vision conversation failed: %v", err)
	}
	fmt.Printf("Art Critic: %s\n", resp.Content)
}

func createClient() llms.LLM {
	// All major providers support vision
	if os.Getenv("OPENAI_API_KEY") != "" {
		client, err := openai.New(openai.WithModel("gpt-4o")) // GPT-4o has vision
		if err == nil {
			return client
		}
	}
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		client, err := anthropic.New() // Claude 3+ has vision
		if err == nil {
			return client
		}
	}
	if os.Getenv("GEMINI_API_KEY") != "" || os.Getenv("GOOGLE_API_KEY") != "" {
		client, err := gemini.New() // Gemini has vision
		if err == nil {
			return client
		}
	}
	return nil
}
