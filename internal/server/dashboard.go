package server

import (
	"encoding/json"
	"net/http"
)

func (s *Server) handleAPIClients(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	clients := s.Hub.All()
	type clientInfo struct {
		ID        string `json:"id"`
		Connected string `json:"connected"`
		LastSeen  string `json:"last_seen"`
		ReqCount  int64  `json:"req_count"`
		BytesIn   int64  `json:"bytes_in"`
		BytesOut  int64  `json:"bytes_out"`
		Routes    int    `json:"routes"`
	}
	list := make([]clientInfo, 0, len(clients))
	for _, c := range clients {
		list = append(list, clientInfo{
			ID:        c.ID,
			Connected: c.Connected.Format("2006-01-02 15:04:05"),
			LastSeen:  c.LastSeen.Format("2006-01-02 15:04:05"),
			ReqCount:  c.ReqCount,
			BytesIn:   c.BytesIn,
			BytesOut:  c.BytesOut,
			Routes:    len(c.Routes),
		})
	}
	json.NewEncoder(w).Encode(list)
}

func (s *Server) handleAPIRoutes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.Hub.AllRoutes())
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
