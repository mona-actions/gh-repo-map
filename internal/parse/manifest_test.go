package parse

import "testing"

func TestParsePackageJSON(t *testing.T) {
content := `{
  "name": "@my-org/auth-service",
  "dependencies": {
    "@my-org/shared-lib": "^2.0.0",
    "express": "^4.18.0"
  },
  "devDependencies": {
    "jest": "^29.0.0"
  }
}`
consumed, published := ParsePackageJSON(content)
if len(published) != 1 || published[0].Name != "@my-org/auth-service" {
t.Errorf("published = %v, want @my-org/auth-service", published)
}
if len(consumed) != 3 {
t.Errorf("got %d consumed, want 3", len(consumed))
}
}

func TestParseGoMod(t *testing.T) {
content := `module github.com/my-org/my-service

go 1.21

require (
github.com/my-org/shared-lib v1.2.3
github.com/gin-gonic/gin v1.9.0
)
`
consumed, published := ParseGoMod(content)
if len(published) != 1 || published[0].Name != "github.com/my-org/my-service" {
t.Errorf("published = %v", published)
}
if len(consumed) != 2 {
t.Errorf("got %d consumed, want 2", len(consumed))
}
}

func TestParsePomXML(t *testing.T) {
content := `<?xml version="1.0"?>
<project>
  <groupId>com.myorg</groupId>
  <artifactId>auth-service</artifactId>
  <dependencies>
    <dependency>
      <groupId>com.myorg</groupId>
      <artifactId>shared-lib</artifactId>
      <version>2.0.0</version>
    </dependency>
  </dependencies>
</project>`
consumed, published := ParsePomXML(content)
if len(published) != 1 || published[0].Name != "com.myorg:auth-service" {
t.Errorf("published = %v", published)
}
if len(consumed) != 1 || consumed[0].Name != "com.myorg:shared-lib" {
t.Errorf("consumed = %v", consumed)
}
}

func TestParsePyprojectTOML(t *testing.T) {
content := `[project]
name = "my-utils"
dependencies = [
  "requests>=2.28",
  "click",
]
`
consumed, published := ParsePyprojectTOML(content)
if len(published) != 1 || published[0].Name != "my-utils" {
t.Errorf("published = %v", published)
}
if len(consumed) != 2 {
t.Errorf("got %d consumed, want 2", len(consumed))
}
}

func TestParseRequirementsTxt(t *testing.T) {
content := `requests>=2.28
# comment
click
-e git+https://github.com/org/repo.git
flask==2.3.0
`
consumed := ParseRequirementsTxt(content)
if len(consumed) != 3 {
t.Errorf("got %d consumed, want 3", len(consumed))
}
}

func TestParseCsproj(t *testing.T) {
content := `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <PackageId>MyOrg.SharedLib</PackageId>
  </PropertyGroup>
  <ItemGroup>
    <PackageReference Include="Newtonsoft.Json" Version="13.0.3" />
    <PackageReference Include="MyOrg.Utils" Version="1.0.0" />
  </ItemGroup>
</Project>`
consumed, published := ParseCsproj(content)
if len(published) != 1 || published[0].Name != "MyOrg.SharedLib" {
t.Errorf("published = %v", published)
}
if len(consumed) != 2 {
t.Errorf("got %d consumed, want 2", len(consumed))
}
}

func TestParseCargoToml(t *testing.T) {
content := `[package]
name = "my-crate"
version = "0.1.0"

[dependencies]
serde = "1.0"
tokio = "1.28"
`
consumed, published := ParseCargoToml(content)
if len(published) != 1 || published[0].Name != "my-crate" {
t.Errorf("published = %v", published)
}
if len(consumed) != 2 {
t.Errorf("got %d consumed, want 2", len(consumed))
}
}

func TestParseComposerJSON(t *testing.T) {
content := `{
  "name": "myorg/auth-service",
  "require": {
    "php": "^8.1",
    "ext-json": "*",
    "myorg/shared-lib": "^2.0"
  }
}`
consumed, published := ParseComposerJSON(content)
if len(published) != 1 || published[0].Name != "myorg/auth-service" {
t.Errorf("published = %v", published)
}
// Should have 1 consumed (php and ext-json are skipped)
if len(consumed) != 1 {
t.Errorf("got %d consumed, want 1", len(consumed))
}
}

func TestParseGemfile(t *testing.T) {
content := `source "https://rubygems.org"
gem "rails", "~> 7.0"
gem "my-org-utils"
`
consumed := ParseGemfile(content)
if len(consumed) != 2 {
t.Errorf("got %d consumed, want 2", len(consumed))
}
}

func TestParseGemspec(t *testing.T) {
content := `Gem::Specification.new do |spec|
  spec.name = "my-gem"
  spec.version = "1.0.0"
end
`
published := ParseGemspec(content)
if len(published) != 1 || published[0].Name != "my-gem" {
t.Errorf("published = %v", published)
}
}
