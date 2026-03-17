package handler

import (
	"codex2api/codex"
	"codex2api/store"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Handler struct {
	db     *store.Store
	client *http.Client
}

func New(db *store.Store) *Handler {
	return &Handler{db: db, client: codex.NewHTTPClient()}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/v1/messages", h.messages)
	mux.HandleFunc("/v1/chat/completions", h.chatCompletions)
	mux.HandleFunc("/v1/models", h.models)
	mux.HandleFunc("/v1/health", h.health)
	mux.HandleFunc("/health", h.health)
	mux.HandleFunc("/admin", h.adminPage)
	mux.HandleFunc("/admin/", h.adminAPI)
}

// ── helpers ───────────────────────────────────────────────────────────────────

// resolveModel always uses the configured default Codex model,
// ignoring the requested model name (which may be a Claude or unsupported model).
func resolveModel(reqModel, cfgModel string) string {
	return cfgModel
}

func jsonResp(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func errResp(w http.ResponseWriter, status int, errType, msg string) {
	jsonResp(w, status, map[string]any{
		"error": map[string]string{"type": errType, "message": msg},
	})
}

func (h *Handler) apiKey(r *http.Request) string {
	key := r.Header.Get("x-api-key")
	if key == "" {
		auth := r.Header.Get("Authorization")
		key = strings.TrimPrefix(strings.TrimPrefix(auth, "Bearer "), "bearer ")
	}
	return strings.TrimSpace(key)
}

// localAuthEnabled returns true if the admin has enabled reading ~/.codex/auth.json.
func (h *Handler) localAuthEnabled() bool {
	return h.db.GetConfig("local_auth_enabled", "false") == "true"
}

// tryLoadLocalAuth reads ~/.codex/auth.json and stores the tokens if local auth is enabled.
func (h *Handler) tryLoadLocalAuth() *store.TokenData {
	if !h.localAuthEnabled() {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	if err != nil {
		return nil
	}
	var f struct {
		Tokens *struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			AccountID    string `json:"account_id"`
		} `json:"tokens"`
		LastRefresh string `json:"last_refresh"`
	}
	if json.Unmarshal(b, &f) != nil || f.Tokens == nil {
		return nil
	}
	lr := f.LastRefresh
	if lr == "" {
		lr = "2000-01-01T00:00:00Z"
	}
	return &store.TokenData{
		AccessToken:  f.Tokens.AccessToken,
		RefreshToken: f.Tokens.RefreshToken,
		AccountID:    f.Tokens.AccountID,
		LastRefresh:  lr,
	}
}

func (h *Handler) getFreshTokens(cfg codex.Config) (*store.TokenData, error) {
	t := h.db.GetTokens()
	if t == nil {
		// Fall back to local auth.json if enabled
		t = h.tryLoadLocalAuth()
		if t == nil {
			return nil, &apiError{"No tokens configured. Upload auth.json via /admin or enable local auth."}
		}
	}
	if codex.IsStale(t) {
		newT, err := codex.Refresh(h.client, cfg, t)
		if err != nil {
			return t, nil // best effort: use stale token
		}
		_ = h.db.SetTokens(newT)
		return newT, nil
	}
	return t, nil
}

type apiError struct{ msg string }

func (e *apiError) Error() string { return e.msg }

// ── shared: call codex and get SSE stream ─────────────────────────────────────

// doCodexStream sends req to Codex (always stream=true) and returns the response.
// Caller must close resp.Body.
func (h *Handler) doCodexStream(cfg codex.Config, tokens *store.TokenData, codexReq *codex.CodexRequest) (*http.Response, error) {
	codexReq.Stream = true
	resp, err := codex.Do(h.client, cfg, tokens, codexReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == 401 {
		newT, rerr := codex.Refresh(h.client, cfg, tokens)
		if rerr == nil {
			_ = h.db.SetTokens(newT)
			resp.Body.Close()
			resp, err = codex.Do(h.client, cfg, newT, codexReq)
			if err != nil {
				return nil, err
			}
		}
	}
	return resp, nil
}

// ── POST /v1/messages ─────────────────────────────────────────────────────────

func (h *Handler) messages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}

	if !h.db.ValidateAPIKey(h.apiKey(r)) {
		errResp(w, 401, "authentication_error", "Invalid API key")
		return
	}

	var req codex.AnthropicRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errResp(w, 400, "invalid_request_error", "Invalid JSON")
		return
	}

	cfg := codex.LoadConfig(h.db)
	tokens, err := h.getFreshTokens(cfg)
	if err != nil {
		errResp(w, 503, "authentication_error", err.Error())
		return
	}

	model := resolveModel(req.Model, cfg.Model)

	codexReq := codex.BuildCodexRequest(&req, model)
	resp, err := h.doCodexStream(cfg, tokens, codexReq)
	if err != nil {
		errResp(w, 502, "api_error", err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		errResp(w, resp.StatusCode, "api_error", "Codex "+resp.Status+": "+string(body))
		return
	}

	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(200)

		flusher, _ := w.(http.Flusher)
		flush := func() {
			if flusher != nil {
				flusher.Flush()
			}
		}

		writer := codex.NewAnthropicSSEWriter(w.Write, flush, model)
		ch := make(chan codex.SSEEvent, 32)
		go codex.ReadSSE(resp.Body, ch)
		for ev := range ch {
			if ev.Data != "" && ev.Data != "[DONE]" {
				writer.HandleChunk(ev.Data)
			}
		}
		writer.Finish()
		return
	}

	// Non-streaming: collect SSE and assemble response
	translated, err := codex.CollectSSEToAnthropic(resp.Body, model)
	if err != nil {
		errResp(w, 502, "api_error", err.Error())
		return
	}
	jsonResp(w, 200, translated)
}

// ── POST /v1/chat/completions (OpenAI-compatible) ─────────────────────────────

func (h *Handler) chatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}

	if !h.db.ValidateAPIKey(h.apiKey(r)) {
		errResp(w, 401, "authentication_error", "Invalid API key")
		return
	}

	var req codex.OpenAIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errResp(w, 400, "invalid_request_error", "Invalid JSON")
		return
	}

	cfg := codex.LoadConfig(h.db)
	tokens, err := h.getFreshTokens(cfg)
	if err != nil {
		errResp(w, 503, "authentication_error", err.Error())
		return
	}

	model := resolveModel(req.Model, cfg.Model)

	codexReq := codex.BuildCodexRequestFromOpenAI(&req, model)
	resp, err := h.doCodexStream(cfg, tokens, codexReq)
	if err != nil {
		errResp(w, 502, "api_error", err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		errResp(w, resp.StatusCode, "api_error", "Codex "+resp.Status+": "+string(body))
		return
	}

	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(200)

		flusher, _ := w.(http.Flusher)
		flush := func() {
			if flusher != nil {
				flusher.Flush()
			}
		}

		writer := codex.NewOpenAISSEWriter(w.Write, flush, model)
		ch := make(chan codex.SSEEvent, 32)
		go codex.ReadSSE(resp.Body, ch)
		for ev := range ch {
			if ev.Data != "" && ev.Data != "[DONE]" {
				writer.HandleChunk(ev.Data)
			}
		}
		writer.Finish()
		return
	}

	translated, err := codex.CollectSSEToOpenAI(resp.Body, model)
	if err != nil {
		errResp(w, 502, "api_error", err.Error())
		return
	}
	jsonResp(w, 200, translated)
}

// ── GET /v1/models ────────────────────────────────────────────────────────────

func (h *Handler) modelList() []string {
	if list := h.db.GetConfig("models_list", ""); list != "" {
		var result []string
		for _, m := range strings.Split(list, "|") {
			if m = strings.TrimSpace(m); m != "" {
				result = append(result, m)
			}
		}
		if len(result) > 0 {
			return result
		}
	}
	return []string{
		"claude-opus-4-6",
		"claude-sonnet-4-6",
		"claude-haiku-4-5",
		"claude-3-5-sonnet-20241022",
		"claude-3-5-haiku-20241022",
		"claude-3-opus-20240229",
	}
}

func (h *Handler) models(w http.ResponseWriter, r *http.Request) {
	now := time.Now().Unix()
	var data []map[string]any
	for _, id := range h.modelList() {
		data = append(data, map[string]any{
			"id": id, "object": "model",
			"created": now, "owned_by": "codex-proxy",
		})
	}
	jsonResp(w, 200, map[string]any{"object": "list", "data": data})
}

// ── GET /health ───────────────────────────────────────────────────────────────

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	jsonResp(w, 200, map[string]any{
		"status":            "ok",
		"tokens_configured": h.db.GetTokens() != nil,
		"local_auth":        h.localAuthEnabled(),
	})
}
