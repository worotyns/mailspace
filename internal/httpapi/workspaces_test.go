// SPDX-License-Identifier: AGPL-3.0-only
package httpapi

import (
	"context"
	"mailspace.local/mailspace/internal/identity"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfileWorkspaceFormsAndRevocation(t *testing.T) {
	ctx := context.Background()
	store, err := identity.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	alice, err := store.CreateUser(ctx, "alice@example.test", "synthetic alice password")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := store.CreateUser(ctx, "bob@example.test", "synthetic bob password")
	if err != nil {
		t.Fatal(err)
	}
	token, session, err := store.NewSession(ctx, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	other, otherSession, err := store.NewSession(ctx, bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewPersonal(t.TempDir(), store, AuthConfig{Origin: "https://mail.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, cookie string, values url.Values) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(values.Encode()))
		req.Header.Set("Origin", "https://mail.example.test")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: "__Host-mailspace", Value: cookie})
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		return res
	}
	if res := request("POST", "/workspaces", token, url.Values{"name": {"Unsafe without CSRF"}}); res.Code != 403 {
		t.Fatal("workspace creation bypassed CSRF")
	}
	if res := request("POST", "/profile", token, url.Values{"csrf": {session.CSRF}, "display_name": {"<script>Alice</script>"}, "timezone": {"Europe/Warsaw"}, "user_id": {bob.ID}}); res.Code != 303 {
		t.Fatal("profile update failed", res.Code)
	}
	res := request("GET", "/profile", token, nil)
	if res.Code != 200 || strings.Contains(res.Body.String(), "<script>Alice") || !strings.Contains(res.Body.String(), "&lt;script&gt;Alice") {
		t.Fatal("profile not escaped or not saved")
	}
	b, _ := store.Session(ctx, other)
	if b.User.Name != "" {
		t.Fatal("profile form updated another user")
	}
	res = request("POST", "/workspaces", token, url.Values{"csrf": {session.CSRF}, "name": {"<img src=x> Team"}})
	if res.Code != 303 {
		t.Fatal("workspace creation failed", res.Code)
	}
	list, err := store.Workspaces(ctx, alice.ID)
	if err != nil || len(list) != 1 {
		t.Fatal("workspace missing")
	}
	id := list[0].ID
	if res = request("GET", "/workspaces/"+id, other, nil); res.Code != 404 {
		t.Fatal("outsider read roster")
	}
	res = request("GET", "/workspaces/"+id, token, nil)
	if res.Code != 200 || strings.Contains(res.Body.String(), "<img src=x>") {
		t.Fatal("workspace HTML not escaped")
	}
	if res = request("POST", "/workspaces/"+id+"/members", token, url.Values{"csrf": {session.CSRF}, "email": {bob.Email}}); res.Code != 303 {
		t.Fatal("add member failed")
	}
	if res = request("POST", "/workspaces/"+id+"/switch", other, url.Values{"csrf": {otherSession.CSRF}}); res.Code != 303 {
		t.Fatal("member could not switch")
	}
	if res = request("POST", "/workspaces/"+id+"/rename", other, url.Values{"csrf": {otherSession.CSRF}, "name": {"Takeover"}}); res.Code == 303 {
		t.Fatal("member renamed organization")
	}
	if res = request("POST", "/workspaces/"+id+"/members/"+bob.ID+"/remove", token, url.Values{"csrf": {session.CSRF}}); res.Code != 303 {
		t.Fatal("remove member failed")
	}
	if res = request("GET", "/workspaces/"+id, other, nil); res.Code != 404 {
		t.Fatal("revoked member kept access")
	}
	active, err := store.ActiveWorkspace(ctx, bob.ID)
	if err != nil || active.ID != "" {
		t.Fatal("revoked active context remained selected")
	}
	if res = request("GET", "/workspaces", other, nil); res.Code != 200 {
		t.Fatal("personal workspace chooser unavailable after removal")
	}
}
