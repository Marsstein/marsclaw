package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/marsstein/marsclaw/internal/agent"
	"github.com/marsstein/marsclaw/internal/channels"
	"github.com/marsstein/marsclaw/internal/llm"
	"github.com/marsstein/marsclaw/internal/security"
	"github.com/marsstein/marsclaw/internal/store"
	"github.com/marsstein/marsclaw/internal/tool"
	t "github.com/marsstein/marsclaw/internal/types"
)

// Config holds server configuration.
type Config struct {
	Addr     string
	Provider t.Provider
	Model    string
	Soul     string
	AgentCfg agent.AgentConfig
	Registry *tool.Registry
	Safety   *security.SafetyChecker
	Cost     t.CostRecorder
	Store    store.Store
	Logger   *slog.Logger
	Tasks    []TaskInfo // scheduler tasks for dashboard
}

// TaskInfo is a read-only view of a scheduled task for the API.
type TaskInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Channel  string `json:"channel"`
	Enabled  bool   `json:"enabled"`
}

// Server serves the HTTP API and Web UI.
type Server struct {
	cfg      Config
	store    store.Store
	logger   *slog.Logger
	mux      *http.ServeMux
}

// New creates a server.
func New(cfg Config) *Server {
	s := &Server{
		cfg:    cfg,
		store:  cfg.Store,
		logger: cfg.Logger,
		mux:    http.NewServeMux(),
	}
	if s.logger == nil {
		s.logger = slog.Default()
	}

	s.mux.HandleFunc("GET /", s.handleUI)
	s.mux.HandleFunc("POST /api/sessions", s.handleCreateSession)
	s.mux.HandleFunc("GET /api/sessions", s.handleListSessions)
	s.mux.HandleFunc("GET /api/sessions/{id}", s.handleGetSession)
	s.mux.HandleFunc("DELETE /api/sessions/{id}", s.handleDeleteSession)
	s.mux.HandleFunc("POST /api/sessions/{id}/messages", s.handleSendMessage)
	s.mux.HandleFunc("GET /api/config", s.handleGetConfig)
	s.mux.HandleFunc("GET /api/scheduler/tasks", s.handleListTasks)
	s.mux.HandleFunc("GET /api/channels", s.handleListChannels)
	s.mux.HandleFunc("DELETE /api/channels/{id}", s.handleDeleteChannel)

	return s
}

// Mount adds an external handler at the given pattern.
func (s *Server) Mount(pattern string, handler http.Handler) {
	s.mux.Handle(pattern, handler)
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe(ctx context.Context) error {
	srv := &http.Server{
		Addr:         s.cfg.Addr,
		Handler:      s.mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 5 * time.Minute, // long for streaming
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	s.logger.Info("server listening", "addr", s.cfg.Addr)
	return srv.ListenAndServe()
}

func (s *Server) newAgent() *agent.Agent {
	return agent.New(
		s.cfg.Provider,
		s.cfg.AgentCfg,
		s.cfg.Registry.Executors(),
		s.cfg.Registry.Defs(),
		agent.WithLogger(s.logger),
		agent.WithCostTracker(s.cfg.Cost),
		agent.WithSafety(s.cfg.Safety),
	)
}

func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	// Serve the UI at / and /app (for nginx reverse proxy).
	if r.URL.Path != "/" && r.URL.Path != "/app" && r.URL.Path != "/app/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, uiHTML)
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	id := fmt.Sprintf("sess_%d", time.Now().UnixNano())
	sess := &store.Session{
		ID:        id,
		Title:     "New conversation",
		Source:    "server",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := s.store.CreateSession(r.Context(), sess); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, sess)
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.store.ListSessions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sessions == nil {
		sessions = []*store.Session{}
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, err := s.store.GetSession(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sess == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	msgs, err := s.store.GetMessages(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"session":  sess,
		"messages": msgs,
	})
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.DeleteSession(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var body struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	sess, err := s.store.GetSession(r.Context(), id)
	if err != nil || sess == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	// Load history.
	history, _ := s.store.GetMessages(r.Context(), id)

	userMsg := t.Message{
		Role:      t.RoleUser,
		Content:   body.Message,
		Timestamp: time.Now(),
	}
	history = append(history, userMsg)

	// Set up streaming via SSE.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	streamFn := func(ev t.StreamEvent) {
		data, _ := json.Marshal(ev)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	a := agent.New(
		s.cfg.Provider,
		s.cfg.AgentCfg,
		s.cfg.Registry.Executors(),
		s.cfg.Registry.Defs(),
		agent.WithLogger(s.logger),
		agent.WithCostTracker(s.cfg.Cost),
		agent.WithSafety(s.cfg.Safety),
		agent.WithStreamHandler(streamFn),
	)

	parts := t.ContextParts{
		SoulPrompt: s.cfg.Soul,
		History:    history,
	}

	result := a.Run(r.Context(), parts)

	// Persist messages.
	newMsgs := []t.Message{userMsg}
	if len(result.History) > len(history) {
		newMsgs = result.History[len(history)-1:]
	}
	s.store.AppendMessages(r.Context(), id, newMsgs)

	// Auto-title on first message.
	if sess.Title == "New conversation" && len(body.Message) > 0 {
		title := body.Message
		if len(title) > 60 {
			title = title[:60] + "..."
		}
		s.store.UpdateTitle(r.Context(), id, title)
	}

	// Send cost info as final event.
	if s.cfg.Cost != nil {
		costLine := s.cfg.Cost.FormatCostLine(s.cfg.Model, result.TotalInput, result.TotalOutput)
		data, _ := json.Marshal(map[string]string{"cost": costLine})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"model": s.cfg.Model,
		"tasks": len(s.cfg.Tasks),
	})
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	tasks := s.cfg.Tasks
	if tasks == nil {
		tasks = []TaskInfo{}
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (s *Server) handleListChannels(w http.ResponseWriter, r *http.Request) {
	cs := channels.NewStore()
	list, err := cs.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []channels.Channel{}
	}

	// Mask tokens for security.
	safe := make([]map[string]any, len(list))
	for i, ch := range list {
		safe[i] = map[string]any{
			"id":       ch.ID,
			"provider": ch.Provider,
			"name":     ch.Name,
			"enabled":  ch.Enabled,
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"channels":  safe,
		"providers": channels.SupportedProviders,
	})
}

func (s *Server) handleDeleteChannel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cs := channels.NewStore()
	if err := cs.Remove(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// Ensure llm package is used (for provider creation in main.go).
var _ = llm.NewCostTracker
