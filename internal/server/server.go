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

	// WebSocket upgrade detection (Vite HMR, etc.)
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		if client == nil {
			// Path didn't match a route prefix — try host-only match.
			// Vite HMR may connect to / instead of /zz/.
			client, target = s.Hub.MatchWSRoute(host)
			pathPrefix = ""
		}
		if client != nil {
			s.handleWSProxy(w, r, client, target, pathPrefix)
			return
		}
	}

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

// handleWSProxy accepts the browser's WebSocket upgrade, relays it through the
// tunnel to relayc, which dials the local target (e.g. Vite HMR server).
func (s *Server) handleWSProxy(w http.ResponseWriter, r *http.Request, client *ClientConn, target, pathPrefix string) {
	// Strip path prefix (same logic as handleHTTP)
	wsPath := r.URL.RequestURI()
	if pathPrefix != "" && strings.HasPrefix(wsPath, pathPrefix) {
		wsPath = wsPath[len(pathPrefix):]
		if wsPath == "" {
			wsPath = "/"
		}
	}

	// Collect the upgrade headers to forward to relayc
	headers := make(map[string]string)
	for k, v := range r.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	// Accept the browser's WebSocket upgrade (writes 101 to browser)
	wsConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("ws accept error: %v", err)
		return
	}

	id := newRequestID()
	stream := &wsStream{ID: id, ClientID: client.ID, Conn: wsConn}
	wsStreamsMgr.mu.Lock()
	wsStreamsMgr.streams[id] = stream
	wsStreamsMgr.mu.Unlock()

	defer func() {
		wsStreamsMgr.mu.Lock()
		delete(wsStreamsMgr.streams, id)
		wsStreamsMgr.mu.Unlock()
		client.WriteJSON(proto.WSClose{Type: proto.TypeWSClose, ID: id})
		wsConn.Close(websocket.StatusNormalClosure, "")
		log.Printf("ws stream %s closed", id)
	}()

	// Tell relayc to open a WebSocket to the local target
	if err := client.WriteJSON(proto.WSConnect{
		Type:    proto.TypeWSConnect,
		ID:      id,
		Target:  target,
		Path:    wsPath,
		Headers: headers,
	}); err != nil {
		log.Printf("ws: send WSConnect failed: %v", err)
		return
	}

	s.Stats.TotalRequests.Add(1)
	log.Printf("ws proxy: stream=%s target=%s%s", id, target, wsPath)

	// Read from browser WS → binary frames → relayc tunnel WS
	ctx := context.Background()
	for {
		typ, data, err := wsConn.Read(ctx)
		if err != nil {
			return
		}
		var frame bytes.Buffer
		proto.WriteTCPFrameFull(&frame, id, data)
		client.mu.Lock()
		client.Conn.Write(client.Ctx, typ, frame.Bytes())
		client.mu.Unlock()
		client.BytesIn += int64(len(data))
		s.Stats.TotalBytesIn.Add(int64(len(data)))
	}
}

// Path rewriting regexes: each has 4 capture groups:
//
//	$1 = prefix before the opening quote (src=, url(, import, .post(, …)
//	$2 = opening quote (", ', or empty for unquoted CSS urls)
//	$3 = path WITHOUT leading /
//	$4 = closing quote + optional suffix
//
// Replacement: $1$2 + prefix + /$3$4
// Example: src="/app.js" with prefix=/zz → src="/zz/app.js"
//
// The leading / is a literal between $2 and $3 so it never appears inside a
// capture group, avoiding double-slash when prefix itself starts with /.

var pathRewriteRE = regexp.MustCompile(
	`((?:src|href|action)\s*=\s*)(["'])\s*/([^/\s][^"'\s]*)\s*(["'])`)

var cssURLRewriteRE = regexp.MustCompile(
	`(url\(\s*)(["']?)\s*/([^/\s][^)"'\s]*)\s*(["']?\s*\))`)

// Rewrites static import/export absolute paths in JavaScript:
//
//	import "/path"  →  import "/prefix/path"
//	from "/path"    →  from "/prefix/path"
var jsImportExportRE = regexp.MustCompile(
	`(\bimport\s+|\bfrom\s+)(["'])\s*/([^/\s][^"'\s]*)\s*(["'])`)

// Rewrites dynamic import() absolute paths in JavaScript:
//
//	import("/path")  →  import("/prefix/path")
var jsDynamicImportRE = regexp.MustCompile(
	`(import\s*\(\s*)(["'])\s*/([^/\s][^)"'\s]*)\s*(["']\s*\))`)

// Rewrites fetch() and axios-style HTTP call paths in JavaScript:
//
//	fetch("/api/x")      →  fetch("/prefix/api/x")
//	.get("/api/x")       →  .get("/prefix/api/x")
//	.post("/api/x", d)   →  .post("/prefix/api/x", d)
var jsHTTPCallRE = regexp.MustCompile(
	`((?:\.(?:get|post|put|delete|patch)\s*\(|fetch\s*\()\s*)(["'])\s*/([^/\s][^"'\s]*)\s*(["'])`)

func shouldRewrite(headers map[string]string) bool {
	ct := headers["Content-Type"]
	if ct == "" {
		return true // Vite may omit Content-Type for TS/JS modules
	}
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
	// Four groups: $1=prefix, $2=opening-quote, $3=path, $4=closing-quote.
	body = pathRewriteRE.ReplaceAllString(body, "${1}${2}"+prefix+"/${3}${4}")
	// Rewrite url(/path) in CSS
	body = cssURLRewriteRE.ReplaceAllString(body, "${1}${2}"+prefix+"/${3}${4}")
	// Rewrite import/export "/path" in JS
	body = jsImportExportRE.ReplaceAllString(body, "${1}${2}"+prefix+"/${3}${4}")
	// Rewrite import("/path") in JS
	body = jsDynamicImportRE.ReplaceAllString(body, "${1}${2}"+prefix+"/${3}${4}")
	// Rewrite fetch("/path") and .get("/path") etc. in JS
	body = jsHTTPCallRE.ReplaceAllString(body, "${1}${2}"+prefix+"/${3}${4}")
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
			case proto.TypeWSClose:
				wsClose := msg.(*proto.WSClose)
				handleWSClose(wsClose.ID)
			}

		case websocket.MessageBinary:
			streamID, payload, err := proto.ReadTCPFrameFull(bytes.NewReader(data))
			if err != nil {
				continue
			}
			// Route to the appropriate stream type (TCP or WS).
			if _, ok := getWSStream(streamID); ok {
				handleWSData(c.ID, streamID, payload)
			} else {
				handleTCPData(c.ID, streamID, payload)
			}
		}
	}
}
