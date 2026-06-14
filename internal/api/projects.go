package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rinfra/rinfra/internal/domain"
)

// ---------- Project request types ----------

type projectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ClientName  string `json:"clientName"`
	LeadID      string `json:"leadId"`
}

type addMemberRequest struct {
	UserID string `json:"userId"`
}

// ---------- Project JSON ----------

func projectToJSON(p domain.Project) map[string]any {
	return map[string]any{
		"id":          p.ID,
		"name":        p.Name,
		"description": p.Description,
		"clientName":  p.ClientName,
		"leadId":      p.LeadID,
		"createdAt":   p.CreatedAt,
		"updatedAt":   p.UpdatedAt,
	}
}

func projectsToJSON(ps []domain.Project) []map[string]any {
	out := make([]map[string]any, 0, len(ps))
	for _, p := range ps {
		out = append(out, projectToJSON(p))
	}
	return out
}

func membersToJSON(ms []domain.ProjectMember) []map[string]any {
	out := make([]map[string]any, 0, len(ms))
	for _, m := range ms {
		out = append(out, map[string]any{
			"projectId": m.ProjectID,
			"userId":    m.UserID,
			"addedAt":   m.AddedAt,
		})
	}
	return out
}

// projectsEnabled guards handlers that require the project subsystem.
func (h *handlers) projectsEnabled(w http.ResponseWriter) bool {
	if h.svc.Projects == nil {
		writeErrorCode(w, http.StatusNotImplemented, "not_implemented", "projects are not enabled")
		return false
	}
	return true
}

// ---------- Handlers ----------

func (h *handlers) listProjects(w http.ResponseWriter, r *http.Request) {
	if !h.projectsEnabled(w) {
		return
	}
	ps, err := h.svc.Projects.List(r.Context(), actorUser(r.Context()))
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": projectsToJSON(ps)})
}

func (h *handlers) createProject(w http.ResponseWriter, r *http.Request) {
	if !h.projectsEnabled(w) {
		return
	}
	var req projectRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	p := domain.Project{
		Name:        req.Name,
		Description: req.Description,
		ClientName:  req.ClientName,
		LeadID:      req.LeadID,
	}
	created, err := h.svc.Projects.Create(r.Context(), actorUser(r.Context()), p)
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"project": projectToJSON(created)})
}

func (h *handlers) getProject(w http.ResponseWriter, r *http.Request) {
	if !h.projectsEnabled(w) {
		return
	}
	id := chi.URLParam(r, "id")
	p, err := h.svc.Projects.Get(r.Context(), actorUser(r.Context()), id)
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"project": projectToJSON(p)})
}

func (h *handlers) patchProject(w http.ResponseWriter, r *http.Request) {
	if !h.projectsEnabled(w) {
		return
	}
	id := chi.URLParam(r, "id")
	var req projectRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	p := domain.Project{
		Name:        req.Name,
		Description: req.Description,
		ClientName:  req.ClientName,
		LeadID:      req.LeadID,
	}
	updated, err := h.svc.Projects.Update(r.Context(), actorUser(r.Context()), id, p)
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"project": projectToJSON(updated)})
}

func (h *handlers) deleteProject(w http.ResponseWriter, r *http.Request) {
	if !h.projectsEnabled(w) {
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.svc.Projects.Delete(r.Context(), actorUser(r.Context()), id); err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

func (h *handlers) listProjectMembers(w http.ResponseWriter, r *http.Request) {
	if !h.projectsEnabled(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ms, err := h.svc.Projects.ListMembers(r.Context(), actorUser(r.Context()), id)
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": membersToJSON(ms)})
}

func (h *handlers) addProjectMember(w http.ResponseWriter, r *http.Request) {
	if !h.projectsEnabled(w) {
		return
	}
	id := chi.URLParam(r, "id")
	var req addMemberRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.UserID == "" {
		writeErrorCode(w, http.StatusBadRequest, "invalid_request", "userId is required")
		return
	}
	if err := h.svc.Projects.AddMember(r.Context(), actorUser(r.Context()), id, req.UserID); err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

func (h *handlers) removeProjectMember(w http.ResponseWriter, r *http.Request) {
	if !h.projectsEnabled(w) {
		return
	}
	id := chi.URLParam(r, "id")
	userID := chi.URLParam(r, "userId")
	if err := h.svc.Projects.RemoveMember(r.Context(), actorUser(r.Context()), id, userID); err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

func (h *handlers) listProjectEngagements(w http.ResponseWriter, r *http.Request) {
	if !h.projectsEnabled(w) {
		return
	}
	id := chi.URLParam(r, "id")
	// Access check via the project service.
	ok, err := h.svc.Projects.CanAccessProject(r.Context(), actorUser(r.Context()), id)
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	if !ok {
		writeErrorCode(w, http.StatusForbidden, "forbidden", "cannot access project")
		return
	}
	engs, err := h.svc.Engagement.ListForProject(r.Context(), id)
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"engagements": engagementsToJSON(engs)})
}
