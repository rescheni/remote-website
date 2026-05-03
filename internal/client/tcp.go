package client

import (
	"bytes"
	"context"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"relay-tunnel/internal/proto"
)

type tcpProxy struct {
	ID   string
	Conn net.Conn
	done chan struct{}
}

var (
	tcpProxies   = map[string]*tcpProxy{}
	tcpProxiesMu sync.Mutex
)

func startTCPProxy(id, target string, wsConn *websocket.Conn) {
	local, err := net.Dial("tcp", target)
	if err != nil {
		log.Printf("TCP dial %s failed: %v", target, err)
		return
	}

	p := &tcpProxy{ID: id, Conn: local, done: make(chan struct{})}
	tcpProxiesMu.Lock()
	tcpProxies[id] = p
	tcpProxiesMu.Unlock()

	log.Printf("TCP stream %s: %s connected", id, target)

	// local -> WS
	go func() {
		ctx := context.Background()
		buf := make([]byte, 32768)
		for {
			n, err := local.Read(buf)
			if n > 0 {
				var frame bytes.Buffer
				proto.WriteTCPFrameFull(&frame, id, buf[:n])
				wsConn.Write(ctx, websocket.MessageBinary, frame.Bytes())
			}
			if err != nil {
				break
			}
		}
		close(p.done)
	}()

	<-p.done
	stopTCPProxy(id)
}

func handleTCPDataFromServer(streamID string, data []byte) {
	tcpProxiesMu.Lock()
	p, ok := tcpProxies[streamID]
	tcpProxiesMu.Unlock()
	if ok {
		p.Conn.Write(data)
	}
}

func stopTCPProxy(id string) {
	tcpProxiesMu.Lock()
	p, ok := tcpProxies[id]
	if ok {
		delete(tcpProxies, id)
	}
	tcpProxiesMu.Unlock()
	if ok {
		p.Conn.Close()
		log.Printf("TCP stream %s closed", id)
	}
}

// --- WS proxy (for Vite HMR, etc.) ---

type wsProxy struct {
	ID   string
	Conn *websocket.Conn
	done chan struct{}
}

var (
	wsProxies   = map[string]*wsProxy{}
	wsProxiesMu sync.Mutex
)

func handleWSConnect(msg *proto.WSConnect, wsConn *websocket.Conn) {
	targetURL := "ws://" + msg.Target + msg.Path
	log.Printf("WS connect: dialing %s (stream %s)", targetURL, msg.ID)

	// Extract subprotocols from the incoming headers (e.g. vite-hmr).
	var subprotocols []string
	if sp := msg.Headers["Sec-Websocket-Protocol"]; sp != "" {
		subprotocols = strings.Split(sp, ",")
		for i := range subprotocols {
			subprotocols[i] = strings.TrimSpace(subprotocols[i])
		}
	}

	// Dial the local WebSocket server (Vite HMR)
	conn, _, err := websocket.Dial(context.Background(), targetURL, &websocket.DialOptions{
		HTTPHeader:   wsHeaders(msg.Headers),
		Subprotocols: subprotocols,
	})
	if err != nil {
		log.Printf("WS dial %s failed: %v (stream %s)", targetURL, err, msg.ID)
		return
	}

	p := &wsProxy{ID: msg.ID, Conn: conn, done: make(chan struct{})}
	wsProxiesMu.Lock()
	wsProxies[msg.ID] = p
	wsProxiesMu.Unlock()

	log.Printf("WS stream %s: %s connected", msg.ID, targetURL)

	// Vite WS → tunnel WS (binary frames)
	go func() {
		ctx := context.Background()
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				break
			}
			var frame bytes.Buffer
			proto.WriteTCPFrameFull(&frame, msg.ID, data)
			wsConn.Write(ctx, websocket.MessageBinary, frame.Bytes())
		}
		close(p.done)
	}()

	<-p.done
	stopWSProxy(msg.ID)

	// Tell relayd the stream is closed
	wsjson.Write(context.Background(), wsConn, proto.WSClose{
		Type: proto.TypeWSClose,
		ID:   msg.ID,
	})
}

func wsHeaders(headers map[string]string) http.Header {
	h := http.Header{}
	for k, v := range headers {
		// Skip hop-by-hop and connection-specific headers
		kl := strings.ToLower(k)
		if kl == "connection" || kl == "upgrade" || kl == "sec-websocket-key" ||
			kl == "sec-websocket-version" || kl == "sec-websocket-extensions" ||
			kl == "sec-websocket-protocol" ||
			strings.HasPrefix(kl, "x-forwarded-") {
			continue
		}
		h.Set(k, v)
	}
	return h
}

func getWSProxy(streamID string) (*wsProxy, bool) {
	wsProxiesMu.Lock()
	p, ok := wsProxies[streamID]
	wsProxiesMu.Unlock()
	return p, ok
}

func handleWSDataFromServer(streamID string, data []byte) {
	wsProxiesMu.Lock()
	p, ok := wsProxies[streamID]
	wsProxiesMu.Unlock()
	if ok {
		p.Conn.Write(context.Background(), websocket.MessageText, data)
	}
}

func stopWSProxy(id string) {
	wsProxiesMu.Lock()
	p, ok := wsProxies[id]
	if ok {
		delete(wsProxies, id)
	}
	wsProxiesMu.Unlock()
	if ok {
		p.Conn.Close(websocket.StatusNormalClosure, "")
		log.Printf("WS stream %s closed", id)
	}
}
