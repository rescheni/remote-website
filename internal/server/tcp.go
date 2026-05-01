package server

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net"
	"sync"

	"github.com/coder/websocket"

	"relay-tunnel/internal/proto"
)

type tcpStream struct {
	ID       string
	ClientID string
	Local    net.Conn
	mu       sync.Mutex
}

type tcpListener struct {
	port     int
	listener net.Listener
	server   *Server
}

var (
	tcpStreamsMgr = struct {
		streams map[string]*tcpStream
		mu      sync.Mutex
	}{streams: make(map[string]*tcpStream)}
)

func (s *Server) startTCPListeners(ports []int) []*tcpListener {
	var listeners []*tcpListener
	for _, port := range ports {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			log.Printf("TCP listen :%d failed: %v", port, err)
			continue
		}
		tl := &tcpListener{port: port, listener: ln, server: s}
		listeners = append(listeners, tl)
		go tl.acceptLoop()
		log.Printf("TCP proxy on :%d", port)
	}
	return listeners
}

func (tl *tcpListener) acceptLoop() {
	for {
		conn, err := tl.listener.Accept()
		if err != nil {
			return
		}
		go tl.handle(conn)
	}
}

func (tl *tcpListener) handle(conn net.Conn) {
	defer conn.Close()

	client, target := tl.server.Hub.MatchTCPRoute(tl.port)
	if client == nil {
		log.Printf("TCP :%d: no route", tl.port)
		return
	}

	id := newRequestID()
	stream := &tcpStream{ID: id, ClientID: client.ID, Local: conn}
	tcpStreamsMgr.mu.Lock()
	tcpStreamsMgr.streams[id] = stream
	tcpStreamsMgr.mu.Unlock()

	defer func() {
		tcpStreamsMgr.mu.Lock()
		delete(tcpStreamsMgr.streams, id)
		tcpStreamsMgr.mu.Unlock()
		client.WriteJSON(proto.TCPClose{Type: proto.TypeTCPClose, ID: id})
	}()

	client.WriteJSON(proto.TCPConnect{
		Type:       proto.TypeTCPConnect,
		ID:         id,
		Target:     target,
		RemotePort: tl.port,
	})

	client.BytesIn += 1
	tl.server.Stats.TotalRequests.Add(1)

	buf := make([]byte, 32768)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			var frame bytes.Buffer
			proto.WriteTCPFrameFull(&frame, id, buf[:n])
			client.mu.Lock()
			client.Conn.Write(client.Ctx, websocket.MessageBinary, frame.Bytes())
			client.mu.Unlock()
			client.BytesIn += int64(n)
			tl.server.Stats.TotalBytesIn.Add(int64(n))
		}
		if err != nil {
			break
		}
	}
}

func handleTCPData(clientID, streamID string, data []byte) {
	tcpStreamsMgr.mu.Lock()
	s, ok := tcpStreamsMgr.streams[streamID]
	tcpStreamsMgr.mu.Unlock()
	if !ok || s.ClientID != clientID {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Local.Write(data)
}

func handleTCPClose(streamID string) {
	tcpStreamsMgr.mu.Lock()
	s, ok := tcpStreamsMgr.streams[streamID]
	if ok {
		delete(tcpStreamsMgr.streams, streamID)
	}
	tcpStreamsMgr.mu.Unlock()
	if ok {
		s.Local.Close()
	}
}

// reader that wraps io.Reader for reading TCP frames from WS binary messages
type frameReader struct {
	r io.Reader
}

func readTCPFrame(r io.Reader) (streamID string, data []byte, err error) {
	return proto.ReadTCPFrameFull(r)
}
