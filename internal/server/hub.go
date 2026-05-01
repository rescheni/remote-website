package server

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"relay-tunnel/internal/proto"
)

type ClientConn struct {
	ID        string
	Conn      *websocket.Conn
	Ctx       context.Context
	Cancel    context.CancelFunc
	Routes    []proto.Route
	Connected time.Time
	LastSeen  time.Time
	ReqCount  int64
	BytesIn   int64
	BytesOut  int64
	mu        sync.Mutex
}

func (c *ClientConn) WriteJSON(v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return wsjson.Write(c.Ctx, c.Conn, v)
}

type Hub struct {
	mu      sync.RWMutex
	clients map[string]*ClientConn
}

func NewHub() *Hub {
	return &Hub{clients: make(map[string]*ClientConn)}
}

func (h *Hub) Add(c *ClientConn) {
	h.mu.Lock()
	h.clients[c.ID] = c
	h.mu.Unlock()
	log.Printf("client connected: %s (routes: %d)", c.ID, len(c.Routes))
}

func (h *Hub) Remove(id string) {
	h.mu.Lock()
	delete(h.clients, id)
	h.mu.Unlock()
	log.Printf("client disconnected: %s", id)
}

func (h *Hub) Get(id string) *ClientConn {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.clients[id]
}

// MatchRoute finds the best matching client+target for a host and path.
// Priority: exact host+path_prefix match > host-only match.
func (h *Hub) MatchRoute(host, path string) (*ClientConn, string) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var bestClient *ClientConn
	var bestTarget string
	bestLen := -1

	for _, c := range h.clients {
		for _, r := range c.Routes {
			if r.Host != host || (r.Type != "" && r.Type != "http") {
				continue
			}
			if r.PathPrefix != "" {
				if len(path) >= len(r.PathPrefix) && path[:len(r.PathPrefix)] == r.PathPrefix {
					if len(r.PathPrefix) > bestLen {
						bestLen = len(r.PathPrefix)
						bestClient = c
						bestTarget = r.Target
					}
				}
			} else {
				if bestLen < 0 {
					bestClient = c
					bestTarget = r.Target
				}
			}
		}
	}
	return bestClient, bestTarget
}

// MatchTCPRoute finds a client+target for a TCP port.
func (h *Hub) MatchTCPRoute(port int) (*ClientConn, string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.clients {
		for _, r := range c.Routes {
			if r.Type == "tcp" && r.RemotePort == port {
				return c, r.Target
			}
		}
	}
	return nil, ""
}

// UpdateRoutes sets routes for a client and returns the new routes.
func (h *Hub) UpdateRoutes(clientID string, routes []proto.Route) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	c, ok := h.clients[clientID]
	if !ok {
		return false
	}
	c.Routes = routes
	return true
}

// AddRoute adds a route to a client.
func (h *Hub) AddRoute(clientID string, route proto.Route) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	c, ok := h.clients[clientID]
	if !ok {
		return false
	}
	c.Routes = append(c.Routes, route)
	return true
}

// RemoveRoute removes a route from a client by index.
func (h *Hub) RemoveRoute(clientID string, idx int) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	c, ok := h.clients[clientID]
	if !ok || idx < 0 || idx >= len(c.Routes) {
		return false
	}
	c.Routes = append(c.Routes[:idx], c.Routes[idx+1:]...)
	return true
}

// All returns all connected clients for the dashboard.
func (h *Hub) All() []*ClientConn {
	h.mu.RLock()
	defer h.mu.RUnlock()
	list := make([]*ClientConn, 0, len(h.clients))
	for _, c := range h.clients {
		list = append(list, c)
	}
	return list
}

// AllRoutes returns all registered routes for the dashboard.
func (h *Hub) AllRoutes() []struct {
	ClientID   string `json:"client_id"`
	Host       string `json:"host"`
	PathPrefix string `json:"path_prefix"`
	Target     string `json:"target"`
	Type       string `json:"type"`
	RemotePort int    `json:"remote_port"`
} {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var routes []struct {
		ClientID   string `json:"client_id"`
		Host       string `json:"host"`
		PathPrefix string `json:"path_prefix"`
		Target     string `json:"target"`
		Type       string `json:"type"`
		RemotePort int    `json:"remote_port"`
	}
	for _, c := range h.clients {
		for _, r := range c.Routes {
			routes = append(routes, struct {
				ClientID   string `json:"client_id"`
				Host       string `json:"host"`
				PathPrefix string `json:"path_prefix"`
				Target     string `json:"target"`
				Type       string `json:"type"`
				RemotePort int    `json:"remote_port"`
			}{c.ID, r.Host, r.PathPrefix, r.Target, r.Type, r.RemotePort})
		}
	}
	return routes
}
