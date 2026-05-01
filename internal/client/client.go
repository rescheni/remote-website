package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
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
	defer conn.Close(websocket.StatusNormalClosure, "")

	log.Printf("connected to %s", url)

	// Send register
	reg := proto.Register{
		Type:     proto.TypeRegister,
		ClientID: cfg.ClientID,
		Routes:   cfg.Routes,
	}
	if err := wsjson.Write(ctx, conn, reg); err != nil {
		return fmt.Errorf("register: %w", err)
	}

	// Start heartbeat
	done := make(chan struct{})
	defer close(done)
	go heartbeat(ctx, conn, cfg.Heartbeat, done)

	// Read loop
	for {
		var raw json.RawMessage
		if err := wsjson.Read(ctx, conn, &raw); err != nil {
			return fmt.Errorf("read: %w", err)
		}

		msgType, msg, err := proto.Decode(raw)
		if err != nil {
			continue
		}

		switch msgType {
		case proto.TypeReq:
			go handleRequest(ctx, conn, msg.(*proto.Request))
		case proto.TypePong:
			// ignore
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
	// req.Target is the base URL like "http://localhost:3000"
	targetURL := req.Target + req.Path
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

func sendError(ctx context.Context, conn *websocket.Conn, id, errStr string) {
	wsjson.Write(ctx, conn, proto.Error{
		Type:  proto.TypeErr,
		ID:    id,
		Error: errStr,
	})
}
