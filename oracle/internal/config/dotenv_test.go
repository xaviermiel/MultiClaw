package config

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testdata/dotenv-cases.json is the real dotenv's parse of tricky files,
// dumped with the TS oracle's dotenv 16.6.1 before its removal.
func TestParseDotenvMatchesDotenv(t *testing.T) {
	data, err := os.ReadFile("testdata/dotenv-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var dump struct {
		Dotenv string `json:"dotenv"`
		Cases  []struct {
			Name   string            `json:"name"`
			Input  string            `json:"input"`
			Parsed map[string]string `json:"parsed"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &dump); err != nil {
		t.Fatal(err)
	}
	if len(dump.Cases) < 20 {
		t.Fatalf("only %d cases", len(dump.Cases))
	}
	for _, c := range dump.Cases {
		t.Run(c.Name, func(t *testing.T) {
			if got := parseDotenv(c.Input); !maps.Equal(got, c.Parsed) {
				t.Fatalf("parsed %q\nwant (dotenv %s) %q", got, dump.Dotenv, c.Parsed)
			}
		})
	}
}

func TestLoadEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	// A line dotenv skips must neither fail the load nor show up in a
	// message (godotenv's parse errors quoted the rest of the file).
	file := "a note without an equals sign\nLOADENV_A=from file\nLOADENV_B=from file\nLOADENV_C=$LOADENV_A\n"
	if err := os.WriteFile(path, []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOADENV_B", "") // set, even to "": it wins, as with dotenv
	t.Cleanup(func() {
		os.Unsetenv("LOADENV_A")
		os.Unsetenv("LOADENV_C")
	})
	if err := LoadEnvFile(path, true); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"LOADENV_A": "from file", "LOADENV_B": "", "LOADENV_C": "$LOADENV_A"} {
		if got := os.Getenv(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}

	missing := filepath.Join(dir, "missing.env")
	if err := LoadEnvFile(missing, false); err != nil {
		t.Errorf("a missing optional file: %v", err)
	}
	if err := LoadEnvFile(missing, true); err == nil {
		t.Error("a missing required file loaded")
	}

	if os.Geteuid() == 0 {
		return // root reads anything
	}
	secret := filepath.Join(dir, "secret.env")
	if err := os.WriteFile(secret, []byte("PRIVATE_KEY=0xsecret\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	err := LoadEnvFile(secret, false)
	if err == nil || strings.Contains(err.Error(), "0xsecret") {
		t.Fatalf("an unreadable file: %v", err)
	}
}
