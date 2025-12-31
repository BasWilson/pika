package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/baswilson/pika/internal/config"
	"github.com/baswilson/pika/internal/server"
	_ "github.com/mattn/go-sqlite3"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

// App struct holds the embedded HTTP server
type App struct {
	ctx        context.Context
	server     *server.Server
	httpServer *http.Server
	port       int
	configPath string
}

// NewApp creates a new App instance
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// shutdown is called when the app terminates
func (a *App) shutdown(ctx context.Context) {
	log.Println("Shutting down...")

	if a.httpServer != nil {
		a.httpServer.Shutdown(ctx)
	}
	if a.server != nil {
		a.server.Shutdown(ctx)
	}
}

// GetServerURL returns the local server URL
func (a *App) GetServerURL() string {
	return fmt.Sprintf("http://localhost:%d", a.port)
}

// getDBPath returns the path to the SQLite database
func getDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "pika.db"
	}
	dir := filepath.Join(home, "Library", "Application Support", "PIKA")
	os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "pika.db")
}

// configExists checks if config has been saved to the database
func configExists() bool {
	dbPath := getDBPath()

	// First, try to migrate from .env if it exists
	migrateEnvToDatabase(dbPath)

	// Check if database file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return false
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return false
	}
	defer db.Close()

	// Check if app_config table exists and has the API key
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM app_config WHERE key = 'requesty_api_key' AND value != ''").Scan(&count)
	if err != nil {
		return false
	}
	return count > 0
}

// migrateEnvToDatabase migrates config from .env file to database (one-time migration)
func migrateEnvToDatabase(dbPath string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}

	envPath := filepath.Join(home, "Library", "Application Support", "PIKA", ".env")

	// Check if .env file exists
	data, err := os.ReadFile(envPath)
	if err != nil {
		return
	}

	// Parse .env file
	envVars := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.ToLower(strings.TrimSpace(parts[0]))
			value := strings.TrimSpace(parts[1])
			envVars[key] = value
		}
	}

	// Check if we have the API key
	if envVars["requesty_api_key"] == "" {
		return
	}

	// Open database
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return
	}
	defer db.Close()

	// Create table if needed
	db.Exec(`
		CREATE TABLE IF NOT EXISTS app_config (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TEXT DEFAULT (datetime('now'))
		)
	`)

	// Check if already migrated
	var count int
	db.QueryRow("SELECT COUNT(*) FROM app_config WHERE key = 'requesty_api_key'").Scan(&count)
	if count > 0 {
		return // Already migrated
	}

	// Migrate values
	log.Println("Migrating config from .env to database...")
	for key, value := range envVars {
		db.Exec(`
			INSERT INTO app_config (key, value, updated_at)
			VALUES (?, ?, datetime('now'))
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')
		`, key, value)
	}
	log.Println("Config migration complete")
}

// setupHandler handles the initial setup/onboarding
type setupHandler struct {
	port       int
	configPath string
	app        *App
}

func (h *setupHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Handle save config POST request
	if r.Method == "POST" && r.URL.Path == "/save-config" {
		h.handleSaveConfig(w, r)
		return
	}

	// Handle open URL request
	if r.Method == "POST" && r.URL.Path == "/open-url" {
		h.handleOpenURL(w, r)
		return
	}

	// Check if config exists in database
	if configExists() {
		// Show loading page and redirect to server
		h.serveLoadingPage(w)
		return
	}

	// Show setup wizard
	h.serveSetupPage(w)
}

func (h *setupHandler) serveLoadingPage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
	<meta charset="UTF-8">
	<title>PIKA</title>
	<style>
		body {
			background: #0a0a0f;
			color: #fbbf24;
			font-family: -apple-system, BlinkMacSystemFont, sans-serif;
			display: flex;
			justify-content: center;
			align-items: center;
			height: 100vh;
			margin: 0;
		}
		.loader { text-align: center; }
		.spinner {
			width: 40px; height: 40px;
			border: 3px solid #333;
			border-top-color: #fbbf24;
			border-radius: 50%%;
			animation: spin 1s linear infinite;
			margin: 0 auto 20px;
		}
		@keyframes spin { to { transform: rotate(360deg); } }
	</style>
</head>
<body>
	<div class="loader">
		<div class="spinner"></div>
		<div>Loading PIKA...</div>
	</div>
	<script>
		setTimeout(function() {
			window.location.href = "http://localhost:%d";
		}, 500);
	</script>
</body>
</html>`, h.port)
}

func (h *setupHandler) handleOpenURL(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	url := strings.TrimSpace(string(body))
	if url == "" {
		http.Error(w, "No URL provided", http.StatusBadRequest)
		return
	}
	// Open URL in system browser using macOS 'open' command
	exec.Command("open", url).Start()
	w.WriteHeader(http.StatusOK)
}

func (h *setupHandler) handleSaveConfig(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	// Parse form data
	values := make(map[string]string)
	for _, pair := range strings.Split(string(body), "&") {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			// URL decode
			value = strings.ReplaceAll(value, "%40", "@")
			value = strings.ReplaceAll(value, "%3A", ":")
			value = strings.ReplaceAll(value, "%2F", "/")
			value = strings.ReplaceAll(value, "%3D", "=")
			value = strings.ReplaceAll(value, "%2B", "+")
			values[key] = value
		}
	}

	// Determine model (use custom if selected)
	model := values["requesty_model"]
	if model == "custom" {
		model = values["custom_model"]
	}

	// Save to SQLite database
	dbPath := getDBPath()
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		http.Error(w, "Failed to open database: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer db.Close()

	// Create app_config table if it doesn't exist
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS app_config (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TEXT DEFAULT (datetime('now'))
		)
	`)
	if err != nil {
		http.Error(w, "Failed to create config table: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Save config values
	configValues := map[string]string{
		"requesty_api_key":     values["requesty_api_key"],
		"requesty_base_url":    "https://router.requesty.ai/v1",
		"requesty_model":       model,
		"google_client_id":     values["google_client_id"],
		"google_client_secret": values["google_client_secret"],
		"google_redirect_url":  "http://localhost:8080/auth/google/callback",
		"memory_context_limit": "2000",
		"memory_top_k":         "10",
	}

	for key, value := range configValues {
		_, err = db.Exec(`
			INSERT INTO app_config (key, value, updated_at)
			VALUES (?, ?, datetime('now'))
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')
		`, key, value)
		if err != nil {
			http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Start the HTTP server now that config is saved
	go h.startServer()

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
	<style>
		body {
			background: #0a0a0f;
			color: #fbbf24;
			font-family: -apple-system, BlinkMacSystemFont, sans-serif;
			display: flex;
			justify-content: center;
			align-items: center;
			height: 100vh;
			margin: 0;
		}
		.loader { text-align: center; }
		.spinner {
			width: 40px; height: 40px;
			border: 3px solid #333;
			border-top-color: #fbbf24;
			border-radius: 50%%;
			animation: spin 1s linear infinite;
			margin: 0 auto 20px;
		}
		@keyframes spin { to { transform: rotate(360deg); } }
	</style>
</head>
<body>
	<div class="loader">
		<div class="spinner"></div>
		<div>Starting PIKA...</div>
	</div>
	<script>
		// Wait for server to be ready, then redirect
		function checkServer() {
			fetch('http://localhost:%d/api/status')
				.then(r => { if (r.ok) window.location.href = 'http://localhost:%d'; else setTimeout(checkServer, 500); })
				.catch(() => setTimeout(checkServer, 500));
		}
		setTimeout(checkServer, 1000);
	</script>
</body>
</html>`, h.port, h.port)
}

// startServer starts the HTTP server after config is saved
func (h *setupHandler) startServer() {
	// Give a moment for the response to be sent
	time.Sleep(500 * time.Millisecond)

	cfg := config.Load()
	srv, err := server.New(cfg, WebFS)
	if err != nil {
		log.Printf("Failed to create server: %v", err)
		return
	}
	h.app.server = srv

	h.app.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", h.port),
		Handler: srv.Router(),
	}

	log.Printf("Starting embedded HTTP server on port %d", h.port)
	if err := h.app.httpServer.ListenAndServe(); err != http.ErrServerClosed {
		log.Printf("HTTP server error: %v", err)
	}
}

func (h *setupHandler) serveSetupPage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<!DOCTYPE html>
<html>
<head>
	<meta charset="UTF-8">
	<title>PIKA - Setup</title>
	<style>
		* { box-sizing: border-box; }
		body {
			background: linear-gradient(135deg, #0a0a0f 0%, #1a1a2e 100%);
			color: #e5e5e5;
			font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
			min-height: 100vh;
			margin: 0;
			padding: 40px 20px;
		}
		.container {
			max-width: 500px;
			margin: 0 auto;
		}
		h1 {
			color: #fbbf24;
			text-align: center;
			font-size: 2.5rem;
			margin-bottom: 10px;
		}
		.subtitle {
			text-align: center;
			color: #888;
			margin-bottom: 40px;
		}
		.form-group {
			margin-bottom: 24px;
		}
		label {
			display: block;
			margin-bottom: 8px;
			color: #fbbf24;
			font-weight: 500;
		}
		.hint {
			font-size: 12px;
			color: #666;
			margin-top: 4px;
		}
		input, select {
			width: 100%;
			padding: 12px 16px;
			background: #1a1a2e;
			border: 1px solid #333;
			border-radius: 8px;
			color: #fff;
			font-size: 14px;
		}
		input:focus, select:focus {
			outline: none;
			border-color: #fbbf24;
		}
		.section {
			background: rgba(255,255,255,0.03);
			border-radius: 12px;
			padding: 24px;
			margin-bottom: 24px;
		}
		.section-title {
			color: #fbbf24;
			font-size: 14px;
			text-transform: uppercase;
			letter-spacing: 1px;
			margin-bottom: 16px;
		}
		.optional {
			color: #666;
			font-size: 12px;
		}
		button {
			width: 100%;
			padding: 16px;
			background: #fbbf24;
			color: #0a0a0f;
			border: none;
			border-radius: 8px;
			font-size: 16px;
			font-weight: 600;
			cursor: pointer;
			transition: background 0.2s;
		}
		button:hover {
			background: #f59e0b;
		}
		a {
			color: #fbbf24;
		}
		.setup-instructions {
			background: rgba(251, 191, 36, 0.05);
			border: 1px solid rgba(251, 191, 36, 0.2);
			border-radius: 8px;
			padding: 16px;
			margin-bottom: 20px;
			font-size: 13px;
		}
		.setup-instructions p {
			margin: 0 0 10px 0;
			color: #fbbf24;
		}
		.setup-instructions ol {
			margin: 0;
			padding-left: 20px;
			color: #aaa;
		}
		.setup-instructions li {
			margin-bottom: 6px;
			line-height: 1.5;
		}
		.setup-instructions code {
			background: #1a1a2e;
			padding: 4px 8px;
			border-radius: 4px;
			font-family: 'SF Mono', Monaco, monospace;
			color: #fbbf24;
			display: inline-block;
			margin-top: 4px;
			word-break: break-all;
		}
	</style>
</head>
<body>
	<div class="container">
		<h1>⚡ PIKA</h1>
		<p class="subtitle">Personal Intelligence Assistant</p>

		<form action="/save-config" method="POST">
			<div class="section">
				<div class="section-title">AI Configuration (Required)</div>

				<div class="form-group">
					<label>Requesty API Key</label>
					<input type="password" name="requesty_api_key" required placeholder="rqsty-sk-...">
					<div class="hint">Get your API key from <a href="https://requesty.ai" target="_blank">requesty.ai</a></div>
				</div>

				<div class="form-group">
					<label>AI Model</label>
					<select name="requesty_model" id="model-select" onchange="toggleCustomModel()">
						<option value="groq/openai/gpt-oss-20b">Groq GPT-OSS 20B (Fastest & Cheapest)</option>
						<option value="google/gemini-2.0-flash-001">Google Gemini 2.0 Flash</option>
						<option value="openai/gpt-4o-mini">OpenAI GPT-4o Mini</option>
						<option value="anthropic/claude-3-haiku">Anthropic Claude 3 Haiku</option>
						<option value="groq/llama-3.1-70b-versatile">Groq Llama 3.1 70B</option>
						<option value="custom">Other (enter custom model)</option>
					</select>
				</div>
				<div class="form-group" id="custom-model-group" style="display:none;">
					<label>Custom Model ID</label>
					<input type="text" name="custom_model" id="custom-model" placeholder="provider/model-name">
					<div class="hint">Enter the model ID from requesty.ai docs</div>
				</div>
				<script>
					function toggleCustomModel() {
						var select = document.getElementById('model-select');
						var customGroup = document.getElementById('custom-model-group');
						customGroup.style.display = select.value === 'custom' ? 'block' : 'none';
					}
					// Open external links in system browser
					document.addEventListener('click', function(e) {
						var target = e.target;
						while (target && target.tagName !== 'A') {
							target = target.parentElement;
						}
						if (target && target.href && target.target === '_blank') {
							e.preventDefault();
							fetch('/open-url', {
								method: 'POST',
								body: target.href
							});
						}
					});
				</script>
			</div>

			<div class="section">
				<div class="section-title">Google Calendar <span class="optional">(Optional)</span></div>

				<div class="setup-instructions">
					<p><strong>Setup steps:</strong></p>
					<ol>
						<li>Go to <a href="https://console.cloud.google.com/apis/credentials" target="_blank">Google Cloud Console</a></li>
						<li>Create a new project (or select existing)</li>
						<li>Enable the <strong>Google Calendar API</strong></li>
						<li>Go to "Credentials" → "Create Credentials" → "OAuth client ID"</li>
						<li>Select "Web application"</li>
						<li>Add this <strong>Authorized redirect URI</strong>:<br>
							<code>http://localhost:8080/auth/google/callback</code></li>
						<li>Copy the Client ID and Client Secret below</li>
					</ol>
				</div>

				<div class="form-group">
					<label>Google Client ID</label>
					<input type="text" name="google_client_id" placeholder="xxxx.apps.googleusercontent.com">
				</div>

				<div class="form-group">
					<label>Google Client Secret</label>
					<input type="password" name="google_client_secret" placeholder="GOCSPX-...">
				</div>
			</div>

			<button type="submit">Save & Start PIKA</button>
		</form>
	</div>
</body>
</html>`)
}

func main() {
	// Create application instance
	app := NewApp()
	dbPath := getDBPath()
	app.configPath = dbPath

	// Try to use port 8080 first (needed for OAuth redirect), fall back to dynamic port
	app.port = 8080
	listener, err := net.Listen("tcp", ":8080")
	if err != nil {
		// Port 8080 in use, find an available port
		listener, err = net.Listen("tcp", ":0")
		if err != nil {
			log.Fatalf("Failed to find available port: %v", err)
		}
		app.port = listener.Addr().(*net.TCPAddr).Port
		log.Printf("Port 8080 in use, using port %d instead (OAuth may not work)", app.port)
	}
	listener.Close()

	// Only start HTTP server if config exists in database
	if configExists() {
		cfg := config.Load()
		srv, err := server.New(cfg, WebFS)
		if err != nil {
			log.Fatalf("Failed to create server: %v", err)
		}
		app.server = srv

		app.httpServer = &http.Server{
			Addr:    fmt.Sprintf(":%d", app.port),
			Handler: srv.Router(),
		}

		go func() {
			log.Printf("Starting embedded HTTP server on port %d", app.port)
			if err := app.httpServer.ListenAndServe(); err != http.ErrServerClosed {
				log.Printf("HTTP server error: %v", err)
			}
		}()

		// Wait for server to be ready
		for i := 0; i < 50; i++ {
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", app.port), 100*time.Millisecond)
			if err == nil {
				conn.Close()
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		log.Printf("HTTP server ready on port %d", app.port)
	} else {
		log.Println("No config found, showing setup wizard")
	}

	// Create Wails application
	err = wails.Run(&options.App{
		Title:     "PIKA",
		Width:     560,
		Height:    980,
		MinWidth:  400,
		MinHeight: 700,
		AssetServer: &assetserver.Options{
			Handler: &setupHandler{port: app.port, configPath: dbPath, app: app},
		},
		BackgroundColour: &options.RGBA{R: 10, G: 10, B: 15, A: 255},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
		Mac: &mac.Options{
			TitleBar: &mac.TitleBar{
				TitlebarAppearsTransparent: false,
				HideTitle:                  false,
				HideTitleBar:               false,
				FullSizeContent:            false,
				UseToolbar:                 false,
			},
			Appearance:           mac.NSAppearanceNameDarkAqua,
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			About: &mac.AboutInfo{
				Title:   "PIKA",
				Message: "Personal Intelligence Assistant\n\nA voice-controlled AI assistant for your desktop.",
				Icon:    nil,
			},
		},
	})

	if err != nil {
		log.Fatalf("Error starting application: %v", err)
	}
}
