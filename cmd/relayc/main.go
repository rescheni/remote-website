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
		ID                 string   `yaml:"id"`
		HeartbeatInterval  string   `yaml:"heartbeat_interval"`
		ReconnectBackoff   []string `yaml:"reconnect_backoff"`
	} `yaml:"client"`
	Routes []struct {
		Host       string `yaml:"host"`
		PathPrefix string `yaml:"path_prefix"`
		Target     string `yaml:"target"`
	} `yaml:"routes"`
}

func main() {
	configPath := flag.String("config", "client.yaml", "path to config file")
	flag.Parse()

	data, err := os.ReadFile(*configPath)
	if err != nil {
		log.Fatalf("read config: %v", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("parse config: %v", err)
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
	for _, r := range routes {
		fmt.Printf("Route: %s%s → %s\n", r.Host, r.PathPrefix, r.Target)
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
