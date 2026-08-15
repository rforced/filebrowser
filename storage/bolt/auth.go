package bolt

import (
	bbolt "go.etcd.io/bbolt"

	"github.com/rforced/filebrowser/v2/auth"
	fberrors "github.com/rforced/filebrowser/v2/errors"
	"github.com/rforced/filebrowser/v2/settings"
)

type authBackend struct {
	db *bbolt.DB
}

func (s authBackend) Get(t settings.AuthMethod) (auth.Auther, error) {
	var auther auth.Auther

	switch t {
	case auth.MethodJSONAuth:
		auther = &auth.JSONAuth{}
	case auth.MethodHookAuth:
		auther = &auth.HookAuth{}
	default:
		return nil, fberrors.ErrInvalidAuthMethod
	}

	return auther, getConfig(s.db, "auther", auther)
}

func (s authBackend) Save(a auth.Auther) error {
	return saveConfig(s.db, "auther", a)
}
