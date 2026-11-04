package mobile

import (
	"encoding/json"
	"encoding/pem"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type exitTransferTrust struct{ transferTrustFixture }

func (p *exitTransferTrust) ApprovedURL(kind, raw string) bool {
	return (kind == "server-exits" || kind == "select-server-exit") && raw == p.url+"/server-exit"
}
func (p *exitTransferTrust) Credentials(ref string) (string, error) {
	b, _ := json.Marshal(map[string]string{"ca": p.ca, "token": strings.Repeat("a", 64)})
	return string(b), nil
}

func TestServerExitServiceTransfer(t *testing.T) {
	for _, kind := range []string{"server-exits", "select-server-exit"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			s, b, srv := serviceFixture(t, 0, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("a", 64) {
					t.Error("missing authentication")
				}
				if kind == "select-server-exit" {
					var c struct {
						ID       string `json:"server_exit_id"`
						Revision string `json:"revision"`
					}
					if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&c) != nil || c.ID != "west" || c.Revision != strings.Repeat("b", 64) {
						t.Error("invalid selection")
					}
				} else if r.Method != "GET" {
					t.Error("invalid fetch")
				}
				w.Write([]byte(`{"selected":"west"}`))
			}))
			s.SetTrust(&exitTransferTrust{transferTrustFixture{srv.URL, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))}})
			out := filepath.Join(t.TempDir(), "view")
			req := map[string]string{"id": "exit", "kind": kind, "https_url": srv.URL + "/server-exit", "output_file": out, "device_auth_ref": "device"}
			if kind == "select-server-exit" {
				req["body"] = `{"server_exit_id":"west","revision":"` + strings.Repeat("b", 64) + `"}`
			}
			raw, _ := json.Marshal(req)
			if err := s.Fetch(string(raw), b.Bind(nil, "wifi")); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatal(calls)
			}
			if data, err := os.ReadFile(out); err != nil || string(data) != `{"selected":"west"}` {
				t.Fatal("response not published", err)
			}
		})
	}
}
