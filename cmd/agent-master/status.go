package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Ken-Chy129/agent-master/internal/config"
	"github.com/Ken-Chy129/agent-master/internal/service"
)

// cmdStatus reports whether the daemon is actually serving by probing its own
// /health endpoint, then prints the connection info. This replaces dumping the
// raw `launchctl print` / `systemctl status` output, which is unreadable.
func cmdStatus(_ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if ver, ok := probeHealth(cfg.Port); ok {
		if ver != "" {
			fmt.Printf("✓ agent-master is running  (v%s)\n", ver)
		} else {
			fmt.Println("✓ agent-master is running.")
		}
		fmt.Println()
		fmt.Println("Add this machine in your client:")
		fmt.Printf("  URL     %s\n", candidateBaseURLs(cfg)[0])
		fmt.Printf("  Token   %s\n", cfg.Token)
		fmt.Println()
		fmt.Println("Manage:  agent-master stop · restart · pair")
		return nil
	}

	// Not responding: distinguish "installed but down" from "never started".
	if service.Installed() {
		fmt.Printf("✗ agent-master is installed but not responding on port %d.\n", cfg.Port)
		fmt.Println()
		fmt.Println("Try:  agent-master restart")
		return nil
	}
	fmt.Println("✗ agent-master is not running.")
	fmt.Println()
	fmt.Println("Start it with:  agent-master start")
	return nil
}

// probeHealth does a short GET /health on localhost. It returns the reported
// version and whether the daemon answered — the truthful "is it serving" signal
// (works whether it runs as a service or a foreground `serve`).
func probeHealth(port int) (version string, ok bool) {
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	var body struct {
		Version string `json:"version"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return body.Version, true
}
