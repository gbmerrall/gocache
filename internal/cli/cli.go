package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"syscall"

	"github.com/gbmerrall/gocache/internal/pidfile"
)

// Client is used to interact with the GoCache Control API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new Client for the Control API.
func NewClient(port int) *Client {
	return &Client{
		baseURL:    fmt.Sprintf("http://127.0.0.1:%d", port),
		httpClient: &http.Client{},
	}
}

// Run executes a command based on the provided arguments.
func Run(port int, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("no command provided")
	}

	client := NewClient(port)
	command := args[0]

	switch command {
	case "status":
		return client.GetStatus()
	case "purge":
		if len(args) < 2 {
			return fmt.Errorf("domain required for purge command")
		}
		return client.PurgeDomain(args[1])
	case "purge-url":
		if len(args) < 2 {
			return fmt.Errorf("url required for purge-url command")
		}
		return client.PurgeURL(args[1])
	case "purge-all":
		fmt.Print("Are you sure you want to clear the entire cache? [y/N] ")
		var response string
		fmt.Scanln(&response)
		if response == "y" || response == "Y" {
			return client.PurgeAll()
		}
		fmt.Println("Operation cancelled.")
		return nil
	case "export-ca":
		var filename string
		if len(args) > 1 {
			filename = args[1]
		}
		return client.ExportCA(filename)
	case "stop":
		return stopDaemon()
	default:
		return fmt.Errorf("unknown command: %s", command)
	}
}

func stopDaemon() error {
	pid, err := pidfile.Read()
	if err != nil {
		return fmt.Errorf("could not read pidfile: %w. Is gocache running?", err)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("could not find process with pid %d: %w", pid, err)
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM to process %d: %w", pid, err)
	}

	fmt.Println("gocache stopped.")
	// The deferred pidfile.Remove() in the server will handle cleanup.
	return nil
}

// GetStatus fetches and displays the cache statistics.
func (c *Client) GetStatus() error {
	resp, err := c.httpClient.Get(c.baseURL + "/stats")
	if err != nil {
		return fmt.Errorf("could not connect to gocache server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned non-200 status: %s\n%s", resp.Status, string(body))
	}

	var stats map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return fmt.Errorf("could not decode server response: %w", err)
	}

	fmt.Println("GoCache Status:")
	fmt.Printf("  Uptime: %s seconds\n", stats["uptime_seconds"])
	fmt.Printf("  Cache Entries: %.0f\n", stats["entry_count"])
	fmt.Printf("  Cache Size: %.2f bytes\n", stats["cache_size_bytes"])
	fmt.Printf("  Hits: %.0f\n", stats["hit_count"])
	fmt.Printf("  Misses: %.0f\n", stats["miss_count"])
	fmt.Printf("  Hit Rate: %s%%\n", stats["hit_rate_percent"])
	fmt.Printf("  Certificate Cache: %.0f entries\n", stats["cert_cache_count"])

	return nil
}

// PurgeAll sends a request to purge the entire cache.
func (c *Client) PurgeAll() error {
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/purge/all", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result map[string]int
	json.NewDecoder(resp.Body).Decode(&result)
	fmt.Printf("Successfully purged %d entries.\n", result["purged_count"])
	return nil
}

// PurgeURL sends a request to purge a specific URL.
func (c *Client) PurgeURL(url string) error {
	body, _ := json.Marshal(map[string]string{"url": url})
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/purge/url", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if result["purged"].(bool) {
		fmt.Printf("Successfully purged URL: %s\n", url)
	} else {
		fmt.Printf("URL not found in cache: %s\n", url)
	}
	return nil
}

// PurgeDomain sends a request to purge a domain.
func (c *Client) PurgeDomain(domain string) error {
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/purge/domain/"+domain, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result map[string]int
	json.NewDecoder(resp.Body).Decode(&result)
	fmt.Printf("Successfully purged %d entries for domain %s.\n", result["purged_count"], domain)
	return nil
}

// ExportCA fetches the CA certificate and saves it to a file.
func (c *Client) ExportCA(filename string) error {
	resp, err := c.httpClient.Get(c.baseURL + "/ca")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned non-200 status: %s\n%s", resp.Status, string(body))
	}

	if filename == "" {
		filename = "gocache-ca.crt"
	}

	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	if err != nil {
		return err
	}

	fmt.Printf("CA certificate exported to %s\n", filename)
	return nil
}
