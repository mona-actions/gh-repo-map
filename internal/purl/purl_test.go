package purl

import "testing"

func TestNormalizeEcosystem(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"go", "golang"},
		{"Go", "golang"},
		{"golang", "golang"},
		{"GOLANG", "golang"},
		{"pip", "pypi"},
		{"PIP", "pypi"},
		{"pypi", "pypi"},
		{"npm", "npm"},
		{"maven", "maven"},
		{"nuget", "nuget"},
		{"cargo", "cargo"},
		{"gem", "gem"},
		{"composer", "composer"},
		{"unknown", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeEcosystem(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeEcosystem(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParsePurl(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantEcosystem string
		wantName      string
	}{
		{
			name:          "npm scoped package",
			input:         "pkg:npm/%40org/auth-lib@2.0.0",
			wantEcosystem: "npm",
			wantName:      "@org/auth-lib",
		},
		{
			name:          "npm unscoped package",
			input:         "pkg:npm/lodash@4.17.21",
			wantEcosystem: "npm",
			wantName:      "lodash",
		},
		{
			name:          "maven package",
			input:         "pkg:maven/com.example/artifact@1.0",
			wantEcosystem: "maven",
			wantName:      "com.example:artifact",
		},
		{
			name:          "go module",
			input:         "pkg:golang/github.com/org/repo@v1.2.3",
			wantEcosystem: "golang",
			wantName:      "github.com/org/repo",
		},
		{
			name:          "go module with major version",
			input:         "pkg:golang/github.com/org/repo/v2@v2.1.0",
			wantEcosystem: "golang",
			wantName:      "github.com/org/repo",
		},
		{
			name:          "pypi package with hyphens",
			input:         "pkg:pypi/my-package@1.0",
			wantEcosystem: "pypi",
			wantName:      "my-package",
		},
		{
			name:          "nuget package",
			input:         "pkg:nuget/Newtonsoft.Json@13.0",
			wantEcosystem: "nuget",
			wantName:      "newtonsoft.json",
		},
		{
			name:          "cargo crate",
			input:         "pkg:cargo/serde@1.0",
			wantEcosystem: "cargo",
			wantName:      "serde",
		},
		{
			name:          "gem package",
			input:         "pkg:gem/Rails@7.0",
			wantEcosystem: "gem",
			wantName:      "rails",
		},
		{
			name:          "composer package",
			input:         "pkg:composer/vendor/package@1.0",
			wantEcosystem: "composer",
			wantName:      "vendor/package",
		},
		{
			name:          "raw name without pkg prefix returns empty",
			input:         "github.com/org/repo@v1.0.0",
			wantEcosystem: "",
			wantName:      "",
		},
		{
			name:          "npm scoped raw name returns empty",
			input:         "@babel/parser@7.24.7",
			wantEcosystem: "",
			wantName:      "",
		},
		{
			name:          "golang.org path returns empty",
			input:         "golang.org/x/sys@v0.39.0",
			wantEcosystem: "",
			wantName:      "",
		},
		{
			name:          "pip ecosystem normalizes to pypi",
			input:         "pkg:pip/requests@2.31.0",
			wantEcosystem: "pypi",
			wantName:      "requests",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ecosystem, name := ParsePurl(tt.input)
			if ecosystem != tt.wantEcosystem {
				t.Errorf("ecosystem = %q, want %q", ecosystem, tt.wantEcosystem)
			}
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
		})
	}
}

func TestNormalizeManifestName(t *testing.T) {
	tests := []struct {
		name      string
		ecosystem string
		rawName   string
		want      string
	}{
		{"npm scoped", "npm", "@org/auth-lib", "@org/auth-lib"},
		{"maven colon", "maven", "com.example:artifact", "com.example:artifact"},
		{"maven slash", "maven", "com.example/artifact", "com.example:artifact"},
		{"go plain", "golang", "github.com/org/repo", "github.com/org/repo"},
		{"go major v2", "golang", "github.com/org/repo/v2", "github.com/org/repo"},
		{"go major v10", "golang", "github.com/org/repo/v10", "github.com/org/repo"},
		{"pypi underscore", "pypi", "my_package", "my-package"},
		{"pypi dots", "pypi", "My.Package", "my-package"},
		{"pypi mixed", "pypi", "My_Package.name", "my-package-name"},
		{"pypi hyphens", "pypi", "my-package", "my-package"},
		{"nuget case", "nuget", "Newtonsoft.Json", "newtonsoft.json"},
		{"cargo exact", "cargo", "serde", "serde"},
		{"gem lowercase", "gem", "Rails", "rails"},
		{"composer exact", "composer", "vendor/package", "vendor/package"},
		{"unknown ecosystem", "unknown", "whatever", "whatever"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeManifestName(tt.ecosystem, tt.rawName)
			if got != tt.want {
				t.Errorf("NormalizeManifestName(%q, %q) = %q, want %q", tt.ecosystem, tt.rawName, got, tt.want)
			}
		})
	}
}

func TestParsePurlMatchesNormalizeManifestName(t *testing.T) {
	// Verify that SBOM purl and manifest raw names normalize to the same value
	pairs := []struct {
		purl     string
		eco      string
		manifest string
	}{
		{"pkg:npm/%40org/auth-lib@2.0.0", "npm", "@org/auth-lib"},
		{"pkg:maven/com.example/artifact@1.0", "maven", "com.example:artifact"},
		{"pkg:golang/github.com/org/repo/v2@v2.1.0", "golang", "github.com/org/repo"},
		{"pkg:pypi/My_Package@1.0", "pypi", "my-package"},
		{"pkg:nuget/Newtonsoft.Json@13.0", "nuget", "newtonsoft.json"},
	}

	for _, p := range pairs {
		_, purlName := ParsePurl(p.purl)
		manifestName := NormalizeManifestName(p.eco, p.manifest)
		if purlName != manifestName {
			t.Errorf("purl %q → %q, manifest %q → %q: they don't match!",
				p.purl, purlName, p.manifest, manifestName)
		}
	}
}

func TestNormalizeEcosystemConsistency(t *testing.T) {
	// Verify that "go" from manifest parsers and "golang" from purl normalize to the same key
	goManifest := NormalizeEcosystem("go")
	goPurl := NormalizeEcosystem("golang")
	if goManifest != goPurl {
		t.Errorf("go/golang mismatch: manifest=%q, purl=%q", goManifest, goPurl)
	}

	pipManifest := NormalizeEcosystem("pip")
	pypiPurl := NormalizeEcosystem("pypi")
	if pipManifest != pypiPurl {
		t.Errorf("pip/pypi mismatch: manifest=%q, purl=%q", pipManifest, pypiPurl)
	}
}
