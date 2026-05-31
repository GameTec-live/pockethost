package master

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
)

func (s *Server) firstRun(e *core.RequestEvent) error {
	count, err := realSuperuserCount(e.App)
	return e.JSON(http.StatusOK, map[string]any{"firstRun": err == nil && count == 0})
}

func (s *Server) createFirstUser(e *core.RequestEvent) error {
	count, err := realSuperuserCount(e.App)
	if err != nil {
		return err
	}
	if count != 0 {
		return e.ForbiddenError("first user already exists", nil)
	}
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("invalid request body", err)
	}
	userCol, err := e.App.FindCollectionByNameOrId("users")
	if err != nil {
		return err
	}
	user := core.NewRecord(userCol)
	user.SetEmail(body.Email)
	user.SetPassword(body.Password)
	user.SetVerified(true)
	user.Set("role", "admin")
	if err := e.App.Save(user); err != nil {
		return e.BadRequestError("failed to create master user", err)
	}
	superCol, err := e.App.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		return err
	}
	superuser := core.NewRecord(superCol)
	superuser.SetEmail(body.Email)
	superuser.SetPassword(body.Password)
	if err := e.App.Save(superuser); err != nil {
		return e.BadRequestError("failed to create PocketBase superuser", err)
	}
	return e.JSON(http.StatusCreated, user)
}

func realSuperuserCount(app core.App) (int64, error) {
	return app.CountRecords(core.CollectionNameSuperusers, dbx.Not(dbx.HashExp{
		"email": core.DefaultInstallerEmail,
	}))
}

func isMasterAdmin(auth *core.Record) bool {
	return auth != nil && (auth.IsSuperuser() || auth.GetString("role") == "admin")
}

func authOwnerID(auth *core.Record) string {
	if auth == nil {
		return ""
	}
	return auth.Id
}

func appUserForAuth(app core.App, auth *core.Record) (*core.Record, error) {
	if auth == nil {
		return nil, nil
	}
	if auth.Collection().Name == "users" {
		return auth, nil
	}
	if auth.IsSuperuser() && auth.Email() != "" {
		return app.FindAuthRecordByEmail("users", auth.Email())
	}
	return nil, nil
}

func (s *Server) setInstanceError(e *core.RequestEvent, rec *core.Record, message string, err error) error {
	rec.Set("status", instanceStopped)
	_ = e.App.Save(rec)
	return e.BadRequestError(message, err)
}

func (s *Server) ensureInstanceDir(e *core.RequestEvent, rec *core.Record) (string, error) {
	dir := instanceDir(s.cfg.DataDir, rec.Id)
	rec.Set("data_dir", dir)
	if err := e.App.Save(rec); err != nil {
		return "", err
	}
	return dir, nil
}

func (s *Server) listInstances(e *core.RequestEvent) error {
	var (
		records []*core.Record
		err     error
	)
	if isMasterAdmin(e.Auth) {
		records, err = e.App.FindAllRecords(collectionInstances)
	} else {
		owner, ownerErr := appUserForAuth(e.App, e.Auth)
		if ownerErr != nil || owner == nil {
			return e.ForbiddenError("no app user is linked to this account", ownerErr)
		}
		records, err = e.App.FindAllRecords(collectionInstances, dbx.HashExp{"owner": owner.Id})
	}
	if err != nil {
		return e.BadRequestError("failed to list instances", err)
	}
	return e.JSON(http.StatusOK, records)
}

func (s *Server) createInvite(e *core.RequestEvent) error {
	if !isMasterAdmin(e.Auth) {
		return e.ForbiddenError("only admins can invite users", nil)
	}
	var body inviteUserRequest
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("invalid request body", err)
	}
	hours := body.ExpiresHours
	if hours <= 0 {
		hours = 72
	}
	if hours > 24*30 {
		return e.BadRequestError("invite expiry cannot exceed 30 days", nil)
	}
	token, err := newInviteToken()
	if err != nil {
		return e.InternalServerError("failed to create invite token", err)
	}
	col, err := e.App.FindCollectionByNameOrId("invites")
	if err != nil {
		return err
	}
	rec := core.NewRecord(col)
	rec.Set("token_hash", hashInviteToken(token))
	rec.Set("created_by", authOwnerID(e.Auth))
	rec.Set("expires_at", time.Now().Add(time.Duration(hours)*time.Hour).UTC().Format(time.RFC3339))
	if err := e.App.Save(rec); err != nil {
		return e.BadRequestError("failed to create invite", err)
	}
	return e.JSON(http.StatusCreated, map[string]any{
		"id":        rec.Id,
		"url":       inviteURL(e, token),
		"expiresAt": rec.GetDateTime("expires_at"),
	})
}

func (s *Server) getInvite(e *core.RequestEvent) error {
	rec, err := s.findUsableInvite(e)
	if err != nil {
		return err
	}
	return e.JSON(http.StatusOK, map[string]any{
		"valid":     true,
		"expiresAt": rec.GetDateTime("expires_at"),
	})
}

func (s *Server) acceptInvite(e *core.RequestEvent) error {
	invite, err := s.findUsableInvite(e)
	if err != nil {
		return err
	}
	var body acceptInviteRequest
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("invalid request body", err)
	}
	if body.Email == "" || body.Password == "" {
		return e.BadRequestError("email and password are required", nil)
	}
	if existing, _ := e.App.FindAuthRecordByEmail("users", body.Email); existing != nil {
		return e.BadRequestError("user already exists", nil)
	}
	col, err := e.App.FindCollectionByNameOrId("users")
	if err != nil {
		return err
	}
	user := core.NewRecord(col)
	user.SetEmail(body.Email)
	user.SetPassword(body.Password)
	user.SetVerified(true)
	user.Set("role", "user")
	if err := e.App.Save(user); err != nil {
		return e.BadRequestError("failed to create user", err)
	}
	invite.Set("used_at", time.Now().UTC().Format(time.RFC3339))
	if err := e.App.Save(invite); err != nil {
		return err
	}
	return e.JSON(http.StatusCreated, user)
}

func (s *Server) findUsableInvite(e *core.RequestEvent) (*core.Record, error) {
	token := e.Request.PathValue("token")
	if token == "" {
		return nil, e.NotFoundError("invite not found", nil)
	}
	rec, err := e.App.FindFirstRecordByFilter("invites", "token_hash = {:hash}", dbx.Params{"hash": hashInviteToken(token)})
	if err != nil {
		return nil, e.NotFoundError("invite not found", err)
	}
	if rec.GetDateTime("used_at").Time().After(time.Time{}) {
		return nil, e.BadRequestError("invite has already been used", nil)
	}
	if rec.GetDateTime("expires_at").Time().Before(time.Now()) {
		return nil, e.BadRequestError("invite has expired", nil)
	}
	return rec, nil
}

func newInviteToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func hashInviteToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func inviteURL(e *core.RequestEvent, token string) string {
	scheme := "http"
	if e.Request.TLS != nil {
		scheme = "https"
	}
	if proto := e.Request.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	return scheme + "://" + e.Request.Host + "/?invite=" + token
}

func (s *Server) createInstance(e *core.RequestEvent) error {
	var body createInstanceRequest
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("invalid request body", err)
	}
	name, err := normalizeInstanceName(body.Name)
	if err != nil {
		return e.BadRequestError(err.Error(), err)
	}
	if body.SuperuserEmail == "" || body.SuperuserPassword == "" {
		return e.BadRequestError("superuser email and password are required", nil)
	}
	if existing, _ := e.App.FindFirstRecordByFilter(collectionInstances, "name = {:name}", dbx.Params{"name": name}); existing != nil {
		return e.BadRequestError("instance name already exists", nil)
	}
	port, err := freeLocalPort()
	if err != nil {
		return e.InternalServerError("failed to allocate port", err)
	}
	col, err := e.App.FindCollectionByNameOrId(collectionInstances)
	if err != nil {
		return err
	}
	rec := core.NewRecord(col)
	owner, err := appUserForAuth(e.App, e.Auth)
	if err != nil || owner == nil {
		return e.ForbiddenError("no app user is linked to this account", err)
	}
	rec.Set("owner", owner.Id)
	rec.Set("name", name)
	rec.Set("status", instanceStopped)
	rec.Set("port", port)
	if err := e.App.Save(rec); err != nil {
		return e.BadRequestError("failed to create instance record", err)
	}
	dir, err := s.ensureInstanceDir(e, rec)
	if err != nil {
		return e.BadRequestError("failed to assign tenant directory", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return s.setInstanceError(e, rec, "failed to create tenant directory", err)
	}
	if err := initializeTenant(dir, body.SuperuserEmail, body.SuperuserPassword); err != nil {
		return s.setInstanceError(e, rec, "failed to initialize tenant", err)
	}
	if err := s.startTenant(context.Background(), rec.Id, port, dir); err != nil {
		return s.setInstanceError(e, rec, "failed to start tenant", err)
	}
	rec.Set("status", instanceRunning)
	if err := e.App.Save(rec); err != nil {
		return err
	}
	return e.JSON(http.StatusCreated, rec)
}

func (s *Server) renameInstance(e *core.RequestEvent) error {
	rec, err := s.findOwnedInstance(e)
	if err != nil {
		return err
	}
	var body renameInstanceRequest
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("invalid request body", err)
	}
	name, err := normalizeInstanceName(body.Name)
	if err != nil {
		return e.BadRequestError(err.Error(), err)
	}
	if existing, _ := e.App.FindFirstRecordByFilter(collectionInstances, "name = {:name} && id != {:id}", dbx.Params{"name": name, "id": rec.Id}); existing != nil {
		return e.BadRequestError("instance name already exists", nil)
	}
	rec.Set("name", name)
	if err := e.App.Save(rec); err != nil {
		return err
	}
	return e.JSON(http.StatusOK, rec)
}

func (s *Server) startInstance(e *core.RequestEvent) error {
	rec, err := s.findOwnedInstance(e)
	if err != nil {
		return err
	}
	if rec.GetString("status") == instanceRunning {
		return e.JSON(http.StatusOK, rec)
	}
	dir := rec.GetString("data_dir")
	if dir == "" {
		dir, err = s.ensureInstanceDir(e, rec)
		if err != nil {
			return e.BadRequestError("failed to assign tenant directory", err)
		}
	}
	if err := s.startTenant(context.Background(), rec.Id, rec.GetInt("port"), dir); err != nil {
		return s.setInstanceError(e, rec, "failed to start tenant", err)
	}
	rec.Set("status", instanceRunning)
	if err := e.App.Save(rec); err != nil {
		return err
	}
	return e.JSON(http.StatusOK, rec)
}

func (s *Server) stopInstance(e *core.RequestEvent) error {
	rec, err := s.findOwnedInstance(e)
	if err != nil {
		return err
	}
	_ = s.manager.Stop(rec.Id)
	rec.Set("status", instanceStopped)
	if err := e.App.Save(rec); err != nil {
		return err
	}
	return e.JSON(http.StatusOK, rec)
}

func (s *Server) deleteInstance(e *core.RequestEvent) error {
	rec, err := s.findOwnedInstance(e)
	if err != nil {
		return err
	}
	_ = s.manager.Stop(rec.Id)
	dir := rec.GetString("data_dir")
	if dir != "" {
		if err := os.RemoveAll(dir); err != nil {
			return e.InternalServerError("failed to purge tenant data", err)
		}
	}
	if err := e.App.Delete(rec); err != nil {
		return err
	}
	return e.JSON(http.StatusOK, map[string]any{"deleted": true})
}

func (s *Server) deployInstanceSite(e *core.RequestEvent) error {
	rec, err := s.findOwnedInstance(e)
	if err != nil {
		return err
	}
	if err := e.Request.ParseMultipartForm(8 << 20); err != nil {
		return e.BadRequestError("invalid upload", err)
	}
	files := e.Request.MultipartForm.File["file"]
	if len(files) == 0 {
		return e.BadRequestError("zip file is required", nil)
	}
	fh := files[0]
	if fh.Size > maxStaticZipUploadBytes {
		return e.BadRequestError("zip file cannot exceed 100 MB", nil)
	}
	if !strings.EqualFold(filepath.Ext(fh.Filename), ".zip") {
		return e.BadRequestError("only zip files are supported", nil)
	}
	file, err := fh.Open()
	if err != nil {
		return e.BadRequestError("failed to open uploaded zip", err)
	}
	defer file.Close()

	dir := rec.GetString("data_dir")
	if dir == "" {
		dir, err = s.ensureInstanceDir(e, rec)
		if err != nil {
			return e.BadRequestError("failed to assign tenant directory", err)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return e.InternalServerError("failed to create tenant directory", err)
	}
	if err := deployStaticZip(dir, file, fh.Size); err != nil {
		return e.BadRequestError("failed to deploy zip", err)
	}
	return e.JSON(http.StatusOK, rec)
}

func deployStaticZip(instanceDir string, file io.ReaderAt, size int64) error {
	zr, err := zip.NewReader(file, size)
	if err != nil {
		return err
	}
	tmpDir, err := os.MkdirTemp(instanceDir, "pb_public_*")
	if err != nil {
		return err
	}
	renamed := false
	defer func() {
		if !renamed {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	var total int64
	for _, zf := range zr.File {
		clean, err := cleanZipPath(zf.Name)
		if err != nil {
			return err
		}
		if clean == "" {
			continue
		}
		if zf.FileInfo().Mode()&os.ModeSymlink != 0 {
			return errors.New("zip cannot contain symlinks")
		}
		target := filepath.Join(tmpDir, filepath.FromSlash(clean))
		rel, err := filepath.Rel(tmpDir, target)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return errors.New("zip contains an unsafe path")
		}
		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if int64(zf.UncompressedSize64) > maxStaticExtractBytes-total {
			return errors.New("zip contents are too large")
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, zf.FileInfo().Mode().Perm())
		if err != nil {
			_ = rc.Close()
			return err
		}
		n, copyErr := io.Copy(out, io.LimitReader(rc, maxStaticExtractBytes-total+1))
		closeErr := errors.Join(out.Close(), rc.Close())
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		total += n
		if total > maxStaticExtractBytes {
			return errors.New("zip contents are too large")
		}
	}

	publicDir := filepath.Join(instanceDir, "pb_public")
	if err := os.RemoveAll(publicDir); err != nil {
		return err
	}
	if err := os.Rename(tmpDir, publicDir); err != nil {
		return err
	}
	renamed = true
	return nil
}

func cleanZipPath(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	clean := path.Clean(strings.TrimPrefix(name, "/"))
	if clean == "." {
		return "", nil
	}
	if strings.HasPrefix(clean, "../") || clean == ".." || path.IsAbs(clean) || filepath.VolumeName(clean) != "" {
		return "", errors.New("zip contains an unsafe path")
	}
	return clean, nil
}

func (s *Server) findOwnedInstance(e *core.RequestEvent) (*core.Record, error) {
	id := e.Request.PathValue("id")
	rec, err := e.App.FindRecordById(collectionInstances, id)
	if err != nil {
		return nil, e.NotFoundError("instance not found", err)
	}
	owner, err := appUserForAuth(e.App, e.Auth)
	if err != nil {
		return nil, err
	}
	if !isMasterAdmin(e.Auth) && (owner == nil || rec.GetString("owner") != owner.Id) {
		return nil, e.ForbiddenError("not allowed", nil)
	}
	return rec, nil
}

func initializeTenant(dir, email, password string) error {
	app := pocketbaseTenantApp(dir)
	if err := app.Bootstrap(); err != nil {
		return err
	}
	defer app.ResetBootstrapState()
	col, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		return err
	}
	rec := core.NewRecord(col)
	rec.SetEmail(email)
	rec.SetPassword(password)
	return app.Save(rec)
}

func pocketbaseTenantApp(dir string) *pocketbase.PocketBase {
	return pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir:      dir,
		HideStartBanner:     true,
		DefaultQueryTimeout: 30 * time.Second,
	})
}
