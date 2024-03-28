// Package migrationhandler has functions related to the creation of migrations
// based on the GORM package
package migrationhandler

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const migrationTemplate string = `-- Write your SQL command here
{{.MigrationSQL}}`

type database struct {
	Db *gorm.DB
}

// DBConfig gets the gorm dialector to connection to the database and the models in it
type DBConfig struct {
	Dialector gorm.Dialector
	Models    []interface{}
}

// Migration gets name of the migration and the path to the migrations folder on the project
type Migration struct {
	FolderPath   string
	Name         string
	id           string
	migrationSQL string
	rollbackSQL  string
}

// CreateMigration receives the database and migration information and creates the requested migration
func CreateMigration(database DBConfig, migration Migration) error {
	migration.id = fmt.Sprint(time.Now().Unix())
	db, err := newDatabase(database)
	if err != nil {
		fmt.Println("Database connection failed skipping auto migration")
	} else {
		migrationSQL := getChangesAuto(db, database.Models)
		if migrationSQL == "" {
			fmt.Println("No auto changes found.")
		}
		migration.migrationSQL = migrationSQL
	}
	err = generateMigrationFile(migration)
	if err != nil {
		return err
	}
	fmt.Printf("Migration '%s' created successfully.\n", migration.Name)
	return nil
}

// RunMigrations gets DB info and gets all migrations from given folder to run on the database
func RunMigrations(connection DBConfig, migrationFolderPath string) error {
	manager, err := setupManager(connection, migrationFolderPath)
	if err != nil {
		return err
	}
	err = manager.Migrate()
	if err != nil {
		return err
	}
	fmt.Println("Migrations successful")
	return nil
}

// RollbackMigration gets DB info and gets migration folder to find and rollback the latest migration
func RollbackMigration(connection DBConfig, migrationFolderPath string) error {
	manager, err := setupManager(connection, migrationFolderPath)
	if err != nil {
		return err
	}
	err = manager.RollbackLast()
	if err != nil {
		return err
	}
	fmt.Println("Rollback successful")
	return nil
}

func setupManager(connection DBConfig, path string) (*gormigrate.Gormigrate, error) {
	db, err := newDatabase(connection)
	if err != nil {
		return nil, errors.New("connection to database failed, can not run migrations")
	}
	migrations, err := getMigrations(path)
	if err != nil {
		return nil, err
	}
	if len(migrations) <= 0 {
		return nil, errors.New("no migrations to run")
	}
	gormMigrations := make([]*gormigrate.Migration, 0)
	for _, migration := range migrations {
		gormMigrations = append(gormMigrations, setupMigration(migration))
	}
	gm := gormigrate.New(db.Db, gormigrate.DefaultOptions, gormMigrations)
	return gm, nil
}

func setupMigration(migration Migration) *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: migration.id,
		Migrate: func(db *gorm.DB) error {
			tx := db.Begin()
			defer tx.Rollback()
			err := tx.Exec(migration.migrationSQL).Error
			if err != nil {
				return err
			}
			return tx.Commit().Error
		},
		Rollback: func(db *gorm.DB) error {
			tx := db.Begin()
			defer tx.Rollback()
			err := tx.Exec(migration.rollbackSQL).Error
			if err != nil {
				return err
			}
			return tx.Commit().Error
		},
	}
}

func getMigrations(path string) (map[string]Migration, error) {
	migrations := make(map[string]Migration)
	migrationsFilter, err := regexp.Compile(`^\d+.*_up.sql$`)
	if err != nil {
		return nil, err
	}
	rollbackFilter, err := regexp.Compile(`^\d+.*_down.sql$`)
	if err != nil {
		return nil, err
	}
	files, err := os.ReadDir(path)
	if err != nil {
		fmt.Println("Error reading folder:", err)
		return nil, err
	}
	for _, file := range files {
		var migration Migration
		if file.IsDir() {
			continue
		}
		migrationID := strings.Split(file.Name(), "_")[0]
		filePath := path + "/" + file.Name()
		content, err := os.ReadFile(filePath)
		if err != nil {
			fmt.Printf("Error reading file %s: %v\n", file.Name(), err)
			continue
		}
		migration = migrations[migrationID]
		migration.id = migrationID
		if migrationsFilter.MatchString(file.Name()) {
			migration.migrationSQL = string(content)
		} else if rollbackFilter.MatchString(file.Name()) {
			migration.rollbackSQL = string(content)
		} else {
			continue
		}
		migrations[migrationID] = migration
	}
	return migrations, nil
}

func newDatabase(connection DBConfig) (*database, error) {
	db, err := gorm.Open(connection.Dialector, &gorm.Config{
		SkipDefaultTransaction: true,
		Logger:                 logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}
	database := database{
		db,
	}
	return &database, nil
}

func getChangesAuto(db *database, models []interface{}) string {
	originalOut := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	_ = db.Db.Session(&gorm.Session{DryRun: true}).AutoMigrate(models...)
	err := w.Close()
	if err != nil {
		fmt.Println("Error:", err)
	}
	os.Stdout = originalOut
	scanner := bufio.NewScanner(r)
	lines := ""
	for scanner.Scan() {
		text := scanner.Text()
		if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(text)), "SELECT") {
			lines += text + "\n"
		}
	}
	err = r.Close()
	if err != nil {
		fmt.Println("Error:", err)
	}
	return lines
}

func generateMigrationFile(migration Migration) error {
	_, err := os.ReadDir(migration.FolderPath)
	if err != nil {
		return fmt.Errorf("could not find dir %s", migration.FolderPath)
	}
	migrationFileName := fmt.Sprintf("%s/%s_%s_up.sql", migration.FolderPath, migration.id, migration.Name)
	rollbackFileName := fmt.Sprintf("%s/%s_%s_down.sql", migration.FolderPath, migration.id, migration.Name)
	// Create files
	migrationFile, err := os.Create(migrationFileName)
	if err != nil {
		return err
	}
	rollbackFile, err := os.Create(rollbackFileName)
	if err != nil {
		return err
	}
	defer func() {
		_ = migrationFile.Close()
		_ = rollbackFile.Close()
	}()
	// Parse and execute template
	tmpl, err := template.New("migration").Parse(migrationTemplate)
	if err != nil {
		return err
	}
	data := struct {
		MigrationSQL string
	}{
		MigrationSQL: migration.migrationSQL,
	}
	err = tmpl.Execute(migrationFile, data)
	if err != nil {
		return err
	}
	data = struct {
		MigrationSQL string
	}{
		MigrationSQL: migration.rollbackSQL,
	}
	err = tmpl.Execute(rollbackFile, data)
	if err != nil {
		return err
	}
	return nil
}
