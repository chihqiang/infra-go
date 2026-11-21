package orm

import (
	"errors"
	"strings"

	"gorm.io/gorm"
)

// Constraint helpers.
//
// GORM only returns gorm.ErrDuplicatedKey / gorm.ErrForeignKeyViolated when
// Config.TranslateError is enabled, and when it is the driver message is
// replaced by the sentinel. These helpers accept both shapes, so callers can
// detect a constraint violation whether or not translation is enabled.

var (
	duplicatedKeyFragments = []string{
		"duplicate entry",          // MySQL
		"unique constraint failed", // SQLite
		"duplicate key value",      // PostgreSQL, SQL Server
		"unique constraint",        // generic
	}
	foreignKeyFragments = []string{
		"foreign key constraint", // MySQL, SQLite
		"violates foreign key",   // PostgreSQL
		"foreign key",
	}
)

// IsDuplicatedKey reports whether err is a unique constraint violation.
func IsDuplicatedKey(err error) bool {
	return matchesConstraint(err, gorm.ErrDuplicatedKey, duplicatedKeyFragments)
}

// IsForeignKeyViolation reports whether err is a foreign key constraint violation.
func IsForeignKeyViolation(err error) bool {
	return matchesConstraint(err, gorm.ErrForeignKeyViolated, foreignKeyFragments)
}

func matchesConstraint(err, sentinel error, fragments []string) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, sentinel) {
		return true
	}

	// Driver messages are matched case-insensitively; translation is optional so
	// the raw driver error may also arrive wrapped by the caller.
	message := strings.ToLower(err.Error())
	for _, fragment := range fragments {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}
