package config

import (
	"github.com/jinzhu/configor"
)

type Config struct {
	AppConfig     AppConfig `env:"APP_CONFIG"`
	DBConfig      DBConfig
	DataDogConfig DataDogConfig
	NatsConfig    NatsConfig
	UserService   UserServiceConfig
}

type AppConfig struct {
	APPName string `default:"notifications-service"`
	Port    int    `env:"PORT" default:"3000"`
	Version string `default:"x.x.x" env:"VERSION"`
	Env     string `default:"development" env:"ENV"`
}

type DBConfig struct {
	Host               string `default:"localhost" env:"DBHOST"`
	DataBase           string `default:"weeb" env:"DBNAME"`
	User               string `default:"weeb" env:"DBUSERNAME"`
	Password           string `required:"true" env:"DBPASSWORD" default:"mysecretpassword"`
	Port               uint   `default:"5432" env:"DBPORT"`
	SSLMode            string `default:"require" env:"DBSSL"`
	MigrationTableName string `env:"DBMIGRATIONTABLE" default:"__migrations_notifications-service"`
}

type DataDogConfig struct {
	DD_AGENT_HOST string `env:"DD_AGENT_HOST" default:"localhost"`
	DD_AGENT_PORT int    `env:"DD_AGENT_PORT" default:"8125"`
}

// NatsConfig covers both consumers and the delivery publisher. Subject names
// are fixed by the producers (user-service, list-service) and by the delivery
// workers, so they are constants in internal/consumers rather than settings.
type NatsConfig struct {
	URL string `default:"nats://localhost:4222" env:"NATSURL"`
	// Durable consumer name prefix; each consumer appends its subject.
	ConsumerGroupName string `default:"notifications-service" env:"NATSCONSUMERGROUPNAME"`
	Offset            string `default:"earliest" env:"NATSOFFSET"`
	// Empty: the subjects are produced by other services' outbox relays, so
	// the driver creates one stream per subject rather than binding to a
	// CDC stream.
	StreamName string `env:"NATSSTREAMNAME"`
}

// UserServiceConfig is where the consumers ask about users: the subgraph's
// own URL, not the gateway, because no user is signed in for these calls.
type UserServiceConfig struct {
	URL string `default:"http://localhost:3002/graphql" env:"USER_SERVICE_URL"`
}

func LoadConfigOrPanic() Config {
	var config = Config{}
	// Try to load config file, but don't fail if it doesn't exist
	// All important config should come from environment variables anyway
	configor.Load(&config)

	return config
}
