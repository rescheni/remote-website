package server

import (
	"bytes"
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"relay-tunnel/internal/proto"
)

type Stats struct {
	TotalRequests atomic.Int64
	TotalBytesIn  atomic.Int64
	TotalBytesOut atomic.Int64
}

type Server struct {
	Hub              *Hub
	Stats            *Stats
	Config           *ServerConfig
	DashboardHandler http.Handler
	tcpListeners     []*tcpListener
}

type ServerConfig struct {
	HTTPPort       string
	HTTPSPort      string
	TunnelPort     string
	DashboardPort  string
	TLSCert        string
	TLSKey         string
	Dashboard      bool
	TCPProxyPorts  []int
}

func New(cfg *ServerConfig) *Server {
	return &Server{
		Hub:    NewHub(),
		Stats:  &Stats{},
		Config: cfg,
	}
}

func (s *Server) Run() error {
	errCh := make(chan error, 6)

	// TCP proxy listeners
	if len(s.Config.TCPProxyPorts) > 0 {
		s.tcpListeners = s.startTCPListeners(s.Config.TCPProxyPorts)
	}

	// Tunnel WebSocket server
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/__tunnel__", s.handleTunnel)
		log.Printf("tunnel endpoint on %s", s.Config.TunnelPort)
		errCh <- http.ListenAndServe(s.Config.TunnelPort, mux)
	}()

	// HTTP reverse proxy server
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/", s.handleHTTP)
		log.Printf("HTTP proxy on %s", s.Config.HTTPPort)
		errCh <- http.ListenAndServe(s.Config.HTTPPort, mux)
	}()

	// Dashboard
	if s.Config.Dashboard {
		go func() {
			mux := http.NewServeMux()
			mux.HandleFunc("/api/clients", s.handleAPIClients)
			mux.HandleFunc("/api/routes", s.handleAPIRoutes)
			mux.HandleFunc("/api/stats", s.handleAPIStats)
			mux.Handle("/", s.DashboardHandler)
			log.Printf("dashboard on %s", s.Config.DashboardPort)
			errCh <- http.ListenAndServe(s.Config.DashboardPort, mux)
		}()
	}

	return <-errCh
}

func (s *Server) handleHTTP(w http.ResponseWriter, r *http.Request) {
	s.Stats.TotalRequests.Add(1)
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	client, target := s.Hub.MatchRoute(host, r.URL.Path)
	if client == nil {
		http.Error(w, "no route for "+host+r.URL.Path, http.StatusNotFound)
		return
	}

	headers := make(map[string]string)
	for k, v := range r.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	headers["X-Forwarded-Host"] = host

	bodyBytes, _ := io.ReadAll(r.Body)
	r.Body.Close()

	reqMsg := proto.Request{
		Type:    proto.TypeReq,
		ID:      newRequestID(),
		Method:  r.Method,
		Path:    r.URL.RequestURI(),
		Target:  target,
		Headers: headers,
		Body:    string(bodyBytes),
	}

	respCh := make(chan responseMsg, 1)
	pendingRequests.Store(reqMsg.ID, respCh)
	defer pendingRequests.Delete(reqMsg.ID)

	s.Stats.TotalBytesIn.Add(int64(len(bodyBytes)))
	client.BytesIn += int64(len(bodyBytes))

	if err := client.WriteJSON(reqMsg); err != nil {
		log.Printf("error forwarding request to %s: %v", client.ID, err)
		http.Error(w, "tunnel error", http.StatusBadGateway)
		return
	}

	select {
	case resp := <-respCh:
		s.Stats.TotalBytesOut.Add(int64(len(resp.Body)))
		client.BytesOut += int64(len(resp.Body))
		for k, v := range resp.Headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(resp.Status)
		w.Write([]byte(resp.Body))
	case <-time.After(30 * time.Second):
		http.Error(w, "tunnel timeout", http.StatusGatewayTimeout)
	}
}

type responseMsg struct {
	Status  int
	Headers map[string]string
	Body    string
}

func (s *Server) handleTunnel(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("ws accept error: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	var reg proto.Register
	if err := wsjson.Read(ctx, conn, &reg); err != nil {
		cancel()
		conn.Close(websocket.StatusProtocolError, "expected register message")
		return
	}
	cancel()

	if reg.Type != proto.TypeRegister {
		conn.Close(websocket.StatusProtocolError, "expected register message")
		return
	}

	cctx, ccancel := context.WithCancel(context.Background())
	client := &ClientConn{
		ID:        reg.ClientID,
		Conn:      conn,
		Ctx:       cctx,
		Cancel:    ccancel,
		Routes:    reg.Routes,
		Connected: time.Now(),
		LastSeen:  time.Now(),
	}

	s.Hub.Remove(reg.ClientID)
	s.Hub.Add(client)

	go s.readLoop(client)
}

func (s *Server) readLoop(c *ClientConn) {
	defer func() {
		c.Cancel()
		s.Hub.Remove(c.ID)
		c.Conn.Close(websocket.StatusNormalClosure, "")
	}()

	for {
		typ, data, err := c.Conn.Read(c.Ctx)
		if err != nil {
			return
		}
		c.LastSeen = time.Now()

		switch typ {
		case websocket.MessageText:
			msgType, msg, err := proto.Decode(data)
			if err != nil {
				continue
			}
			switch msgType {
			case proto.TypeRes:
				res := msg.(*proto.Response)
				v, ok := pendingRequests.Load(res.ID)
				if ok {
					v.(chan responseMsg) <- responseMsg{
						Status:  res.Status,
						Headers: res.Headers,
						Body:    res.Body,
					}
				}
			case proto.TypeErr:
				errMsg := msg.(*proto.Error)
				v, ok := pendingRequests.Load(errMsg.ID)
				if ok {
					v.(chan responseMsg) <- responseMsg{
						Status:  502,
						Headers: map[string]string{"Content-Type": "text/plain"},
						Body:    errMsg.Error,
					}
				}
			case proto.TypePing:
				c.WriteJSON(proto.Pong{Type: proto.TypePong})
			case proto.TypeTCPClose:
				tcpClose := msg.(*proto.TCPClose)
				handleTCPClose(tcpClose.ID)
			}

		case websocket.MessageBinary:
			streamID, payload, err := proto.ReadTCPFrameFull(bytes.NewReader(data))
			if err != nil {
				continue
			}
			handleTCPData(c.ID, streamID, payload)
		}
	}
}
