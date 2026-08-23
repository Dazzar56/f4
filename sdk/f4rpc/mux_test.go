package f4rpc

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/vmihailenco/msgpack/v5"
)

func TestSession_CallAndServe(t *testing.T) {
	// Эмулируем стандартные потоки с помощью in-memory пайпов
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()

	client := NewSession(s2cR, c2sW)
	server := NewSession(c2sR, s2cW)

	// Регистрируем хендлер на сервере
	server.Register("Test.Echo", func(data msgpack.RawMessage) (any, error) {
		var msg string
		if err := msgpack.Unmarshal(data, &msg); err != nil {
			return nil, err
		}
		return msg + " (RPC)", nil
	})

	// Запускаем слушателей в фоне
	serverErr := make(chan error, 1)
	clientErr := make(chan error, 1)
	go func() { serverErr <- server.Serve() }()
	go func() { clientErr <- client.Serve() }()
	t.Cleanup(func() {
		if err := c2sW.Close(); err != nil {
			t.Errorf("close client pipe: %v", err)
		}
		if err := s2cW.Close(); err != nil {
			t.Errorf("close server pipe: %v", err)
		}
		if err := <-serverErr; err != nil {
			t.Errorf("server Serve: %v", err)
		}
		if err := <-clientErr; err != nil {
			t.Errorf("client Serve: %v", err)
		}
	})

	// Выполняем синхронный вызов с клиента на сервер
	var res string
	err := client.Call("Test.Echo", "Hello", &res)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if res != "Hello (RPC)" {
		t.Errorf("Unexpected RPC response: %q", res)
	}
}

func TestSession_MethodNotFound(t *testing.T) {
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()

	client := NewSession(s2cR, c2sW)
	server := NewSession(c2sR, s2cW)

	serverErr := make(chan error, 1)
	clientErr := make(chan error, 1)
	go func() { serverErr <- server.Serve() }()
	go func() { clientErr <- client.Serve() }()
	t.Cleanup(func() {
		if err := c2sW.Close(); err != nil {
			t.Errorf("close client pipe: %v", err)
		}
		if err := s2cW.Close(); err != nil {
			t.Errorf("close server pipe: %v", err)
		}
		if err := <-serverErr; err != nil {
			t.Errorf("server Serve: %v", err)
		}
		if err := <-clientErr; err != nil {
			t.Errorf("client Serve: %v", err)
		}
	})

	err := client.Call("Unknown.Method", nil, nil)
	if err == nil {
		t.Fatal("Expected error for unknown method, got nil")
	}
	if !strings.Contains(err.Error(), "method \"Unknown.Method\" not found") {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestSession_Concurrency(t *testing.T) {
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()

	client := NewSession(s2cR, c2sW)
	server := NewSession(c2sR, s2cW)

	server.Register("Ping", func(data msgpack.RawMessage) (any, error) {
		time.Sleep(10 * time.Millisecond) // Имитация задержки обработки
		return "Pong", nil
	})

	serverErr := make(chan error, 1)
	clientErr := make(chan error, 1)
	go func() { serverErr <- server.Serve() }()
	go func() { clientErr <- client.Serve() }()
	t.Cleanup(func() {
		if err := c2sW.Close(); err != nil {
			t.Errorf("close client pipe: %v", err)
		}
		if err := s2cW.Close(); err != nil {
			t.Errorf("close server pipe: %v", err)
		}
		if err := <-serverErr; err != nil {
			t.Errorf("server Serve: %v", err)
		}
		if err := <-clientErr; err != nil {
			t.Errorf("client Serve: %v", err)
		}
	})

	done := make(chan bool)
	for i := 0; i < 50; i++ {
		go func() {
			var res string
			err := client.Call("Ping", nil, &res)
			if err != nil || res != "Pong" {
				t.Errorf("Concurrent ping failed: err=%v, res=%s", err, res)
			}
			done <- true
		}()
	}

	// Ожидаем завершения всех 50 горутин
	timeout := time.After(2 * time.Second)
	for i := 0; i < 50; i++ {
		select {
		case <-done:
		case <-timeout:
			t.Fatal("Timeout waiting for concurrent calls to finish")
		}
	}
}
