package codex

import (
	"bufio"
	"bytes"
	"codex2api/store"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Defaults — all overridable via DB config or env vars.
const (
	DefaultCodexBase    = "https://chatgpt.com/backend-api/codex"
	DefaultRefreshURL   = "https://auth.openai.com/oauth/token"
	DefaultClientID     = "app_EMoamEEZ73f0CkXa**hr***"
	DefaultModel        = "gpt-5.2"
	RefreshHours        = 8
)

// Config holds runtime-resolved configuration.
type Config struct {
	CodexBase  string
	RefreshURL string
	ClientID   string
	Model      string
}

// LoadConfig resolves config from DB, falling back to env vars, then defaults.
func LoadConfig(db *store.Store) Config {
	get := func(key, envKey, def string) string {
		if v := db.GetConfig(key, ""); v != "" {
			return v
		}
		if v := os.Getenv(envKey); v != "" {
			return v
		}
		return def
	}

	// models_list first item takes priority as default model
	model := ""
	if list := db.GetConfig("models_list", ""); list != "" {
		if first := strings.SplitN(list, "|", 2)[0]; first != "" {
			model = first
		}
	}
	if model == "" {
		model = get("default_model", "CODEX_DEFAULT_MODEL", DefaultModel)
	}

	return Config{
		CodexBase:  get("codex_base", "CODEX_BASE", DefaultCodexBase),
		RefreshURL: get("refresh_url", "CODEX_REFRESH_URL", DefaultRefreshURL),
		ClientID:   get("client_id", "CODEX_CLIENT_ID", DefaultClientID),
		Model:      model,
	}
}

// NewHTTPClient creates an http.Client that respects HTTP_PROXY / HTTPS_PROXY.
func NewHTTPClient() *http.Client {
	transport := &http.Transport{}
	for _, env := range []string{"HTTPS_PROXY", "HTTP_PROXY", "https_proxy", "http_proxy"} {
		if proxy := os.Getenv(env); proxy != "" {
			if u, err := url.Parse(proxy); err == nil {
				transport.Proxy = http.ProxyURL(u)
				break
			}
		}
	}
	return &http.Client{Transport: transport, Timeout: 120 * time.Second}
}

// IsStale returns true if the token is older than RefreshHours.
func IsStale(t *store.TokenData) bool {
	last, err := time.Parse(time.RFC3339Nano, t.LastRefresh)
	if err != nil {
		return true
	}
	return time.Since(last) > RefreshHours*time.Hour
}

// Refresh exchanges the refresh token for a new access token.
func Refresh(client *http.Client, cfg Config, t *store.TokenData) (*store.TokenData, error) {
	body, _ := json.Marshal(map[string]string{
		"client_id":     cfg.ClientID,
		"grant_type":    "refresh_token",
		"refresh_token": t.RefreshToken,
	})
	resp, err := client.Post(cfg.RefreshURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("refresh failed %d: %s", resp.StatusCode, b)
	}
	var r struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	newT := *t
	if r.AccessToken != "" {
		newT.AccessToken = r.AccessToken
	}
	if r.RefreshToken != "" {
		newT.RefreshToken = r.RefreshToken
	}
	newT.LastRefresh = time.Now().UTC().Format(time.RFC3339Nano)
	return &newT, nil
}

// CodexRequest is the body sent to the Codex responses endpoint.
type CodexRequest struct {
	Model        string      `json:"model"`
	Instructions string      `json:"instructions"`
	Input        []InputItem `json:"input"`
	Stream       bool        `json:"stream"`
	Store        bool        `json:"store"`
	Temperature  *float64    `json:"temperature,omitempty"`
}

type InputItem struct {
	Type    string        `json:"type"`
	Role    string        `json:"role"`
	Content []ContentPart `json:"content"`
}

type ContentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Do sends a request to the Codex API.
func Do(client *http.Client, cfg Config, tokens *store.TokenData, req *CodexRequest) (*http.Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	endpoint := strings.TrimRight(cfg.CodexBase, "/") + "/responses"
	httpReq, err := http.NewRequest("POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	accept := "application/json"
	if req.Stream {
		accept = "text/event-stream"
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", accept)
	httpReq.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	httpReq.Header.Set("ChatGPT-Account-ID", tokens.AccountID)
	httpReq.Header.Set("User-Agent", "OpenAI/codex")

	return client.Do(httpReq)
}

// SSEEvent is a parsed server-sent event.
type SSEEvent struct {
	Event string
	Data  string
}

// ReadSSE reads SSE lines and sends events to ch, then closes it.
func ReadSSE(r io.Reader, ch chan<- SSEEvent) {
	defer close(ch)
	scanner := bufio.NewScanner(r)
	var event, data string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if data != "" {
				ch <- SSEEvent{Event: event, Data: data}
			}
			event, data = "", ""
			continue
		}
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(line[6:])
		} else if strings.HasPrefix(line, "data:") {
			data = strings.TrimSpace(line[5:])
		}
	}
}
