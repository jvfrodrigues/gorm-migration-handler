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
	tests := []struct {
		name                   string
		dbConfig               migrationhandler.DBConfig
		expectedMigrationLines int
		expectedError          error
	}{
		{
			name: "Test if no models available, empty migration files are created",
			dbConfig: migrationhandler.DBConfig{
				Dialector:            dialector,
				MigrationsFolderPath: "./" + dir,
			},
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
				MigrationsFolderPath: "./" + dir,
			},
			expectedMigrationLines: 1,
			expectedError:          nil,
		},
		{
			name: "Test if it errors on non existing folder",
			dbConfig: migrationhandler.DBConfig{
				Dialector:            dialector,
				MigrationsFolderPath: "./non-existing-dir",
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
				MigrationsFolderPath: "./" + dir,
			},
			expectedMigrationLines: 0,
			expectedError:          nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := migrationhandler.CreateMigration(tc.dbConfig, "test")
			if err != nil && tc.expectedError != nil {
				if err.Error() != tc.expectedError.Error() {
					t.Errorf("expected: %+v, got: %+v", tc.expectedError, err)
				}
				return
			}
			dirFiles, err := os.ReadDir(tc.dbConfig.MigrationsFolderPath)
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
				migration, err = os.ReadFile(tc.dbConfig.MigrationsFolderPath + "/" + entry.Name())
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

func onEachRunMigrations(t *testing.T, dbConfig migrationhandler.DBConfig, migrationsToRun int) {
	for i := 0; i < migrationsToRun; i++ {
		err := migrationhandler.CreateMigration(dbConfig, fmt.Sprintf("test%v", i))
		if err != nil {
			t.Fatalf("test error: %v", err)
		}
	}
}

func TestRunMigrations(t *testing.T) {
	dialector := sqlite.Open("file::memory:?cache=shared")
	db, err := gorm.Open(dialector, &gorm.Config{
		SkipDefaultTransaction: true,
		Logger:                 logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("test error: %v", err)
	}
	tests := []struct {
		name            string
		dbConfig        migrationhandler.DBConfig
		migrationsToRun int
		expectedError   error
	}{
		{
			name: "Test if migrations run successfully",
			dbConfig: migrationhandler.DBConfig{
				Dialector:            dialector,
				MigrationsFolderPath: "./" + tempDir(t),
			},
			migrationsToRun: 1,
			expectedError:   nil,
		},
		{
			name: "Test if it errors on no connection to database",
			dbConfig: migrationhandler.DBConfig{Dialector: mysql.New(mysql.Config{
				DriverName: "my_mysql_driver",
				DSN:        "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8&parseTime=True&loc=Local", // data source name, refer https://github.com/go-sql-driver/mysql#dsn-data-source-name
			}),
				MigrationsFolderPath: "./" + tempDir(t),
			},
			migrationsToRun: 0,
			expectedError:   errors.New("connection to database failed, can not run migrations"),
		},
		{
			name: "Test if it errors on non existing migration folder",
			dbConfig: migrationhandler.DBConfig{
				Dialector:            dialector,
				MigrationsFolderPath: "./non-existing-folder",
			},
			migrationsToRun: 0,
			expectedError:   errors.New("open ./non-existing-folder: no such file or directory"),
		},
		{
			name: "Test if it errors on no migrations to run",
			dbConfig: migrationhandler.DBConfig{
				Dialector:            dialector,
				MigrationsFolderPath: "./" + tempDir(t),
			},
			migrationsToRun: 0,
			expectedError:   errors.New("no migrations to run"),
		},
		{
			name: "Test if it errors if there is more than one migration with the same ID",
			dbConfig: migrationhandler.DBConfig{
				Dialector:            dialector,
				MigrationsFolderPath: "./" + tempDir(t),
			},
			migrationsToRun: 2,
			expectedError:   errors.New("gormigrate: Duplicated migration ID"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				db.Exec("DROP TABLE 'migrations'")
				_ = os.RemoveAll(tc.dbConfig.MigrationsFolderPath)
			}()
			onEachRunMigrations(t, tc.dbConfig, tc.migrationsToRun)
			err := migrationhandler.RunMigrations(tc.dbConfig)
			if err != nil && tc.expectedError != nil {
				if !strings.Contains(err.Error(), tc.expectedError.Error()) {
					t.Errorf("expected: %+v, got: %+v", tc.expectedError, err)
				}
				return
			}
			var count int64
			db.Table("migrations").Count(&count)
			if count != int64(tc.migrationsToRun) {
				t.Errorf("expected: %+v, got: %+v", tc.migrationsToRun, count)
			}
		})
	}
}

func beforeEachRollback(t *testing.T, dialector gorm.Dialector) string {
	dir := tempDir(t)
	dbconfig := migrationhandler.DBConfig{
		Dialector:            dialector,
		MigrationsFolderPath: "./" + dir,
	}
	err := migrationhandler.CreateMigration(
		dbconfig,
		"test",
	)
	if err != nil {
		t.Fatalf("test error: %v", err)
	}
	err = migrationhandler.RunMigrations(dbconfig)
	if err != nil {
		t.Fatalf("test error: %v", err)
	}
	return dir
}

func TestRollbackMigrations(t *testing.T) {
	dialector := sqlite.Open("file::memory:?cache=shared")
	db, err := gorm.Open(dialector, &gorm.Config{
		SkipDefaultTransaction: true,
		Logger:                 logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("test error: %v", err)
	}
	tests := []struct {
		name          string
		dbConfig      migrationhandler.DBConfig
		expectedError error
	}{
		{
			name: "Test if migrations rollback successfully",
			dbConfig: migrationhandler.DBConfig{
				Dialector:            dialector,
				MigrationsFolderPath: beforeEachRollback(t, dialector),
			},
			expectedError: nil,
		},
		{
			name: "Test if it errors on no connection to database",
			dbConfig: migrationhandler.DBConfig{Dialector: mysql.New(mysql.Config{
				DriverName: "my_mysql_driver",
				DSN:        "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8&parseTime=True&loc=Local", // data source name, refer https://github.com/go-sql-driver/mysql#dsn-data-source-name
			}),
				MigrationsFolderPath: beforeEachRollback(t, dialector),
			},
			expectedError: errors.New("connection to database failed, can not run migrations"),
		},
		{
			name: "Test if it errors on non existing migration folder",
			dbConfig: migrationhandler.DBConfig{
				Dialector:            dialector,
				MigrationsFolderPath: "./non-existing-folder",
			},
			expectedError: errors.New("open ./non-existing-folder: no such file or directory"),
		},
		{
			name: "Test if it errors on no migrations to rollback",
			dbConfig: migrationhandler.DBConfig{
				Dialector:            dialector,
				MigrationsFolderPath: beforeEachRollback(t, dialector),
			},
			expectedError: errors.New("gormigrate: Could not find last run migration"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				_ = os.RemoveAll(tc.dbConfig.MigrationsFolderPath)
			}()
			err := migrationhandler.RollbackMigration(tc.dbConfig)
			if err != nil && tc.expectedError != nil {
				if err.Error() != tc.expectedError.Error() {
					t.Errorf("expected: %+v, got: %+v", tc.expectedError, err)
				}
				return
			}
			var count int64
			db.Table("migrations").Count(&count)
			if count != 0 {
				t.Errorf("expected: %+v, got: %+v", 0, count)
			}
		})
	}
}
