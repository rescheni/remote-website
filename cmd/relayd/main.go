package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"

	"gopkg.in/yaml.v3"

	"relay-tunnel/internal/server"
)

//go:embed web/dist/*
var webAssets embed.FS

type Config struct {
	Listen struct {
		HTTP      string `yaml:"http"`
		HTTPS     string `yaml:"https"`
		Tunnel    string `yaml:"tunnel"`
		Dashboard string `yaml:"dashboard"`
	} `yaml:"listen"`
	TCPUDP struct {
		Proxy []int `yaml:"tcp_proxy"`
	} `yaml:"tcp_udp"`
	Dashboard struct {
		Enabled bool `yaml:"enabled"`
	} `yaml:"dashboard"`
}

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	data, err := os.ReadFile(*configPath)
	if err != nil {
		log.Fatalf("read config: %v", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("parse config: %v", err)
	}

	srv := server.New(&server.ServerConfig{
		HTTPPort:      cfg.Listen.HTTP,
		HTTPSPort:     cfg.Listen.HTTPS,
		TunnelPort:    cfg.Listen.Tunnel,
		DashboardPort: cfg.Listen.Dashboard,
		Dashboard:     cfg.Dashboard.Enabled,
		TCPProxyPorts: cfg.TCPUDP.Proxy,
	})

	// Set up dashboard handler
	if cfg.Dashboard.Enabled {
		dist, err := fs.Sub(webAssets, "web/dist")
		if err != nil {
			log.Printf("dashboard assets not found (run 'make web' to build): %v", err)
			srv.DashboardHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Write([]byte(`<!doctype html><html><body>
				<h1>Dashboard not built</h1><p>Run <code>make web</code> to build the frontend.</p>
				</body></html>`))
			})
		} else {
			srv.DashboardHandler = http.FileServer(http.FS(dist))
		}
	}

	fmt.Println("=== relayd starting ===")
	fmt.Printf("HTTP proxy:  %s\n", cfg.Listen.HTTP)
	fmt.Printf("Tunnel WS:   %s\n", cfg.Listen.Tunnel)
	for _, p := range cfg.TCPUDP.Proxy {
		fmt.Printf("TCP proxy:   :%d\n", p)
	}
	if cfg.Dashboard.Enabled {
		fmt.Printf("Dashboard:   %s\n", cfg.Listen.Dashboard)
	}

	log.Fatal(srv.Run())
}
