package user

import "testing"

func TestRuleMatchesPrefix(t *testing.T) {
	rule := &Rule{
		Path:  "/docs",
		Regex: false,
	}
	if !rule.Matches("/docs/file.txt") {
		t.Fatalf("expected prefix rule to match")
	}
	if rule.Matches("/doc/file.txt") {
		t.Fatalf("expected prefix rule to not match")
	}
}

func TestRuleMatchesRegex(t *testing.T) {
	rule := &Rule{
		Path:  `^/docs/.+\.txt$`,
		Regex: true,
	}
	if !rule.Matches("/docs/a.txt") {
		t.Fatalf("expected regex rule to match")
	}
	if rule.Matches("/docs/a.png") {
		t.Fatalf("expected regex rule to not match")
	}
}

func TestRuleMatchesRegexInvalid(t *testing.T) {
	rule := &Rule{
		Path:  "[",
		Regex: true,
	}
	if rule.Matches("/docs/a.txt") {
		t.Fatalf("expected invalid regex rule to not match")
	}
}
