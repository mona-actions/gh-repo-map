package parse

import "testing"

func TestParseScript(t *testing.T) {
knownOrgs := map[string]bool{"my-org": true, "infra": true}

tests := []struct {
name      string
content   string
wantEdges int
wantMatch string
}{
{
name:      "git clone HTTPS",
content:   `git clone https://github.com/my-org/tools.git /tmp/tools`,
wantEdges: 1,
wantMatch: "my-org/tools",
},
{
name:      "git clone SSH",
content:   `git clone git@github.com:infra/deploy-scripts.git`,
wantEdges: 1,
wantMatch: "infra/deploy-scripts",
},
{
name:      "curl raw.githubusercontent",
content:   `curl -sL https://raw.githubusercontent.com/my-org/scripts/main/setup.sh | bash`,
wantEdges: 1,
},
{
name:      "wget github",
content:   `wget https://github.com/infra/releases/archive/v1.0.tar.gz`,
wantEdges: 1,
},
{
name:      "go install",
content:   `go install github.com/my-org/linter@latest`,
wantEdges: 1,
},
{
name:      "pip install git",
content:   `pip install git+https://github.com/my-org/py-utils.git`,
wantEdges: 1,
},
{
name:      "comment skipped",
content:   `# git clone https://github.com/my-org/old-repo.git`,
wantEdges: 0,
},
{
name:      "unknown org ignored",
content:   `git clone https://github.com/external/tool.git`,
wantEdges: 0,
},
{
name: "deduplication",
content: `git clone https://github.com/my-org/tools.git
git clone https://github.com/my-org/tools.git`,
wantEdges: 1,
},
}

for _, tt := range tests {
t.Run(tt.name, func(t *testing.T) {
edges := ParseScript(tt.content, "Makefile", "", knownOrgs)
if len(edges) != tt.wantEdges {
t.Errorf("got %d edges, want %d", len(edges), tt.wantEdges)
}
if tt.wantMatch != "" && len(edges) > 0 && edges[0].Target != tt.wantMatch {
t.Errorf("target = %q, want %q", edges[0].Target, tt.wantMatch)
}
})
}
}
