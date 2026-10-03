package notifications

import (
	"database/sql/driver"
	"fmt"
)

// JSON is a jsonb column value sent as text, because database/sql hands a
// []byte to the driver as bytea and Postgres refuses bytea for jsonb.
type JSON []byte

// Value implements driver.Valuer.
func (j JSON) Value() (driver.Value, error) {
	if len(j) == 0 {
		return "{}", nil
	}

	return string(j), nil
}

// Scan implements sql.Scanner.
func (j *JSON) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*j = nil
	case []byte:
		*j = append(JSON(nil), v...)
	case string:
		*j = JSON(v)
	default:
		return fmt.Errorf("notifications: cannot scan %T into JSON", src)
	}

	return nil
}
