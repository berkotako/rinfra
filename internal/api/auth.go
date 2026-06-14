package api

import (
	"net/http"

	"github.com/rinfra/rinfra/internal/domain"
)

// ---------- Auth request types ----------

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// ---------- User JSON (never includes the password hash) ----------

func userToJSON(u domain.User) map[string]any {
	return map[string]any{
		"id":          u.ID,
		"username":    u.Username,
		"displayName": u.DisplayName,
		"email":       u.Email,
		"role":        string(u.Role),
		"managerId":   u.ManagerID,
		"disabled":    u.Disabled,
		"createdAt":   u.CreatedAt,
		"updatedAt":   u.UpdatedAt,
	}
}

func usersToJSON(us []domain.User) []map[string]any {
	out := make([]map[string]any, 0, len(us))
	for _, u := range us {
		out = append(out, userToJSON(u))
	}
	return out
}

// ---------- Handlers ----------

func (h *handlers) login(w http.ResponseWriter, r *http.Request) {
	if h.svc.Auth == nil {
		writeErrorCode(w, http.StatusNotImplemented, "not_implemented", "authentication is not enabled")
		return
	}
	var req loginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	token, user, err := h.svc.Auth.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token": token,
		"user":  userToJSON(user),
	})
}

func (h *handlers) logout(w http.ResponseWriter, r *http.Request) {
	if h.svc.Auth != nil {
		_ = h.svc.Auth.Logout(r.Context(), bearerToken(r))
	}
	writeJSON(w, http.StatusNoContent, nil)
}

func (h *handlers) me(w http.ResponseWriter, r *http.Request) {
	u := actorUser(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"user": userToJSON(u)})
}
