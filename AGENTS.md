# AGENTS.md

This file provides guidance to AI coding agents working in this repository.

## Project Overview

`axonhub-operator` is a Kubernetes operator that manages [AxonHub](https://github.com/looplj/axonhub)
(an all-in-one AI gateway platform, Go backend + React frontend). The operator's job is to deploy
and manage AxonHub instances on Kubernetes through the `AxonHub` custom resource.

## Technology

- Go (kubebuilder v4 layout, controller-runtime)
- CRD group: `axonhub.onrender.com`, version `v1alpha1`, kind `AxonHub`
- Module: `github.com/amghazanfari/axonhub-operator`

## Repository Map

- `api/v1alpha1/axonhub_types.go` — **the only file to edit** for CRD spec/status changes
- `api/v1alpha1/zz_generated.deepcopy.go` — GENERATED, never edit
- `internal/controller/axonhub_controller.go` — reconcile logic
- `internal/controller/axonhub_controller_test.go` — envtest-based controller tests
- `config/` — kustomize manifests (CRD, RBAC, manager deployment, samples)
- `config/samples/` — example AxonHub CRs
- `cmd/main.go` — manager entrypoint
- `PROJECT` — kubebuilder project config, do not edit manually

## Hard Rules

1. After editing `api/v1alpha1/*.go`, ALWAYS run `make generate && make manifests` so deepcopy
   functions and CRD YAML stay in sync. Never hand-edit generated files
   (`zz_generated.deepcopy.go`, `config/crd/bases/*`).
2. RBAC: when the controller touches a new resource type, add the required
   `kubebuilder:rbac` markers to the controller and regenerate manifests. CI will fail on missing permissions.
3. Do not commit unless `make test` and `make lint` pass.
4. Work in small vertical slices: one spec field + its reconcile wiring + tests per change.
5. All owned child objects must carry labels identifying the owning AxonHub CR, and the
   controller must set an OwnerReference on every object it creates.

## Commands

- `make generate` — regenerate deepcopy code
- `make manifests` — regenerate CRD/RBAC manifests
- `make test` — unit + envtest tests (needs kube-apiserver binaries; `make envtest` to fetch)
- `make lint` — golangci-lint
- `make docker-build` — build manager image
- `make deploy` — deploy to current kubectl context (uses kind in dev)
- `kubectl apply -f config/samples/` — apply sample CR

## CRD Scope (current)

The `AxonHub` spec manages a full AxonHub deployment:

- `spec.replicas` — AxonHub pod count
- `spec.image` — container image (with a sane default)
- `spec.version` — AxonHub version tag
- persistence (SQLite on a PVC by default)
- Service exposure

Status must report readiness, observed generation, and conditions following Kubernetes
API conventions.

## Conventions

- Follow standard controller patterns: idempotent reconcile, no panics in reconcile, log with
  the reconciler's logger, return errors wrapped with context.
- Use `Conditions` (via `meta.SetStatusCondition`) for status reporting.
- Prefer Server-Side Apply / `controllerutil.CreateOrUpdate` for child resources.
- Never store secrets in the repo. Sample CRs must not contain real credentials.
