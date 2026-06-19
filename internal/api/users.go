package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/service"
)

// ---------- User request types ----------

type createUserRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
	Role        string `json:"role"`
	Password    string `json:"password"`
}

type patchUserRequest struct {
	DisplayName *string `json:"displayName"`
	Email       *string `json:"email"`
	Role        *string `json:"role"`
	Disabled    *bool   `json:"disabled"`
	ManagerID   *string `json:"managerId"`
}

type changePasswordRequest struct {
	NewPassword     string `json:"newPassword"`
	CurrentPassword string `json:"currentPassword"`
}

// ---------- Handlers ----------

func (h *handlers) listUsers(w http.ResponseWriter, r *http.Request) {
	if h.svc.Auth == nil {
		writeErrorCode(w, http.StatusNotImplemented, "not_implemented", "user management is not enabled")
		return
	}
	users, err := h.svc.Auth.ListUsers(r.Context(), actorUser(r.Context()))
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": usersToJSON(users)})
}

func (h *handlers) createUser(w http.ResponseWriter, r *http.Request) {
	if h.svc.Auth == nil {
		writeErrorCode(w, http.StatusNotImplemented, "not_implemented", "user management is not enabled")
		return
	}
	var req createUserRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	newUser := domain.User{
		Username:    req.Username,
		DisplayName: req.DisplayName,
		Email:       req.Email,
		Role:        domain.Role(req.Role),
	}
	created, err := h.svc.Auth.CreateUser(r.Context(), actorUser(r.Context()), newUser, req.Password)
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user": userToJSON(created)})
}

func (h *handlers) getUser(w http.ResponseWriter, r *http.Request) {
	if h.svc.Auth == nil {
		writeErrorCode(w, http.StatusNotImplemented, "not_implemented", "user management is not enabled")
		return
	}
	id := chi.URLParam(r, "id")
	u, err := h.svc.Auth.GetUser(r.Context(), actorUser(r.Context()), id)
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": userToJSON(u)})
}

func (h *handlers) patchUser(w http.ResponseWriter, r *http.Request) {
	if h.svc.Auth == nil {
		writeErrorCode(w, http.StatusNotImplemented, "not_implemented", "user management is not enabled")
		return
	}
	id := chi.URLParam(r, "id")
	var req patchUserRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	upd := service.UserUpdate{
		DisplayName: req.DisplayName,
		Email:       req.Email,
		Disabled:    req.Disabled,
		ManagerID:   req.ManagerID,
	}
	if req.Role != nil {
		role := domain.Role(*req.Role)
		upd.Role = &role
	}
	u, err := h.svc.Auth.UpdateUser(r.Context(), actorUser(r.Context()), id, upd)
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": userToJSON(u)})
}

func (h *handlers) changePassword(w http.ResponseWriter, r *http.Request) {
	if h.svc.Auth == nil {
		writeErrorCode(w, http.StatusNotImplemented, "not_implemented", "user management is not enabled")
		return
	}
	id := chi.URLParam(r, "id")
	var req changePasswordRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.svc.Auth.ChangePassword(r.Context(), actorUser(r.Context()), id, req.CurrentPassword, req.NewPassword); err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}
