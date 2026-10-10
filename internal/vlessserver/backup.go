package vlessserver

import (
	"encoding/json"
	"errors"
	"regexp"
)

var backupUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func ValidateBackupState(raw []byte) error {
	if len(raw) > 1<<20 {
		return errors.New("VLESS backup state too large")
	}
	var record workerRecord
	if json.Unmarshal(raw, &record) != nil || record.Revoked == nil {
		return errors.New("invalid VLESS backup state")
	}
	if _, e := normalizeSnapshot(record.Desired); e != nil {
		return errors.New("invalid VLESS backup desired state")
	}
	for id, revoked := range record.Revoked {
		if !backupUUID.MatchString(id) || !revoked {
			return errors.New("invalid VLESS backup revocation")
		}
	}
	return nil
}
