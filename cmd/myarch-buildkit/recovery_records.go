package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Recovery adapters validate the fields used for filesystem changes while the
// original map retains unknown fields from older and newer report producers.
// The on-disk report schema and existing field names remain unchanged.
type fileRecoveryRecord struct {
	Path           string  `json:"path"`
	Backup         string  `json:"backup"`
	Mode           *uint32 `json:"mode"`
	SHA256         string  `json:"sha256"`
	PendingSHA256  string  `json:"pending_sha256"`
	PreviousSHA256 string  `json:"previous_sha256"`
	OriginalSHA256 string  `json:"original_sha256"`
	JournalVersion int     `json:"write_journal_version"`
	Restored       bool    `json:"restored"`
}

func validRecoveryHash(value string) bool {
	if value == "" {
		return true
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func validatedFileRecoveryRecords(value any) ([]map[string]any, error) {
	var records []map[string]any
	switch items := value.(type) {
	case nil:
		return nil, nil
	case []map[string]any:
		records = items
	case []any:
		for _, item := range items {
			record, ok := item.(map[string]any)
			if !ok || record == nil {
				return nil, fmt.Errorf("invalid file recovery record in journal")
			}
			records = append(records, record)
		}
	default:
		return nil, fmt.Errorf("invalid file recovery journal: expected an array")
	}
	for _, record := range records {
		if _, err := parseFileRecoveryRecord(record); err != nil {
			return nil, err
		}
	}
	return records, nil
}

func parseFileRecoveryRecord(value map[string]any) (fileRecoveryRecord, error) {
	var record fileRecoveryRecord
	data, err := json.Marshal(value)
	if err == nil {
		err = json.Unmarshal(data, &record)
	}
	if err != nil {
		return record, fmt.Errorf("invalid file recovery record: %w", err)
	}
	if !filepath.IsAbs(record.Path) || filepath.Clean(record.Path) != record.Path || record.Backup != "" && (!filepath.IsAbs(record.Backup) || filepath.Clean(record.Backup) != record.Backup) {
		return record, fmt.Errorf("unsafe file recovery path: %s", record.Path)
	}
	if record.JournalVersion < 0 || record.JournalVersion > 2 || record.Mode != nil && *record.Mode & ^uint32(0777) != 0 {
		return record, fmt.Errorf("unsupported file recovery record: %s", record.Path)
	}
	for _, hash := range []string{record.SHA256, record.PendingSHA256, record.PreviousSHA256, record.OriginalSHA256} {
		if !validRecoveryHash(hash) {
			return record, fmt.Errorf("invalid recovery checksum: %s", record.Path)
		}
	}
	return record, nil
}

func (r fileRecoveryRecord) mode(fallback os.FileMode) os.FileMode {
	if r.Mode != nil {
		return os.FileMode(*r.Mode)
	}
	return fallback.Perm()
}

func (r fileRecoveryRecord) accepts(actual, original string) bool {
	if actual == "" {
		return true
	}
	known := false
	for _, hash := range []string{r.SHA256, r.PendingSHA256, r.PreviousSHA256, original} {
		if hash == "" {
			continue
		}
		known = true
		if actual == hash {
			return true
		}
	}
	return !known && r.JournalVersion < 2
}

type recoveryStatus string

const (
	recoveryRestored recoveryStatus = "restored"
	recoveryPartial  recoveryStatus = "restore_partial"
)

func setRecoveryStatus(report map[string]any, status recoveryStatus) {
	report["status"] = string(status)
}

type manifestStatus string

const (
	manifestBuilding  manifestStatus = "building"
	manifestPending   manifestStatus = "pending"
	manifestApplying  manifestStatus = "applying"
	manifestApplied   manifestStatus = "applied"
	manifestCancelled manifestStatus = "cancelled"
)

func (s *manifestStatus) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	switch manifestStatus(value) {
	case manifestBuilding, manifestPending, manifestApplying, manifestApplied, manifestCancelled:
		*s = manifestStatus(value)
		return nil
	default:
		return fmt.Errorf("unsupported pending manifest status: %q", value)
	}
}
