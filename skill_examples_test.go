package prometheuscli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestSkillErrorHandlingExample(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is unavailable")
	}
	data, err := SkillFS.ReadFile(SkillRoot + "/references/errors-and-exit-codes.md")
	if err != nil {
		t.Fatal(err)
	}
	lf := strings.ReplaceAll(string(data), "\r\n", "\n")
	for _, tc := range []struct {
		name, newline string
	}{
		{"LF", "\n"},
		{"CRLF", "\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testSkillErrorHandlingExample(t, bash, strings.ReplaceAll(lf, "\n", tc.newline))
		})
	}
}

func testSkillErrorHandlingExample(t *testing.T, bash, markdown string) {
	t.Helper()
	markdown = strings.ReplaceAll(markdown, "\r\n", "\n")
	blocks := strings.Split(markdown, "```bash\n")
	if len(blocks) != 2 {
		t.Fatalf("expected one executable bash example, got %d", len(blocks)-1)
	}
	script, _, ok := strings.Cut(blocks[1], "```")
	if !ok {
		t.Fatal("unterminated bash example")
	}
	// The stub prints a result on stdout and, when it fails, a structured
	// error on stderr whose retryable field the example must read: an exit 9
	// is a transient failure only when the error says it is retryable.
	stub := `prometheus-cli() {
  printf '{"series_count":1}\n'
  if [ "$PROMETHEUS_EXAMPLE_EXIT" -ne 0 ]; then
    printf '{\n  "error": {\n    "code": "EXAMPLE",\n    "retryable": %s\n  }\n}\n' "$PROMETHEUS_EXAMPLE_RETRYABLE" >&2
  fi
  return "$PROMETHEUS_EXAMPLE_EXIT"
}
`
	for _, tc := range []struct {
		code      int
		retryable bool
		advice    string
	}{
		{0, false, ""},
		{2, false, "Fix the query"},
		{3, false, "auth/config"},
		{4, false, "auth/config"},
		{5, false, "Not permitted"},
		{7, true, "Transient read failure"},
		{8, true, "Transient read failure"},
		{9, true, "Transient read failure"},
		{9, false, "Not transient"},
		{8, false, "Not transient"},
	} {
		t.Run(fmt.Sprintf("%d retryable=%t", tc.code, tc.retryable), func(t *testing.T) {
			cmd := exec.Command(bash, "-eu", "-c", stub+script)
			cmd.Dir = t.TempDir()
			cmd.Env = append(os.Environ(), fmt.Sprintf("PROMETHEUS_EXAMPLE_EXIT=%d", tc.code),
				fmt.Sprintf("PROMETHEUS_EXAMPLE_RETRYABLE=%t", tc.retryable))
			var out, diagnostics bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &diagnostics
			err := cmd.Run()
			if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != tc.code {
				t.Fatalf("exit = %v, want %d; stderr: %s", err, tc.code, &diagnostics)
			}
			if tc.code == 0 {
				if strings.TrimSpace(out.String()) != `{"series_count":1}` || diagnostics.Len() != 0 {
					t.Fatalf("success output = %q, stderr = %q", &out, &diagnostics)
				}
				return
			}
			stderr := diagnostics.String()
			if out.Len() != 0 || !strings.Contains(stderr, `"code": "EXAMPLE"`) || !strings.Contains(stderr, tc.advice) {
				t.Fatalf("failure stdout = %q, stderr = %q; want the error and %q", &out, stderr, tc.advice)
			}
			if !tc.retryable && strings.Contains(stderr, "Transient read failure") {
				t.Fatalf("a non-retryable error was called transient: %q", stderr)
			}
		})
	}
}
