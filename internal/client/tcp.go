package client

import (
	"bytes"
	"context"
	"log"
	"net"
	"sync"

	"github.com/coder/websocket"

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
