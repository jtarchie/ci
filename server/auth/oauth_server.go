package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/sessions"
)

// authCode represents an in-flight authorization code awaiting exchange.
type authCode struct {
	UserID        string
	User          *User
	CodeChallenge string
	RedirectURI   string
	Scopes        []string
	ExpiresAt     time.Time
}

// OAuthServer implements a minimal OAuth 2.0 Authorization Server for MCP
// clients, supporting authorization code flow with PKCE (S256).
type OAuthServer struct {
	cfg    *Config
	store  *sessions.CookieStore
	logger *slog.Logger

	mu    sync.Mutex
	codes map[string]*authCode
}

// NewOAuthServer creates an OAuthServer with the given config.
func NewOAuthServer(cfg *Config, store *sessions.CookieStore, logger *slog.Logger) *OAuthServer {
	return &OAuthServer{
		cfg:    cfg,
		store:  store,
		logger: logger,
		codes:  make(map[string]*authCode),
	}
}

// HandleAuthorize handles GET /oauth/authorize. If the user has an active
// session it issues an authorization code and redirects. Otherwise it saves
// the OAuth parameters in the session and redirects to the login page.
func (s *OAuthServer) HandleAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}

	q := r.URL.Query()
	responseType := q.Get("response_type")
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	state := q.Get("state")
	codeChallenge := q.Get("code_challenge")
	codeChallengeMethod := q.Get("code_challenge_method")

	// Validate required parameters.
	if responseType != "code" {
		jsonError(w, "unsupported_response_type", "response_type must be 'code'", http.StatusBadRequest)

		return
	}

	if clientID == "" {
		jsonError(w, "invalid_request", "client_id is required", http.StatusBadRequest)

		return
	}

	if redirectURI == "" {
		jsonError(w, "invalid_request", "redirect_uri is required", http.StatusBadRequest)

		return
	}

	if codeChallenge == "" || codeChallengeMethod != "S256" {
		jsonError(w, "invalid_request", "code_challenge with method S256 is required", http.StatusBadRequest)

		return
	}

	// Check if user already has a session.
	user := GetUserFromSession(r, s.store)
	if user != nil {
		s.issueCodeAndRedirect(w, user, redirectURI, state, codeChallenge)

		return
	}

	// No session — save OAuth params and redirect to login.
	session, err := s.store.Get(r, sessionName)
	if err != nil {
		s.logger.Error("oauth.authorize.session.error", "error", err)
		jsonError(w, "server_error", "session error", http.StatusInternalServerError)

		return
	}

	session.Values["oauth_redirect_uri"] = redirectURI
	session.Values["oauth_state"] = state
	session.Values["oauth_code_challenge"] = codeChallenge
	session.Values["oauth_client_id"] = clientID

	if err := session.Save(r, w); err != nil {
		s.logger.Error("oauth.authorize.session.save.error", "error", err)
		jsonError(w, "server_error", "session error", http.StatusInternalServerError)

		return
	}

	http.Redirect(w, r, "/auth/login?oauth=1", http.StatusFound)
}

// CompleteAuthorize is called after OAuth login to complete a pending
// authorization code flow. Returns true if there was a pending flow.
func (s *OAuthServer) CompleteAuthorize(w http.ResponseWriter, r *http.Request, user *User) bool {
	session, err := s.store.Get(r, sessionName)
	if err != nil {
		return false
	}

	redirectURI, ok := session.Values["oauth_redirect_uri"].(string)
	if !ok || redirectURI == "" {
		return false
	}

	state, _ := session.Values["oauth_state"].(string)
	codeChallenge, _ := session.Values["oauth_code_challenge"].(string)

	// Clear the pending OAuth params.
	delete(session.Values, "oauth_redirect_uri")
	delete(session.Values, "oauth_state")
	delete(session.Values, "oauth_code_challenge")
	delete(session.Values, "oauth_client_id")

	_ = session.Save(r, w)

	if codeChallenge == "" {
		return false
	}

	s.issueCodeAndRedirect(w, user, redirectURI, state, codeChallenge)

	return true
}

// HandleToken handles POST /oauth/token. Validates the authorization code
// and PKCE code_verifier, then issues a scoped JWT with ci:read.
func (s *OAuthServer) HandleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}

	if err := r.ParseForm(); err != nil {
		jsonError(w, "invalid_request", "could not parse form", http.StatusBadRequest)

		return
	}

	grantType := r.FormValue("grant_type")
	code := r.FormValue("code")
	codeVerifier := r.FormValue("code_verifier")
	redirectURI := r.FormValue("redirect_uri")

	if grantType != "authorization_code" {
		jsonError(w, "unsupported_grant_type", "grant_type must be 'authorization_code'", http.StatusBadRequest)

		return
	}

	if code == "" || codeVerifier == "" {
		jsonError(w, "invalid_request", "code and code_verifier are required", http.StatusBadRequest)

		return
	}

	// Look up and consume the authorization code.
	s.mu.Lock()
	ac, ok := s.codes[code]
	if ok {
		delete(s.codes, code)
	}
	s.mu.Unlock()

	if !ok {
		jsonError(w, "invalid_grant", "unknown or already consumed authorization code", http.StatusBadRequest)

		return
	}

	if time.Now().After(ac.ExpiresAt) {
		jsonError(w, "invalid_grant", "authorization code expired", http.StatusBadRequest)

		return
	}

	if redirectURI != ac.RedirectURI {
		jsonError(w, "invalid_grant", "redirect_uri mismatch", http.StatusBadRequest)

		return
	}

	// Verify PKCE: SHA256(code_verifier) must equal code_challenge (base64url).
	h := sha256.Sum256([]byte(codeVerifier))
	computed := base64.RawURLEncoding.EncodeToString(h[:])

	if computed != ac.CodeChallenge {
		s.logger.Warn("oauth.token.pkce.mismatch")
		jsonError(w, "invalid_grant", "code_verifier does not match code_challenge", http.StatusBadRequest)

		return
	}

	// Issue a scoped JWT (always ci:read).
	scopes := []string{MCPScope}
	ttl := 30 * 24 * time.Hour

	token, err := GenerateToken(ac.User, s.cfg.SessionSecret, ttl, scopes)
	if err != nil {
		s.logger.Error("oauth.token.generate.error", "error", err)
		jsonError(w, "server_error", "could not generate token", http.StatusInternalServerError)

		return
	}

	s.logger.Info("oauth.token.issued",
		"user_id", ac.UserID,
		"scopes", scopes,
	)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")

	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_in":   int(ttl.Seconds()),
		"scope":        MCPScope,
	})
}

func (s *OAuthServer) issueCodeAndRedirect(w http.ResponseWriter, user *User, redirectURI, state, codeChallenge string) {
	code, err := generateRandomCode()
	if err != nil {
		s.logger.Error("oauth.authorize.code.error", "error", err)
		jsonError(w, "server_error", "could not generate code", http.StatusInternalServerError)

		return
	}

	s.mu.Lock()
	s.codes[code] = &authCode{
		UserID:        user.UserID,
		User:          user,
		CodeChallenge: codeChallenge,
		RedirectURI:   redirectURI,
		Scopes:        []string{MCPScope},
		ExpiresAt:     time.Now().Add(10 * time.Minute),
	}
	s.mu.Unlock()

	// Clean up expired codes in background.
	go s.cleanupExpiredCodes()

	dest := redirectURI + "?code=" + code
	if state != "" {
		dest += "&state=" + state
	}

	s.logger.Info("oauth.authorize.success",
		"user_id", user.UserID,
	)

	w.Header().Set("Location", dest)
	w.WriteHeader(http.StatusFound)
}

func (s *OAuthServer) cleanupExpiredCodes() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for code, ac := range s.codes {
		if now.After(ac.ExpiresAt) {
			delete(s.codes, code)
		}
	}
}

func jsonError(w http.ResponseWriter, errCode, description string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":             errCode,
		"error_description": description,
	})
}
