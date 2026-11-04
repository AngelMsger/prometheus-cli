package apiclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
)

// redirectingServer answers every request under /old with a redirect of
// status to the same path without /old, and serves a vector there to a POST
// whose form still carries the query. It counts the requests that reached
// the target without one, which a redirect followed as a GET would make.
func redirectingServer(status int, bodyless *int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/old/") {
			http.Redirect(w, r, strings.TrimPrefix(r.URL.Path, "/old"), status)
			return
		}
		if r.Method != http.MethodPost || r.FormValue("query") == "" {
			*bodyless++
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>sign in</html>"))
			return
		}
		serveData(`{"resultType":"vector","result":[]}`)(w, r)
	}
}

// newRedirectedClient points a client at /old on a test server, so every
// request it sends is redirected first.
func newRedirectedClient(t *testing.T, handler http.HandlerFunc) Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client, err := BuildClient(BuildParams{BaseURL: srv.URL + "/old", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// A query is posted, so before transport.AsRead the kit's v0.7.0 transport
// handed its 307 or 308 back like a write's. It now follows it with the same
// form, as v0.6.0 did.
func TestPostedQueryFollowsAPreservingRedirect(t *testing.T) {
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		var bodyless int
		client := newRedirectedClient(t, redirectingServer(status, &bodyless))
		if _, err := client.Query(context.Background(), InstantRequest{Query: "up"}); err != nil || bodyless != 0 {
			t.Fatalf("%d: got %v after %d bodyless requests; want the query followed", status, err, bodyless)
		}
	}
}

// A 301, 302 or 303 would turn the posted query into a GET without the
// query, so the client reports the redirect, with its status and a hint on
// the URL, instead of following it. It used to say the answer was not
// Prometheus (the login page it landed on).
func TestPostedQueryRedirectIsReported(t *testing.T) {
	for _, status := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther} {
		var bodyless int
		client := newRedirectedClient(t, redirectingServer(status, &bodyless))
		_, err := client.Query(context.Background(), InstantRequest{Query: "up"})
		ce := cerrors.AsCLIError(err)
		if ce == nil || ce.HTTPStatus != status || ce.Retryable || bodyless != 0 ||
			!strings.Contains(ce.Hint, "redirected the request") {
			t.Fatalf("%d: got %+v after %d bodyless requests; want the redirect reported", status, ce, bodyless)
		}
	}
}

// An admin write is never followed through a redirect, so its 302 comes back
// as the redirect, not as a delete that a login page confirmed.
func TestAdminWriteRedirectIsReported(t *testing.T) {
	var bodyless int
	client := newRedirectedClient(t, redirectingServer(http.StatusTemporaryRedirect, &bodyless))
	err := client.DeleteSeries(context.Background(), DeleteSeriesRequest{Match: []string{`up{job="x"}`}})
	ce := cerrors.AsCLIError(err)
	if ce == nil || ce.HTTPStatus != http.StatusTemporaryRedirect || ce.Retryable || bodyless != 0 {
		t.Fatalf("got %+v after %d requests at the target; want the 307 itself", ce, bodyless)
	}
}
