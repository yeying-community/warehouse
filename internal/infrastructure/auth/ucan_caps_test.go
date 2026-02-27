package auth

import "testing"

func TestExtractAppCapsFromCapsRejectsWildcardAndInvalidAppID(t *testing.T) {
	caps := []UcanCapability{
		{Resource: "app:*", Action: "write"},
		{Resource: "app:good.example.com", Action: "read"},
		{Resource: "app:bad*", Action: "read"},
		{Resource: "app:bad/path", Action: "read"},
		{Resource: "profile", Action: "read"},
	}

	extracted := extractAppCapsFromCaps(caps, "app:")

	if !extracted.HasAppCaps {
		t.Fatalf("expected HasAppCaps=true")
	}

	actions, ok := extracted.AppCaps["good.example.com"]
	if !ok {
		t.Fatalf("expected app cap for good.example.com")
	}
	if len(actions) != 1 || actions[0] != "read" {
		t.Fatalf("unexpected actions: %#v", actions)
	}

	if !containsString(extracted.InvalidAppCaps, "app:*#write") {
		t.Fatalf("expected invalid cap app:*#write, got %#v", extracted.InvalidAppCaps)
	}
	if !containsString(extracted.InvalidAppCaps, "app:bad*#read") {
		t.Fatalf("expected invalid cap app:bad*#read, got %#v", extracted.InvalidAppCaps)
	}
	if !containsString(extracted.InvalidAppCaps, "app:bad/path#read") {
		t.Fatalf("expected invalid cap app:bad/path#read, got %#v", extracted.InvalidAppCaps)
	}
}

func TestExtractAppCapsFromCapsWithoutAppResource(t *testing.T) {
	caps := []UcanCapability{
		{Resource: "profile", Action: "read"},
	}

	extracted := extractAppCapsFromCaps(caps, "app:")
	if extracted.HasAppCaps {
		t.Fatalf("expected HasAppCaps=false")
	}
	if len(extracted.AppCaps) != 0 {
		t.Fatalf("expected empty AppCaps, got %#v", extracted.AppCaps)
	}
	if len(extracted.InvalidAppCaps) != 0 {
		t.Fatalf("expected empty InvalidAppCaps, got %#v", extracted.InvalidAppCaps)
	}
}

func containsString(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}
