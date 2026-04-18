// Package dbfactory constructs db.Driver instances from config.Environment.
// It lives in a separate package (not internal/db) to avoid an import cycle:
// internal/db/oracle (and other driver sub-packages) already import internal/db
// for the Driver interface, so internal/db cannot import them in return.
package dbfactory

import (
	"fmt"

	"github.com/nilm987521/adt/internal/config"
	"github.com/nilm987521/adt/internal/db"
	"github.com/nilm987521/adt/internal/db/mssql"
	"github.com/nilm987521/adt/internal/db/mysql"
	"github.com/nilm987521/adt/internal/db/oracle"
	"github.com/nilm987521/adt/internal/db/postgres"
	"github.com/nilm987521/adt/internal/db/sqlite"
)

// NewDriver constructs the appropriate Driver for the given environment.
// password is provided separately (fetched from keyring by the caller).
func NewDriver(env *config.Environment, password string) (db.Driver, error) {
	switch env.EffectiveDriver() {
	case "oracle":
		return oracle.New(env.User, password, env.Host, env.Port, env.Service)
	case "postgres":
		return postgres.New(env.User, password, env.Host, env.Port, env.Database)
	case "mysql":
		return mysql.New(env.User, password, env.Host, env.Port, env.Database)
	case "mssql":
		return mssql.New(env.User, password, env.Host, env.Port, env.Database)
	case "sqlite":
		return sqlite.New(env.Database)
	default:
		return nil, fmt.Errorf("unsupported driver %q", env.EffectiveDriver())
	}
}
