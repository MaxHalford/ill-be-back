package app_test

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxhalford/ill-be-back/internal/app"
)

func TestLegacyConnectionsSurviveUpgradeAndRestart(t *testing.T) {
	databases := map[string]string{"sqlite": filepath.Join(t.TempDir(), "legacy.db")}
	if dsn := os.Getenv("TEST_POSTGRES_URL"); dsn != "" {
		databases["pgx"] = dsn
	}
	for driver, dsn := range databases {
		t.Run(driver, func(t *testing.T) {
			// A released-schema fixture supplies the upgrade input. All assertions use HTTP.
			db, err := sql.Open(driver, dsn)
			if err != nil {
				t.Fatal(err)
			}
			fixture, err := os.ReadFile("testdata/schema-v1.sql")
			if err != nil {
				t.Fatal(err)
			}
			for _, statement := range strings.Split(string(fixture), ";") {
				if strings.TrimSpace(statement) != "" {
					if _, err = db.Exec(statement); err != nil {
						t.Fatal(err)
					}
				}
			}
			block, _ := aes.NewCipher(bytes.Repeat([]byte{7}, 32))
			aead, _ := cipher.NewGCM(block)
			nonce := make([]byte, aead.NonceSize())
			encrypted := base64.RawURLEncoding.EncodeToString(aead.Seal(nonce, nonce, []byte("test-github-secret"), []byte("github:1")))
			if _, err = db.Exec(`INSERT INTO accounts(id,name,desired_status) VALUES('legacy-owner','Worker','away')`); err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(`INSERT INTO connections(account_id,provider,remote_id,label,token,message,applied_message,status) VALUES('legacy-owner','github','1','@worker1',$1,'Friday off','On holiday','away')`, encrypted); err != nil {
				t.Fatal(err)
			}
			hash := fmt.Sprintf("%x", sha256.Sum256([]byte("legacy-cookie")))
			if _, err = db.Exec(`INSERT INTO sessions(id,account_id,csrf,expires) VALUES($1,'legacy-owner','legacy-csrf',4102444800)`, hash); err != nil {
				t.Fatal(err)
			}
			db.Close()
			provider := &providerStub{githubID: 2, slackTeam: "T1", slackUser: "U1"}
			cfg := app.Config{DatabaseURL: dsn, EncryptionKey: bytes.Repeat([]byte{7}, 32), AppURL: "http://localhost:8000", Development: true, GitHubClientID: "id", GitHubClientSecret: "secret", HTTPClient: &http.Client{Transport: provider}}
			launch := func() (*testApp, func()) {
				a, err := app.New(cfg)
				if err != nil {
					t.Fatal(err)
				}
				server := httptest.NewServer(a)
				jar, _ := cookiejar.New(nil)
				origin, _ := url.Parse(server.URL)
				jar.SetCookies(origin, []*http.Cookie{{Name: "ibb_session", Value: "legacy-cookie", Path: "/"}})
				return &testApp{server, &http.Client{Jar: jar, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, provider}, func() { server.Close(); a.Close() }
			}
			a, closeApp := launch()
			s := a.state(t)
			if !s.Authenticated || s.DesiredStatus != "away" || len(s.Connections) != 1 || s.Connections[0].Message != "Friday off" || s.Connections[0].AppliedMessage != "On holiday" || s.Connections[0].Status != "away" {
				t.Fatalf("upgrade lost data: %+v", s)
			}
			original := s.Connections[0].ID
			code, data := a.call(t, "POST", "/api/status", s.CSRFToken, map[string]string{"status": "available"})
			if code != 200 || readState(t, data).Connections[0].Status != "available" {
				t.Fatalf("migrated token unusable: %d %s", code, data)
			}
			closeApp()
			a, closeApp = launch()
			defer closeApp()
			s = a.state(t)
			if s.Connections[0].ID != original || s.Connections[0].Status != "available" || s.Connections[0].Message != "Friday off" {
				t.Fatal("restart reran migration or lost preferences")
			}
			a.connect(t, "github")
			if len(a.state(t).Connections) != 2 {
				t.Fatal("upgraded account cannot add second GitHub identity")
			}
		})
	}
}
