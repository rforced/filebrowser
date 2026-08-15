package bolt

import (
	bbolt "go.etcd.io/bbolt"

	"github.com/rforced/filebrowser/v2/settings"
)

type settingsBackend struct {
	db *bbolt.DB
}

func (s settingsBackend) Get() (*settings.Settings, error) {
	set := &settings.Settings{}
	return set, getConfig(s.db, "settings", set)
}

func (s settingsBackend) Save(set *settings.Settings) error {
	return saveConfig(s.db, "settings", set)
}

func (s settingsBackend) GetServer() (*settings.Server, error) {
	server := &settings.Server{}
	return server, getConfig(s.db, "server", server)
}

func (s settingsBackend) SaveServer(server *settings.Server) error {
	return saveConfig(s.db, "server", server)
}
