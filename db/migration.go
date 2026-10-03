package db

import (
	"embed"
	_ "github.com/lib/pq"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source"
	"github.com/golang-migrate/migrate/v4/source/httpfs"
	_ "github.com/golang-migrate/migrate/v4/source/httpfs"
	"github.com/weeb-vip/notifications-service/config"
	"github.com/weeb-vip/notifications-service/internal/db"
	"log"
	"net/http"
)

var (
	//go:embed migrations/*.sql
	migrations embed.FS
)

type driver struct {
	httpfs.PartialDriver
}

func (d *driver) Open(rawURL string) (source.Driver, error) {
	err := d.PartialDriver.Init(http.FS(migrations), "migrations")
	if err != nil {
		return nil, err
	}

	return d, nil
}
func getMigration() (*migrate.Migrate, error) {
	cfg := config.LoadConfigOrPanic()
	database := db.NewDatabase(cfg.DBConfig)
	sqldb, err := database.DB.DB()
	if err != nil {
		return nil, err
	}
	dbdriver, err := postgres.WithInstance(sqldb, &postgres.Config{
		MigrationsTable: cfg.DBConfig.MigrationTableName,
	})
	// log files in migrations folder
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		println(file.Name())
	}

	source.Register("embed", &driver{})

	return migrate.NewWithDatabaseInstance("embed://", cfg.DBConfig.DataBase, dbdriver)
}

func MigrateUp() error {
	log.Println("Migrating up")
	m, err := getMigration()
	if err != nil {
		return err
	}

	err = m.Up()
	if err != nil && err != migrate.ErrNoChange {
		return err
	}

	return nil
}

func MigrateDown() error {
	m, err := getMigration()
	if err != nil {
		return err
	}

	return m.Down()
}
