// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"context"
	"net/http"
	"os"
	"strings"

	"zxy-panel/backend/internal/security"
	"zxy-panel/backend/internal/store"
)

type Router struct {
	store       *store.Store
	jwtSecret   string
	agentSecret string
}

type ctxKey string

const claimsKey ctxKey = "claims"

func NewRouter(s *store.Store) http.Handler {
	r := &Router{store: s, jwtSecret: getenv("ZXY_JWT_SECRET", "dev-secret-change-me"), agentSecret: getenv("ZXY_AGENT_SHARED_SECRET", "change-agent-secret")}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", r.health)
	mux.HandleFunc("/api/auth/login", r.login)
	mux.HandleFunc("/api/auth/change-password", r.withAuth(r.changePassword))
	mux.HandleFunc("/api/dashboard", r.withAuth(r.dashboard))
	mux.HandleFunc("/api/servers", r.withAuth(r.servers))
	mux.HandleFunc("/api/servers/", r.withAuth(r.serverByID))
	mux.HandleFunc("/api/nodes", r.withAuth(r.nodes))
	mux.HandleFunc("/api/nodes/", r.withAuth(r.nodeByID))
	mux.HandleFunc("/api/clients", r.withAuth(r.clients))
	mux.HandleFunc("/api/clients/", r.withAuth(r.clientByID))
	mux.HandleFunc("/api/relays", r.withAuth(r.relays))
	mux.HandleFunc("/api/relays/", r.withAuth(r.relayByID))
	mux.HandleFunc("/api/landing-exits", r.withAuth(r.landingExits))
	mux.HandleFunc("/api/landing-exits/", r.withAuth(r.landingExitByID))
	mux.HandleFunc("/api/logs", r.withAuth(r.logs))
	mux.HandleFunc("/api/system/checks", r.withAuth(r.systemChecks))
	mux.HandleFunc("/api/system/report", r.withAuth(r.systemReport))
	mux.HandleFunc("/api/updates/status", r.withAuth(r.updateStatus))
	mux.HandleFunc("/api/updates/check", r.withAuth(r.updateCheck))
	mux.HandleFunc("/api/updates/panel-command", r.withAuth(r.updatePanelCommand))
	mux.HandleFunc("/api/updates/xray-status", r.withAuth(r.updateXrayStatus))
	mux.HandleFunc("/api/tools/test-socks5", r.withAuth(r.testSocks5))
	mux.HandleFunc("/api/agent/heartbeat", r.agentHeartbeat)
	mux.HandleFunc("/api/agent/sync", r.agentSync)
	mux.HandleFunc("/sub/", r.subscription)
	mux.HandleFunc("/s/", r.shortShare)
	return cors(mux)
}

func (r *Router) withAuth(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		header := req.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing bearer token"})
			return
		}
		claims, err := security.VerifyJWT(r.jwtSecret, strings.TrimPrefix(header, "Bearer "))
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
			return
		}
		ctx := context.WithValue(req.Context(), claimsKey, claims)
		fn(w, req.WithContext(ctx))
	}
}

func currentClaims(req *http.Request) security.Claims {
	v, _ := req.Context().Value(claimsKey).(security.Claims)
	return v
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Agent-Token")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
