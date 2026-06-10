package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/service"
)

// handlers holds all HTTP handler methods.
type handlers struct {
	svc Services
	log *slog.Logger
}

// ---------- Engagement handlers ----------

func (h *handlers) listEngagements(w http.ResponseWriter, r *http.Request) {
	engs, err := h.svc.Engagement.List(r.Context())
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"engagements": engagementsToJSON(engs)})
}

func (h *handlers) createEngagement(w http.ResponseWriter, r *http.Request) {
	var req createEngagementRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	e := req.toDomain()
	created, err := h.svc.Engagement.Create(r.Context(), e, actorFrom(r.Context()))
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"engagement": engagementToJSON(created)})
}

func (h *handlers) getEngagement(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	eng, err := h.svc.Engagement.Get(r.Context(), id)
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"engagement": engagementToJSON(eng)})
}

func (h *handlers) patchEngagement(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req patchEngagementRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	actor := actorFrom(r.Context())

	if req.Status != "" {
		if err := h.svc.Engagement.UpdateStatus(r.Context(), id, domain.EngagementStatus(req.Status), actor); err != nil {
			writeError(w, h.log, err)
			return
		}
	}

	if req.Authorization != nil {
		auth := domain.Authorization{
			AuthorizedBy: req.Authorization.AuthorizedBy,
			DocumentRef:  req.Authorization.DocumentRef,
		}
		if req.Authorization.GrantedAt != "" {
			if t, err := time.Parse(time.RFC3339, req.Authorization.GrantedAt); err == nil {
				auth.GrantedAt = t
			}
		}
		if req.Authorization.ExpiresAt != "" {
			if t, err := time.Parse(time.RFC3339, req.Authorization.ExpiresAt); err == nil {
				auth.ExpiresAt = t
			}
		}
		if _, err := h.svc.Engagement.Authorize(r.Context(), id, auth, actor); err != nil {
			writeError(w, h.log, err)
			return
		}
	}

	eng, err := h.svc.Engagement.Get(r.Context(), id)
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"engagement": engagementToJSON(eng)})
}

// ---------- Topology handlers ----------

func (h *handlers) getTopology(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	t, err := h.svc.Infra.GetTopology(r.Context(), id)
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, topologyToJSON(t))
}

func (h *handlers) putTopology(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req topologyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	t := req.toDomain(id)
	if err := h.svc.Infra.SaveTopology(r.Context(), id, t, actorFrom(r.Context())); err != nil {
		writeError(w, h.log, err)
		return
	}
	saved, err := h.svc.Infra.GetTopology(r.Context(), id)
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, topologyToJSON(saved))
}

func (h *handlers) validateTopology(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	problems, err := h.svc.Infra.ValidateTopology(r.Context(), id)
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	valid := len(problems) == 0
	writeJSON(w, http.StatusOK, map[string]any{
		"valid":    valid,
		"problems": problems,
	})
}

// ---------- Deploy / Teardown ----------

func (h *handlers) deploy(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	jobID, err := h.svc.Infra.Deploy(r.Context(), id, actorFrom(r.Context()))
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"jobId": jobID})
}

func (h *handlers) teardown(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	jobID, err := h.svc.Infra.Teardown(r.Context(), id, actorFrom(r.Context()))
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"jobId": jobID})
}

// ---------- Credentials ----------

func (h *handlers) putCredentials(w http.ResponseWriter, r *http.Request) {
	engagementID := chi.URLParam(r, "id")
	provider := chi.URLParam(r, "provider")

	var req credentialsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.Values) == 0 {
		http.Error(w, `{"error":"credentials values map must not be empty"}`, http.StatusBadRequest)
		return
	}

	plaintext, err := service.MarshalCredentials(req.Values)
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	if err := h.svc.Infra.PutCredentials(r.Context(), engagementID, provider, plaintext, actorFrom(r.Context())); err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

func (h *handlers) getCredentialsMeta(w http.ResponseWriter, r *http.Request) {
	engagementID := chi.URLParam(r, "id")
	provider := chi.URLParam(r, "provider")

	meta, err := h.svc.Infra.GetCredentialMeta(r.Context(), engagementID, provider)
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, credentialMetaToJSON(meta))
}

// ---------- SSE ----------

// flusherWriter wraps a ResponseWriter that may also be an http.Flusher.
// When w does not directly implement Flusher, we use a no-op flush.
type flusherWriter struct {
	http.ResponseWriter
	flush func()
}

func (f flusherWriter) Flush() { f.flush() }

func (h *handlers) sseEvents(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Chi may wrap the underlying ResponseWriter; unwrap to find http.Flusher.
	var flusher http.Flusher
	type unwrapper interface{ Unwrap() http.ResponseWriter }
	rw := w
	for {
		if f, ok := rw.(http.Flusher); ok {
			flusher = f
			break
		}
		if u, ok := rw.(unwrapper); ok {
			rw = u.Unwrap()
		} else {
			break
		}
	}
	if flusher == nil {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	_ = flusher

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch, unsub := h.svc.Hub.Subscribe(id)
	defer unsub()

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	// Send an initial connection event.
	_, _ = fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			_, _ = fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(ev.Data)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", string(ev.Kind), data)
			flusher.Flush()
		}
	}
}

// ---------- Audit ----------

func (h *handlers) listAuditEvents(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	events, err := h.svc.AuditLog.List(r.Context(), id, limit, offset)
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": auditEventsToJSON(events)})
}

// ---------- C2 frameworks ----------

func (h *handlers) listC2Frameworks(w http.ResponseWriter, r *http.Request) {
	frameworks := c2.List()
	out := make([]map[string]any, 0, len(frameworks))
	for _, f := range frameworks {
		gated := f.Name() == "cobaltstrike" || f.Name() == "bruteratel"
		out = append(out, map[string]any{
			"id":    f.Name(),
			"name":  frameworkDisplayName(f.Name()),
			"tier":  f.Tier().String(),
			"gated": gated,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"frameworks": out})
}

func frameworkDisplayName(id string) string {
	names := map[string]string{
		"sliver":       "Sliver",
		"mythic":       "Mythic",
		"havoc":        "Havoc",
		"cobaltstrike": "Cobalt Strike",
		"bruteratel":   "Brute Ratel C4",
		"metasploit":   "Metasploit",
		"poshc2":       "PoshC2",
		"custom":       "Custom",
	}
	if n, ok := names[id]; ok {
		return n
	}
	return id
}

// ---------- Scenarios ----------

func (h *handlers) listScenarios(w http.ResponseWriter, r *http.Request) {
	scenarios := h.svc.Emulation.ListScenarios()
	out := make([]map[string]any, 0, len(scenarios))
	for _, s := range scenarios {
		techniques := make([]map[string]any, 0, len(s.Techniques))
		for _, t := range s.Techniques {
			techniques = append(techniques, map[string]any{
				"id":     t.AttackID,
				"name":   t.Name,
				"tactic": t.Tactic,
			})
		}
		out = append(out, map[string]any{
			"id":         s.ID,
			"name":       s.Name,
			"actor":      s.AdversaryProfile,
			"techniques": techniques,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"scenarios": out})
}

// ---------- Runs ----------

func (h *handlers) startRun(w http.ResponseWriter, r *http.Request) {
	engagementID := chi.URLParam(r, "id")
	var req startRunRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	runID, err := h.svc.Emulation.Start(r.Context(), engagementID, req.ScenarioID, actorFrom(r.Context()))
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"runId": runID})
}

func (h *handlers) getRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	run, err := h.svc.Emulation.GetRun(r.Context(), id)
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": runToJSON(run)})
}

// ---------- Coverage + Navigator ----------

func (h *handlers) getCoverage(w http.ResponseWriter, r *http.Request) {
	engagementID := chi.URLParam(r, "id")
	coverage, err := h.svc.Emulation.GetCoverage(r.Context(), engagementID)
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, coverage)
}

func (h *handlers) getNavigator(w http.ResponseWriter, r *http.Request) {
	engagementID := chi.URLParam(r, "id")

	// Fetch engagement name for the Navigator layer name.
	layerName := "RInfra Coverage Export"
	if eng, err := h.svc.Engagement.Get(r.Context(), engagementID); err == nil {
		layerName = fmt.Sprintf("RInfra · %s · %s", eng.Client, eng.Codename)
	}

	layer, err := h.svc.Emulation.GetNavigatorLayer(r.Context(), engagementID, layerName)
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	// The Navigator layer is delivered as JSON (application/json for download).
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="navigator-%s.json"`, engagementID))
	writeJSON(w, http.StatusOK, layer)
}
