package master

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

func ensureSchema(app core.App) error {
	if err := ensureUsersCollection(app); err != nil {
		return err
	}
	if err := ensureInstancesCollection(app); err != nil {
		return err
	}
	return ensureInvitesCollection(app)
}

func ensureUsersCollection(app core.App) error {
	col, err := app.FindCollectionByNameOrId("users")
	if err == nil {
		if col.Fields.GetByName("role") == nil {
			col.Fields.Add(&core.SelectField{Name: "role", Values: []string{"admin", "user"}, MaxSelect: 1})
			return app.Save(col)
		}
		return nil
	}

	col = core.NewAuthCollection("users")
	col.Fields.Add(&core.SelectField{Name: "role", Values: []string{"admin", "user"}, MaxSelect: 1})
	col.ListRule = types.Pointer("id = @request.auth.id || @request.auth.role = 'admin'")
	col.ViewRule = types.Pointer("id = @request.auth.id || @request.auth.role = 'admin'")
	col.CreateRule = nil
	col.UpdateRule = types.Pointer("id = @request.auth.id || @request.auth.role = 'admin'")
	col.DeleteRule = types.Pointer("@request.auth.role = 'admin'")
	return app.Save(col)
}

func ensureInvitesCollection(app core.App) error {
	col, err := app.FindCollectionByNameOrId("invites")
	if err == nil {
		return ensureInviteFields(app, col)
	}
	col = core.NewBaseCollection("invites")
	col.Fields = core.NewFieldsList(
		&core.TextField{Name: "token_hash", Required: true, Hidden: true, Max: 128},
		&core.TextField{Name: "created_by", Required: true, Max: 64},
		&core.DateField{Name: "expires_at", Required: true},
		&core.DateField{Name: "used_at"},
	)
	col.ListRule = types.Pointer("@request.auth.collectionName = '_superusers' || @request.auth.role = 'admin'")
	col.ViewRule = types.Pointer("@request.auth.collectionName = '_superusers' || @request.auth.role = 'admin'")
	col.CreateRule = nil
	col.UpdateRule = nil
	col.DeleteRule = nil
	col.Indexes = types.JSONArray[string]{
		"CREATE UNIQUE INDEX idx_invites_token_hash ON invites (token_hash)",
	}
	return app.Save(col)
}

func ensureInviteFields(app core.App, col *core.Collection) error {
	changed := false
	if col.Fields.GetByName("token_hash") == nil {
		col.Fields.Add(&core.TextField{Name: "token_hash", Required: true, Hidden: true, Max: 128})
		changed = true
	}
	if col.Fields.GetByName("created_by") == nil {
		col.Fields.Add(&core.TextField{Name: "created_by", Required: true, Max: 64})
		changed = true
	}
	if col.Fields.GetByName("expires_at") == nil {
		col.Fields.Add(&core.DateField{Name: "expires_at", Required: true})
		changed = true
	}
	if col.Fields.GetByName("used_at") == nil {
		col.Fields.Add(&core.DateField{Name: "used_at"})
		changed = true
	}
	if changed {
		return app.Save(col)
	}
	return nil
}

func ensureInstancesCollection(app core.App) error {
	col, err := app.FindCollectionByNameOrId(collectionInstances)
	if err == nil {
		return ensureInstanceFields(app, col)
	}
	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		return err
	}

	col = core.NewBaseCollection(collectionInstances)
	col.Fields = core.NewFieldsList(
		&core.RelationField{Name: "owner", CollectionId: users.Id, MaxSelect: 1, Required: true},
		&core.TextField{Name: "name", Required: true, Pattern: subdomainPattern.String(), Max: 63},
		&core.SelectField{Name: "status", Values: []string{instanceRunning, instanceStopped, instanceDeleted}, MaxSelect: 1, Required: true},
		&core.NumberField{Name: "port", Required: true, Min: types.Pointer[float64](1), Max: types.Pointer[float64](65535)},
		&core.TextField{Name: "data_dir", Hidden: true},
	)
	col.ListRule = types.Pointer("owner = @request.auth.id || @request.auth.role = 'admin'")
	col.ViewRule = types.Pointer("owner = @request.auth.id || @request.auth.role = 'admin'")
	col.CreateRule = nil
	col.UpdateRule = nil
	col.DeleteRule = nil
	col.Indexes = types.JSONArray[string]{
		"CREATE UNIQUE INDEX idx_instances_name ON instances (name)",
	}
	return app.Save(col)
}

func ensureInstanceFields(app core.App, col *core.Collection) error {
	changed := false
	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		return err
	}
	if owner := col.Fields.GetByName("owner"); owner == nil || owner.Type() != core.FieldTypeRelation {
		col.Fields.RemoveByName("owner")
		col.Fields.AddAt(0, &core.RelationField{Name: "owner", CollectionId: users.Id, MaxSelect: 1, Required: true})
		changed = true
	}
	if col.Fields.GetByName("data_dir") == nil {
		col.Fields.Add(&core.TextField{Name: "data_dir", Hidden: true})
		changed = true
	}
	listRule := "owner = @request.auth.id || @request.auth.role = 'admin'"
	if col.ListRule == nil || *col.ListRule != listRule {
		col.ListRule = types.Pointer(listRule)
		col.ViewRule = types.Pointer(listRule)
		changed = true
	}
	if changed {
		return app.Save(col)
	}
	return nil
}
