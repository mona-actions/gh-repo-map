package parse

import (
"testing"

"github.com/mona-actions/gh-repo-map/internal/model"
)

func TestParseActionsWorkflow(t *testing.T) {
knownOrgs := map[string]bool{"my-org": true, "infra": true}

tests := []struct {
name           string
content        string
sourceFile     string
wantEdges      int
wantUnresolved int
wantTypes      []model.DependencyType
}{
{
name: "reusable workflow",
content: `
jobs:
  build:
    uses: my-org/shared-workflows/.github/workflows/ci.yml@main
`,
sourceFile: ".github/workflows/build.yml",
wantEdges:  1,
wantTypes:  []model.DependencyType{model.DepTypeWorkflow},
},
{
name: "action reference",
content: `
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: my-org/checkout-action@v2
      - uses: infra/deploy-action@v1
`,
sourceFile: ".github/workflows/ci.yml",
wantEdges:  2,
wantTypes:  []model.DependencyType{model.DepTypeAction, model.DepTypeAction},
},
{
name: "skips local and docker actions",
content: `
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: ./local-action
      - uses: docker://alpine:3.14
      - uses: my-org/real-action@v1
`,
sourceFile: ".github/workflows/ci.yml",
wantEdges:  1,
wantTypes:  []model.DependencyType{model.DepTypeAction},
},
{
name: "skips unknown orgs",
content: `
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: my-org/my-action@v1
`,
sourceFile: ".github/workflows/ci.yml",
wantEdges:  1,
},
{
name: "templated uses logged as unresolvable",
content: `
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: ${{ matrix.action }}
`,
sourceFile:     ".github/workflows/matrix.yml",
wantEdges:      0,
wantUnresolved: 1,
},
{
name: "action with subpath",
content: `
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: my-org/monorepo/actions/build@main
`,
sourceFile: ".github/workflows/ci.yml",
wantEdges:  1,
wantTypes:  []model.DependencyType{model.DepTypeAction},
},
}

for _, tt := range tests {
t.Run(tt.name, func(t *testing.T) {
edges, unresolvable := ParseActionsWorkflow(tt.content, tt.sourceFile, knownOrgs)
if len(edges) != tt.wantEdges {
t.Errorf("got %d edges, want %d", len(edges), tt.wantEdges)
}
if tt.wantUnresolved > 0 && len(unresolvable) != tt.wantUnresolved {
t.Errorf("got %d unresolvable, want %d", len(unresolvable), tt.wantUnresolved)
}
for i, wantType := range tt.wantTypes {
if i < len(edges) && edges[i].Type != wantType {
t.Errorf("edge[%d].Type = %q, want %q", i, edges[i].Type, wantType)
}
}
})
}
}
