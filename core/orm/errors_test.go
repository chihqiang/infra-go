package orm_test

import (
	"errors"
	"testing"

	"github.com/chihqiang/infra-go/core/orm"
	"gorm.io/gorm"
)

type helperUser struct {
	ID    uint   `gorm:"primaryKey"`
	Email string `gorm:"size:64;uniqueIndex"`
}

type helperOrder struct {
	ID     uint `gorm:"primaryKey"`
	UserID uint `gorm:"not null;index"`
	User   *helperUser
}

func TestIsDuplicatedKey(t *testing.T) {
	db := orm.MustNewSQLite(orm.Config{})
	defer func() { _ = orm.Close(db) }()
	if err := db.AutoMigrate(&helperUser{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if err := db.Create(&helperUser{Email: "a@test.local"}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	err := db.Create(&helperUser{Email: "a@test.local"}).Error
	if err == nil {
		t.Fatal("expected duplicate error")
	}
	if !orm.IsDuplicatedKey(err) {
		t.Fatalf("IsDuplicatedKey(%v) = false", err)
	}
	if orm.IsDuplicatedKey(nil) || orm.IsDuplicatedKey(errors.New("boom")) {
		t.Error("unexpected match for non-constraint errors")
	}
}

func TestIsDuplicatedKeyWithTranslation(t *testing.T) {
	db := orm.MustNewSQLite(orm.Config{TranslateError: true})
	defer func() { _ = orm.Close(db) }()
	if err := db.AutoMigrate(&helperUser{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Create(&helperUser{Email: "b@test.local"}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	err := db.Create(&helperUser{Email: "b@test.local"}).Error
	if !errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Fatalf("error = %v, want gorm.ErrDuplicatedKey", err)
	}
	if !orm.IsDuplicatedKey(err) {
		t.Fatalf("IsDuplicatedKey(%v) = false", err)
	}
}

func TestIsForeignKeyViolation(t *testing.T) {
	db := orm.MustNewSQLite(orm.Config{})
	defer func() { _ = orm.Close(db) }()
	if err := db.AutoMigrate(&helperUser{}, &helperOrder{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// SQLite enforces foreign keys only when the pragma is on.
	if err := db.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}

	err := db.Create(&helperOrder{UserID: 999}).Error
	if err == nil {
		t.Skip("sqlite accepted the orphan row; foreign keys not enforced")
	}
	if !orm.IsForeignKeyViolation(err) {
		t.Fatalf("IsForeignKeyViolation(%v) = false", err)
	}
	if orm.IsDuplicatedKey(err) {
		t.Error("foreign key error matched duplicated key")
	}
}
