package files

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/router"
	"github.com/pocketbase/pocketbase/tools/types"
)

const (
	publicImageURL     = "/api/files/_pb_users_auth_/4q1xlclmfloku33/300_1SEi6Q6U72.png"
	protectedImageURL  = "/api/files/demo1/al1h9ijdeojtsjy/300_Jsjq7RdBgA.png"
	superuserFileToken = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpZCI6InN5d2JoZWNuaDQ2cmhtMCIsImV4cCI6MjUyNDYwNDQ2MSwidHlwZSI6ImZpbGUiLCJjb2xsZWN0aW9uSWQiOiJwYmNfMzE0MjYzNTgyMyJ9.Lupz541xRvrktwkrl55p5pPCF77T69ZRsohsIcb2dxc"
	expiredFileToken   = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpZCI6InN5d2JoZWNuaDQ2cmhtMCIsImV4cCI6MTY0MDk5MTY2MSwidHlwZSI6ImZpbGUiLCJjb2xsZWN0aW9uSWQiOiJwYmNfMzE0MjYzNTgyMyJ9.nqqtqpPhxU0045F4XP_ruAkzAidYBc5oPy9ErN3XBq0"
	userFileToken      = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpZCI6IjRxMXhsY2xtZmxva3UzMyIsImV4cCI6MjUyNDYwNDQ2MSwidHlwZSI6ImZpbGUiLCJjb2xsZWN0aW9uSWQiOiJfcGJfdXNlcnNfYXV0aF8ifQ.nSTLuCPcGpWn2K2l-BFkC3Vlzc-ZTDPByYq8dN1oPSo"
)

func testApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)
	return app
}

func fileEvent(app core.App, rawURL string, headers map[string]string) *core.RequestEvent {
	req := httptest.NewRequest(http.MethodGet, rawURL, nil)
	req.RemoteAddr = "127.0.0.1:1234"
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return &core.RequestEvent{
		App: app,
		Event: router.Event{
			Request:  req,
			Response: httptest.NewRecorder(),
		},
	}
}

func isStatus(err error, status int) bool {
	var apiErr *router.ApiError
	return errors.As(err, &apiErr) && apiErr.Status == status
}

func TestAuthorizePublicFile(t *testing.T) {
	app := testApp(t)
	got, err := authorizeFileRequest(fileEvent(app, publicImageURL, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got.servedPath, "/300_1SEi6Q6U72.png") {
		t.Fatalf("servedPath = %q", got.servedPath)
	}
	if got.servedName != "300_1SEi6Q6U72.png" {
		t.Fatalf("servedName = %q", got.servedName)
	}
}

func TestAuthorizePublicFileThumbRewritesPath(t *testing.T) {
	app := testApp(t)
	got, err := authorizeFileRequest(fileEvent(app, publicImageURL+"?thumb=100x100", nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.servedPath, "/thumbs_300_1SEi6Q6U72.png/") {
		t.Fatalf("thumb path = %q", got.servedPath)
	}
}

func TestAuthorizeProtectedFileRequiresToken(t *testing.T) {
	app := testApp(t)
	_, err := authorizeFileRequest(fileEvent(app, protectedImageURL, nil))
	if !isStatus(err, http.StatusNotFound) {
		t.Fatalf("guest without view rule err = %v", err)
	}
}

func TestAuthorizeProtectedFileExpiredToken(t *testing.T) {
	app := testApp(t)
	_, err := authorizeFileRequest(fileEvent(app, protectedImageURL+"?token="+expiredFileToken, nil))
	if !isStatus(err, http.StatusNotFound) {
		t.Fatalf("expired token err = %v", err)
	}
}

func TestAuthorizeProtectedFileSuperuserToken(t *testing.T) {
	app := testApp(t)
	got, err := authorizeFileRequest(fileEvent(app, protectedImageURL+"?token="+superuserFileToken, nil))
	if err != nil {
		t.Fatal(err)
	}
	if got.servedName != "300_Jsjq7RdBgA.png" {
		t.Fatalf("servedName = %q", got.servedName)
	}
}

func TestAuthorizeProtectedFileSuperuserIPDeny(t *testing.T) {
	app := testApp(t)
	app.Settings().TrustedProxy = core.TrustedProxyConfig{Headers: []string{"x-test-ip"}}
	app.Settings().SuperuserIPs = []string{"0.0.0.0"}
	if err := app.Save(app.Settings()); err != nil {
		t.Fatal(err)
	}
	_, err := authorizeFileRequest(fileEvent(app, protectedImageURL+"?token="+superuserFileToken, map[string]string{"x-test-ip": "127.0.0.1"}))
	if !isStatus(err, http.StatusNotFound) {
		t.Fatalf("non-whitelisted IP err = %v", err)
	}
}

func TestAuthorizeProtectedFileSuperuserIPAllow(t *testing.T) {
	app := testApp(t)
	app.Settings().TrustedProxy = core.TrustedProxyConfig{Headers: []string{"x-test-ip"}}
	app.Settings().SuperuserIPs = []string{"127.0.0.1"}
	if err := app.Save(app.Settings()); err != nil {
		t.Fatal(err)
	}
	if _, err := authorizeFileRequest(fileEvent(app, protectedImageURL+"?token="+superuserFileToken, map[string]string{"x-test-ip": "127.0.0.1"})); err != nil {
		t.Fatal(err)
	}
}

func TestAuthorizeProtectedFileGuestViewRule(t *testing.T) {
	app := testApp(t)
	c, err := app.FindCachedCollectionByNameOrId("demo1")
	if err != nil {
		t.Fatal(err)
	}
	c.ViewRule = types.Pointer("")
	if err := app.UnsafeWithoutHooks().Save(c); err != nil {
		t.Fatal(err)
	}
	if _, err := authorizeFileRequest(fileEvent(app, protectedImageURL, nil)); err != nil {
		t.Fatal(err)
	}
}

func TestAuthorizeProtectedFileAuthWithoutView(t *testing.T) {
	app := testApp(t)
	c, err := app.FindCachedCollectionByNameOrId("demo1")
	if err != nil {
		t.Fatal(err)
	}
	c.ViewRule = types.Pointer("@request.auth.verified = true")
	if err := app.UnsafeWithoutHooks().Save(c); err != nil {
		t.Fatal(err)
	}
	_, err = authorizeFileRequest(fileEvent(app, protectedImageURL+"?token="+userFileToken, nil))
	if !isStatus(err, http.StatusNotFound) {
		t.Fatalf("unverified user err = %v", err)
	}
}

func TestAuthorizeMissingCollection(t *testing.T) {
	app := testApp(t)
	_, err := authorizeFileRequest(fileEvent(app, "/api/files/missing/rec/file.png", nil))
	if !isStatus(err, http.StatusNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestAuthorizeTokenNotInServedPath(t *testing.T) {
	app := testApp(t)
	got, err := authorizeFileRequest(fileEvent(app, protectedImageURL+"?token="+superuserFileToken, nil))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.servedPath, "token") || strings.Contains(got.servedPath, superuserFileToken) {
		t.Fatalf("token leaked into cache key %q", got.servedPath)
	}
}
