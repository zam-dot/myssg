package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/yuin/goldmark"
	"gopkg.in/yaml.v3"
)

type Site struct {
	Config    *Config            // Pointer to shared config
	Posts     []*Post            // Pointer to avoid copying large Posts
	Pages     []*Page            // Same here
	cache     *TemplateCache     // Internal Cache
	Templates *template.Template // Add this
}

type Post struct {
	Title   string
	Content string
	Slug    string
	Date    time.Time // Add this
	Tags    []string  // Add this
	Draft   bool      // Add this
	Excerpt string    // Add this
}

type BuildCache struct {
	LastBuild  time.Time
	FileHashes map[string]string
	mutex      sync.RWMutex
}

type TemplateData struct {
	Title            string
	Content          string
	Date             time.Time
	Tags             []string
	Excerpt          string
	CurrentYear      int
	LiveReloadScript string
}

type Config struct {
}

type TemplateCache struct {
}

type Page struct {
}

// Global build cache
var buildCache = &BuildCache{
	FileHashes: make(map[string]string),
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
		force := len(os.Args) > 2 && os.Args[2] == "--force"
		buildSite(force)
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

func buildSite(force bool) {
	fmt.Println("🚀 Building site...")

	if force {
		fmt.Println("🔨 Force rebuilding all files...")
		buildCache.mutex.Lock()
		buildCache.FileHashes = make(map[string]string)
		buildCache.mutex.Unlock()
	}

	// ⭐⭐ SMARTER TEMPLATE LOADING ⭐⭐
	// Load base template first
	baseTmpl, err := template.ParseFiles("templates/base.html")
	if err != nil {
		fmt.Printf("❌ Error loading base template: %v\n", err)
		return
	}

	// Parse post template and associate it with base
	postTmpl, err := baseTmpl.Clone()
	if err != nil {
		fmt.Printf("❌ Error cloning template: %v\n", err)
		return
	}

	postTmpl, err = postTmpl.ParseFiles("templates/post.html")
	if err != nil {
		fmt.Printf("❌ Error loading post template: %v\n", err)
		return
	}

	// Create a new site with templates
	site := &Site{
		Posts:     []*Post{},
		Templates: postTmpl, // Use the post-specific template
	}

	// ⭐⭐ THIS IS THE MISSING CALL ⭐⭐
	err = processContentFolder(site, "content")
	if err != nil {
		fmt.Println("Error processing content:", err)
		return
	}

	// Add a build timestamp to help live reload detect changes
	buildTime := time.Now().UnixMilli()
	fmt.Printf("📅 Build timestamp: %d\n", buildTime)

	fmt.Printf("📚 Processed %d posts\n", len(site.Posts))

	// Generate HTML for all posts
	for _, post := range site.Posts {
		html, err := renderPost(site.Templates, post)
		if err != nil {
			fmt.Printf("❌ Error rendering post %s: %v\n", post.Slug, err)
			return
		}

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

// =================== NEEDS REBUILD =======================

func (bc *BuildCache) needsRebuild(filepath string) bool {
	bc.mutex.RLock()
	defer bc.mutex.RUnlock()

	// Check if file exists and can be read
	if _, err := os.Stat(filepath); err != nil {
		return true
	}

	// Check if we have a previous hash
	oldHash, exists := bc.FileHashes[filepath]
	if !exists {
		return true
	}

	// Calculate current hash
	newHash, err := calculateFileHash(filepath)
	if err != nil {
		return true
	}

	return oldHash != newHash
}

func (bc *BuildCache) updateFile(filepath string) error {
	hash, err := calculateFileHash(filepath)
	if err != nil {
		return err
	}

	bc.mutex.Lock()
	defer bc.mutex.Unlock()

	bc.FileHashes[filepath] = hash
	bc.LastBuild = time.Now()

	return nil
}

// ================ FILE HASH CALCULATION ====================

func calculateFileHash(filepath string) (string, error) {
	content, err := os.ReadFile(filepath)
	if err != nil {
		return "", err
	}

	// Simple hash based on file size and modification time
	// For a more robust solution, you could use crypto/sha256
	info, err := os.Stat(filepath)
	if err != nil {
		return "", err
	}

	// Combine file size and modification time for a simple hash
	hash := fmt.Sprintf("%d-%d", len(content), info.ModTime().Unix())
	return hash, nil
}

// ===================== SERVE SITE =========================

func serveSite() {
	fmt.Println("🚀 Starting development server on http://localhost:3000")
	fmt.Println("👀 Watching content/ for changes...")
	fmt.Println("Press Ctrl+C to stop")

	// First build - don't force
	buildSite(false) // ← Add false parameter here

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

			if isEditorTempFile(event.Name) {
				continue
			}

			if event.Has(fsnotify.Write) && strings.HasSuffix(event.Name, ".md") {
				fmt.Printf("🔄 Detected change: %s\n", filepath.Base(event.Name))
				fmt.Println("📦 Rebuilding site...")
				buildSite(false) // ← Add false parameter here
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
	entries, err := os.ReadDir(contentDir)
	if err != nil {
		return fmt.Errorf("could not read content directory: %v", err)
	}

	fmt.Printf("🔍 Found %d entries in %s directory\n", len(entries), contentDir)

	var changedFiles []string
	totalMarkdownFiles := 0

	// First pass: check which files need rebuilding
	for _, entry := range entries {
		fmt.Printf("📁 Looking at: %s (dir: %v)\n", entry.Name(), entry.IsDir())

		if !entry.IsDir() && hasMarkdownExtension(entry.Name()) {
			totalMarkdownFiles++
			filename := contentDir + "/" + entry.Name()

			fmt.Printf("📄 Markdown file found: %s\n", entry.Name())

			if buildCache.needsRebuild(filename) {
				changedFiles = append(changedFiles, filename)
				fmt.Printf("🔄 Detected changes in: %s\n", entry.Name())
			} else {
				fmt.Printf("✅ No changes in: %s\n", entry.Name())
			}
		}
	}

	fmt.Printf("📊 Summary: %d total entries, %d markdown files, %d need rebuilding\n",
		len(entries), totalMarkdownFiles, len(changedFiles))

	// Second pass: only process changed files
	for _, filename := range changedFiles {
		content, err := readMarkDownFile(filename)
		if err != nil {
			return err
		}

		entryName := filepath.Base(filename)

		// Parse front matter
		fm, contentBody, err := parseMarkdownWithFrontMatter(content)
		if err != nil {
			fmt.Printf("⚠️  Error parsing front matter in %s: %v\n", filename, err)
			// Fall back to original processing
			post := &Post{
				Title:   extractTitle(entryName),
				Content: content,
				Slug:    generateSlug(entryName),
			}
			site.AddPost(post)
		} else {
			// Use front matter data
			title := fm.Title
			if title == "" {
				title = extractTitle(entryName)
			}

			post := &Post{
				Title:   title,
				Content: contentBody,
				Slug:    generateSlug(entryName),
				Date:    parseDate(fm.Date),
				Tags:    fm.Tags,
				Draft:   fm.Draft,
				Excerpt: fm.Excerpt,
			}

			if !post.Draft {
				site.AddPost(post)
				fmt.Printf("📖 Processed: %s\n", entryName)
			} else {
				fmt.Printf("⏭️  Skipped draft: %s\n", entryName)
			}
		}

		// Update cache for this file
		if err := buildCache.updateFile(filename); err != nil {
			fmt.Printf("⚠️  Could not update cache for %s: %v\n", filename, err)
		}
	}

	if len(changedFiles) == 0 {
		fmt.Println("✅ No changes detected - build skipped")
	} else {
		fmt.Printf("🎉 Processed %d files in this build\n", len(changedFiles))
	}

	return nil
}

// ================ CACHE PERSISTENCE ====================

func (bc *BuildCache) Save() error {
	data, err := json.Marshal(bc.FileHashes)
	if err != nil {
		return err
	}
	return os.WriteFile(".buildcache", data, 0644)
}

func (bc *BuildCache) Load() error {
	data, err := os.ReadFile(".buildcache")
	if err != nil {
		return nil // No cache file is ok
	}
	return json.Unmarshal(data, &bc.FileHashes)
}

// ================ DATE PARSER HELPER ====================

func parseDate(dateStr string) time.Time {
	if dateStr == "" {
		return time.Now() // Default to current time
	}

	// Try common date formats
	formats := []string{
		"2006-01-02",
		"2006-01-02 15:04",
		"January 2, 2006",
		"02 Jan 2006",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, dateStr); err == nil {
			return t
		}
	}

	// If all parsing fails, return current time
	fmt.Printf("⚠️  Could not parse date: %s, using current time\n", dateStr)
	return time.Now()
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

// ============= FRONT MATTER PARSER =================

type FrontMatter struct {
	Title   string   `yaml:"title"`
	Date    string   `yaml:"date"` // We'll parse this as string first
	Tags    []string `yaml:"tags"`
	Draft   bool     `yaml:"draft"`
	Excerpt string   `yaml:"excerpt"`
}

func parseMarkdownWithFrontMatter(content string) (FrontMatter, string, error) {
	var fm FrontMatter

	lines := strings.Split(content, "\n")

	// Check if file starts with front matter (---)
	if len(lines) > 2 && lines[0] == "---" {
		var fmLines []string

		// Collect lines between the --- delimiters
		for i := 1; i < len(lines); i++ {
			if lines[i] == "---" {
				// Join the front matter lines and parse as YAML
				fmContent := strings.Join(fmLines, "\n")
				if err := yaml.Unmarshal([]byte(fmContent), &fm); err != nil {
					return fm, content, fmt.Errorf("failed to parse front matter: %v", err)
				}

				// The rest is the actual content
				remainingContent := strings.Join(lines[i+1:], "\n")
				return fm, strings.TrimSpace(remainingContent), nil
			}
			fmLines = append(fmLines, lines[i])
		}
	}

	// If no front matter found, return empty front matter and original content
	return fm, content, nil
}

// ================ TEMPLATE RENDERING ====================

func renderPost(tmpl *template.Template, post *Post) (string, error) {
	var buf bytes.Buffer

	data := TemplateData{
		Title:            post.Title,
		Content:          convertToHTML(post.Content),
		Date:             post.Date,
		Tags:             post.Tags,
		Excerpt:          post.Excerpt,
		CurrentYear:      time.Now().Year(),
		LiveReloadScript: liveReloadScript,
	}

	// ⭐⭐ EXPLICITLY USE POST TEMPLATE WITHIN BASE ⭐⭐
	// First, look for the post template
	postTmpl := tmpl.Lookup("post.html")
	if postTmpl == nil {
		return "", fmt.Errorf("post.html template not found")
	}

	// Now execute the base template, which will use post.html for the content
	err := tmpl.ExecuteTemplate(&buf, "base.html", data)
	if err != nil {
		return "", fmt.Errorf("error executing template for post '%s': %v", post.Title, err)
	}

	return buf.String(), nil
}
