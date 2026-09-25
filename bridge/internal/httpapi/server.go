package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/RCooLeR/AjaxBridge/internal/devicecatalog"
	"github.com/RCooLeR/AjaxBridge/internal/jeedom"
	"github.com/RCooLeR/AjaxBridge/internal/notifications"
	"github.com/RCooLeR/AjaxBridge/internal/state"
	"github.com/RCooLeR/AjaxBridge/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
)

const jsonContentType = "application/json; charset=utf-8"

type Server struct {
	addr             string
	state            *state.Engine
	store            *store.Store
	devices          *devicecatalog.Catalog
	jeedom           *jeedom.Store
	jeedomController *jeedom.Controller
	notifications    *notifications.Manager
	reg              *prometheus.Registry
	log              zerolog.Logger
	server           *http.Server
	onCatalogChanged func(state.Snapshot)
}

func New(addr string, stateEngine *state.Engine, store *store.Store, devices *devicecatalog.Catalog, jeedomStore *jeedom.Store, jeedomController *jeedom.Controller, notifications *notifications.Manager, reg *prometheus.Registry, log zerolog.Logger, onCatalogChanged func(state.Snapshot)) *Server {
	return &Server{addr: addr, state: stateEngine, store: store, devices: devices, jeedom: jeedomStore, jeedomController: jeedomController, notifications: notifications, reg: reg, log: log, onCatalogChanged: onCatalogChanged}
}

func (s *Server) Run(ctx context.Context) error {
	router := chi.NewRouter()
	router.Get("/healthz", s.health)
	router.Get("/readyz", s.ready)
	router.Get("/state", s.currentState)
	router.Get("/events", s.events)
	router.Get("/devices", s.devicesJSON)
	router.Get("/admin", s.admin)
	router.Get("/admin/logo.png", s.logo)
	router.Get("/api/admin/bootstrap", s.adminBootstrap)
	router.Put("/api/admin/devices", s.adminSaveDevices)
	router.Put("/api/admin/notifications", s.adminSaveNotifications)
	router.Get("/api/admin/notifications/history", s.adminNotificationHistory)
	if s.jeedom != nil {
		router.Get("/jeedom/devices", s.jeedomDevices)
		router.Get("/jeedom/devices/{device_slug}", s.jeedomDevice)
		router.Get("/jeedom/commands", s.jeedomCommands)
		router.Get("/jeedom/actions", s.jeedomActions)
		router.Get("/jeedom/control-audit", s.jeedomControlAudit)
		router.Post("/jeedom/devices/{device_slug}/control", s.jeedomControl)
	}
	router.Handle("/metrics", s.metricsHandler())

	s.server = &http.Server{
		Addr:              s.addr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.server.Shutdown(shutdownCtx)
	}()

	s.log.Info().Str("addr", s.addr).Msg("HTTP server started")
	err := s.server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) metricsHandler() http.Handler {
	return promhttp.HandlerFor(s.reg, promhttp.HandlerOpts{EnableOpenMetrics: true})
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) currentState(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", jsonContentType)
	_ = json.NewEncoder(w).Encode(s.state.Snapshot())
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err == nil {
			limit = parsed
		}
	}

	w.Header().Set("Content-Type", jsonContentType)
	_ = json.NewEncoder(w).Encode(s.store.ListEvents(limit))
}

func (s *Server) devicesJSON(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", jsonContentType)
	_ = json.NewEncoder(w).Encode(s.devices.Devices())
}

func (s *Server) admin(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(adminHTML))
}

func (s *Server) logo(w http.ResponseWriter, r *http.Request) {
	for _, path := range []string{"logo.png", "bridge/logo.png", "/app/logo.png"} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			http.ServeFile(w, r, path)
			return
		}
	}
	http.NotFound(w, r)
}

func (s *Server) adminBootstrap(w http.ResponseWriter, _ *http.Request) {
	payload := map[string]any{
		"state":                 s.state.Snapshot(),
		"devices":               s.devices.Devices(),
		"devices_path":          s.devices.Path(),
		"notifications":         s.notificationConfig(),
		"notifications_path":    s.notificationPath(),
		"notification_history":  s.notificationHistory(50),
		"jeedom_devices":        []jeedom.Device{},
		"jeedom_commands":       []jeedom.Command{},
		"jeedom_actions":        []jeedom.Action{},
		"jeedom_controls_ready": s.jeedomController != nil && s.jeedomController.Enabled(),
	}
	if s.jeedom != nil {
		payload["jeedom_devices"] = s.jeedom.Devices()
		payload["jeedom_commands"] = s.jeedom.Commands()
		payload["jeedom_actions"] = s.jeedom.Actions()
	}
	writeJSON(w, payload)
}

func (s *Server) adminSaveDevices(w http.ResponseWriter, r *http.Request) {
	var devices []devicecatalog.Device
	if err := json.NewDecoder(r.Body).Decode(&devices); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	updated, err := s.devices.Replace(r.Context(), devices)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	snapshot := s.state.ReloadCatalog()
	if s.onCatalogChanged != nil {
		s.onCatalogChanged(snapshot)
	}
	writeJSON(w, map[string]any{
		"devices": updated,
		"state":   snapshot,
	})
}

func (s *Server) adminSaveNotifications(w http.ResponseWriter, r *http.Request) {
	if s.notifications == nil {
		http.Error(w, "notifications are not configured", http.StatusServiceUnavailable)
		return
	}
	var cfg notifications.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	updated, err := s.notifications.SaveConfig(r.Context(), cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, updated)
}

func (s *Server) adminNotificationHistory(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		if parsed, err := strconv.Atoi(rawLimit); err == nil {
			limit = parsed
		}
	}
	writeJSON(w, s.notificationHistory(limit))
}

func (s *Server) jeedomDevices(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", jsonContentType)
	_ = json.NewEncoder(w).Encode(s.jeedom.Devices())
}

func (s *Server) jeedomDevice(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "device_slug")
	device, ok := s.jeedom.Device(slug)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", jsonContentType)
	_ = json.NewEncoder(w).Encode(device)
}

func (s *Server) jeedomCommands(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", jsonContentType)
	_ = json.NewEncoder(w).Encode(s.jeedom.Commands())
}

func (s *Server) jeedomActions(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", jsonContentType)
	_ = json.NewEncoder(w).Encode(s.jeedom.Actions())
}

func (s *Server) jeedomControlAudit(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		if parsed, err := strconv.Atoi(rawLimit); err == nil {
			limit = parsed
		}
	}
	w.Header().Set("Content-Type", jsonContentType)
	_ = json.NewEncoder(w).Encode(s.jeedom.ControlAudit(limit))
}

func (s *Server) jeedomControl(w http.ResponseWriter, r *http.Request) {
	if s.jeedomController == nil || !s.jeedomController.Enabled() {
		http.Error(w, "Jeedom controls are disabled", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Action string `json:"action"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Action == "" {
		body.Action = r.URL.Query().Get("action")
	}
	if body.Action == "" {
		http.Error(w, "missing action", http.StatusBadRequest)
		return
	}
	result, err := s.jeedomController.Execute(r.Context(), chi.URLParam(r, "device_slug"), body.Action, "http:"+r.RemoteAddr)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, jeedom.ErrControlDisabled):
			status = http.StatusServiceUnavailable
		case errors.Is(err, jeedom.ErrActionNotFound):
			status = http.StatusNotFound
		case errors.Is(err, jeedom.ErrActionDenied):
			status = http.StatusForbidden
		}
		w.Header().Set("Content-Type", jsonContentType)
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":  err.Error(),
			"result": result,
		})
		return
	}
	w.Header().Set("Content-Type", jsonContentType)
	_ = json.NewEncoder(w).Encode(result)
}

func (s *Server) notificationConfig() notifications.Config {
	if s.notifications == nil {
		return notifications.Config{}
	}
	return s.notifications.Config()
}

func (s *Server) notificationPath() string {
	if s.notifications == nil || s.notifications.Store() == nil {
		return ""
	}
	return s.notifications.Store().Path()
}

func (s *Server) notificationHistory(limit int) []notifications.Delivery {
	if s.notifications == nil {
		return nil
	}
	return s.notifications.History(limit)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", jsonContentType)
	_ = json.NewEncoder(w).Encode(value)
}
