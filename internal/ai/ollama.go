package ai

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// OllamaManager manages the lifecycle of a bundled Ollama binary.
type OllamaManager struct {
	binaryPath string
	dataDir    string
	port       int
	cmd        *exec.Cmd
	running    bool
	mu         sync.RWMutex
}

// NewOllamaManager creates a new Ollama manager.
// appDir should be the application data directory.
func NewOllamaManager(appDir string) *OllamaManager {
	// Try to find bundled Ollama in Resources directory
	// For development, fall back to PATH
	binaryPath := "ollama" // Default to PATH lookup

	// Check for bundled Ollama in app bundle (macOS)
	bundledPath := filepath.Join(appDir, "..", "Resources", "ollama", "ollama")
	if _, err := os.Stat(bundledPath); err == nil {
		binaryPath = bundledPath
	}

	return &OllamaManager{
		binaryPath: binaryPath,
		dataDir:    filepath.Join(appDir, "ollama_data"),
		port:       11434,
	}
}

// Start starts the Ollama server.
func (m *OllamaManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return nil // Already running
	}

	// Check if Ollama is already running externally
	if m.isServerReady() {
		m.running = true
		return nil
	}

	// Ensure data directory exists
	if err := os.MkdirAll(m.dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create Ollama data directory: %w", err)
	}

	// Set environment for Ollama
	env := append(os.Environ(),
		fmt.Sprintf("OLLAMA_HOST=127.0.0.1:%d", m.port),
		fmt.Sprintf("OLLAMA_MODELS=%s", m.dataDir),
	)

	m.cmd = exec.CommandContext(ctx, m.binaryPath, "serve")
	m.cmd.Env = env
	m.cmd.Stdout = os.Stdout
	m.cmd.Stderr = os.Stderr

	if err := m.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start Ollama: %w", err)
	}

	// Wait for server to be ready
	if err := m.waitForReady(30 * time.Second); err != nil {
		m.cmd.Process.Kill()
		return err
	}

	m.running = true
	return nil
}

// Stop stops the Ollama server.
func (m *OllamaManager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.running = false

	if m.cmd != nil && m.cmd.Process != nil {
		if err := m.cmd.Process.Signal(os.Interrupt); err != nil {
			// If interrupt fails, try kill
			return m.cmd.Process.Kill()
		}
		// Wait for process to exit
		m.cmd.Wait()
	}

	return nil
}

// IsRunning returns whether Ollama is currently running.
func (m *OllamaManager) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}

// GetURL returns the Ollama API URL.
func (m *OllamaManager) GetURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", m.port)
}

// EnsureModel pulls a model if it's not already available.
func (m *OllamaManager) EnsureModel(model string) error {
	m.mu.RLock()
	if !m.running {
		m.mu.RUnlock()
		return fmt.Errorf("Ollama is not running")
	}
	m.mu.RUnlock()

	// Check if model exists by trying to get its info
	resp, err := http.Get(fmt.Sprintf("%s/api/show", m.GetURL()))
	if err != nil {
		return err
	}
	resp.Body.Close()

	// Pull the model
	cmd := exec.Command(m.binaryPath, "pull", model)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("OLLAMA_HOST=127.0.0.1:%d", m.port),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// waitForReady waits for Ollama to be ready to accept requests.
func (m *OllamaManager) waitForReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		if m.isServerReadyWithClient(client) {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("Ollama did not start within %v", timeout)
}

func (m *OllamaManager) isServerReady() bool {
	client := &http.Client{Timeout: 2 * time.Second}
	return m.isServerReadyWithClient(client)
}

func (m *OllamaManager) isServerReadyWithClient(client *http.Client) bool {
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/tags", m.port))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
