package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"relay-tunnel/internal/client"
	"relay-tunnel/internal/proto"
)

type Config struct {
	Server struct {
		Addr   string `yaml:"addr"`
		TLS    bool   `yaml:"tls"`
		Secret string `yaml:"secret"`
	} `yaml:"server"`
	Client struct {
		ID                string   `yaml:"id"`
		HeartbeatInterval string   `yaml:"heartbeat_interval"`
		ReconnectBackoff  []string `yaml:"reconnect_backoff"`
	} `yaml:"client"`
	Routes []struct {
		Host       string `yaml:"host"`
		PathPrefix string `yaml:"path_prefix"`
		Target     string `yaml:"target"`
	} `yaml:"routes"`
}

func main() {
	// CLI flags
	configPath := flag.String("config", "", "path to config file (optional, use CLI flags instead)")
	serverAddr := flag.String("server", "", "server address (e.g. your-server.com:8443)")
	clientID := flag.String("id", "", "client ID (e.g. my-phone)")
	secret := flag.String("secret", "", "shared secret for auth")
	tls := flag.Bool("tls", false, "use TLS/WSS")
	flag.Parse()

	var cfg Config

	// Load config file if specified
	if *configPath != "" {
		data, err := os.ReadFile(*configPath)
		if err != nil {
			log.Fatalf("read config: %v", err)
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			log.Fatalf("parse config: %v", err)
		}
	}

	// CLI flags override config file values
	if *serverAddr != "" {
		cfg.Server.Addr = *serverAddr
	}
	if *clientID != "" {
		cfg.Client.ID = *clientID
	}
	if *secret != "" {
		cfg.Server.Secret = *secret
	}
	if isFlagSet("tls") {
		cfg.Server.TLS = *tls
	}

	if cfg.Server.Addr == "" {
		log.Fatal("server address required: use -config or -server")
	}
	if cfg.Client.ID == "" {
		// Default client ID from hostname
		host, _ := os.Hostname()
		cfg.Client.ID = host
		if cfg.Client.ID == "" {
			cfg.Client.ID = "client"
		}
	}

	heartbeat, _ := time.ParseDuration(cfg.Client.HeartbeatInterval)
	if heartbeat == 0 {
		heartbeat = 30 * time.Second
	}

	var backoff []time.Duration
	for _, d := range cfg.Client.ReconnectBackoff {
		dur, _ := time.ParseDuration(d)
		backoff = append(backoff, dur)
	}
	if len(backoff) == 0 {
		backoff = []time.Duration{1 * time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second}
	}

	var routes []proto.Route
	for _, r := range cfg.Routes {
		routes = append(routes, proto.Route{
			Host:       r.Host,
			PathPrefix: r.PathPrefix,
			Target:     r.Target,
		})
	}

	fmt.Printf("=== relayc starting ===\n")
	fmt.Printf("Server: %s (tls=%v)\n", cfg.Server.Addr, cfg.Server.TLS)
	fmt.Printf("Client ID: %s\n", cfg.Client.ID)
	if len(routes) > 0 {
		fmt.Printf("Routes: %d (from config)\n", len(routes))
	} else {
		fmt.Printf("Routes: managed via Dashboard\n")
	}

	log.Fatal(client.Run(&client.Config{
		ServerAddr:       cfg.Server.Addr,
		TLS:              cfg.Server.TLS,
		Secret:           cfg.Server.Secret,
		ClientID:         cfg.Client.ID,
		Heartbeat:        heartbeat,
		ReconnectBackoff: backoff,
		Routes:           routes,
	}))
}

func isFlagSet(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}
