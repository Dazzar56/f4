package f4rpc

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/vmihailenco/msgpack/v5"
)

// Message is the generic envelope for all RPC traffic.
type Message struct {
	Type   int                `msgpack:"t"` // 0: Request, 1: Response
	ID     uint32             `msgpack:"i"`
	Method string             `msgpack:"m,omitempty"`
	Data   msgpack.RawMessage `msgpack:"d,omitempty"`
	Error  string             `msgpack:"e,omitempty"`
}

// Handler defines a callback for processing incoming requests.
type Handler func(data msgpack.RawMessage) (any, error)

// Session multiplexes concurrent requests and responses over an io.Reader and io.Writer.
type Session struct {
	enc      *msgpack.Encoder
	dec      *msgpack.Decoder
	mu       sync.Mutex
	handlers map[string]Handler
	pending  map[uint32]chan *Message
	nextID   uint32
}

// NewSession creates a new RPC session.
func NewSession(r io.Reader, w io.Writer) *Session {
	return &Session{
		enc:      msgpack.NewEncoder(w),
		dec:      msgpack.NewDecoder(r),
		handlers: make(map[string]Handler),
		pending:  make(map[uint32]chan *Message),
	}
}

// Register assigns a callback to a specific RPC method name.
func (s *Session) Register(method string, h Handler) {
	s.handlers[method] = h
}

// Call makes a synchronous RPC call to the remote endpoint.
func (s *Session) Call(method string, params any, result any) error {
	id := atomic.AddUint32(&s.nextID, 1)
	ch := make(chan *Message, 1)

	s.mu.Lock()
	s.pending[id] = ch
	s.mu.Unlock()

	var rawParams msgpack.RawMessage
	if params != nil {
		b, err := msgpack.Marshal(params)
		if err != nil {
			return fmt.Errorf("marshal params error: %w", err)
		}
		rawParams = b
	}

	req := &Message{
		Type:   0,
		ID:     id,
		Method: method,
		Data:   rawParams,
	}

	s.mu.Lock()
	err := s.enc.Encode(req)
	s.mu.Unlock()

	if err != nil {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
		return fmt.Errorf("send request error: %w", err)
	}

	resp := <-ch
	if resp.Error != "" {
		return fmt.Errorf("rpc error: %s", resp.Error)
	}

	if result != nil && len(resp.Data) > 0 {
		if err := msgpack.Unmarshal(resp.Data, result); err != nil {
			return fmt.Errorf("unmarshal result error: %w", err)
		}
	}
	return nil
}

// Serve starts the blocking loop that reads incoming messages.
func (s *Session) Serve() error {
	for {
		var msg Message
		if err := s.dec.Decode(&msg); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("decode error: %w", err)
		}

		if msg.Type == 1 { // Response
			s.mu.Lock()
			ch, ok := s.pending[msg.ID]
			if ok {
				delete(s.pending, msg.ID)
			}
			s.mu.Unlock()
			if ok {
				ch <- &msg
			}
		} else if msg.Type == 0 { // Request
			go s.handleRequest(&msg)
		}
	}
}

func (s *Session) handleRequest(req *Message) {
	s.mu.Lock()
	h, ok := s.handlers[req.Method]
	s.mu.Unlock()

	resp := &Message{
		Type: 1,
		ID:   req.ID,
	}

	if !ok {
		resp.Error = fmt.Sprintf("method %q not found", req.Method)
	} else {
		res, err := h(req.Data)
		if err != nil {
			resp.Error = err.Error()
		} else if res != nil {
			b, err := msgpack.Marshal(res)
			if err != nil {
				resp.Error = "failed to marshal response"
			} else {
				resp.Data = b
			}
		}
	}

	s.mu.Lock()
	s.enc.Encode(resp)
	s.mu.Unlock()
}