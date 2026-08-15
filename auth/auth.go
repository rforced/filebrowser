package auth

import (
	"net/http"

	"github.com/rforced/filebrowser/v2/settings"
	"github.com/rforced/filebrowser/v2/users"
)

type Auther interface {
	Auth(r *http.Request, usr users.Store, stg *settings.Settings, srv *settings.Server) (*users.User, error)
}
