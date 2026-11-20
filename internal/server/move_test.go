package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-sphere/internal/config"
)

// moveHarness starts Sphere behind a fake Core whose pcx_<who>_<export|import|read>
// tokens are Pocket Move links acting for alice@ or bob@.
func moveHarness(t *testing.T) *harness {
	t.Helper()
	coreSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/auth/introspect" {
			io.WriteString(w, `{}`)
			return
		}
		var in struct {
			Token string `json:"token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		parts := strings.SplitN(strings.TrimPrefix(in.Token, "pcx_"), "_", 2)
		if len(parts) != 2 {
			io.WriteString(w, `{"active":false}`)
			return
		}
		scope := map[string]string{"export": core.ScopeMoveExport, "import": core.ScopeMoveImport, "read": "sphere:read"}[parts[1]]
		email := parts[0] + "@example.com"
		fmt.Fprintf(w, `{"active":true,"kind":"delegated","scope":%q,"audience":"sphere","user":{"id":1,"sub":%q,"email":%q,"name":%q,"role":"member"},"app":{"id":7,"slug":"move","name":"Pocket Move"}}`, scope, email, email, parts[0])
	}))
	t.Cleanup(coreSrv.Close)
	return newHarnessWith(t, func(c *config.Config) { c.CoreURL = coreSrv.URL })
}

func (h *harness) bearer(tok, method, path string, body []byte) (int, []byte) {
	h.t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func TestMoveLinkScopes(t *testing.T) {
	h := moveHarness(t)
	exp, imp, read := "pcx_bob_export", "pcx_bob_import", "pcx_bob_read"
	for _, c := range []struct {
		tok, method, path string
		want              int
	}{
		{exp, "GET", core.PortablePath, 200},
		{exp, "GET", "/api/v1/export.json", 200},
		{exp, "POST", "/api/v1/import", 403},
		{exp, "GET", "/api/v1/settings", 403},
		{exp, "GET", "/api/v1/scenes", 403},
		{exp, "GET", "/api/v1/backup", 403},
		{imp, "GET", core.PortablePath, 200},
		{imp, "GET", "/api/v1/export.json", 403},
		{imp, "PUT", "/api/v1/settings", 403},
		{imp, "POST", "/api/v1/import/stash", 403},
		{imp, "POST", "/api/v1/downloads", 403},
		{read, "GET", core.PortablePath, 403},
		{read, "GET", "/api/v1/export.json", 403},
	} {
		if code, body := h.bearer(c.tok, c.method, c.path, []byte("{}")); code != c.want {
			t.Errorf("%s %s %s → %d, want %d (%s)", c.tok, c.method, c.path, code, c.want, body)
		}
	}
	_, body := h.bearer(exp, "GET", core.PortablePath, nil)
	var p core.Portable
	if err := json.Unmarshal(body, &p); err != nil || len(p.Exports) != 1 || len(p.Imports) != 1 || p.Exports[0].Kind != core.KindPocket {
		t.Fatalf("portable: %s", body)
	}
}

// Move brings Alice's marks on the library to Bob: the second run adds
// nothing, and nothing of one person shows up for the other.
func TestMoveImportsAddOnce(t *testing.T) {
	h := moveHarness(t)
	id := h.sceneIDs()[0]
	h.do(h.ac, "PUT", "/api/v1/scenes/"+itoa(id)+"/mine", map[string]any{"rating": 8, "favorite": true})
	h.do(h.ac, "POST", "/api/v1/scenes/"+itoa(id)+"/o", nil)
	h.do(h.ac, "POST", "/api/v1/subscriptions", map[string]any{"url": "https://videos.example/channel/garden", "name": "Garden"})

	code, export := h.bearer("pcx_alice_export", "GET", "/api/v1/export.json", nil)
	if code != 200 || !strings.Contains(string(export), `"rating":8`) {
		t.Fatalf("alice's export: %d %s", code, export)
	}
	if _, bob := h.bearer("pcx_bob_export", "GET", "/api/v1/export.json", nil); strings.Contains(string(bob), `"rating":8`) || strings.Contains(string(bob), "garden") {
		t.Fatalf("bob's export holds alice's marks: %s", bob)
	}

	var first, second map[string]float64
	code, b := h.bearer("pcx_bob_import", "POST", "/api/v1/import", export)
	_ = json.Unmarshal(b, &first)
	code2, b2 := h.bearer("pcx_bob_import", "POST", "/api/v1/import", export)
	_ = json.Unmarshal(b2, &second)
	if code != 200 || code2 != 200 || first["scenes"] != 1 || first["os"] != 1 {
		t.Fatalf("first import: %d %s", code, b)
	}
	if second["scenes"]+second["os"]+second["plays"]+second["marks"]+second["filters"]+second["subscriptions"] != 0 {
		t.Fatalf("second import added: %s", b2)
	}
	var subs int
	_ = h.st.DB.QueryRow(`SELECT COUNT(1) FROM subscriptions WHERE user_id = ?`, h.bob.ID).Scan(&subs)
	if subs != 1 {
		t.Fatalf("bob's channels: %d", subs)
	}
	_, m, _ := h.do(h.bc, "GET", "/api/v1/scenes/"+itoa(id), nil)
	if mine := m["scene"].(map[string]any)["mine"].(map[string]any); mine["rating"].(float64) != 8 || mine["o_count"].(float64) != 1 {
		t.Fatalf("bob after two imports: %v", mine)
	}
	_ = h.st.DB.QueryRow(`SELECT COUNT(1) FROM subscriptions WHERE user_id = ?`, h.alice.ID).Scan(&subs)
	if subs != 1 {
		t.Fatalf("alice's channels changed: %d", subs)
	}
}
