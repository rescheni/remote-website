package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"relay-tunnel/internal/proto"
)

type Config struct {
	ServerAddr       string
	TLS              bool
	Secret           string
	ClientID         string
	Heartbeat        time.Duration
	ReconnectBackoff []time.Duration
	Routes           []proto.Route
}

// Currently connected WebSocket conn reference for TCP proxy writes
var (
	currentConn   *websocket.Conn
	currentConnMu sync.RWMutex
)

func setCurrentConn(conn *websocket.Conn) {
	currentConnMu.Lock()
	currentConn = conn
	currentConnMu.Unlock()
}

func getCurrentConn() *websocket.Conn {
	currentConnMu.RLock()
	defer currentConnMu.RUnlock()
	return currentConn
}

func Run(cfg *Config) error {
	backoffIdx := 0
	for {
		if err := connect(cfg); err != nil {
			delay := cfg.ReconnectBackoff[backoffIdx]
			log.Printf("connection failed: %v, reconnecting in %v", err, delay)
			time.Sleep(delay)
			if backoffIdx < len(cfg.ReconnectBackoff)-1 {
				backoffIdx++
			}
			continue
		}
		backoffIdx = 0
		time.Sleep(time.Second)
	}
}

func connect(cfg *Config) error {
	scheme := "ws"
	if cfg.TLS {
		scheme = "wss"
	}
	url := fmt.Sprintf("%s://%s/__tunnel__", scheme, cfg.ServerAddr)

	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization": []string{"Bearer " + cfg.Secret},
		},
	})
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	conn.SetReadLimit(-1) // disable 32KB default limit for tunneled HTTP responses
	defer conn.Close(websocket.StatusNormalClosure, "")
	setCurrentConn(conn)
	defer setCurrentConn(nil)

	log.Printf("connected to %s", url)

	reg := proto.Register{
		Type:     proto.TypeRegister,
		ClientID: cfg.ClientID,
		Routes:   cfg.Routes,
	}
	if err := wsjson.Write(ctx, conn, reg); err != nil {
		return fmt.Errorf("register: %w", err)
	}

	done := make(chan struct{})
	defer close(done)
	go heartbeat(ctx, conn, cfg.Heartbeat, done)

	// Read loop: handles both text (JSON) and binary (TCP data)
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}

		switch typ {
		case websocket.MessageText:
			msgType, msg, err := proto.Decode(data)
			if err != nil {
				continue
			}
			switch msgType {
			case proto.TypeReq:
				go handleRequest(ctx, conn, msg.(*proto.Request))
			case proto.TypeTCPConnect:
				go handleTCPConnect(msg.(*proto.TCPConnect))
			case proto.TypeTCPClose:
				stopTCPProxy(msg.(*proto.TCPClose).ID)
			case proto.TypeRouteUpdate:
				handleRouteUpdate(cfg, msg.(*proto.RouteUpdate))
			case proto.TypePong:
				// ignore
			}

		case websocket.MessageBinary:
			streamID, payload, err := proto.ReadTCPFrameFull(bytes.NewReader(data))
			if err != nil {
				continue
			}
			handleTCPDataFromServer(streamID, payload)
		}
	}
}

func heartbeat(ctx context.Context, conn *websocket.Conn, interval time.Duration, done chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := wsjson.Write(ctx, conn, proto.Ping{Type: proto.TypePing}); err != nil {
				return
			}
		case <-done:
			return
		}
	}
}

func handleRequest(ctx context.Context, conn *websocket.Conn, req *proto.Request) {
	targetURL := req.Target + req.Path
	if !strings.Contains(targetURL, "://") {
		targetURL = "http://" + targetURL
	}
	bodyReader := strings.NewReader(req.Body)
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, targetURL, bodyReader)
	if err != nil {
		sendError(ctx, conn, req.ID, err.Error())
		return
	}
	for k, v := range req.Headers {
		if !strings.HasPrefix(k, "X-Forwarded-") {
			httpReq.Header.Set(k, v)
		}
	}
	httpReq.RequestURI = ""
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		sendError(ctx, conn, req.ID, err.Error())
		return
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	headers := make(map[string]string)
	for k, v := range resp.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	resMsg := proto.Response{
		Type:    proto.TypeRes,
		ID:      req.ID,
		Status:  resp.StatusCode,
		Headers: headers,
		Body:    string(bodyBytes),
	}
	wsjson.Write(ctx, conn, resMsg)
}

func handleTCPConnect(msg *proto.TCPConnect) {
	wsConn := getCurrentConn()
	if wsConn == nil {
		return
	}
	go startTCPProxy(msg.ID, msg.Target, wsConn)
}

func handleRouteUpdate(cfg *Config, msg *proto.RouteUpdate) {
	cfg.Routes = msg.Routes
	log.Printf("routes updated: %d routes", len(msg.Routes))
	for _, r := range msg.Routes {
		if r.Type == "tcp" {
			log.Printf("  TCP %s -> :%d", r.Target, r.RemotePort)
		} else {
			log.Printf("  HTTP %s%s -> %s", r.Host, r.PathPrefix, r.Target)
		}
	}
}

func sendError(ctx context.Context, conn *websocket.Conn, id, errStr string) {
	wsjson.Write(ctx, conn, proto.Error{
		Type:  proto.TypeErr,
		ID:    id,
		Error: errStr,
	})
}
