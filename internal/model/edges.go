package model

// NewPackageEdge creates a DependencyEdge of type "package".
func NewPackageEdge(repo, sourceFile, pkgName, ecosystem, version string, targetScanned bool) DependencyEdge {
return DependencyEdge{
Repo:          repo,
Type:          DepTypePackage,
Confidence:    ConfidenceHigh,
TargetScanned: targetScanned,
Detail:        PackageDetail{Type: DepTypePackage, PackageName: pkgName, Ecosystem: ecosystem, Version: version},
SourceFile:    sourceFile,
}
}

// NewWorkflowEdge creates a DependencyEdge of type "workflow".
func NewWorkflowEdge(repo, sourceFile, uses string, targetScanned bool) DependencyEdge {
return DependencyEdge{
Repo:          repo,
Type:          DepTypeWorkflow,
Confidence:    ConfidenceHigh,
TargetScanned: targetScanned,
Detail:        WorkflowDetail{Type: DepTypeWorkflow, Uses: uses},
SourceFile:    sourceFile,
}
}

// NewActionEdge creates a DependencyEdge of type "action".
func NewActionEdge(repo, sourceFile, uses string, targetScanned bool) DependencyEdge {
return DependencyEdge{
Repo:          repo,
Type:          DepTypeAction,
Confidence:    ConfidenceHigh,
TargetScanned: targetScanned,
Detail:        ActionDetail{Type: DepTypeAction, Uses: uses},
SourceFile:    sourceFile,
}
}

// NewSubmoduleEdge creates a DependencyEdge of type "submodule".
func NewSubmoduleEdge(repo, sourceFile, url, path string, targetScanned bool) DependencyEdge {
return DependencyEdge{
Repo:          repo,
Type:          DepTypeSubmodule,
Confidence:    ConfidenceHigh,
TargetScanned: targetScanned,
Detail:        SubmoduleDetail{Type: DepTypeSubmodule, URL: url, Path: path},
SourceFile:    sourceFile,
}
}

// NewDockerEdge creates a DependencyEdge of type "docker".
func NewDockerEdge(repo, sourceFile, image string, targetScanned bool) DependencyEdge {
return DependencyEdge{
Repo:          repo,
Type:          DepTypeDocker,
Confidence:    ConfidenceHigh,
TargetScanned: targetScanned,
Detail:        DockerDetail{Type: DepTypeDocker, Image: image},
SourceFile:    sourceFile,
}
}

// NewTerraformEdge creates a DependencyEdge of type "terraform".
func NewTerraformEdge(repo, sourceFile, source, ref string, targetScanned bool) DependencyEdge {
return DependencyEdge{
Repo:          repo,
Type:          DepTypeTerraform,
Confidence:    ConfidenceHigh,
TargetScanned: targetScanned,
Detail:        TerraformDetail{Type: DepTypeTerraform, Source: source, Ref: ref},
SourceFile:    sourceFile,
}
}

// NewScriptEdge creates a DependencyEdge of type "script".
// Scripts always get low confidence since they rely on regex matching.
func NewScriptEdge(repo, sourceFile, match, matchType string, targetScanned bool) DependencyEdge {
return DependencyEdge{
Repo:          repo,
Type:          DepTypeScript,
Confidence:    ConfidenceLow,
TargetScanned: targetScanned,
Detail:        ScriptDetail{Type: DepTypeScript, Match: match, MatchType: matchType},
SourceFile:    sourceFile,
}
}
