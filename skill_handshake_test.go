package prometheuscli

import (
	"regexp"
	"strings"
	"testing"
)

// The Skill tells agents to export PROMETHEUS_CLI_SKILL with its own version
// once they have loaded it, and the CLI compares that value with the
// version in the Skill's frontmatter. A bump that misses the handshake line
// makes every agent that follows the Skill read as running an old copy.
func TestSkillHandshakeNamesItsVersion(t *testing.T) {
	data, err := SkillFS.ReadFile(SkillRoot + "/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	skill := strings.ReplaceAll(string(data), "\r\n", "\n")
	m := regexp.MustCompile(`(?m)^version: *(\S+)$`).FindStringSubmatch(skill)
	if m == nil {
		t.Fatal("SKILL.md has no version in its frontmatter")
	}
	handshakes := regexp.MustCompile(`PROMETHEUS_CLI_SKILL=([0-9][^\s`+"`"+`]*)`).FindAllStringSubmatch(skill, -1)
	if len(handshakes) == 0 {
		t.Fatal("SKILL.md does not tell agents which PROMETHEUS_CLI_SKILL to export")
	}
	for _, h := range handshakes {
		if h[1] != m[1] {
			t.Fatalf("SKILL.md says export PROMETHEUS_CLI_SKILL=%s; its version is %s", h[1], m[1])
		}
	}
}
