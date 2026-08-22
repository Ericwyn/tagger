package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretBoxRoundTripAndKeyPermissions(t *testing.T) {
	directory := t.TempDir()
	box, err := openSecretBox(directory)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := box.seal([]byte(`{"auth":"secret"}`))
	if err != nil || !strings.HasPrefix(sealed, secretPrefix) {
		t.Fatalf("sealed=%q err=%v", sealed, err)
	}
	plain, encrypted, err := box.open([]byte(sealed))
	if err != nil || !encrypted || string(plain) != `{"auth":"secret"}` {
		t.Fatalf("plain=%q encrypted=%v err=%v", plain, encrypted, err)
	}
	info, err := os.Stat(filepath.Join(directory, ".tagger-secrets.key"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key permissions = %o", info.Mode().Perm())
	}
}

func TestProviderConfigurationMigratesLegacyPlaintext(t *testing.T) {
	store := openTestStore(t)
	legacy, _ := json.Marshal(map[string]string{"baseUrl": "https://legacy.example", "auth": "old-secret"})
	if _, err := store.db.ExecContext(context.Background(), `INSERT INTO provider_settings(provider_id, enabled, config_json, updated_at) VALUES(?, 1, ?, ?)`, "legacy", legacy, formatTime(store.now().UTC())); err != nil {
		t.Fatal(err)
	}
	configurations, err := store.LoadProviderConfigurations(context.Background())
	if err != nil || configurations["legacy"]["auth"] != "old-secret" {
		t.Fatalf("configurations=%#v err=%v", configurations, err)
	}
	var payload []byte
	if err := store.db.QueryRowContext(context.Background(), `SELECT config_json FROM provider_settings WHERE provider_id=?`, "legacy").Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "old-secret") || !strings.HasPrefix(string(payload), secretPrefix) {
		t.Fatalf("legacy payload was not encrypted: %q", payload)
	}
}
