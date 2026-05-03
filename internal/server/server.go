package server

import (
	"bytes"
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"regexp"
	"strings"
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

	client, target, pathPrefix := s.Hub.MatchRoute(host, r.URL.Path)
	if client == nil {
		log.Printf("no route: host=%s path=%s", host, r.URL.Path)
		http.Error(w, "no route for "+host+r.URL.Path, http.StatusNotFound)
		return
	}
	log.Printf("route match: host=%s path=%s target=%s prefix=%q", host, r.URL.Path, target, pathPrefix)

	headers := make(map[string]string)
	for k, v := range r.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	headers["X-Forwarded-Host"] = host

	// Strip path prefix so the client requests the clean path from the local backend.
	// The prefix is re-injected into HTML responses via rewriteResponseBody.
	reqPath := r.URL.RequestURI()
	if pathPrefix != "" && strings.HasPrefix(reqPath, pathPrefix) {
		reqPath = reqPath[len(pathPrefix):]
		if reqPath == "" {
			reqPath = "/"
		}
	}

	bodyBytes, _ := io.ReadAll(r.Body)
	r.Body.Close()

	reqMsg := proto.Request{
		Type:       proto.TypeReq,
		ID:         newRequestID(),
		Method:     r.Method,
		Path:       reqPath,
		Target:     target,
		PathPrefix: pathPrefix,
		Headers:    headers,
		Body:       string(bodyBytes),
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
		body := resp.Body
		if pathPrefix != "" {
			if shouldRewrite(resp.Headers) {
				origLen := len(body)
				body = rewriteResponseBody(body, pathPrefix)
				log.Printf("rewrite: prefix=%s, body %d->%d bytes", pathPrefix, origLen, len(body))
				delete(resp.Headers, "Content-Length")
			} else {
				log.Printf("rewrite: skipped prefix=%s, content-type=%q", pathPrefix, resp.Headers["Content-Type"])
			}
		}
		for k, v := range resp.Headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(resp.Status)
		w.Write([]byte(body))
	case <-time.After(30 * time.Second):
		log.Printf("timeout: host=%s path=%s target=%s client=%s", host, r.URL.Path, target, client.ID)
		http.Error(w, "tunnel timeout", http.StatusGatewayTimeout)
	}
}

// Path rewriting regexp: matches absolute paths in HTML/CSS attributes,
// rewriting them to include the path prefix so the browser routes requests
// through the relay server correctly.
//
// Captures: attribute=" / path ", avoiding protocol-relative (//) and
// absolute URLs (http://, https://, data:).
var pathRewriteRE = regexp.MustCompile(
	`((?:src|href|action)\s*=\s*["'])\s*(/[^/\s][^"'\s]*)\s*(["'])`)

var cssURLRewriteRE = regexp.MustCompile(
	`(url\(\s*["']?)\s*(/[^/\s][^)"'\s]*)\s*(["']?\s*\))`)

// Rewrites static import/export absolute paths in JavaScript:
//
//	import "/path"  →  import "/prefix/path"
//	from "/path"    →  from "/prefix/path"
var jsImportExportRE = regexp.MustCompile(
	`(\bimport\s+["']|\bfrom\s+["'])\s*(/[^/\s][^"'\s]*)\s*(["'])`)

// Rewrites dynamic import() absolute paths in JavaScript:
//
//	import("/path")  →  import("/prefix/path")
var jsDynamicImportRE = regexp.MustCompile(
	`(import\s*\(\s*["'])\s*(/[^/\s][^)"'\s]*)\s*(["']\s*\))`)

func shouldRewrite(headers map[string]string) bool {
	ct := headers["Content-Type"]
	return strings.Contains(ct, "text/html") ||
		strings.Contains(ct, "application/xhtml") ||
		strings.Contains(ct, "text/javascript") ||
		strings.Contains(ct, "application/javascript") ||
		strings.Contains(ct, "text/css")
}

func rewriteResponseBody(body, prefix string) string {
	// Ensure prefix has leading / and no trailing /
	prefix = "/" + strings.Trim(prefix, "/")
	// Handle the special case where prefix is just "/"
	if prefix == "/" {
		return body
	}

	// Rewrite src="/path", href="/path", action="/path" in HTML
	// NOTE: ${prefix} inside the replacement string is NOT a Go variable —
	// it is a Go regexp capture-group reference (to a group named "prefix").
	// We must concatenate the Go prefix variable outside the pattern.
	body = pathRewriteRE.ReplaceAllString(body, "${1}"+prefix+"${2}${3}")
	// Rewrite url(/path) in CSS
	body = cssURLRewriteRE.ReplaceAllString(body, "${1}"+prefix+"${2}${3}")
	// Rewrite import/export "/path" in JS
	body = jsImportExportRE.ReplaceAllString(body, "${1}"+prefix+"${2}${3}")
	// Rewrite import("/path") in JS
	body = jsDynamicImportRE.ReplaceAllString(body, "${1}"+prefix+"${2}${3}")
	return body
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
	conn.SetReadLimit(-1) // disable 32KB default limit for tunneled HTTP responses

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

	if existing := s.Hub.Get(reg.ClientID); existing != nil {
		log.Printf("WARNING: replacing client %s (was connected %v ago, last seen %v ago)",
			reg.ClientID,
			time.Since(existing.Connected).Round(time.Second),
			time.Since(existing.LastSeen).Round(time.Second))
		if len(existing.Routes) > 0 {
			client.Routes = existing.Routes
			log.Printf("preserved %d routes from previous connection", len(existing.Routes))
		}
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
