# Review Notes

## Consistency Check

### Cross-Document Consistency

All `.agents/summary/` documents are consistent on these key facts:
- Go version: 1.25.7 (codebase_info, dependencies)
- controller-runtime version: v0.19.7 via replace (architecture, dependencies)
- Kubernetes API version: v0.35.1 (codebase_info, dependencies)
- Number of operators: 4 (architecture, components)
- Number of binaries: 6 (codebase_info, components)
- Platform count: 9 platform implementations including None (architecture, interfaces, data_models)
- API groups: 5 groups, 14 root type registrations, 13 unique CRD kind names (interfaces, data_models)
- v2 component count: 39 directories (architecture)
- Platform interface: 7 methods (architecture, interfaces)

### Known Divergence: AGENTS.md vs .agents/summary/

The project's `AGENTS.md` (which is synced with `CLAUDE.md` and maintained outside this summary process) contains version references that differ from what `go.mod` shows:
- AGENTS.md states "Kubernetes 0.34.x APIs" — go.mod shows `k8s.io/api v0.35.1`
- AGENTS.md states "Controller-runtime 0.22.x" — go.mod shows v0.22.4 in `require` but `replace => v0.19.7`

The `.agents/summary/` files use the verified values. The AGENTS.md discrepancy is inherited content and should be corrected in the upstream CLAUDE.md.

## Completeness Check

### Well-Documented Areas
- Core operator architecture and controller listings (all 14 hypershift-operator controller directories, all 6 CPO controller directories covered)
- CRD type definitions and relationships (all 14 root type registrations across 5 API groups)
- Platform abstraction pattern (all 7 Platform interface methods, all 9 implementations)
- v2 component framework design (all 39 component directories categorized)
- Build/test/CI workflows
- Dependency inventory with replace directive rationale (exact fork repository names)

### Areas With Limited Detail

1. **HostedControlPlane field-level documentation**: Key spec/status fields are listed but not at the same depth as HostedCluster or NodePool. The type mirrors HostedClusterSpec closely, so the HostedCluster documentation serves as a reference.

2. **Platform-specific implementation internals**: Each platform's implementation of the Platform interface is identified, but the internal logic (exact AWS resources created, Azure networking setup) is not detailed. Implementation details change frequently and are better read from source.

3. **v2 component adapt functions**: All 39 components are listed by name and category, but the specific resources each component creates beyond its primary workload require reading the component's adapt function and embedded YAML manifests.

4. **E2E test coverage map**: Test files are listed by name but there is no mapping of which tests cover which features or platforms.

5. **Scheduling controller interaction model**: The request-serving isolation controllers are listed with purpose but their scheduling algorithm and interaction model are not detailed.

6. **Release process**: The release workflow is mentioned but not documented as a workflow diagram. Release branching strategy is noted in renovate config but not elaborated.

### No Language/Framework Gaps
- Pure Go project — no unsupported language issues.
- Shell scripts in `hack/` are build tooling, not core logic.
- Python tooling (codespell, gitlint, MkDocs) is noted in dependencies but not analyzed as code.

## Recommendations

1. **For deeper component analysis**: Read individual component source files in `control-plane-operator/controllers/hostedcontrolplane/v2/` when working on specific components.
2. **For platform-specific details**: Read platform implementation files in `hypershift-operator/controllers/hostedcluster/internal/platform/` and corresponding type files in `api/hypershift/v1beta1/`.
3. **For test pattern guidance**: Read `test/e2e/v2/AGENTS.md` and `test/envtest/README.md` for framework-specific conventions.
4. **For API conventions**: Read `api/AGENTS.md` and `.golangci.yml` (root and api/) for enforced conventions.
5. **Keep documentation current**: Re-run the analysis when significant architectural changes occur (new operators, new API groups, new platforms).
