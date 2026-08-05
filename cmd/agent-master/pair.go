package main

import (
	"fmt"
	"net"
	"net/url"

	"github.com/mdp/qrterminal/v3"

	"github.com/Ken-Chy129/agent-master/internal/config"
)

// cmdPair prints this machine's full connection info for a client: candidate
// base URLs, the token, an `agentmaster://pair` deep link, and a QR of that link.
func cmdPair(_ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	fmt.Println("将客户端与这台机器配对：")
	fmt.Println()
	printPairBody(cfg, true)
	return nil
}

// printPairBody prints the URLs / token / deep link. With withQR it also renders
// a scannable QR and the Tailscale tip; without, it points at `agent-master pair`.
func printPairBody(cfg *config.Config, withQR bool) {
	urls := candidateBaseURLs(cfg)
	deeplink := fmt.Sprintf(
		"agentmaster://pair?url=%s&token=%s",
		url.QueryEscape(urls[0]), url.QueryEscape(cfg.Token),
	)

	fmt.Println("  可用地址：")
	for _, u := range urls {
		fmt.Printf("    %s\n", u)
	}
	fmt.Printf("  令牌：%s\n", cfg.Token)
	fmt.Println()
	fmt.Println("  深链接（桌面端打开 / 手机扫码）：")
	fmt.Printf("    %s\n", deeplink)
	fmt.Println()

	if withQR {
		qrterminal.GenerateHalfBlock(deeplink, qrterminal.L, stdoutWriter{})
		fmt.Println()
		fmt.Println("提示：若需在任意网络下访问且不暴露公网端口，")
		fmt.Println("请将本机与客户端接入同一 Tailscale 网络，")
		fmt.Println("并使用该网络地址（在配置中设为 public_url）。")
	} else {
		fmt.Println("  扫码配对手机：agent-master pair")
	}
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
