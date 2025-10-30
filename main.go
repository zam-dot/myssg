package main

import (
	"bytes"
	"fmt"
	"os"

	"github.com/yuin/goldmark"
)

// ================ MAIN FUNCTION ====================

func main() {
	fmt.Println("🚀 My SSG Starting...")

	// Step 1 - Read markdown file
	content, err := readMarkDownFile("content/first-post.md")
	if err != nil {
		fmt.Println("Error reading file:", err)
		return
	}

	fmt.Printf("📖 Read %d characters from markdown file\n", len(content))
	fmt.Println("First 100 chars:", content[:min(100, len(content))])

	// Step 2 - Convert to HTML
	html := convertToHTML(content)

	// Step 3 - Write HTML file
	err = writeHTMLFile("public/first-post.html", html)
	if err != nil {
		fmt.Println("Error writing to file:", err)
		return
	}

	fmt.Println("✅ Build complete! Check public/first-post.html")
}

// ================= READ MARKDOWN =====================

func readMarkDownFile(filename string) (string, error) {
	// Read the entire file
	content, err := os.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("could not read file %s: %v", filename, err)
	}

	// convert []byte to string and return
	return string(content), nil
}

// ================ CONVERT TO HTML ====================

func convertToHTML(markdown string) string {
	// Create a new Goldmark parser
	md := goldmark.New()

	// Convert markdown to HTML
	var buf bytes.Buffer
	if err := md.Convert([]byte(markdown), &buf); err != nil {
		// If conversion failes, return a basic error message
		return fmt.Sprintf("<p>Error converting markdown: %v</p>", err)
	}

	return buf.String()
}

// ================ WRITE HTML FILE ====================

func writeHTMLFile(filename string, content string) error {
	return nil
}

// Helper function to avoid index out of bounds
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
