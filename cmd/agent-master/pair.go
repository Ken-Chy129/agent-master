package main

import (
	"fmt"
	"net"
	"net/url"

	"github.com/mdp/qrterminal/v3"

	"github.com/Ken-Chy129/agent-master/internal/config"
)

// cmdPair prints this machine's connection info for a client: candidate base
// URLs, the token, an `agentmaster://pair` deep link, and a QR of that link.
func cmdPair(_ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	urls := candidateBaseURLs(cfg)
	primary := urls[0]
	deeplink := fmt.Sprintf(
		"agentmaster://pair?url=%s&token=%s",
		url.QueryEscape(primary), url.QueryEscape(cfg.Token),
	)

	fmt.Println("Pair a client with this machine:")
	fmt.Println()
	fmt.Println("  Base URL(s):")
	for _, u := range urls {
		fmt.Printf("    %s\n", u)
	}
	fmt.Printf("  Token: %s\n", cfg.Token)
	fmt.Println()
	fmt.Println("  Deep link (desktop app / scan on phone):")
	fmt.Printf("    %s\n", deeplink)
	fmt.Println()
	qrterminal.GenerateHalfBlock(deeplink, qrterminal.L, stdoutWriter{})
	fmt.Println()
	fmt.Println("Tip: for access from anywhere without exposing a public port,")
	fmt.Println("put this machine and your client on the same Tailscale tailnet")
	fmt.Println("and use the tailnet URL (set it as public_url in config).")
	return nil
}

// candidateBaseURLs returns likely reachable base URLs, most specific first.
func candidateBaseURLs(cfg *config.Config) []string {
	if cfg.PublicURL != "" {
		return []string{cfg.PublicURL}
	}
	var urls []string
	for _, ip := range nonLoopbackIPv4() {
		urls = append(urls, fmt.Sprintf("http://%s:%d", ip, cfg.Port))
	}
	urls = append(urls, fmt.Sprintf("http://127.0.0.1:%d", cfg.Port))
	return urls
}

func nonLoopbackIPv4() []string {
	var out []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		ip4 := ipnet.IP.To4()
		if ip4 != nil {
			out = append(out, ip4.String())
		}
	}
	return out
}

// stdoutWriter adapts os.Stdout for qrterminal without importing os here.
type stdoutWriter struct{}

func (stdoutWriter) Write(p []byte) (int, error) { return fmt.Print(string(p)) }
