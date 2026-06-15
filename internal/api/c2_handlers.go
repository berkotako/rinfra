package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// c2ManualAccess returns how to connect a native operator client to the
// engagement's deployed C2 teamserver by hand (manual-access usage mode).
func (h *handlers) c2ManualAccess(w http.ResponseWriter, r *http.Request) {
	if h.svc.C2 == nil {
		writeErrorCode(w, http.StatusNotImplemented, "not_implemented", "manual access is not configured")
		return
	}
	id := chi.URLParam(r, "id")
	view, err := h.svc.C2.ManualAccess(r.Context(), id, actorFrom(r.Context()))
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// c2ListTeamservers returns a manual-access descriptor for every live C2 server
// node in the engagement (the Alive C2s view).
func (h *handlers) c2ListTeamservers(w http.ResponseWriter, r *http.Request) {
	if h.svc.C2 == nil {
		writeErrorCode(w, http.StatusNotImplemented, "not_implemented", "manual access is not configured")
		return
	}
	id := chi.URLParam(r, "id")
	views, err := h.svc.C2.ListTeamservers(r.Context(), id, actorFrom(r.Context()))
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"teamservers": views})
}

// c2OpenTunnel opens an SSH local port-forward to the teamserver operator port.
func (h *handlers) c2OpenTunnel(w http.ResponseWriter, r *http.Request) {
	if h.svc.C2 == nil {
		writeErrorCode(w, http.StatusNotImplemented, "not_implemented", "manual access is not configured")
		return
	}
	id := chi.URLParam(r, "id")
	view, err := h.svc.C2.OpenTunnel(r.Context(), id, actorUser(r.Context()))
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// c2ListTunnels returns the engagement's active tunnels for reconcile/audit.
func (h *handlers) c2ListTunnels(w http.ResponseWriter, r *http.Request) {
	if h.svc.C2 == nil {
		writeErrorCode(w, http.StatusNotImplemented, "not_implemented", "manual access is not configured")
		return
	}
	id := chi.URLParam(r, "id")
	writeJSON(w, http.StatusOK, map[string]any{"tunnels": h.svc.C2.ListTunnels(r.Context(), id)})
}

// c2CloseTunnel tears down a previously opened tunnel.
func (h *handlers) c2CloseTunnel(w http.ResponseWriter, r *http.Request) {
	if h.svc.C2 == nil {
		writeErrorCode(w, http.StatusNotImplemented, "not_implemented", "manual access is not configured")
		return
	}
	id := chi.URLParam(r, "id")
	tunnelID := chi.URLParam(r, "tunnelId")
	if err := h.svc.C2.CloseTunnel(r.Context(), id, tunnelID, actorUser(r.Context())); err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"closed": true})
}
