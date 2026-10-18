# axonhub-operator architecture

## Overview

`axonhub-operator` is a Kubernetes operator built with
[kubebuilder v4](https://book.kubebuilder.io/) and
[controller-runtime](https://github.com/kubernetes-sigs/controller-runtime).
It manages [AxonHub](https://github.com/looplj/axonhub) instances through the
`AxonHub` custom resource (group `axonhub.onrender.com`, version `v1alpha1`).

## Repository layout

| Path | Purpose |
| --- | --- |
| `api/v1alpha1/axonhub_types.go` | CRD spec/status types — the only file to edit for API changes |
| `api/v1alpha1/zz_generated.deepcopy.go` | generated deepcopy code — never edit |
| `internal/controller/axonhub_controller.go` | reconcile logic |
| `internal/controller/axonhub_controller_test.go` | envtest-based controller tests |
| `cmd/main.go` | manager entrypoint |
| `config/` | kustomize manifests (CRD, RBAC, manager deployment, samples) |
| `config/samples/` | example AxonHub CRs |
| `test/e2e/` | end-to-end tests |
| `hack/`, `Makefile` | build/test tooling |
| `PROJECT` | kubebuilder project config |

## Reconciliation model

The controller (`AxonHubReconciler`) follows the standard
observe → diff → act loop:

1. **Fetch** the `AxonHub` resource; ignore NotFound (owned children are
   garbage-collected via owner references).
2. **Ensure the Deployment** (`reconcileDeployment`) using
   `controllerutil.CreateOrUpdate`:
   - The controller reference, labels, replicas, and image are always mutated.
   - The full pod template is set on **creation only** (the selector is
     immutable); on updates only the labels and the `axonhub` container image
     are mutated, so API-server-defaulted fields are preserved and
     steady-state reconciles are no-ops.
3. **Ensure the Service** (`reconcileService`) the same way: a ClusterIP
   Service named after the CR, selecting the pods by label and routing
   port `8090` to the `http` target port. The selector is set on creation
   only; on updates only labels and the port are mutated.
4. **Update status** (`updateStatus`) with the observed generation, ready
   replica count, and a `Ready` condition (via `meta.SetStatusCondition`).
   Status writes only happen when the status actually changed.

The manager watches `AxonHub` resources and owned `Deployment`/`Service`
objects (`Owns(&appsv1.Deployment{})`, `Owns(&corev1.Service{})`), so changes
to any of them trigger a reconcile.
Reconcile is fully idempotent and never panics.

## Owned resources

For each `AxonHub` CR the operator creates, in the same namespace and named
after the CR:

- a **Deployment** with:
  - labels: `app.kubernetes.io/name=axonhub`,
    `app.kubernetes.io/instance=<CR name>`,
    `app.kubernetes.io/managed-by=axonhub-operator`
  - a single `axonhub` container from `spec.image`, HTTP port `8090`
- a **ClusterIP Service** selecting those pods, exposing TCP port `8090`
  (`http` → target port `http`)

Both carry an owner reference (`ctrl.SetControllerReference`) set inside the
mutate callback so pre-existing same-named objects are adopted. Adoption is
best-effort: a pre-existing Service whose immutable selector does not match
the managed pods fails the reconcile with a descriptive error, and an adopted
Service of another type (NodePort/LoadBalancer) keeps its type since the API
server forbids changing it back to ClusterIP.

## CRD API

Current spec surface:

- `spec.replicas` (`*int32`, minimum 1, default 1)
- `spec.image` (string, default `looplj/axonhub:latest`)

Status surface: `observedGeneration`, `readyReplicas`, `conditions`
(`Ready` = `DeploymentReady` / `DeploymentNotReady`).

## RBAC

Permissions are declared via `kubebuilder:rbac` markers on the reconciler and
generated into `config/rbac/`:

- `axonhubs` (get/list/watch), `axonhubs/status` (get/update/patch),
  `axonhubs/finalizers` (update)
- `apps/deployments` (get/list/watch/create/update/patch)
- core `services` (get/list/watch/create/update/patch)

Any new resource type the controller touches requires new markers followed by
`make manifests`.

## Testing

- `internal/controller/axonhub_controller_test.go` uses **envtest** (a real
  kube-apiserver + etcd) to verify reconciliation, ownership, and status
  behavior end to end at the API level.
- `test/e2e/` covers against a real cluster.

## Invariants to preserve when changing the controller

- Reconcile must be idempotent and panic-free; wrap errors with context.
- Every owned child object carries identifying labels and an owner reference.
- Generated files (`zz_generated.deepcopy.go`, `config/crd/bases/*`) come only
  from `make generate` / `make manifests`.
- API edits always run `make generate && make manifests` before commit.
