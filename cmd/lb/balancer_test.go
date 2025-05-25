package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func makeHealthyStubServer(t *testing.T, response string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		io.WriteString(w, response)
	}))
}

func TestHealthHealthyServer(t *testing.T) {
	server := makeHealthyStubServer(t, "")
	defer server.Close()

	s := &Server{
		URL: server.Listener.Addr().String(),
	}

	*https = false
	timeout = time.Second * 1

	health(s)

	if !s.IsHealthy {
		t.Errorf("Expected IsHealthy = true, got false")
	}
}

func TestHealthUnhealthyServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := &Server{
		URL: srv.Listener.Addr().String(),
	}

	health(s)

	if s.IsHealthy {
		t.Errorf("Expected IsHealthy = false, got true")
	}
}

func TestForward(t *testing.T) {
	expected := "response-from-backend"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, expected)
	}))
	defer srv.Close()

	req := httptest.NewRequest(http.MethodGet, "http://fake/", nil)
	w := httptest.NewRecorder()

	err := forward(srv.Listener.Addr().String(), w, req)
	if err != nil {
		t.Fatalf("forward returned error: %v", err)
	}

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
	if string(body) != expected {
		t.Errorf("Expected body %q, got %q", expected, body)
	}
}

func TestFindLeastBusyServer(t *testing.T) {
	serversPool = []*Server{
		{URL: "s1", ActiveConns: 2, IsHealthy: true, Mutex: sync.Mutex{}},
		{URL: "s2", ActiveConns: 1, IsHealthy: true, Mutex: sync.Mutex{}},
		{URL: "s3", ActiveConns: 0, IsHealthy: false, Mutex: sync.Mutex{}},
	}

	server := findLeastBusyServer()
	if server == nil {
		t.Fatal("Expected to find a healthy server")
	}
	if server.URL != "s2" {
		t.Errorf("Expected s2, got %s", server.URL)
	}
}
