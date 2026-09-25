package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestMetricsNegotiatesOpenMetricsWithoutChangingLegacyScrapes(t *testing.T) {
	registry := prometheus.NewRegistry()
	gauge := prometheus.NewGauge(prometheus.GaugeOpts{Name: "ajax_test_value", Help: "Test observation."})
	registry.MustRegister(gauge)
	gauge.Set(23.5)
	handler := (&Server{reg: registry}).metricsHandler()
	for _, tc := range []struct {
		accept, contentType string
		openMetrics         bool
	}{
		{"", "text/plain", false},
		{"application/openmetrics-text; version=1.0.0", "application/openmetrics-text", true},
	} {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		req.Header.Set("Accept", tc.accept)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), tc.contentType) {
			t.Fatalf("Accept %q: status=%d content-type=%q", tc.accept, rec.Code, rec.Header().Get("Content-Type"))
		}
		if !strings.Contains(rec.Body.String(), "ajax_test_value 23.5") || strings.Contains(rec.Body.String(), "# EOF") != tc.openMetrics {
			t.Fatalf("Accept %q: unexpected metrics body %q", tc.accept, rec.Body.String())
		}
	}
}

func TestWriteJSONDeclaresUTF8(t *testing.T) {
	rec := httptest.NewRecorder()

	writeJSON(rec, map[string]string{"name": "Рома"})

	if got := rec.Header().Get("Content-Type"); got != jsonContentType {
		t.Fatalf("Content-Type = %q, want %q", got, jsonContentType)
	}
	if !strings.Contains(rec.Body.String(), "Рома") {
		t.Fatalf("JSON body did not preserve UTF-8 text: %q", rec.Body.String())
	}
}

func TestLogoSkipsDirectories(t *testing.T) {
	chdir(t, t.TempDir())
	if err := os.Mkdir("logo.png", 0o755); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	(&Server{}).logo(rec, httptest.NewRequest(http.MethodGet, "/admin/logo.png", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestLogoServesFile(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "logo.png"), []byte("logo"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	(&Server{}).logo(rec, httptest.NewRequest(http.MethodGet, "/admin/logo.png", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); body != "logo" {
		t.Fatalf("body = %q, want %q", body, "logo")
	}
}

func TestAdminHTMLIncludesExpandedJeedomMatching(t *testing.T) {
	for _, want := range []string{
		`id="matchingSummary"`,
		`Canonical metric`,
		`HA component / class`,
		`Current value`,
		`Name / raw`,
		`Diagnostic`,
		`Unlinked`,
		`command.visible`,
		`command.historized`,
		`function mergeJeedomCommandIDs(deviceSlug)`,
		`(model.jeedom_actions || []).forEach(function(action)`,
		`Control action`,
		`model.devices = collectDevices()`,
		`catalogDevice.jeedom_command_ids = mergedIDs`,
		`return /^\d+$/.test`,
		`Press Save catalog to persist`,
	} {
		if !strings.Contains(adminHTML, want) {
			t.Errorf("admin HTML is missing %q", want)
		}
	}
}

func TestAdminHTMLBuildsNotificationMetricsFromDefaultsAndCommands(t *testing.T) {
	for _, want := range []string{
		`CORE_NOTIFICATION_METRICS`,
		`'issue_count'`,
		`'co2_ppm'`,
		`'device_last_update'`,
		`'alarm'`,
		`'valve'`,
		`'battery_percent'`,
		`'battery_state'`,
		`'status'`,
		`(model.jeedom_commands || []).forEach(discover)`,
		`Object.values(device.raw_commands || {}).forEach(discover)`,
		`add(selected)`,
		`notificationMetricOptions(rule.metric)`,
	} {
		if !strings.Contains(adminHTML, want) {
			t.Errorf("admin HTML is missing %q", want)
		}
	}
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldwd); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
}
