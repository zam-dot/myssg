package main

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
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

// ================== MAIN FUNCTION =======================

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: myssg <command>")
		fmt.Println("Commands: build, serve")
		return
	}

	command := os.Args[1]
	switch command {
	case "build":
		buildSite()
	case "serve":
		serveSite()
	default:
		fmt.Printf("Unknown command: %s\n", command)
	}
}

// ================ LIVE RELOAD ====================

const liveReloadScript = `
<script>
// Smart live reload - uses Server-Sent Events
(function() {
    const eventSource = new EventSource('/_livereload');
    
    eventSource.onmessage = function(event) {
        if (event.data === 'reload') {
            console.log('🔄 Live reload: rebuilding complete, refreshing page...');
            window.location.reload();
        }
    };
    
    eventSource.onerror = function(error) {
        console.log('Live reload connection error:', error);
        // Optionally try to reconnect
    };
    
    console.log('✨ Live reload enabled');
})();
</script>
`

// ================ LIVE RELOAD MANAGER ====================

var liveReloadClients = make(map[string]chan bool) // track connected clients
var liveReloadMutex = sync.Mutex{}

// Notify all connected clients to reload
func notifyLiveReload() {
	liveReloadMutex.Lock()
	defer liveReloadMutex.Unlock()

	fmt.Printf("🔔 Notifying %d clients to reload...\n", len(liveReloadClients))

	for id, ch := range liveReloadClients {
		select {
		case ch <- true:
			// Notification sent
		default:
			// Client might be disconnected, remove it
			delete(liveReloadClients, id)
		}
	}
}

// ===================== BUILD SITE  ==========================

func buildSite() {
	fmt.Println("🚀 Building site...")

	// Add a build timestamp to help live reload detect changes
	buildTime := time.Now().UnixMilli()
	fmt.Printf("📅 Build timestamp: %d\n", buildTime)

	// MOVE all the build logic here from main()
	// Create a new site
	site := &Site{Posts: []*Post{}}

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

// ===================== SERVE SITE =========================

func serveSite() {
	fmt.Println("🚀 Starting development server on http://localhost:3000")
	fmt.Println("👀 Watching content/ for changes...")
	fmt.Println("Press Ctrl+C to stop")

	// First build
	buildSite()

	// Start file watcher
	go watchFiles()

	// Serve static files
	http.Handle("/", http.FileServer(http.Dir("public")))

	// Live reload endpoint
	http.HandleFunc("/_livereload", handleLiveReload)

	fmt.Println("🌐 Server running at http://localhost:3000")
	fmt.Println("✨ Live reload enabled!")
	err := http.ListenAndServe(":3000", nil)
	if err != nil {
		fmt.Printf("Error starting server: %v\n", err)
	}
}

// Handle live reload connections
func handleLiveReload(w http.ResponseWriter, r *http.Request) {
	// Set headers for Server-Sent Events
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Create a channel for this client
	clientID := fmt.Sprintf("%p", w) // Simple ID based on memory address
	reloadChan := make(chan bool, 1)

	liveReloadMutex.Lock()
	liveReloadClients[clientID] = reloadChan
	liveReloadMutex.Unlock()

	fmt.Printf("➕ Live reload client connected: %s\n", clientID)

	// Clean up when client disconnects
	defer func() {
		liveReloadMutex.Lock()
		delete(liveReloadClients, clientID)
		liveReloadMutex.Unlock()
		fmt.Printf("➖ Live reload client disconnected: %s\n", clientID)
	}()

	// Keep connection open and wait for reload signals
	for {
		select {
		case <-reloadChan:
			fmt.Printf("📡 Sending reload signal to client: %s\n", clientID)
			fmt.Fprintf(w, "data: reload\n\n")
			w.(http.Flusher).Flush()

		case <-r.Context().Done():
			// Client disconnected
			return
		}
	}
}

// ================== WATCH FILES =======================

func watchFiles() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		fmt.Printf("Error creating watcher: %v\n", err)
		return
	}
	defer watcher.Close()

	err = watcher.Add("content")
	if err != nil {
		fmt.Printf("Error watching content/: %v\n", err)
		return
	}

	fmt.Println("✅ Now watching content/ for changes...")

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			// Filter out editor temporary files
			if isEditorTempFile(event.Name) {
				continue
			}

			if event.Has(fsnotify.Write) && strings.HasSuffix(event.Name, ".md") {
				fmt.Printf("🔄 Detected change: %s\n", filepath.Base(event.Name))
				fmt.Println("📦 Rebuilding site...")
				buildSite()
				fmt.Println("✅ Rebuild complete!")

				// 🔥 TRIGGER LIVE RELOAD!
				notifyLiveReload()
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			fmt.Printf("❌ Watcher error: %v\n", err)
		}
	}
}

// Helper to filter out editor temporary files
func isEditorTempFile(filename string) bool {
	base := filepath.Base(filename)

	// Common editor temporary file patterns
	tempPatterns := []string{
		"~",    // Vim/Emacs backups
		".swp", // Vim swap files
		".swx", // Vim swap files
		".tmp", // General temp files
		".TMP", // General temp files
		".git", // Git internals
		"4913", // Your specific temp file
	}

	for _, pattern := range tempPatterns {
		if strings.Contains(base, pattern) {
			fmt.Printf("🔇 Ignoring temp file: %s\n", base) // Debug output
			return true
		}
	}

	return false
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

	// Inject live reload script before closing </body> tag
	finalContent := injectLiveReload(content)

	// Write the HTML content to file
	if err := os.WriteFile(filename, []byte(finalContent), 0644); err != nil {
		return err
	}

	return nil
}

func injectLiveReload(html string) string {
	// If there's a closing </body> tag, inject script before it
	if strings.Contains(html, "</body>") {
		return strings.Replace(html, "</body>", liveReloadScript+"\n</body>", 1)
	}

	// If no body tag, just append the script
	return html + liveReloadScript
}

// ================ POINTER ====================
func (s *Site) AddPost(post *Post) { // Pointer receiver
	s.Posts = append(s.Posts, post)
}
