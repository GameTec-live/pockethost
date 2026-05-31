package master

const (
	collectionInstances = "instances"
	instanceRunning     = "running"
	instanceStopped     = "stopped"
	instanceDeleted     = "deleted"

	maxStaticZipUploadBytes = 100 << 20
	maxStaticExtractBytes   = 512 << 20
)

type createInstanceRequest struct {
	Name              string `json:"name"`
	SuperuserEmail    string `json:"superuserEmail"`
	SuperuserPassword string `json:"superuserPassword"`
}

type renameInstanceRequest struct {
	Name string `json:"name"`
}

type inviteUserRequest struct {
	ExpiresHours int `json:"expiresHours"`
}

type acceptInviteRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}
