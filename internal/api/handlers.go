package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
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
	ctx := r.Context()
	actor := actorUser(ctx)
	projectID := r.URL.Query().Get("projectId")

	// Explicit project filter: enforce access then list that project's work.
	if projectID != "" {
		if h.svc.Projects != nil {
			ok, err := h.svc.Projects.CanAccessProject(ctx, actor, projectID)
			if err != nil {
				writeError(w, h.log, err)
				return
			}
			if !ok {
				writeErrorCode(w, http.StatusForbidden, "forbidden", "cannot access project")
				return
			}
		}
		engs, err := h.svc.Engagement.ListForProject(ctx, projectID)
		if err != nil {
			writeError(w, h.log, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"engagements": engagementsToJSON(engs)})
		return
	}

	// Admins (and the legacy/dev synthetic admin when auth is disabled) see all.
	if actor.Role == domain.RoleAdmin || h.svc.Projects == nil {
		engs, err := h.svc.Engagement.List(ctx)
		if err != nil {
			writeError(w, h.log, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"engagements": engagementsToJSON(engs)})
		return
	}

	// Non-admins see only engagements within the projects they can access.
	projs, err := h.svc.Projects.List(ctx, actor)
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	var engs []domain.Engagement
	for _, p := range projs {
		pe, err := h.svc.Engagement.ListForProject(ctx, p.ID)
		if err != nil {
			writeError(w, h.log, err)
			return
		}
		engs = append(engs, pe...)
	}
	writeJSON(w, http.StatusOK, map[string]any{"engagements": engagementsToJSON(engs)})
}

func (h *handlers) createEngagement(w http.ResponseWriter, r *http.Request) {
	var req createEngagementRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	e, err := req.toDomain()
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	// If bound to a project, the actor must be able to access it.
	if e.ProjectID != "" && h.svc.Projects != nil {
		ok, err := h.svc.Projects.CanAccessProject(r.Context(), actorUser(r.Context()), e.ProjectID)
		if err != nil {
			writeError(w, h.log, err)
			return
		}
		if !ok {
			writeErrorCode(w, http.StatusForbidden, "forbidden", "cannot access project")
			return
		}
	}
	created, err := h.svc.Engagement.Create(r.Context(), e, actorFrom(r.Context()))
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"engagement": engagementToJSON(created)})
}

// canAccessEngagement enforces project-membership authorization for an
// engagement: admins see everything; everyone else only engagements within a
// project they lead or belong to. When the project subsystem is not wired
// (dev/legacy/test), access is allowed — matching the synthetic-admin actor.
func (h *handlers) canAccessEngagement(ctx context.Context, user domain.User, eng domain.Engagement) bool {
	if user.Role == domain.RoleAdmin {
		return true
	}
	if h.svc.Projects == nil {
		return true
	}
	if eng.ProjectID == "" {
		// Project-less engagement: only admins (handled above).
		return false
	}
	ok, err := h.svc.Projects.CanAccessProject(ctx, user, eng.ProjectID)
	return err == nil && ok
}

// requireEngagementAccess is middleware for every /engagements/{id} route. It
// loads the engagement, authorizes the actor against project membership, and
// rejects with 403 before any handler (read, deploy, teardown, credentials,
// run, C2, shell) runs.
func (h *handlers) requireEngagementAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		eng, err := h.svc.Engagement.Get(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, h.log, err)
			return
		}
		if !h.canAccessEngagement(r.Context(), actorUser(r.Context()), eng) {
			writeErrorCode(w, http.StatusForbidden, "forbidden", "you do not have access to this engagement")
			return
		}
		next.ServeHTTP(w, r)
	})
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
			t, err := time.Parse(time.RFC3339, req.Authorization.GrantedAt)
			if err != nil {
				writeErrorCode(w, http.StatusBadRequest, "invalid_request", "grantedAt must be an RFC3339 timestamp")
				return
			}
			auth.GrantedAt = t
		}
		if req.Authorization.ExpiresAt != "" {
			t, err := time.Parse(time.RFC3339, req.Authorization.ExpiresAt)
			if err != nil {
				writeErrorCode(w, http.StatusBadRequest, "invalid_request", "expiresAt must be an RFC3339 timestamp")
				return
			}
			auth.ExpiresAt = t
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
		writeErrorCode(w, http.StatusBadRequest, "invalid_request", "credentials values map must not be empty")
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
	const maxLimit = 500
	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxLimit {
		limit = maxLimit // cap to avoid unbounded result sets
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
		out = append(out, scenarioToJSON(s))
	}
	writeJSON(w, http.StatusOK, map[string]any{"scenarios": out})
}

func (h *handlers) createScenario(w http.ResponseWriter, r *http.Request) {
	var req createScenarioRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	created, err := h.svc.Emulation.CreateScenario(r.Context(), req.toDomain(), actorFrom(r.Context()))
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"scenario": scenarioToJSON(created)})
}

func (h *handlers) updateScenario(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req createScenarioRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	sc := req.toDomain()
	sc.ID = id
	updated, err := h.svc.Emulation.UpdateScenario(r.Context(), sc, actorFrom(r.Context()))
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"scenario": scenarioToJSON(updated)})
}

// importScenario ingests an SRA-format benchmark index (YAML request body) and
// creates a scenario + TTP-library entries from it.
func (h *handlers) importScenario(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(io.LimitReader(r.Body, 4<<20)) // 4 MiB cap
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "invalid_request", "could not read request body")
		return
	}
	created, err := h.svc.Emulation.ImportIndex(r.Context(), data, actorFrom(r.Context()))
	if err != nil {
		writeErrorCode(w, http.StatusUnprocessableEntity, "invalid_index", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"scenario": scenarioToJSON(created)})
}

func (h *handlers) deleteScenario(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.svc.Emulation.DeleteScenario(r.Context(), id, actorFrom(r.Context())); err != nil {
		writeError(w, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- TTP library (operator-authored techniques) ----------

func (h *handlers) listTechniques(w http.ResponseWriter, r *http.Request) {
	techniques, err := h.svc.Emulation.ListTechniques(r.Context())
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	out := make([]map[string]any, 0, len(techniques))
	for _, t := range techniques {
		out = append(out, techniqueToJSON(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{"techniques": out})
}

func (h *handlers) createTechnique(w http.ResponseWriter, r *http.Request) {
	var req techniqueRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	created, err := h.svc.Emulation.CreateTechnique(r.Context(), req.toDomain(), actorFrom(r.Context()))
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"technique": techniqueToJSON(created)})
}

func (h *handlers) updateTechnique(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "attackId")
	var req techniqueRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	t := req.toDomain()
	t.AttackID = id
	updated, err := h.svc.Emulation.UpdateTechnique(r.Context(), t, actorFrom(r.Context()))
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"technique": techniqueToJSON(updated)})
}

func (h *handlers) deleteTechnique(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "attackId")
	if err := h.svc.Emulation.DeleteTechnique(r.Context(), id, actorFrom(r.Context())); err != nil {
		writeError(w, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- C2 web shell (WebSocket) ----------

// c2Shell upgrades to a WebSocket and bridges the in-browser operator terminal
// to a deployed teamserver. Authorization (CanDeploy) and node resolution happen
// before the upgrade so failures surface as normal HTTP errors. The streamed
// command surface is read-only and controlled by service.RespondShell — it never
// executes arbitrary commands on the control plane.
func (h *handlers) c2Shell(w http.ResponseWriter, r *http.Request) {
	engagementID := chi.URLParam(r, "id")
	nodeID := chi.URLParam(r, "nodeId")
	actor := actorFrom(r.Context())

	info, err := h.svc.C2.OpenShell(r.Context(), engagementID, nodeID, actor)
	if err != nil {
		writeError(w, h.log, err)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
	if err != nil {
		return // Accept already wrote the failure
	}
	defer conn.Close(websocket.StatusInternalError, "shell closed")

	ctx := r.Context()
	if err := conn.Write(ctx, websocket.MessageText, []byte(service.ShellBanner(info))); err != nil {
		return
	}

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return // client disconnected
		}
		out, closed := service.RespondShell(info, strings.TrimRight(string(data), "\r\n"))
		if out != "" {
			if err := conn.Write(ctx, websocket.MessageText, []byte(out)); err != nil {
				return
			}
		}
		if closed {
			conn.Close(websocket.StatusNormalClosure, "exit")
			return
		}
	}
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
	// Runs are fetched cross-engagement by id; authorize against the owning
	// engagement's project membership.
	if eng, err := h.svc.Engagement.Get(r.Context(), run.EngagementID); err == nil {
		if !h.canAccessEngagement(r.Context(), actorUser(r.Context()), eng) {
			writeErrorCode(w, http.StatusForbidden, "forbidden", "you do not have access to this run")
			return
		}
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
