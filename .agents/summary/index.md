# HyperShift Codebase Knowledge Base

This index serves as the primary entry point for AI assistants working with the HyperShift codebase. Each linked document provides detailed analysis of a specific aspect of the system. Use this index to determine which documents to consult based on the type of question or task.

## How to Use This Knowledge Base

1. **Start here**: This index contains summaries of each document's content to help determine relevance without reading every file.
2. **Navigate by topic**: Use the table below to find the most relevant document for your question.
3. **Cross-reference**: Documents reference each other — follow cross-references for deeper understanding.
4. **Prefer current code**: This documentation is a snapshot. When acting on specific claims about files, APIs, or functions, verify against the current codebase.

## Document Index

| Document | Summary | Consult When... |
|----------|---------|-----------------|
| [codebase_info.md](codebase_info.md) | Project identity, multi-module structure, technologies, binaries, platforms, container images | You need basic project facts: language version, module structure, what binaries exist, supported platforms |
| [architecture.md](architecture.md) | System architecture diagrams, operator design, controller listings, v2 component framework, platform abstraction, design patterns | Understanding how operators interact, what controllers exist, how the v2 framework works, multi-cloud abstraction |
| [components.md](components.md) | Detailed component catalog: operators, CLIs, sidecars, etcd utilities, infrastructure components, support library packages | Finding a specific component, understanding what a binary/package does, locating support utilities |
| [interfaces.md](interfaces.md) | CRD API surface, internal Go interfaces (Platform, ControlPlaneComponent, etc.), generated clients, CLI commands, validation approach, metrics | Working with APIs, understanding CRD structure, finding client packages, validation rules |
| [data_models.md](data_models.md) | CRD type hierarchy, all type definitions with spec/status fields, platform-specific types, code generation markers | Understanding CRD types and their fields, type relationships, feature gates, code generation |
| [workflows.md](workflows.md) | Cluster lifecycle (create/delete), component reconciliation flow, NodePool lifecycle, certificate management, etcd operations, CI pipeline, development workflow | Understanding operational flows, how to contribute, CI process, how components interact at runtime |
| [dependencies.md](dependencies.md) | All Go module dependencies categorized by domain, version information, replace directives, build tooling | Dependency questions, understanding version constraints, replace directive reasons, build tool setup |

## Quick Reference

### Key Entry Points
- **Main binary**: `main.go` (hypershift CLI)
- **Operator entry**: `hypershift-operator/main.go`
- **CPO entry**: `control-plane-operator/main.go`
- **API types**: `api/hypershift/v1beta1/` (separate Go module)
- **Support library**: `support/`
- **Tests**: `test/e2e/`, `test/e2e/v2/`, `test/envtest/`, `test/integration/`

### Architecture Quick Facts
- Go 1.25.7, Kubernetes v0.35.1 APIs, controller-runtime v0.19.7
- 4 operators: hypershift-operator, CPO, PKI operator, karpenter-operator
- 6 binaries: hypershift, hcp, hypershift-operator, control-plane-operator, control-plane-pki-operator, karpenter-operator
- 8 platforms: AWS, Azure, GCP, IBM Cloud, KubeVirt, OpenStack, Agent, PowerVS
- 13 CRD kinds across 5 API groups
- ~40 control plane components managed by CPO v2 framework
- Multi-module repo: root + api/ + hack/tools/

### Common Tasks Mapping

| Task | Start With |
|------|-----------|
| Add a new CRD field | [data_models.md](data_models.md) for type patterns, [interfaces.md](interfaces.md) for validation, then `api/hypershift/v1beta1/` |
| Add a new control plane component | [architecture.md](architecture.md) for v2 framework, then `control-plane-operator/controllers/hostedcontrolplane/v2/` |
| Add platform support for a feature | [architecture.md](architecture.md) for Platform interface, [components.md](components.md) for platform packages |
| Debug a controller | [architecture.md](architecture.md) for controller listing, [components.md](components.md) for package locations |
| Understand a workflow | [workflows.md](workflows.md) for lifecycle diagrams |
| Check dependency versions | [dependencies.md](dependencies.md) for version tables and replace directives |
| Run tests | [workflows.md](workflows.md) for CI pipeline, project CLAUDE.md for make targets |
| Understand private networking | [architecture.md](architecture.md) for private connectivity patterns, [interfaces.md](interfaces.md) for endpoint CRDs |
