package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestEmitListEnvelope(t *testing.T) {
	var buf bytes.Buffer
	items := []map[string]any{{"name": "app"}, {"name": "web"}}
	if err := EmitList(items, "", false, Options{Format: FormatJSON, Writer: &buf}); err != nil {
		t.Fatal(err)
	}
	var env struct {
		Items   []map[string]any `json:"items"`
		HasMore bool             `json:"has_more"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("output is not the list envelope: %v\n%s", err, buf.String())
	}
	if len(env.Items) != 2 || env.HasMore {
		t.Errorf("unexpected envelope: %+v", env)
	}
}

func TestFieldProjection(t *testing.T) {
	var buf bytes.Buffer
	v := map[string]any{"name": "app", "secret": "x", "nested": map[string]any{"keep": 1}}
	if err := Emit(v, Options{Format: FormatJSON, Writer: &buf, Fields: []string{"name", "nested.keep"}}); err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if _, ok := out["secret"]; ok {
		t.Error("projection should have dropped 'secret'")
	}
	if out["name"] != "app" {
		t.Errorf("projection dropped 'name': %+v", out)
	}
	if _, ok := out["nested.keep"]; !ok {
		t.Errorf("projection missing flattened dot-path: %+v", out)
	}
}

func TestNDJSONStreamsRows(t *testing.T) {
	var buf bytes.Buffer
	items := []map[string]any{{"a": 1}, {"a": 2}}
	if err := EmitList(items, "", false, Options{Format: FormatNDJSON, Writer: &buf}); err != nil {
		t.Fatal(err)
	}
	lines := bytes.Count(buf.Bytes(), []byte("\n"))
	if lines != 2 {
		t.Errorf("ndjson should emit one line per row, got %d lines:\n%s", lines, buf.String())
	}
}

func TestBadFormat(t *testing.T) {
	var buf bytes.Buffer
	if err := Emit(map[string]any{}, Options{Format: "yaml", Writer: &buf}); err == nil {
		t.Error("expected error for unknown format")
	}
}

func TestNDJSONPaginationNoticeSurvivesProjectionAndEmptyPages(t *testing.T) {
	for _, items := range [][]map[string]any{nil, {{"name": "rule", "extra": true}}} {
		var data, notices bytes.Buffer
		if err := EmitList(items, "opaque-cursor", true, Options{
			Format: FormatNDJSON, Writer: &data, NoticeWriter: &notices, Fields: []string{"name"},
		}); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data.Bytes(), []byte("opaque-cursor")) || bytes.Contains(data.Bytes(), []byte("extra")) {
			t.Fatalf("metadata contaminated rows: %s", data.String())
		}
		if bytes.Count(data.Bytes(), []byte("\n")) != len(items) {
			t.Fatal("row count changed")
		}
		var notice struct {
			Notice struct {
				Pagination struct {
					Next    string `json:"next"`
					HasMore bool   `json:"has_more"`
				} `json:"pagination"`
			} `json:"_notice"`
		}
		if err := json.Unmarshal(notices.Bytes(), &notice); err != nil {
			t.Fatal(err)
		}
		if notice.Notice.Pagination.Next != "opaque-cursor" || !notice.Notice.Pagination.HasMore {
			t.Fatalf("cursor lost: %s", notices.String())
		}
	}
	var data, notices bytes.Buffer
	if err := EmitList(nil, "", false, Options{Format: FormatNDJSON, Writer: &data, NoticeWriter: &notices}); err != nil {
		t.Fatal(err)
	}
	if notices.Len() != 0 {
		t.Fatal("a complete stream should not emit a continuation notice")
	}
}

func TestPaginationUsesTheCommandsContinuationFlag(t *testing.T) {
	for _, format := range []string{FormatNDJSON, FormatTable} {
		t.Run(format, func(t *testing.T) {
			var data, notices bytes.Buffer
			if err := EmitList([]map[string]any{{"name": "row"}}, "7", true, Options{
				Format: format, Writer: &data, NoticeWriter: &notices, NextFlag: "--offset",
			}); err != nil {
				t.Fatal(err)
			}
			combined := data.String() + notices.String()
			if !strings.Contains(combined, "--offset") || strings.Contains(combined, "--cursor") {
				t.Fatalf("wrong continuation flag: %s", combined)
			}
		})
	}
}

type failingPaginationWriter struct{}

func (failingPaginationWriter) Write([]byte) (int, error) {
	return 0, errors.New("row write failed")
}

func TestFailedNDJSONWriteDoesNotEmitContinuation(t *testing.T) {
	var notices bytes.Buffer
	err := EmitList([]map[string]any{{"name": "row"}}, "next", true, Options{
		Format: FormatNDJSON, Writer: failingPaginationWriter{}, NoticeWriter: &notices,
	})
	if err == nil || notices.Len() != 0 {
		t.Fatalf("err=%v notices=%s", err, notices.String())
	}
}
