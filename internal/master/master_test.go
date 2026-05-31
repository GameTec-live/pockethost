package master

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

func TestTenantHostProxyPreemptsPocketBaseRoutes(t *testing.T) {
	tenant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Tenant-Path", r.URL.Path)
		w.Header().Set("X-Tenant-Method", r.Method)
		_, _ = w.Write([]byte("tenant"))
	}))
	defer tenant.Close()

	tenantURL, err := url.Parse(tenant.URL)
	if err != nil {
		t.Fatal(err)
	}
	if tenantURL.Port() == "" {
		t.Fatalf("test tenant URL has no port: %s", tenant.URL)
	}
	tenantPort, err := strconv.Atoi(tenantURL.Port())
	if err != nil {
		t.Fatal(err)
	}

	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir:      t.TempDir(),
		DefaultQueryTimeout: 30 * time.Second,
	})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	defer app.ResetBootstrapState()

	if err := ensureSchema(app); err != nil {
		t.Fatal(err)
	}

	userCol, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatal(err)
	}
	user := core.NewRecord(userCol)
	user.SetEmail("owner@example.com")
	user.SetPassword("1234567890")
	if err := app.Save(user); err != nil {
		t.Fatal(err)
	}

	instancesCol, err := app.FindCollectionByNameOrId(collectionInstances)
	if err != nil {
		t.Fatal(err)
	}
	instance := core.NewRecord(instancesCol)
	instance.Set("owner", user.Id)
	instance.Set("name", "test")
	instance.Set("status", instanceRunning)
	instance.Set("port", tenantPort)
	if err := app.Save(instance); err != nil {
		t.Fatal(err)
	}

	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{app: app, cfg: Config{BaseHost: "pocketbase.domain.internal", DataDir: t.TempDir()}}
	serveEvent := &core.ServeEvent{App: app, Router: router, Server: &http.Server{}}
	if err := server.registerRoutes(serveEvent); err != nil {
		t.Fatal(err)
	}
	handler, err := router.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	serveEvent.Server.Handler = handler
	server.wrapTenantProxy(serveEvent)
	handler = serveEvent.Server.Handler

	for _, path := range []string{"/_", "/_/", "/api/collections"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Host = "test.pocketbase.domain.internal"
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
			}
			if got := rec.Body.String(); got != "tenant" {
				t.Fatalf("expected tenant response, got %q", got)
			}
			if got := rec.Header().Get("X-Tenant-Path"); got != path {
				t.Fatalf("expected proxied path %q, got %q", path, got)
			}
		})
	}

	t.Run("tenant auth post", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/collections/_superusers/auth-with-password", bytes.NewBufferString(`{"identity":"user@example.com","password":"secret"}`))
		req.Host = "test.pocketbase.domain.internal"
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
		if got := rec.Body.String(); got != "tenant" {
			t.Fatalf("expected tenant response, got %q", got)
		}
		if got := rec.Header().Get("X-Tenant-Path"); got != "/api/collections/_superusers/auth-with-password" {
			t.Fatalf("expected auth request to be proxied, got %q", got)
		}
		if got := rec.Header().Get("X-Tenant-Method"); got != http.MethodPost {
			t.Fatalf("expected POST to be proxied, got %q", got)
		}
	})

	t.Run("master host still serves master routes", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/pockethost/first-run", nil)
		req.Host = "pocketbase.domain.internal"
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
		if got := rec.Body.String(); got == "tenant" {
			t.Fatal("expected master response, got tenant proxy")
		}
	})

}
