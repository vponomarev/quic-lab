package admin

import (
	"database/sql"
	"os"
	"path/filepath"
	"quiclab/internal/backup"
	"quiclab/internal/cellgeo"
	"strings"
	"testing"
)

func TestBackupRejectsInvalidCellDatabase(t *testing.T) {
	for _, kind := range []string{"corrupt", "future", "missing-table"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "data/cellgeo/cells.sqlite")
			store, err := cellgeo.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			if err = store.Close(); err != nil {
				t.Fatal(err)
			}
			if kind == "corrupt" {
				if err = os.WriteFile(path, []byte("not a SQLite database"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				db, err := sql.Open("sqlite", path)
				if err != nil {
					t.Fatal(err)
				}
				statement := "PRAGMA user_version=999"
				if kind == "missing-table" {
					statement = "DROP TABLE history"
				}
				_, err = db.Exec(statement)
				db.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			before, _ := os.ReadFile(path)
			err = ValidateBackup(&backup.Verified{Dir: dir}, false)
			if err == nil || !strings.Contains(err.Error(), "cell database") {
				t.Fatalf("expected cell database validation failure, got %v", err)
			}
			after, _ := os.ReadFile(path)
			if string(before) != string(after) {
				t.Fatal("validation mutated database")
			}
		})
	}
}
