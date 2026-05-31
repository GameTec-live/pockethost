package tenant

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

func Run(args []string) error {
	fs := flag.NewFlagSet("tenant", flag.ExitOnError)
	dataDir := fs.String("dir", "", "tenant PocketBase data directory")
	httpAddr := fs.String("http", "127.0.0.1:0", "tenant HTTP listen address")
	dev := fs.Bool("dev", false, "run tenant in dev mode")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dataDir == "" {
		return fmt.Errorf("tenant --dir is required")
	}

	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir:      *dataDir,
		DefaultDev:          *dev,
		DefaultQueryTimeout: 30 * time.Second,
		HideStartBanner:     true,
	})
	if err := app.Bootstrap(); err != nil {
		return err
	}
	defer app.ResetBootstrapState()
	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		e.Router.GET("/{path...}", apis.Static(os.DirFS(filepath.Join(*dataDir, "pb_public")), true))
		return e.Next()
	})

	return apis.Serve(app, apis.ServeConfig{
		HttpAddr:        *httpAddr,
		ShowStartBanner: false,
		AllowedOrigins:  []string{"*"},
	})
}
