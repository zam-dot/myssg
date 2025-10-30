package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/yuin/goldmark"
)

type Site struct {
	Config *Config        // Pointer to shared config
	Posts  []*Post        // Pointer to avoid copying large Posts
	Pages  []*Page        // Same here
	cache  *TemplateCache // Internal Cache
}

type Post struct {
	Title   string
	Content string
	Slug    string // URL-friendly name
}

type Config struct {
}

type TemplateCache struct {
}

type Page struct {
}

// ================ MAIN FUNCTION ====================

func main() {
	fmt.Println("🚀 My SSG Starting...")

	// Create a new site
	site := &Site{
		Posts: []*Post{},
	}

	// Process all markdown files in content/
	err := processContentFolder(site, "content")
	if err != nil {
		fmt.Println("Error processing content:", err)
		return
	}

	fmt.Printf("📚 Processed %d posts\n", len(site.Posts))

	// Generate HTML for all posts
	for _, post := range site.Posts {
		html := convertToHTML(post.Content)
		filename := fmt.Sprintf("public/%s.html", post.Slug)

		err = writeHTMLFile(filename, html)
		if err != nil {
			fmt.Println("Error writing file:", err)
			return
		}

		fmt.Printf("✅ Generated: %s\n", filename)
	}

	fmt.Println("🎉 Build complete! Check public/ folder")
}

// ================ PROCESS CONTENT FOLDER ====================

func processContentFolder(site *Site, contentDir string) error {
	// Read the content directory
	entries, err := os.ReadDir(contentDir)
	if err != nil {
		return fmt.Errorf("could not read content directory: %v", err)
	}

	// Process each markdown file
	for _, entry := range entries {
		if !entry.IsDir() && hasMarkdownExtension(entry.Name()) {
			filename := contentDir + "/" + entry.Name()

			// Read the file
			content, err := readMarkDownFile(filename)
			if err != nil {
				return err
			}

			// Create a new Post
			post := &Post{
				Title:   extractTitle(entry.Name()), // Simple title from filename
				Content: content,
				Slug:    generateSlug(entry.Name()),
			}

			// Add to site using the pointer method you created!
			site.AddPost(post)

			fmt.Printf("📖 Processed: %s\n", entry.Name())
		}
	}

	return nil
}

// ================ HELPER FUNCTIONS ====================

func hasMarkdownExtension(filename string) bool {
	return len(filename) > 3 && filename[len(filename)-3:] == ".md"
}

func extractTitle(filename string) string {
	// Remove .md extension and make it pretty
	name := filename[:len(filename)-3]
	// Simple capitalization - you can improve this later
	if len(name) > 0 {
		return string(name[0]-32) + name[1:] // Capitalize first letter
	}
	return name
}

func generateSlug(filename string) string {
	// Convert "My Post.md" to "my-post"
	name := filename[:len(filename)-3]
	// Simple slug for now - just lowercase
	// You can add proper slugification later
	return strings.ToLower(name)
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
	// Create directory if it doesn't exist
	if err := os.MkdirAll("public", 0755); err != nil {
		return err
	}

	// Write the HTML content to file
	if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
		return err
	}

	return nil
}

// ================ POINTER ====================
func (s *Site) AddPost(post *Post) { // Pointer receiver
	s.Posts = append(s.Posts, post)
}

// Helper function to avoid index out of bounds
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
