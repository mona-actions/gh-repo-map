package model

// NewPackageEdge creates a DependencyEdge of type "package".
func NewPackageEdge(repo, sourceFile, pkgName, ecosystem, version string, targetScanned bool) DependencyEdge {
return DependencyEdge{
Repo:          repo,
Type:          DepTypePackage,
Confidence:    ConfidenceHigh,
TargetScanned: targetScanned,
Detail:        PackageDetail{PackageName: pkgName, Ecosystem: ecosystem, Version: version},
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
Detail:        WorkflowDetail{Uses: uses},
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
Detail:        ActionDetail{Uses: uses},
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
Detail:        SubmoduleDetail{URL: url, Path: path},
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
Detail:        DockerDetail{Image: image},
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
Detail:        TerraformDetail{Source: source, Ref: ref},
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
Detail:        ScriptDetail{Match: match, MatchType: matchType},
SourceFile:    sourceFile,
}
}
