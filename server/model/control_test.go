package model

import (
	"encoding/json"
	"strings"
	"testing"
)

// The dashboard reads online/offline/total directly. If those get `omitempty`
// they vanish whenever the value is 0, and the frontend renders undefined.
func TestControlResponseKeepsZeroCounters(t *testing.T) {
	payload, err := json.Marshal(ControlResponse{OK: true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(payload)
	for _, field := range []string{`"total":0`, `"online":0`, `"offline":0`, `"checked_at":0`} {
		if !strings.Contains(body, field) {
			t.Fatalf("expected %s in %s", field, body)
		}
	}
}

func TestControlResponseOmitsEmptyOptionalSections(t *testing.T) {
	payload, err := json.Marshal(ControlResponse{OK: true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(payload)
	for _, field := range []string{`"error"`, `"tunnels"`, `"wifi"`, `"services"`, `"cloudflare"`} {
		if strings.Contains(body, field) {
			t.Fatalf("expected %s to be omitted in %s", field, body)
		}
	}
}

func TestServiceCatalogTableName(t *testing.T) {
	if got := (ServiceCatalog{}).TableName(); got != "k20mon_service_catalog" {
		t.Fatalf("unexpected table name: %s", got)
	}
}

func TestDefaultServiceCatalogIsOrderedAndValid(t *testing.T) {
	rows := DefaultServiceCatalog()
	if len(rows) == 0 {
		t.Fatal("expected at least one seed row")
	}
	for index, row := range rows {
		if row.SortOrder != index {
			t.Fatalf("row %d has sort order %d", index, row.SortOrder)
		}
		if err := validateSeedRow(row); err != nil {
			t.Fatalf("seed row %s: %v", row.ID, err)
		}
	}
}

func validateSeedRow(row ServiceCatalog) error {
	if row.ID == "" || row.Name == "" || row.URL == "" {
		return errString("empty field")
	}
	if !strings.HasPrefix(row.URL, "http://") && !strings.HasPrefix(row.URL, "https://") {
		return errString("bad scheme")
	}
	if row.OwnerType != "mine" && row.OwnerType != "other" {
		return errString("bad owner type")
	}
	return nil
}

type errString string

func (e errString) Error() string { return string(e) }
