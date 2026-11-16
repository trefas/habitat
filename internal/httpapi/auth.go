package httpapi

import (
	"context"
	"encoding/json"
	"net"
	"net/http"

	"habitat/internal/domain"
)

type AuthService interface {
	Register(ctx context.Context, email, password, displayName string) (*domain.User, error)
	Login(ctx context.Context, email, password string, meta domain.SessionMeta) (*domain.TokenPair, *domain.User, error)
	Refresh(ctx context.Context, rawRefresh string, meta domain.SessionMeta) (*domain.TokenPair, error)
	Logout(ctx context.Context, rawRefresh string) error
}

func RegisterHandler(svc AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email       string `json:"email"`
			Password    string `json:"password"`
			DisplayName string `json:"display_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "Некорректный JSON")
			return
		}

		u, err := svc.Register(r.Context(), req.Email, req.Password, req.DisplayName)
		if err != nil {
			mapDomainError(w, err)
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{
			"id":           u.ID,
			"email":        u.Email,
			"display_name": u.DisplayName,
			"role":         u.Role,
			"created_at":   u.CreatedAt,
		})
	}
}

func LoginHandler(svc AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "Некорректный JSON")
			return
		}

		meta := extractSessionMeta(r)
		tp, u, err := svc.Login(r.Context(), req.Email, req.Password, meta)
		if err != nil {
			mapDomainError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"access_token":  tp.AccessToken,
			"refresh_token": tp.RefreshToken,
			"token_type":    "Bearer",
			"expires_in":    tp.ExpiresIn,
			"user": map[string]any{
				"id":           u.ID,
				"email":        u.Email,
				"display_name": u.DisplayName,
				"role":         u.Role,
			},
		})
	}
}

func RefreshHandler(svc AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "Некорректный JSON")
			return
		}

		meta := extractSessionMeta(r)
		tp, err := svc.Refresh(r.Context(), req.RefreshToken, meta)
		if err != nil {
			mapDomainError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"access_token":  tp.AccessToken,
			"refresh_token": tp.RefreshToken,
			"token_type":    "Bearer",
			"expires_in":    tp.ExpiresIn,
		})
	}
}

func LogoutHandler(svc AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "Некорректный JSON")
			return
		}

		if err := svc.Logout(r.Context(), req.RefreshToken); err != nil {
			mapDomainError(w, err)
			return
		}

		w.WriteHeader(http.StatusNoContent) // 204 No Content
	}
}

func extractSessionMeta(r *http.Request) domain.SessionMeta {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ua := r.UserAgent()
	if len(ua) > 512 {
		ua = ua[:512]
	}
	return domain.SessionMeta{
		IP:        host,
		UserAgent: ua,
	}
}
