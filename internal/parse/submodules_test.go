package parse

import "testing"

func TestParseGitmodules(t *testing.T) {
	knownOrgs := map[string]bool{"my-org": true, "infra": true}

	content := `[submodule "vendor/lib"]
path = vendor/lib
url = git@github.com:my-org/shared-lib.git
[submodule "external"]
path = external/tool
url = https://github.com/other-org/tool.git
[submodule "infra-utils"]
path = infra/utils
url = https://github.com/infra/terraform-modules.git
`

	edges := ParseGitmodules(content, ".gitmodules", "", knownOrgs)
	if len(edges) != 2 {
		t.Fatalf("got %d edges, want 2", len(edges))
	}

	if edges[0].Target != "my-org/shared-lib" {
		t.Errorf("edges[0].Target = %q, want %q", edges[0].Target, "my-org/shared-lib")
	}
	if edges[1].Target != "infra/terraform-modules" {
		t.Errorf("edges[1].Target = %q, want %q", edges[1].Target, "infra/terraform-modules")
	}
}

func TestParseGitmodulesGHES(t *testing.T) {
	knownOrgs := map[string]bool{"my-org": true}
	content := `[submodule "lib"]
path = lib
url = https://github.mycompany.com/my-org/shared-lib.git
`
	edges := ParseGitmodules(content, ".gitmodules", "github.mycompany.com", knownOrgs)
	if len(edges) != 1 {
		t.Fatalf("got %d edges, want 1", len(edges))
	}
	if edges[0].Target != "my-org/shared-lib" {
		t.Errorf("target = %q, want %q", edges[0].Target, "my-org/shared-lib")
	}
}
