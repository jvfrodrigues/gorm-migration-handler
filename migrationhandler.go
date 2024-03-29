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

type templateStruct struct {
	MigrationSQL string
}

const migrationTemplate string = `-- Write your SQL command here
{{.MigrationSQL}}`

type database struct {
	Db *gorm.DB
}

// DBConfig gets the gorm dialector to connect to the database, the models in the project and your migrations folder path
type DBConfig struct {
	Dialector            gorm.Dialector
	Models               []interface{}
	MigrationsFolderPath string
}

type migration struct {
	id           string
	name         string
	migrationSQL string
	rollbackSQL  string
}

// CreateMigration requires the dbConfig and your migration folder path and the name of the migration you want to create
func CreateMigration(databaseConfig DBConfig, migrationName string) error {
	newMigration := migration{
		id:   fmt.Sprint(time.Now().Unix()),
		name: migrationName,
	}
	db, err := newDatabase(databaseConfig)
	if err != nil {
		fmt.Println("Database connection failed skipping auto migration")
	} else {
		migrationSQL := getChangesAuto(db, databaseConfig.Models)
		if migrationSQL == "" {
			fmt.Println("No auto changes found.")
		}
		newMigration.migrationSQL = migrationSQL
	}
	err = generateFiles(newMigration, databaseConfig.MigrationsFolderPath)
	if err != nil {
		return err
	}
	fmt.Printf("Migration '%s' created successfully.\n", newMigration.name)
	return nil
}

// RunMigrations gets DB info and gets all migrations from given folder to run on the database
func RunMigrations(dbConfig DBConfig) error {
	manager, err := setupManager(dbConfig)
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
func RollbackMigration(dbConfig DBConfig) error {
	manager, err := setupManager(dbConfig)
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

func setupManager(dbConfig DBConfig) (*gormigrate.Gormigrate, error) {
	db, err := newDatabase(dbConfig)
	if err != nil {
		return nil, errors.New("connection to database failed, can not run migrations")
	}
	migrations, err := getMigrations(dbConfig.MigrationsFolderPath)
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

func setupMigration(migration migration) *gormigrate.Migration {
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

func getMigrations(path string) (map[string]migration, error) {
	migrations := make(map[string]migration)
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
		return nil, err
	}
	for _, file := range files {
		var foundMigration migration
		if file.IsDir() {
			continue
		}
		fileName := file.Name()
		filePath := path + "/" + fileName
		content, err := os.ReadFile(filePath)
		if err != nil {
			fmt.Printf("Error reading file %s: %v\n", fileName, err)
			continue
		}
		splitName := strings.Split(file.Name(), "_")
		migrationID := splitName[0]
		migrationName := strings.Join(splitName[:2], "_")
		foundMigration = migrations[migrationName]
		foundMigration.id = migrationID
		if migrationsFilter.MatchString(fileName) {
			foundMigration.migrationSQL = string(content)
		} else if rollbackFilter.MatchString(fileName) {
			foundMigration.rollbackSQL = string(content)
		} else {
			continue
		}
		migrations[migrationName] = foundMigration
	}
	return migrations, nil
}

func newDatabase(dbConfig DBConfig) (*database, error) {
	db, err := gorm.Open(dbConfig.Dialector, &gorm.Config{
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
	_ = w.Close()
	os.Stdout = originalOut
	scanner := bufio.NewScanner(r)
	lines := ""
	for scanner.Scan() {
		text := scanner.Text()
		if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(text)), "SELECT") {
			lines += text + "\n"
		}
	}
	_ = r.Close()
	return lines
}

func generateFiles(migration migration, folderPath string) error {
	_, err := os.ReadDir(folderPath)
	if err != nil {
		return fmt.Errorf("could not find dir %s", folderPath)
	}
	migrationFileName := fmt.Sprintf("%s/%s_%s_up.sql", folderPath, migration.id, migration.name)
	rollbackFileName := fmt.Sprintf("%s/%s_%s_down.sql", folderPath, migration.id, migration.name)
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
	data := &templateStruct{
		MigrationSQL: migration.migrationSQL,
	}
	err = tmpl.Execute(migrationFile, data)
	if err != nil {
		return err
	}
	data.MigrationSQL = migration.rollbackSQL
	err = tmpl.Execute(rollbackFile, data)
	if err != nil {
		return err
	}
	return nil
}
