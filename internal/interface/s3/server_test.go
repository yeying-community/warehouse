package s3

import (
	"encoding/xml"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeying-community/warehouse/internal/application/service"
	"github.com/yeying-community/warehouse/internal/domain/s3credential"
	"github.com/yeying-community/warehouse/internal/domain/user"
)

func TestHandleDeleteObjectsDeletesRequestedKeys(t *testing.T) {
	root := t.TempDir()
	objects := service.NewObjectService(root)
	owner := user.NewUser("alice", "alice")
	if _, err := objects.PutForUser(t.Context(), owner, "personal", "docs/report.txt", strings.NewReader("hello")); err != nil {
		t.Fatalf("put object: %v", err)
	}

	server := &Server{objects: objects}
	credential := &s3credential.Credential{
		OwnerUserID: owner.ID,
		RootPath:    "/personal",
		Permissions: "delete",
	}
	req := httptest.NewRequest("POST", "/personal/?delete=", strings.NewReader(`<Delete><Object><Key>docs/report.txt</Key></Object></Delete>`))
	resp := httptest.NewRecorder()

	server.handleDeleteObjects(resp, req, credential, owner, "personal", "")

	if resp.Code != 200 {
		t.Fatalf("status = %d, want 200", resp.Code)
	}
	if _, err := objects.Stat(t.Context(), owner.Directory, "personal", "docs/report.txt"); err == nil {
		t.Fatal("expected object to be deleted")
	}
	var result deleteObjectsResult
	if err := xml.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(result.Deleted) != 1 || result.Deleted[0].Key != "docs/report.txt" {
		t.Fatalf("unexpected deleted response: %+v", result)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %+v", result.Errors)
	}
}
