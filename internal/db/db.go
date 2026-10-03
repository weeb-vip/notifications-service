package db

import (
	"fmt"
	"github.com/weeb-vip/notifications-service/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"time"
)

type DB struct {
	DB *gorm.DB
}

func NewDatabase(cfg config.DBConfig) *DB {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s", cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.DataBase, cfg.SSLMode)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		panic("failed to connect database")
	}

	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to get database connection")
	}

	// The pool is sized to be reused rather than refilled.
	//
	// MaxIdleConns matches MaxOpenConns deliberately: Go only retains up to
	// MaxIdleConns, so anything opened above it is closed again the moment the
	// query finishes. The previous settings kept far fewer idle than they were
	// willing to open, which meant most connections were built per-query --
	// TCP, TLS and auth each time, against RDS over the internet.
	//
	// 5 is small because the database is small. db.t4g.micro allows 79
	// connections in total, and roughly 36 pods share them; a warm pool of 5
	// serves more traffic than a churning pool of 25 while claiming a fraction
	// of that budget.
	sqlDB.SetMaxOpenConns(5)
	sqlDB.SetMaxIdleConns(5)

	// Connections are kept for hours because building one is expensive here.
	//
	// Measured against the production RDS instance: opening a connection costs
	// 6-52 seconds, while queries on an already-open connection cost about 7ms.
	// The gap is TLS and SCRAM authentication, both CPU-bound, on a db.t4g.micro
	// whose CPU credit balance sits at zero.
	//
	// The old 10 minute idle timeout emptied the pool during any quiet period,
	// so the first request after a lull paid that cost. This service showed the
	// same cold-start curve as anime-api -- 8.4s falling to 0.22s over
	// successive calls -- because the bottleneck is the shared database, not
	// anything either service does.
	//
	// It also preserves the statement cache: pgx v5 caches prepared statements
	// per connection, so closing one discards its plans as well.
	//
	// Four hours rather than never, so a failover or DNS change is still picked
	// up without needing a restart.
	sqlDB.SetConnMaxLifetime(4 * time.Hour)
	sqlDB.SetConnMaxIdleTime(1 * time.Hour)

	// Add tracing plugin
	err = db.Use(&TracingPlugin{})
	if err != nil {
		panic("failed to register tracing plugin")
	}

	return &DB{DB: db}
}
