package handler

import (
	"codex2api/codex"
	"codex2api/store"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ── Session store ─────────────────────────────────────────────────────────────

var (
	sessions   = map[string]time.Time{}
	sessionsMu sync.Mutex
)

const sessionTTL = 24 * time.Hour
const sessionCookie = "admin_session"

func newSession() string {
	b := make([]byte, 16)
	rand.Read(b)
	token := hex.EncodeToString(b)
	sessionsMu.Lock()
	sessions[token] = time.Now().Add(sessionTTL)
	sessionsMu.Unlock()
	return token
}

func validSession(token string) bool {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	exp, ok := sessions[token]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(sessions, token)
		return false
	}
	return true
}

func deleteSession(token string) {
	sessionsMu.Lock()
	delete(sessions, token)
	sessionsMu.Unlock()
}

func (h *Handler) sessionToken(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

func (h *Handler) isLoggedIn(r *http.Request) bool {
	return validSession(h.sessionToken(r))
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func (h *Handler) adminPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	w.Write([]byte(adminHTML))
}

func (h *Handler) adminAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/admin")

	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonResp(w, 400, map[string]string{"error": "Invalid JSON"})
		return
	}

	// Public endpoints (no session required)
	switch path {
	case "/init-password":
		pw, _ := body["password"].(string)
		if len(pw) < 8 {
			jsonResp(w, 400, map[string]string{"error": "密码至少 8 位"})
			return
		}
		if h.db.HasAdminPassword() {
			jsonResp(w, 400, map[string]string{"error": "密码已设置，请使用登录后的改密功能"})
			return
		}
		if err := h.db.SetAdminPassword(pw); err != nil {
			jsonResp(w, 500, map[string]string{"error": err.Error()})
			return
		}
		jsonResp(w, 200, map[string]string{"message": "密码设置成功"})
		return

	case "/login":
		pw, _ := body["password"].(string)
		if !h.db.CheckAdminPassword(pw) {
			jsonResp(w, 401, map[string]string{"error": "密码错误"})
			return
		}
		token := newSession()
		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookie,
			Value:    token,
			Path:     "/admin",
			MaxAge:   int(sessionTTL.Seconds()),
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
		jsonResp(w, 200, map[string]string{"message": "登录成功"})
		return

	case "/logout":
		deleteSession(h.sessionToken(r))
		http.SetCookie(w, &http.Cookie{
			Name:   sessionCookie,
			Value:  "",
			Path:   "/admin",
			MaxAge: -1,
		})
		jsonResp(w, 200, map[string]string{"message": "已退出"})
		return

	case "/check-session":
		jsonResp(w, 200, map[string]bool{"logged_in": h.isLoggedIn(r)})
		return
	}

	// All other endpoints require a valid session
	if !h.isLoggedIn(r) {
		jsonResp(w, 401, map[string]string{"error": "未登录"})
		return
	}

	switch path {
	case "/upload-auth":
		auth, _ := body["auth"].(map[string]any)
		if auth == nil {
			jsonResp(w, 400, map[string]string{"error": "缺少 auth 字段"})
			return
		}
		tokens, _ := auth["tokens"].(map[string]any)
		if tokens == nil {
			jsonResp(w, 400, map[string]string{"error": "auth.json 缺少 tokens 字段"})
			return
		}
		at, _ := tokens["access_token"].(string)
		rt, _ := tokens["refresh_token"].(string)
		aid, _ := tokens["account_id"].(string)
		if at == "" || rt == "" {
			jsonResp(w, 400, map[string]string{"error": "tokens 缺少 access_token 或 refresh_token"})
			return
		}
		t := &store.TokenData{
			AccessToken:  at,
			RefreshToken: rt,
			AccountID:    aid,
			LastRefresh:  "2000-01-01T00:00:00Z",
		}
		if err := h.db.SetTokens(t); err != nil {
			jsonResp(w, 500, map[string]string{"error": err.Error()})
			return
		}
		jsonResp(w, 200, map[string]string{"message": "auth.json 上传成功"})

	case "/generate-key":
		b := make([]byte, 16)
		rand.Read(b)
		key := "sk-codex-" + hex.EncodeToString(b)
		if err := h.db.AddAPIKey(key); err != nil {
			jsonResp(w, 500, map[string]string{"error": err.Error()})
			return
		}
		jsonResp(w, 200, map[string]string{"key": key})

	case "/list-keys":
		jsonResp(w, 200, map[string]any{"keys": h.db.ListAPIKeys()})

	case "/delete-key":
		key, _ := body["key"].(string)
		if err := h.db.DeleteAPIKey(key); err != nil {
			jsonResp(w, 500, map[string]string{"error": err.Error()})
			return
		}
		jsonResp(w, 200, map[string]string{"message": "已删除"})

	case "/get-config":
		cfg := codex.LoadConfig(h.db)
		jsonResp(w, 200, map[string]any{
			"codex_base":         cfg.CodexBase,
			"refresh_url":        cfg.RefreshURL,
			"client_id":          cfg.ClientID,
			"default_model":      cfg.Model,
			"models_list":        h.db.GetConfig("models_list", ""),
			"local_auth_enabled": h.db.GetConfig("local_auth_enabled", "false") == "true",
		})

	case "/set-config":
		fields := map[string]string{
			"codex_base":    "codex_base",
			"refresh_url":   "refresh_url",
			"client_id":     "client_id",
			"default_model": "default_model",
			"models_list":   "models_list",
		}
		for jsonKey, dbKey := range fields {
			if v, ok := body[jsonKey].(string); ok {
				if v != "" {
					h.db.SetConfig(dbKey, v)
				} else if dbKey == "models_list" {
					h.db.SetConfig(dbKey, "")
				}
			}
		}
		if v, ok := body["local_auth_enabled"].(bool); ok {
			val := "false"
			if v {
				val = "true"
			}
			h.db.SetConfig("local_auth_enabled", val)
		}
		jsonResp(w, 200, map[string]string{"message": "配置已保存"})

	case "/change-password":
		oldPw, _ := body["old_password"].(string)
		newPw, _ := body["new_password"].(string)
		if !h.db.CheckAdminPassword(oldPw) {
			jsonResp(w, 401, map[string]string{"error": "旧密码错误"})
			return
		}
		if len(newPw) < 8 {
			jsonResp(w, 400, map[string]string{"error": "新密码至少 8 位"})
			return
		}
		if err := h.db.SetAdminPassword(newPw); err != nil {
			jsonResp(w, 500, map[string]string{"error": err.Error()})
			return
		}
		jsonResp(w, 200, map[string]string{"message": "密码已更新"})

	default:
		http.NotFound(w, r)
	}
}
