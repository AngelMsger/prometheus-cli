package apiclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
	"github.com/angelmsger/prometheus-cli/pkg/timeutil"
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
// the URL, instead of following it. Before, it was followed as a GET, and the
// login page it landed on failed to decode.
func TestPostedQueryRedirectIsReported(t *testing.T) {
	for _, status := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther} {
		var bodyless int
		client := newRedirectedClient(t, redirectingServer(status, &bodyless))
		_, err := client.Query(context.Background(), InstantRequest{Query: "up"})
		ce := cerrors.AsCLIError(err)
		if ce == nil || ce.HTTPStatus != status || ce.Retryable || bodyless != 0 ||
			!strings.Contains(ce.Hint, "redirected the request") || strings.Contains(ce.Hint, "may still have applied") {
			t.Fatalf("%d: got %+v after %d bodyless requests; want the redirect reported", status, ce, bodyless)
		}
	}
}

// An admin write is never followed through a redirect, so its redirect comes
// back as itself, not as a delete that a login page confirmed. A proxy can
// pass the write on before it redirects, so the hint says the write may have
// applied. Each write says what fits it: a snapshot points at the snapshot
// directory, a clean-up is safe to repeat, and a delete stays unknown.
func TestAdminWriteRedirectIsReported(t *testing.T) {
	writes := []struct {
		name  string
		write func(Client) error
		hint  string
		step  string
	}{
		{"delete series", func(c Client) error {
			return c.DeleteSeries(context.Background(), DeleteSeriesRequest{Match: []string{"up"}})
		}, "outcome stays unknown", ""},
		{"clean tombstones", func(c Client) error { return c.CleanTombstones(context.Background()) },
			"Repeating it is safe", ""},
		{"snapshot", func(c Client) error {
			_, err := c.Snapshot(context.Background(), false)
			return err
		}, "snapshots/", "prometheus-cli status flags"},
	}
	for _, w := range writes {
		for _, status := range []int{http.StatusFound, http.StatusTemporaryRedirect} {
			var bodyless int
			client := newRedirectedClient(t, redirectingServer(status, &bodyless))
			ce := cerrors.AsCLIError(w.write(client))
			if ce == nil || ce.HTTPStatus != status || ce.Retryable || bodyless != 0 ||
				!strings.Contains(ce.Hint, "may still have applied") || !strings.Contains(ce.Hint, w.hint) {
				t.Fatalf("%s %d: got %+v after %d requests at the target; want the redirect itself", w.name, status, ce, bodyless)
			}
			if w.step != "" && ce.NextSteps[len(ce.NextSteps)-1] != w.step {
				t.Fatalf("%s %d: next steps %q; want the check %q last", w.name, status, ce.NextSteps, w.step)
			}
		}
	}
}

// A delete that a redirect answered has an unknown outcome, and nothing the
// CLI reads settles it: Prometheus can keep listing a series after its
// samples were deleted, even after clean-tombstones. So no step calls a
// series list proof that the delete failed, and none repeats it. The only
// command offered is a --dry-run preview with the window fixed: from the
// delete's start (or the epoch) to its end, or, for a delete without one, to
// the moment of this attempt, so a repeat the user confirms cannot reach
// samples collected since. Its --from and --to read back as exactly that
// window; an explicit epoch start used to come out as "0", which --from
// reads as now.
func TestRedirectedDeleteStaysUnknown(t *testing.T) {
	start, end := time.Unix(1700000000, 0), time.Unix(1700003600, 0)
	before := time.Now().Truncate(time.Second)
	for _, tc := range []struct {
		name     string
		req      DeleteSeriesRequest
		from, to time.Time
	}{
		{"with a window", DeleteSeriesRequest{Match: []string{`up{job="x"}`, "down"}, Start: start, End: end}, start, end},
		{"up to an end", DeleteSeriesRequest{Match: []string{"up"}, End: end}, time.Unix(0, 0), end},
		{"from the epoch", DeleteSeriesRequest{Match: []string{"up"}, Start: time.Unix(0, 0), End: end}, time.Unix(0, 0), end},
		{"from a fraction of a second", DeleteSeriesRequest{Match: []string{"up"}, Start: start.Add(250 * time.Millisecond), End: end},
			start.Add(250 * time.Millisecond), end},
		{"without an end", DeleteSeriesRequest{Match: []string{"up"}, Start: start}, start, time.Time{}},
	} {
		var bodyless int
		client := newRedirectedClient(t, redirectingServer(http.StatusFound, &bodyless))
		ce := cerrors.AsCLIError(client.DeleteSeries(context.Background(), tc.req))
		if ce == nil || ce.Retryable || strings.Contains(ce.Hint, "were not deleted") ||
			!strings.Contains(ce.Hint, "only if the user confirms") {
			t.Fatalf("%s: got %+v; want an unknown outcome that a repeat needs the user for", tc.name, ce)
		}
		for _, step := range ce.NextSteps {
			if strings.Contains(step, "series list") || strings.Contains(step, "--yes") {
				t.Fatalf("%s: step %q reads the outcome or repeats the delete", tc.name, step)
			}
		}
		preview := ce.NextSteps[len(ce.NextSteps)-1]
		matches := ""
		for _, m := range tc.req.Match {
			matches += " --match " + cerrors.ShellArg(m)
		}
		rest, ok := strings.CutPrefix(preview, "prometheus-cli admin delete-series"+matches+" --from ")
		fromArg, toArg, ok2 := strings.Cut(strings.TrimSuffix(rest, " --dry-run"), " --to ")
		if !ok || !ok2 || !strings.HasSuffix(preview, " --dry-run") {
			t.Fatalf("%s: last step %q; want a dry run of the same selectors with --from and --to", tc.name, preview)
		}
		now := time.Now()
		from, err1 := timeutil.ParseInstant(fromArg, now)
		to, err2 := timeutil.ParseInstant(toArg, now)
		if err1 != nil || err2 != nil || !from.Equal(tc.from) {
			t.Fatalf("%s: --from %q --to %q read as %v, %v (%v, %v); want from %v", tc.name, fromArg, toArg, from, to, err1, err2, tc.from)
		}
		if tc.to.IsZero() {
			if to.Before(before) || to.After(now) {
				t.Fatalf("%s: --to %q is %v; want the moment of the attempt", tc.name, toArg, to)
			}
		} else if !to.Equal(tc.to) {
			t.Fatalf("%s: --to %q is %v; want %v", tc.name, toArg, to, tc.to)
		}
	}
}

// Go cannot follow a redirect without a Location, even for a GET, so it
// comes back as is and is reported as the redirect. 0.5.0 took its empty
// body for a response that is not Prometheus (NOT_PROMETHEUS_API, exit 10).
func TestRedirectWithoutLocationIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	client, err := BuildClient(BuildParams{BaseURL: srv.URL, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.BuildInfo(context.Background())
	ce := cerrors.AsCLIError(err)
	if ce == nil || ce.Code != "HTTP_FOUND" || ce.Category != cerrors.CategoryServer || ce.Retryable ||
		ce.HTTPStatus != http.StatusFound {
		t.Fatalf("got %+v; want HTTP_FOUND, server, not retryable", ce)
	}
}
