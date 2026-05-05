package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"relay-tunnel/internal/proto"
)

func (s *Server) handleAPIClients(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	clients := s.Hub.All()
	type clientInfo struct {
		ID        string         `json:"id"`
		Connected string         `json:"connected"`
		LastSeen  string         `json:"last_seen"`
		ReqCount  int64          `json:"req_count"`
		BytesIn   int64          `json:"bytes_in"`
		BytesOut  int64          `json:"bytes_out"`
		Routes    []proto.Route  `json:"routes"`
		RouteCnt  int            `json:"route_count"`
	}
	list := make([]clientInfo, 0, len(clients))
	for _, c := range clients {
		routes := c.Routes
		if routes == nil {
			routes = []proto.Route{}
		}
		list = append(list, clientInfo{
			ID:        c.ID,
			Connected: c.Connected.Format("2006-01-02 15:04:05"),
			LastSeen:  c.LastSeen.Format("2006-01-02 15:04:05"),
			ReqCount:  c.ReqCount,
			BytesIn:   c.BytesIn,
			BytesOut:  c.BytesOut,
			Routes:    routes,
			RouteCnt:  len(routes),
		})
	}
	json.NewEncoder(w).Encode(list)
}

func (s *Server) handleAPIRoutes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case "GET":
		json.NewEncoder(w).Encode(s.Hub.AllRoutes())
	case "POST":
		s.handleAddRoute(w, r)
	case "DELETE":
		s.handleDeleteRoute(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type addRouteReq struct {
	ClientID string      `json:"client_id"`
	Route    proto.Route `json:"route"`
}

func (s *Server) handleAddRoute(w http.ResponseWriter, r *http.Request) {
	var req addRouteReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// Trim whitespace from fields that users might fat-finger.
	req.Route.Host = strings.TrimSpace(req.Route.Host)
	req.Route.PathPrefix = strings.TrimSpace(req.Route.PathPrefix)
	req.Route.Target = strings.TrimSpace(req.Route.Target)
	if req.Route.Type == "" {
		req.Route.Type = "http"
	}
	if !s.Hub.AddRoute(req.ClientID, req.Route) {
		http.Error(w, "client not found", http.StatusNotFound)
		return
	}
	// Push updated routes to client
	s.pushRouteUpdate(req.ClientID)
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleDeleteRoute(w http.ResponseWriter, r *http.Request) {
	// URL: /api/routes?client=my-phone&idx=0
	clientID := r.URL.Query().Get("client")
	idxStr := r.URL.Query().Get("idx")
	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !s.Hub.RemoveRoute(clientID, idx) {
		http.Error(w, "client or route not found", http.StatusNotFound)
		return
	}
	s.pushRouteUpdate(clientID)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleAPIStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"total_requests":  s.Stats.TotalRequests.Load(),
		"total_bytes_in":  s.Stats.TotalBytesIn.Load(),
		"total_bytes_out": s.Stats.TotalBytesOut.Load(),
		"online_clients":  len(s.Hub.All()),
	})
}

func (s *Server) pushRouteUpdate(clientID string) {
	c := s.Hub.Get(clientID)
	if c == nil {
		return
	}
	routes := c.Routes
	if routes == nil {
		routes = []proto.Route{}
	}
	msg := proto.RouteUpdate{
		Type:   proto.TypeRouteUpdate,
		Routes: routes,
	}
	if err := c.WriteJSON(msg); err != nil {
		log.Printf("push route_update to %s failed: %v", clientID, err)
	}
}
