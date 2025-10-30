package main

import "fmt"

func main() {
	fmt.Println("🚀 My SSG Starting...")

	// Step 1 - Read markdown file
	content, err := readMarkDownFile("content/first-post.md")
	if err != nil {
		fmt.Println("Error reading file:", err)
		return
	}

	// Step 2 - Convert to HTML
	html := convertToHTML(content)

	// Step 3 - Write HTML file
	err = writeHTMLFile("public/first-post.html", html)
	if err != nil {
		fmt.Println("Error writing to file:", err)
		return
	}

	fmt.Println("✅ Build complete!")
}

func readMarkDownFile(filename string) (string, error) {
	return "", nil
}

func convertToHTML(markdown string) string {
	return "<h1>Temporary HTML</h1>"
}

func writeHTMLFile(filename string, content string) error {
	return nil
}
