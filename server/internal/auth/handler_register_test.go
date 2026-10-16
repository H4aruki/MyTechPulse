package auth

import "testing"

// OpenAPIのcookieAuthは、Handlerに設定した実際のCookie名(本番は __Host- 付き)を示す。
func TestRegisterDescribesTheConfiguredCookieName(t *testing.T) {
	for _, name := range []string{"mtp_session", "__Host-mtp_session"} {
		a := newAPIHarness(name, name != "mtp_session")
		scheme := a.api.OpenAPI().Components.SecuritySchemes[cookieAuthScheme]
		if scheme == nil || scheme.Type != "apiKey" || scheme.In != "cookie" || scheme.Name != name {
			t.Fatalf("cookieAuth for %q = %+v", name, scheme)
		}
	}
}
