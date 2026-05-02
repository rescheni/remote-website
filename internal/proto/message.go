package proto

import (
	"encoding/binary"
	"encoding/json"
	"io"
)

type MsgType string

const (
	TypeRegister    MsgType = "register"
	TypePing        MsgType = "ping"
	TypePong        MsgType = "pong"
	TypeReq         MsgType = "req"
	TypeRes         MsgType = "res"
	TypeErr         MsgType = "err"
	TypeTCPConnect  MsgType = "tcp_connect"
	TypeTCPClose    MsgType = "tcp_close"
	TypeRouteUpdate MsgType = "route_update"
)

type Route struct {
	Host       string `json:"host,omitempty"`
	PathPrefix string `json:"path_prefix,omitempty"`
	Target     string `json:"target"`
	Type       string `json:"type,omitempty"`       // "http" or "tcp", default "http"
	RemotePort int    `json:"remote_port,omitempty"` // for tcp: server port to listen
}

type Register struct {
	Type     MsgType `json:"type"`
	ClientID string  `json:"client_id"`
	Routes   []Route `json:"routes"`
}

type Ping struct {
	Type MsgType `json:"type"`
}

type Pong struct {
	Type MsgType `json:"type"`
}

type Request struct {
	Type       MsgType           `json:"type"`
	ID         string            `json:"id"`
	Method     string            `json:"method"`
	Path       string            `json:"path"`
	Target     string            `json:"target"`
	PathPrefix string            `json:"path_prefix,omitempty"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
}

type Response struct {
	Type    MsgType           `json:"type"`
	ID      string            `json:"id"`
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

type Error struct {
	Type  MsgType `json:"type"`
	ID    string  `json:"id"`
	Error string  `json:"error"`
}

type TCPConnect struct {
	Type       MsgType `json:"type"`
	ID         string  `json:"id"`
	Target     string  `json:"target"`
	RemotePort int     `json:"remote_port"`
}

type TCPClose struct {
	Type MsgType `json:"type"`
	ID   string  `json:"id"`
}

type RouteUpdate struct {
	Type   MsgType `json:"type"`
	Routes []Route `json:"routes"`
}

func Decode(data []byte) (MsgType, any, error) {
	var header struct {
		Type MsgType `json:"type"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return "", nil, err
	}
	switch header.Type {
	case TypeRegister:
		var m Register
		if err := json.Unmarshal(data, &m); err != nil {
			return "", nil, err
		}
		return TypeRegister, &m, nil
	case TypePing:
		return TypePing, &Ping{Type: TypePing}, nil
	case TypePong:
		return TypePong, &Pong{Type: TypePong}, nil
	case TypeReq:
		var m Request
		if err := json.Unmarshal(data, &m); err != nil {
			return "", nil, err
		}
		return TypeReq, &m, nil
	case TypeRes:
		var m Response
		if err := json.Unmarshal(data, &m); err != nil {
			return "", nil, err
		}
		return TypeRes, &m, nil
	case TypeErr:
		var m Error
		if err := json.Unmarshal(data, &m); err != nil {
			return "", nil, err
		}
		return TypeErr, &m, nil
	case TypeTCPConnect:
		var m TCPConnect
		if err := json.Unmarshal(data, &m); err != nil {
			return "", nil, err
		}
		return TypeTCPConnect, &m, nil
	case TypeTCPClose:
		var m TCPClose
		if err := json.Unmarshal(data, &m); err != nil {
			return "", nil, err
		}
		return TypeTCPClose, &m, nil
	case TypeRouteUpdate:
		var m RouteUpdate
		if err := json.Unmarshal(data, &m); err != nil {
			return "", nil, err
		}
		return TypeRouteUpdate, &m, nil
	default:
		return header.Type, nil, nil
	}
}

// Binary frame format for TCP data:
// [4 bytes big-endian: stream_id byte length][stream_id bytes][raw data]

func WriteTCPFrame(w io.Writer, streamID string, data []byte) error {
	header := make([]byte, 4+len(streamID))
	binary.BigEndian.PutUint32(header, uint32(len(streamID)))
	copy(header[4:], streamID)
	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}

func ReadTCPFrame(r io.Reader) (streamID string, data []byte, err error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return "", nil, err
	}
	idLen := binary.BigEndian.Uint32(lenBuf[:])
	if idLen > 256 {
		return "", nil, io.ErrUnexpectedEOF
	}
	idBuf := make([]byte, idLen)
	if _, err := io.ReadFull(r, idBuf); err != nil {
		return "", nil, err
	}
	streamID = string(idBuf)
	// For now read the rest as one chunk; a production implementation
	// would use length-prefixed or streaming chunks.
	data = make([]byte, 0)
	return
}

// WriteTCPFrameFull writes a complete binary frame with length prefix for the data portion.
func WriteTCPFrameFull(w io.Writer, streamID string, data []byte) error {
	header := make([]byte, 8+len(streamID))
	binary.BigEndian.PutUint32(header, uint32(len(streamID)))
	binary.BigEndian.PutUint32(header[4:], uint32(len(data)))
	copy(header[8:], streamID)
	if _, err := w.Write(header); err != nil {
		return err
	}
	if len(data) > 0 {
		_, err := w.Write(data)
		return err
	}
	return nil
}

func ReadTCPFrameFull(r io.Reader) (streamID string, data []byte, err error) {
	var hdr [8]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return "", nil, err
	}
	idLen := binary.BigEndian.Uint32(hdr[0:4])
	dataLen := binary.BigEndian.Uint32(hdr[4:8])
	if idLen > 256 || dataLen > 65536 {
		return "", nil, io.ErrUnexpectedEOF
	}
	idBuf := make([]byte, idLen)
	if _, err := io.ReadFull(r, idBuf); err != nil {
		return "", nil, err
	}
	data = make([]byte, dataLen)
	if dataLen > 0 {
		if _, err := io.ReadFull(r, data); err != nil {
			return "", nil, err
		}
	}
	return string(idBuf), data, nil
}
