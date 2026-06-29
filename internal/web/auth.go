package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tylersriver/mend/internal/web/view"
)

const (
	sessionCookie = "mend_session"
	sessionTTL    = 30 * 24 * time.Hour
)

// sessionSecret keys the cookie HMAC. An explicit SESSION_SECRET is preferred;
// otherwise we derive a stable secret from the password so sessions survive
// restarts (they invalidate if the password changes).
func (s *Server) sessionSecret() []byte {
	if s.env.SessionSecret != "" {
		sum := sha256.Sum256([]byte("mend:" + s.env.SessionSecret))
		return sum[:]
	}
	sum := sha256.Sum256([]byte("mend-session:" + s.env.AuthPassword))
	return sum[:]
}

func (s *Server) sign(payload string) string {
	m := hmac.New(sha256.New, s.sessionSecret())
	m.Write([]byte(payload))
	return hex.EncodeToString(m.Sum(nil))
}

func (s *Server) issueSession(w http.ResponseWriter, r *http.Request) {
	exp := time.Now().Add(sessionTTL).Unix()
	payload := strconv.FormatInt(exp, 10)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    payload + "." + s.sign(payload),
		Path:     "/",
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(exp, 0),
		MaxAge:   int(sessionTTL / time.Second),
	})
}

func (s *Server) clearSession(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func (s *Server) validSession(r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	payload, sig, ok := strings.Cut(c.Value, ".")
	if !ok {
		return false
	}
	if !hmac.Equal([]byte(sig), []byte(s.sign(payload))) {
		return false
	}
	exp, err := strconv.ParseInt(payload, 10, 64)
	if err != nil {
		return false
	}
	return time.Now().Unix() < exp
}

// requireAuth gates everything except the public allowlist when auth is enabled.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.env.AuthEnabled() || isPublicPath(r.URL.Path) || s.validSession(r) {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodGet {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
		} else {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		}
	})
}

// isPublicPath lists routes reachable without a session: the login flow, health,
// and the PWA shell assets (so an install/offline page works pre-login).
func isPublicPath(p string) bool {
	switch p {
	case "/login", "/logout", "/healthz", "/sw.js", "/manifest.webmanifest",
		"/offline", "/apple-touch-icon.png", "/favicon.ico":
		return true
	}
	return strings.HasPrefix(p, "/static/")
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	if !s.env.AuthEnabled() || s.validSession(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.render(w, r, view.LoginPage(false))
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.env.AuthEnabled() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	pw := r.FormValue("password")
	if subtle.ConstantTimeCompare([]byte(pw), []byte(s.env.AuthPassword)) == 1 {
		s.issueSession(w, r)
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	w.WriteHeader(http.StatusUnauthorized)
	s.render(w, r, view.LoginPage(true))
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.clearSession(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// isHTTPS reports whether the original request was secure, honoring the proxy
// header Railway (and most platforms) set when terminating TLS upstream.
func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
