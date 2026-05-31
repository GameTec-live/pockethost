package master

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/GameTec-live/pockethost/internal/site"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/router"
)

type Server struct {
	app     *pocketbase.PocketBase
	cfg     Config
	manager *ProcessManager
}

func Run() error {
	cfg := loadConfig()
	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir:      cfg.DataDir,
		DefaultQueryTimeout: 30 * time.Second,
	})
	s := &Server{app: app, cfg: cfg, manager: NewProcessManager(cfg.DataDir)}

	app.OnBootstrap().Bind(&hook.Handler[*core.BootstrapEvent]{
		Func: func(e *core.BootstrapEvent) error {
			if err := e.Next(); err != nil {
				return err
			}
			if err := ensureSchema(e.App); err != nil {
				return err
			}
			return s.restartRunningTenants(e.App)
		},
	})
	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		e.InstallerFunc = nil
		if err := s.registerRoutes(e); err != nil {
			return err
		}
		if err := e.Next(); err != nil {
			return err
		}
		s.wrapTenantProxy(e)
		return nil
	})

	return app.Start()
}

func (s *Server) registerRoutes(e *core.ServeEvent) error {
	api := e.Router.Group("/api/pockethost")
	api.GET("/first-run", s.firstRun)
	api.POST("/first-run", s.createFirstUser)
	api.GET("/invites/{token}", s.getInvite)
	api.POST("/invites/{token}/accept", s.acceptInvite)
	api.GET("/instances", s.listInstances).Bind(apis.RequireAuth("users", core.CollectionNameSuperusers))
	api.POST("/instances", s.createInstance).Bind(apis.RequireAuth("users", core.CollectionNameSuperusers))
	api.POST("/invites", s.createInvite).Bind(apis.RequireAuth("users", core.CollectionNameSuperusers))
	api.PATCH("/instances/{id}", s.renameInstance).Bind(apis.RequireAuth("users", core.CollectionNameSuperusers))
	api.POST("/instances/{id}/start", s.startInstance).Bind(apis.RequireAuth("users", core.CollectionNameSuperusers))
	api.POST("/instances/{id}/stop", s.stopInstance).Bind(apis.RequireAuth("users", core.CollectionNameSuperusers))
	api.POST("/instances/{id}/deploy", s.deployInstanceSite).
		Unbind(apis.DefaultBodyLimitMiddlewareId).
		Bind(apis.BodyLimit(maxStaticZipUploadBytes+(1<<20)), apis.RequireAuth("users", core.CollectionNameSuperusers))
	api.DELETE("/instances/{id}", s.deleteInstance).Bind(apis.RequireAuth("users", core.CollectionNameSuperusers))

	dist, err := fs.Sub(site.Dist, "dist")
	if err != nil {
		return err
	}
	e.Router.GET("/{path...}", func(re *core.RequestEvent) error {
		return apis.Static(dist, true)(re)
	})
	return nil
}

func (s *Server) wrapTenantProxy(e *core.ServeEvent) {
	masterHandler := e.Server.Handler
	e.Server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := stripPort(r.Host)
		if host != "" && !s.isMasterHost(host) {
			re := &core.RequestEvent{
				App: e.App,
				Event: router.Event{
					Request:  r,
					Response: w,
				},
			}
			if err := s.proxyTenant(re); err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
			}
			return
		}
		masterHandler.ServeHTTP(w, r)
	})
}

func (s *Server) isMasterHost(host string) bool {
	host = strings.ToLower(host)
	return host == "" ||
		host == s.cfg.BaseHost ||
		host == "localhost" ||
		host == "127.0.0.1" ||
		host == "::1"
}

func stripPort(host string) string {
	if i := strings.IndexByte(host, ':'); i >= 0 {
		return strings.ToLower(host[:i])
	}
	return strings.ToLower(host)
}

func (s *Server) tenantNameFromHost(host string) string {
	host = stripPort(host)
	suffix := "." + s.cfg.BaseHost
	if !strings.HasSuffix(host, suffix) {
		return ""
	}
	return strings.TrimSuffix(host, suffix)
}

func (s *Server) proxyTenant(e *core.RequestEvent) error {
	name := s.tenantNameFromHost(e.Request.Host)
	if name == "" {
		return e.NotFoundError("unknown tenant host", nil)
	}
	rec, err := e.App.FindFirstRecordByFilter(collectionInstances, "name = {:name} && status = {:status}", dbx.Params{
		"name": name, "status": instanceRunning,
	})
	if err != nil {
		return e.NotFoundError("tenant not found", err)
	}
	port := rec.GetInt("port")
	if port <= 0 {
		return e.NotFoundError("tenant port is not configured", nil)
	}
	if s.manager != nil && rec.GetString("data_dir") != "" {
		if err := s.startTenant(context.Background(), rec.Id, port, rec.GetString("data_dir")); err != nil {
			e.App.Logger().Error("failed to ensure tenant is running", "id", rec.Id, "error", err)
			http.Error(e.Response, "tenant unavailable", http.StatusBadGateway)
			return nil
		}
	}
	target := &url.URL{Scheme: "http", Host: "127.0.0.1:" + strconv.Itoa(port)}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		e.App.Logger().Error("tenant proxy request failed", "id", rec.Id, "port", port, "path", r.URL.Path, "error", err)
		http.Error(w, "tenant unavailable", http.StatusBadGateway)
	}
	proxy.ServeHTTP(e.Response, e.Request)
	return nil
}

func instanceDir(root, id string) string {
	return filepath.Join(root, "tenants", id)
}

func (s *Server) startTenant(ctx context.Context, id string, port int, dir string) error {
	return s.manager.Start(ctx, id, port, dir)
}

func (s *Server) restartRunningTenants(app core.App) error {
	records, err := app.FindRecordsByFilter(collectionInstances, "status = {:status}", "", 0, 0, dbx.Params{"status": instanceRunning})
	if err != nil {
		return err
	}
	for _, rec := range records {
		if rec.GetString("data_dir") == "" || rec.GetInt("port") == 0 {
			continue
		}
		if err := s.startTenant(context.Background(), rec.Id, rec.GetInt("port"), rec.GetString("data_dir")); err != nil {
			app.Logger().Error("failed to restart tenant", "id", rec.Id, "error", err)
			rec.Set("status", instanceStopped)
			_ = app.Save(rec)
		}
	}
	return nil
}
