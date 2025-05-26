package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/ypapish/software-architecture-lab4/httptools"
	"github.com/ypapish/software-architecture-lab4/signal"
)

var (
	port       = flag.Int("port", 8090, "load balancer port")
	timeoutSec = flag.Int("timeout-sec", 3, "request timeout time in seconds")
	https      = flag.Bool("https", false, "whether backends support HTTPs")

	traceEnabled = flag.Bool("trace", false, "whether to include tracing information into responses")
)

type Server struct {
	URL         string
	ActiveConns int
	Mutex       sync.Mutex
	IsHealthy   bool
}

var (
	timeout     = time.Duration(*timeoutSec) * time.Second
	serversPool = []*Server{
		{URL: "server1:8080"},
		{URL: "server2:8080"},
		{URL: "server3:8080"},
	}
	poolMutex sync.RWMutex
)

func scheme() string {
	if *https {
		return "https"
	}
	return "http"
}

func health(server *Server) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("%s://%s/health", scheme(), server.URL), nil)

	resp, err := http.DefaultClient.Do(req)
	server.Mutex.Lock()
	defer server.Mutex.Unlock()
	if err != nil || resp.StatusCode != http.StatusOK {
		server.IsHealthy = false
		return
	}
	server.IsHealthy = true
}

func forward(dst string, rw http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	fwdRequest := r.Clone(ctx)
	fwdRequest.RequestURI = ""
	fwdRequest.URL.Host = dst
	fwdRequest.URL.Scheme = scheme()
	fwdRequest.Host = dst

	resp, err := http.DefaultClient.Do(fwdRequest)
	if err != nil {
		log.Printf("Failed to get response from %s: %s", dst, err)
		rw.WriteHeader(http.StatusServiceUnavailable)
		return err
	}
	defer resp.Body.Close()

	for k, values := range resp.Header {
		for _, value := range values {
			rw.Header().Add(k, value)
		}
	}
	if *traceEnabled {
		rw.Header().Set("lb-from", dst)
	}
	log.Println("fwd", resp.StatusCode, resp.Request.URL)
	rw.WriteHeader(resp.StatusCode)
	_, err = io.Copy(rw, resp.Body)
	if err != nil {
		log.Printf("Failed to write response: %s", err)
	}
	return nil
}

func findLeastBusyServer() *Server {
	poolMutex.RLock()
	defer poolMutex.RUnlock()

	var leastBusyServer *Server

	for _, server := range serversPool {
		server.Mutex.Lock()
		if server.IsHealthy {
			if leastBusyServer == nil || server.ActiveConns < leastBusyServer.ActiveConns {
				leastBusyServer = server
			}
		}
		server.Mutex.Unlock()
	}
	return leastBusyServer
}

func main() {
	flag.Parse()

	go func() {
		for {
			for _, server := range serversPool {
				health(server)
			}
			time.Sleep(10 * time.Second)
		}
	}()

	frontend := httptools.CreateServer(*port, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		server := findLeastBusyServer()
		if server == nil {
			rw.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		server.Mutex.Lock()
		server.ActiveConns++
		server.Mutex.Unlock()

		defer func() {
			server.Mutex.Lock()
			server.ActiveConns--
			server.Mutex.Unlock()
		}()

		forward(server.URL, rw, r)
	}))

	log.Println("Starting load balancer...")
	log.Printf("Tracing support enabled: %t", *traceEnabled)
	frontend.Start()
	signal.WaitForTerminationSignal()
}
