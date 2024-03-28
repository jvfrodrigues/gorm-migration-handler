package migrationhandler_test

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	migrationhandler "github.com/jvfrodrigues/gorm-migration-handler"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func tempDir(t *testing.T) string {
	dir, err := os.MkdirTemp("./", "test_migrations")
	if err != nil {
		t.Fatalf("test error: %v", err)
	}
	return dir
}

func TestCreateMigration(t *testing.T) {
	migrationsFilter, err := regexp.Compile(`^\d+.*_up.sql$`)
	if err != nil {
		t.Fatalf("test error: %v", err)
	}
	dialector := sqlite.Open("file::memory:?cache=shared")
	dir := tempDir(t)
	defer func() {
		_ = os.RemoveAll(dir)
	}()
	if err != nil {
		t.Fatalf("test error: %v", err)
	}
	testMigration := migrationhandler.Migration{
		FolderPath: "./" + dir,
		Name:       "test",
	}
	tests := []struct {
		name                   string
		dbConfig               migrationhandler.DBConfig
		migration              migrationhandler.Migration
		expectedMigrationLines int
		expectedError          error
	}{
		{
			name: "Test if no models available, empty migration files are created",
			dbConfig: migrationhandler.DBConfig{
				Dialector: dialector,
				Models:    []interface{}{},
			},
			migration:              testMigration,
			expectedMigrationLines: 0,
			expectedError:          nil,
		},
		{
			name: "Test if there are models, migration up file has the auto sql command",
			dbConfig: migrationhandler.DBConfig{
				Dialector: dialector,
				Models: []interface{}{
					struct {
						Name string
						Age  int
					}{
						Name: "Alice",
						Age:  30,
					},
				},
			},
			migration:              testMigration,
			expectedMigrationLines: 1,
			expectedError:          nil,
		},
		{
			name: "Test if it errors on non existing folder",
			dbConfig: migrationhandler.DBConfig{
				Dialector: dialector,
			},
			migration: migrationhandler.Migration{
				FolderPath: "./non-existing-dir",
				Name:       "test",
			},
			expectedMigrationLines: 0,
			expectedError:          fmt.Errorf("could not find dir ./non-existing-dir"),
		},
		{
			name: "Test if it can't access database, empty migration files are created",
			dbConfig: migrationhandler.DBConfig{
				Dialector: mysql.New(mysql.Config{
					DriverName: "my_mysql_driver",
					DSN:        "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8&parseTime=True&loc=Local", // data source name, refer https://github.com/go-sql-driver/mysql#dsn-data-source-name
				}),
			},
			migration:              testMigration,
			expectedMigrationLines: 0,
			expectedError:          nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := migrationhandler.CreateMigration(tc.dbConfig, tc.migration)
			if err != nil && tc.expectedError != nil {
				if err.Error() != tc.expectedError.Error() {
					t.Errorf("expected: %+v, got: %+v", tc.expectedError, err)
				}
				return
			}
			dirFiles, err := os.ReadDir(tc.migration.FolderPath)
			if err != nil {
				t.Fatalf("test error: %v", err)
			}
			if len(dirFiles) != 2 {
				t.Errorf("expected 2 files on folder got %v", len(dirFiles))
			}
			var migration []byte
			for _, entry := range dirFiles {
				if entry.IsDir() || !migrationsFilter.MatchString(entry.Name()) {
					continue
				}
				migration, err = os.ReadFile(tc.migration.FolderPath + "/" + entry.Name())
				if err != nil {
					t.Fatalf("test error: %v", err)
				}
			}
			str := string(migration)
			lines := strings.Split(str, "\n")
			lineQuant := len(lines) - 2
			if lineQuant != tc.expectedMigrationLines {
				t.Errorf("expected migration lines to be: %v, got: %v", tc.expectedMigrationLines, lineQuant)
			}
		})
	}
}

func TestRunMigrations(t *testing.T) {
	dialector := sqlite.Open("file::memory:?cache=shared")
	tests := []struct {
		name                string
		dbConfig            migrationhandler.DBConfig
		migrationsToRun     []migrationhandler.Migration
		migrationFolderName string
		expectedError       error
	}{
		{
			name:     "Test if migrations run successfully",
			dbConfig: migrationhandler.DBConfig{Dialector: dialector},
			migrationsToRun: []migrationhandler.Migration{
				{
					Name: "test",
				},
			},
			migrationFolderName: tempDir(t),
			expectedError:       nil,
		},
		{
			name: "Test if it errors on no connection to database",
			dbConfig: migrationhandler.DBConfig{Dialector: mysql.New(mysql.Config{
				DriverName: "my_mysql_driver",
				DSN:        "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8&parseTime=True&loc=Local", // data source name, refer https://github.com/go-sql-driver/mysql#dsn-data-source-name
			})},
			migrationsToRun:     []migrationhandler.Migration{},
			migrationFolderName: tempDir(t),
			expectedError:       errors.New("connection to database failed, can not run migrations"),
		},
		{
			name:                "Test if it errors on non existing migration folder",
			dbConfig:            migrationhandler.DBConfig{Dialector: dialector},
			migrationsToRun:     []migrationhandler.Migration{},
			migrationFolderName: "non-existing-folder",
			expectedError:       errors.New("open ./non-existing-folder: no such file or directory"),
		},
		{
			name:                "Test if it errors on no migrations to run",
			dbConfig:            migrationhandler.DBConfig{Dialector: dialector},
			migrationsToRun:     []migrationhandler.Migration{},
			migrationFolderName: tempDir(t),
			expectedError:       errors.New("no migrations to run"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				_ = os.RemoveAll(tc.migrationFolderName)
			}()
			for _, migration := range tc.migrationsToRun {
				migration.FolderPath = "./" + tc.migrationFolderName
				err := migrationhandler.CreateMigration(tc.dbConfig, migration)
				if err != nil {
					t.Fatalf("test error: %v", err)
				}
			}
			err := migrationhandler.RunMigrations(tc.dbConfig, "./"+tc.migrationFolderName)
			if err != nil && tc.expectedError != nil {
				if err.Error() != tc.expectedError.Error() {
					t.Errorf("expected: %+v, got: %+v", tc.expectedError, err)
				}
				return
			}
			db, err := gorm.Open(tc.dbConfig.Dialector, &gorm.Config{
				SkipDefaultTransaction: true,
				Logger:                 logger.Default.LogMode(logger.Silent),
			})
			if err != nil {
				t.Fatalf("test error: %v", err)
			}
			var count int64
			db.Table("migrations").Count(&count)
			if count != int64(len(tc.migrationsToRun)) {
				t.Errorf("expected: %+v, got: %+v", len(tc.migrationsToRun), count)
			}
		})
	}
}

func beforeEachRollback(t *testing.T, dialector gorm.Dialector) string {
	dir := tempDir(t)
	dbconfig := migrationhandler.DBConfig{Dialector: dialector}
	err := migrationhandler.CreateMigration(
		dbconfig,
		migrationhandler.Migration{
			Name:       "test",
			FolderPath: "./" + dir,
		},
	)
	if err != nil {
		t.Fatalf("test error: %v", err)
	}
	err = migrationhandler.RunMigrations(dbconfig, "./"+dir)
	if err != nil {
		t.Fatalf("test error: %v", err)
	}
	return dir
}

func TestRollbackMigrations(t *testing.T) {
	dialector := sqlite.Open("file::memory:?cache=shared")
	tests := []struct {
		name                string
		dbConfig            migrationhandler.DBConfig
		migrationFolderName string
		expectedError       error
	}{
		{
			name:                "Test if migrations rollback successfully",
			dbConfig:            migrationhandler.DBConfig{Dialector: dialector},
			migrationFolderName: beforeEachRollback(t, dialector),
			expectedError:       nil,
		},
		{
			name: "Test if it errors on no connection to database",
			dbConfig: migrationhandler.DBConfig{Dialector: mysql.New(mysql.Config{
				DriverName: "my_mysql_driver",
				DSN:        "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8&parseTime=True&loc=Local", // data source name, refer https://github.com/go-sql-driver/mysql#dsn-data-source-name
			})},
			migrationFolderName: beforeEachRollback(t, dialector),
			expectedError:       errors.New("connection to database failed, can not run migrations"),
		},
		{
			name:                "Test if it errors on non existing migration folder",
			dbConfig:            migrationhandler.DBConfig{Dialector: dialector},
			migrationFolderName: "non-existing-folder",
			expectedError:       errors.New("open ./non-existing-folder: no such file or directory"),
		},
		{
			name:                "Test if it errors on no migrations to rollback",
			dbConfig:            migrationhandler.DBConfig{Dialector: dialector},
			migrationFolderName: beforeEachRollback(t, dialector),
			expectedError:       errors.New("gormigrate: Could not find last run migration"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				_ = os.RemoveAll(tc.migrationFolderName)
			}()
			err := migrationhandler.RollbackMigration(tc.dbConfig, "./"+tc.migrationFolderName)
			if err != nil && tc.expectedError != nil {
				if err.Error() != tc.expectedError.Error() {
					t.Errorf("expected: %+v, got: %+v", tc.expectedError, err)
				}
				return
			}
			db, err := gorm.Open(tc.dbConfig.Dialector, &gorm.Config{
				SkipDefaultTransaction: true,
				Logger:                 logger.Default.LogMode(logger.Silent),
			})
			if err != nil {
				t.Fatalf("test error: %v", err)
			}
			var count int64
			db.Table("migrations").Count(&count)
			if count != 0 {
				t.Errorf("expected: %+v, got: %+v", 0, count)
			}
		})
	}
}
