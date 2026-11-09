package cellgeo

import (
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
)

// ValidateBackup checks an existing snapshot without creating or migrating it.
func ValidateBackup(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("invalid cell database file")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs), RawQuery: "mode=ro&immutable=1"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var integrity string
	if err = db.QueryRow("PRAGMA quick_check").Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return errors.New("corrupt cell database")
	}
	var version int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version != 1 {
		return errors.New("unsupported cell database version")
	}
	for _, query := range []string{
		"SELECT id,name,version,url,license,priority,enabled FROM sources LIMIT 0",
		"SELECT key,identity FROM cells LIMIT 0",
		"SELECT id,cell,source,source_version,fetched_at,status,lat,lon,radius,error FROM history LIMIT 0",
		"SELECT cell,source,priority,state,attempts,next_at FROM jobs LIMIT 0",
		"SELECT key,value FROM metadata LIMIT 0",
	} {
		rows, err := db.Query(query)
		if err != nil {
			return err
		}
		if err = rows.Close(); err != nil {
			return err
		}
	}
	return nil
}
