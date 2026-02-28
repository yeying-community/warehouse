package service

import (
	"context"
	"path"
	"strings"

	"github.com/yeying-community/warehouse/internal/domain/auth"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
)

type appScopeInfo struct {
	active  bool
	prefix  string
	actions map[string]appActionSet
}

type appActionSet struct {
	Read   bool
	Write  bool
	Create bool
	Update bool
	Delete bool
	Move   bool
	Copy   bool
}

func resolveAppScope(ctx context.Context, cfg *config.Config) (appScopeInfo, error) {
	if cfg == nil {
		return appScopeInfo{}, nil
	}
	requireAppScope := requiresAppScope(cfg)
	prefix := normalizeScopePrefix(cfg.Web3.UCAN.AppScope.PathPrefix)

	ucanCtx, ok := middleware.GetUcanContext(ctx)
	if !ok {
		return appScopeInfo{}, nil
	}
	if len(ucanCtx.InvalidAppCaps) > 0 {
		return appScopeInfo{}, auth.ErrAppScopeDenied
	}
	if requireAppScope && !ucanCtx.HasAppCaps {
		return appScopeInfo{}, auth.ErrAppScopeRequired
	}
	if len(ucanCtx.AppCaps) == 0 {
		if requireAppScope {
			return appScopeInfo{active: true, prefix: prefix, actions: map[string]appActionSet{}}, nil
		}
		return appScopeInfo{}, nil
	}
	allowedActions, hasFilter := parseAllowedActions(cfg.Web3.UCAN.RequiredAction)
	actions := make(map[string]appActionSet, len(ucanCtx.AppCaps))
	for appID, rawActions := range ucanCtx.AppCaps {
		set := buildActionSet(rawActions)
		if hasFilter {
			set = applyAllowedFilter(set, allowedActions)
		}
		actions[appID] = set
	}
	return appScopeInfo{
		active:  true,
		prefix:  prefix,
		actions: actions,
	}, nil
}

func enforceAppScope(ctx context.Context, cfg *config.Config, rawPath string, requiredActions ...string) error {
	scope, err := resolveAppScope(ctx, cfg)
	if err != nil {
		return err
	}
	if !scope.active {
		return nil
	}
	if !scope.allowsAny(rawPath, requiredActions...) {
		return auth.ErrAppScopeDenied
	}
	return nil
}

func (s appScopeInfo) allowsAny(rawPath string, requiredActions ...string) bool {
	if !s.active {
		return true
	}
	appID, ok := s.matchAppID(rawPath)
	if !ok {
		return false
	}
	set, ok := s.actions[appID]
	if !ok {
		return false
	}
	if len(requiredActions) == 0 {
		return set.allows("read")
	}
	for _, action := range requiredActions {
		if set.allows(action) {
			return true
		}
	}
	return false
}

func (s appActionSet) allows(requiredAction string) bool {
	action := strings.ToLower(strings.TrimSpace(requiredAction))
	switch action {
	case "", "read":
		return s.Read || s.Write
	case "write":
		return s.Write || s.Create || s.Update || s.Delete || s.Move || s.Copy
	case "create":
		return s.Write || s.Create
	case "update":
		return s.Write || s.Update
	case "delete":
		return s.Write || s.Delete
	case "move":
		return s.Write || s.Move
	case "copy":
		return s.Write || s.Copy
	default:
		return false
	}
}

func (s appScopeInfo) matchAppID(rawPath string) (string, bool) {
	normalized := normalizeScopePath(rawPath)
	prefix := strings.TrimSuffix(s.prefix, "/")
	if prefix == "" {
		prefix = "/apps"
	}
	if normalized == prefix || normalized == prefix+"/" {
		return "", false
	}
	if !strings.HasPrefix(normalized, prefix+"/") {
		return "", false
	}
	rest := strings.TrimPrefix(normalized, prefix+"/")
	if rest == "" {
		return "", false
	}
	parts := strings.SplitN(rest, "/", 2)
	appID := strings.TrimSpace(parts[0])
	if appID == "" {
		return "", false
	}
	return appID, true
}

func normalizeScopePrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "/apps"
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	prefix = path.Clean(prefix)
	if prefix == "." || prefix == "/" {
		prefix = "/apps"
	}
	return prefix
}

func normalizeScopePath(rawPath string) string {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return "/"
	}
	clean := path.Clean("/" + strings.TrimLeft(rawPath, "/"))
	if clean == "." {
		return "/"
	}
	return clean
}

func parseAllowedActions(raw string) (appActionSet, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return appActionSet{}, false
	}
	parts := splitActionList(raw)
	return buildActionSet(parts), true
}

func buildActionSet(actions []string) appActionSet {
	set := appActionSet{}
	for _, raw := range actions {
		action := strings.ToLower(strings.TrimSpace(raw))
		switch {
		case action == "":
			continue
		case action == "*", action == "access", strings.HasSuffix(action, "*"):
			set.Read = true
			set.Write = true
			set.Create = true
			set.Update = true
			set.Delete = true
			set.Move = true
			set.Copy = true
		case action == "read":
			set.Read = true
		case action == "write":
			set.Write = true
		case action == "create":
			set.Create = true
		case action == "update":
			set.Update = true
		case action == "delete":
			set.Delete = true
		case action == "move":
			set.Move = true
		case action == "copy":
			set.Copy = true
		}
	}
	return set
}

func applyAllowedFilter(set appActionSet, allowed appActionSet) appActionSet {
	if !allowed.Read {
		set.Read = false
	}
	if !allowed.Write {
		set.Write = false
	}
	if !allowed.Create {
		set.Create = false
	}
	if !allowed.Update {
		set.Update = false
	}
	if !allowed.Delete {
		set.Delete = false
	}
	if !allowed.Move {
		set.Move = false
	}
	if !allowed.Copy {
		set.Copy = false
	}
	return set
}

func splitActionList(raw string) []string {
	raw = strings.ReplaceAll(raw, "|", ",")
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func requiresAppScope(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	resource := strings.TrimSpace(cfg.Web3.UCAN.RequiredResource)
	if resource == "" {
		return false
	}
	resource = strings.ReplaceAll(resource, "|", ",")
	for _, part := range strings.Split(resource, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		if strings.HasPrefix(part, "app:") {
			return true
		}
	}
	return false
}
