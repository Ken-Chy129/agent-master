// Command agent-master is the per-machine daemon that controls Claude Code on
// this host and exposes an HTTP API for remote clients (desktop, web, mobile).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Ken-Chy129/agent-master/internal/config"
	"github.com/Ken-Chy129/agent-master/internal/server"
	"github.com/Ken-Chy129/agent-master/internal/service"
	"github.com/Ken-Chy129/agent-master/internal/store"
	"github.com/Ken-Chy129/agent-master/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	switch args[0] {
	case "serve":
		return cmdServe(args[1:])
	case "token":
		return cmdToken(args[1:])
	case "service":
		return cmdService(args[1:])
	case "version", "-v", "--version":
		fmt.Println(version.Version)
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	port := fs.Int("port", 0, "override listen port (default: config value)")
	host := fs.String("host", "", "override listen host")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if *port != 0 {
		cfg.Port = *port
	}
	if *host != "" {
		cfg.Host = *host
	}

	dbPath, err := config.DBPath()
	if err != nil {
		return err
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	srv := server.New(cfg, st)
	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errCh:
		return err
	case <-sigCh:
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}
}

func cmdToken(_ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	fmt.Println(cfg.Token)
	return nil
}

func cmdService(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: agent-master service <install|uninstall|status>")
	}
	switch args[0] {
	case "install":
		return service.Install()
	case "uninstall":
		return service.Uninstall()
	case "status":
		return service.Status()
	default:
		return fmt.Errorf("unknown service subcommand: %s", args[0])
	}
}

func usage() {
	fmt.Print(`agent-master — control Claude Code on this machine, manage it remotely.

Usage:
  agent-master serve [--port N] [--host H]      Run the daemon in the foreground
  agent-master service install|uninstall|status  Manage the background service
  agent-master token                            Print this machine's auth token
  agent-master version

Config & data live under ~/.agent-master/ (default port 8888).
`)
}
