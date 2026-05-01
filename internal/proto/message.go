package proto

import "encoding/json"

type MsgType string

const (
	TypeRegister MsgType = "register"
	TypePing     MsgType = "ping"
	TypePong     MsgType = "pong"
	TypeReq      MsgType = "req"
	TypeRes      MsgType = "res"
	TypeErr      MsgType = "err"
)

type Route struct {
	Host       string `json:"host"`
	PathPrefix string `json:"path_prefix,omitempty"`
	Target     string `json:"target"`
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
	Type    MsgType           `json:"type"`
	ID      string            `json:"id"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Target  string            `json:"target"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
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
	default:
		return header.Type, nil, nil
	}
}
