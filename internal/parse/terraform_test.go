package parse

import "testing"

func TestParseTerraform(t *testing.T) {
	knownOrgs := map[string]bool{"my-org": true, "infra": true}

	tests := []struct {
		name      string
		content   string
		wantEdges int
		wantFirst string
	}{
		{
			name: "github.com short source",
			content: `
module "vpc" {
  source = "github.com/infra/terraform-modules//vpc?ref=v1.2.0"
}
`,
			wantEdges: 1,
			wantFirst: "infra/terraform-modules",
		},
		{
			name: "git::https source",
			content: `
module "database" {
  source = "git::https://github.com/my-org/tf-database.git?ref=main"
}
`,
			wantEdges: 1,
			wantFirst: "my-org/tf-database",
		},
		{
			name: "git::ssh source",
			content: `
module "networking" {
  source = "git::ssh://git@github.com/infra/tf-networking.git"
}
`,
			wantEdges: 1,
			wantFirst: "infra/tf-networking",
		},
		{
			name: "terraform registry source (ignored)",
			content: `
module "s3" {
  source  = "terraform-aws-modules/s3-bucket/aws"
  version = "3.0.0"
}
`,
			wantEdges: 0,
		},
		{
			name: "comment skipped",
			content: `
# source = "github.com/my-org/old-module"
`,
			wantEdges: 0,
		},
		{
			name: "deduplication",
			content: `
module "a" {
  source = "github.com/infra/shared"
}
module "b" {
  source = "github.com/infra/shared"
}
`,
			wantEdges: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edges := ParseTerraform(tt.content, "main.tf", "", knownOrgs)
			if len(edges) != tt.wantEdges {
				t.Errorf("got %d edges, want %d", len(edges), tt.wantEdges)
			}
			if tt.wantFirst != "" && len(edges) > 0 && edges[0].Target != tt.wantFirst {
				t.Errorf("edges[0].Target = %q, want %q", edges[0].Target, tt.wantFirst)
			}
		})
	}
}
