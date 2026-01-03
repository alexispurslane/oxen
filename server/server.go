package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type sseClient struct {
	writer http.ResponseWriter
	done   chan struct{}
}

type Server struct {
	Dir        string
	Port       int
	clients    sync.Map
	reloadChan chan struct{}
	httpServer *http.Server
}

func NewServer(dir string, port int) *Server {
	return &Server{
		Dir:        dir,
		Port:       port,
		clients:    sync.Map{},
		reloadChan: make(chan struct{}, 1),
	}
}

func (s *Server) NotifyReload() {
	select {
	case s.reloadChan <- struct{}{}:
	default:
	}
}

func (s *Server) HandleSSE(w http.ResponseWriter, r *http.Request) {
	slog.Debug("New SSE client connected", "remote_addr", r.RemoteAddr)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	client := &sseClient{
		writer: w,
		done:   make(chan struct{}),
	}
	s.clients.Store(r.RemoteAddr, client)

	<-client.done
	s.clients.Delete(r.RemoteAddr)
	slog.Debug("SSE client disconnected", "remote_addr", r.RemoteAddr)
}

func (s *Server) startReloadBroadcaster() {
	for range s.reloadChan {
		clientCount := 0
		s.clients.Range(func(key, value any) bool {
			client := value.(*sseClient)
			client.writer.Write([]byte("data: reload\n\n"))
			client.writer.(http.Flusher).Flush()
			clientCount++
			return true
		})
		slog.Debug("Broadcasted reload signal to clients", "client_count", clientCount)
	}
}

func (s *Server) Run() error {
	absDir, err := filepath.Abs(s.Dir)
	if err != nil {
		return fmt.Errorf("error getting absolute path: %w", err)
	}

	slog.Info("Starting HTTP server", "address", fmt.Sprintf(":%d", s.Port), "dir", absDir)
	go s.startReloadBroadcaster()

	reloadScript := `<script>
if (typeof EventSource !== 'undefined') {
    const es = new EventSource('/reload');
    es.onmessage = () => location.reload();
}
</script>`

	mux := http.NewServeMux()
	mux.HandleFunc("/reload", func(w http.ResponseWriter, r *http.Request) {
		s.HandleSSE(w, r)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		slog.Debug("HTTP request", "method", r.Method, "path", r.URL.Path, "remote_addr", r.RemoteAddr)

		if strings.HasSuffix(r.URL.Path, "/") {
			r.URL.Path += "index.html"
		}

		fullPath := filepath.Join(absDir, r.URL.Path)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			slog.Warn("File not found", "path", r.URL.Path, "remote_addr", r.RemoteAddr)
			http.NotFound(w, r)
			return
		}

		if strings.HasSuffix(r.URL.Path, ".html") {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			data, err := os.ReadFile(fullPath)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			w.Write(data)
			if !strings.Contains(string(data), "</body>") {
				return
			}
			w.Write([]byte(reloadScript))
			return
		}

		http.ServeFile(w, r, fullPath)
	})

	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.Port),
		Handler: mux,
	}

	slog.Info("Server listening", "address", fmt.Sprintf("http://localhost:%d", s.Port), "dir", absDir)
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown() error {
	if s.httpServer == nil {
		return nil
	}
	return s.httpServer.Close()
}
