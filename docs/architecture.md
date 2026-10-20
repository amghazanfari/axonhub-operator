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
     immutable); on updates the labels and the `axonhub` container spec
     (image, pull policy, env, ports, security context, resources) are
     mutated, so API-server-defaulted fields are preserved and
     steady-state reconciles are no-ops. In particular, changing `spec.db`
     swaps `AXONHUB_DB_DSN` between a literal value and a `secretKeyRef`.
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
  - a single `axonhub` container from `spec.image`, HTTP port `8090`, with
    the `AXONHUB_DB_DIALECT` / `AXONHUB_DB_DSN` environment variables set
    when `spec.db` is configured (`AXONHUB_DB_DSN` is a literal `value` for
    `spec.db.address` and an `env.valueFrom.secretKeyRef` for
    `spec.db.addressSecret`)
- a **ClusterIP Service** selecting those pods, exposing TCP port `8090`
  (`http` → target port `http`)

Both carry an owner reference (`ctrl.SetControllerReference`) set inside the
mutate callback so pre-existing same-named objects are adopted. Adoption is
best-effort: a pre-existing Service whose immutable selector does not match
the managed pods fails the reconcile with a descriptive error, and an adopted
Service of another type (NodePort/LoadBalancer) keeps its type since the API
server forbids changing it back to ClusterIP.

A pre-existing child object that is already **controlled by another
controller** cannot be adopted (`controllerutil.AlreadyOwnedError`). Instead
of failing the reconcile in a hot retry loop, the controller:

- sets the `Ready` condition to `False` with reason
  `ChildObjectAlreadyOwned` and an actionable message ("delete the
  conflicting object or rename the AxonHub resource"),
- emits a warning Event on the AxonHub resource when the condition
  transitions (not on every requeue; the reconciler holds an event recorder
  and RBAC grants create/patch on events),
- returns a periodic requeue (every 2 minutes) rather than an error, so the
  conflict is retried calmly and resolves automatically once the conflicting
  object is deleted or the resource renamed. Since the conflicting object is
  not owned by this controller, its deletion does not trigger a watch event,
  so recovery can lag up to the full requeue interval.

## CRD API

Current spec surface:

- `spec.replicas` (`*int32`, minimum 1, default 1)
- `spec.image` (string, default `looplj/axonhub:latest`)
- `spec.db` (optional `DatabaseSpec`): `type` enum (`sqlite3`, `postgres`,
  `mysql` — the dialects AxonHub supports) plus **exactly one of** `address`
  (a credential-free DSN) or `addressSecret` (`name`/`key` of a Secret in the
  same namespace), mapped to the `AXONHUB_DB_DIALECT` / `AXONHUB_DB_DSN`
  container environment variables. The CRD enforces the exactly-one rule with
  a CEL validation. For `addressSecret` the controller sets
  `env.valueFrom.secretKeyRef` and never reads the Secret itself (so no
  `secrets` RBAC is required); the kubelet resolves it at pod start and
  rotation requires a pod restart.

Status surface: `observedGeneration`, `readyReplicas`, `conditions`
(`Ready`; True reason `DeploymentReady`, False reasons include
`DeploymentStatusNotObservedYet`, `DeploymentReplicasNotReady`,
`DeploymentRolloutInProgress`, `ChildObjectAlreadyOwned`, and the
stage-neutral `ReconcileFailed` whose message names the failing stage).

## RBAC

Permissions are declared via `kubebuilder:rbac` markers on the reconciler and
generated into `config/rbac/`:

- `axonhubs` (get/list/watch), `axonhubs/status` (get/update/patch),
  `axonhubs/finalizers` (update)
- `apps/deployments` (get/list/watch/create/update/patch)
- core `services` (get/list/watch/create/update/patch)
- core `events` (create/patch) for the warning Events described above

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
