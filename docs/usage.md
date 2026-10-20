# Using axonhub-operator

`axonhub-operator` deploys and manages [AxonHub](https://github.com/looplj/axonhub)
(an all-in-one AI gateway platform) on Kubernetes through the `AxonHub` custom
resource.

## Prerequisites

- Go v1.24.0+ (for building from source)
- Docker 17.03+
- kubectl v1.11.3+
- Access to a Kubernetes cluster (kind works for development)

## Installation

### From a released image

```sh
# Build and push the operator image
make docker-build docker-push IMG=<some-registry>/axonhub-operator:tag

# Install the CRD into the cluster
make install

# Deploy the operator manager
make deploy IMG=<some-registry>/axonhub-operator:tag
```

### From a YAML bundle

```sh
make build-installer IMG=<some-registry>/axonhub-operator:tag
kubectl apply -f dist/install.yaml
```

## Creating an AxonHub instance

Apply the sample custom resource:

```sh
kubectl apply -k config/samples/
```

or write your own:

```yaml
apiVersion: axonhub.onrender.com/v1alpha1
kind: AxonHub
metadata:
  name: my-axonhub
spec:
  # Number of AxonHub pods (defaults to 1; minimum 1).
  replicas: 2
  # AxonHub container image. Defaults to looplj/axonhub:latest when omitted.
  image: looplj/axonhub:latest
```

Both `spec.replicas` and `spec.image` are optional; the CRD applies sane
defaults (`1` replica and `looplj/axonhub:latest`).

### Database selection

AxonHub supports the `sqlite3`, `postgres`, and `mysql` dialects. Configure
which one to use — and how to reach it — with the `spec.db` dictionary.

For a credential-free DSN, set `spec.db.address`:

```yaml
apiVersion: axonhub.onrender.com/v1alpha1
kind: AxonHub
metadata:
  name: my-axonhub
spec:
  db:
    type: postgres
    address: postgres://postgres.default.svc:5432/axonhub?sslmode=disable
```

For a DSN that contains credentials, keep it in a `Secret` in the same
namespace as the `AxonHub` resource and reference it with
`spec.db.addressSecret` (a `name`/`key` pair) instead:

```sh
kubectl create secret generic axonhub-db \
  --from-literal=dsn='postgres://user:pass@postgres.default.svc:5432/axonhub?sslmode=disable'
```

```yaml
apiVersion: axonhub.onrender.com/v1alpha1
kind: AxonHub
metadata:
  name: my-axonhub
spec:
  db:
    type: postgres
    addressSecret:
      name: axonhub-db
      key: dsn
```

`spec.db.type` is an enum (`sqlite3`, `postgres`, `mysql`) validated by the
CRD; `spec.db.address` is the data source name for that database, e.g.
`file:axonhub.db?cache=shared&_fk=1` for SQLite. Exactly one of `address` and
`addressSecret` must be set (the CRD rejects a `spec.db` that sets neither or
both). The operator maps these to the `AXONHUB_DB_DIALECT` and
`AXONHUB_DB_DSN` environment variables of the AxonHub container; with
`addressSecret` the DSN is injected via `env.valueFrom.secretKeyRef`, so it
never appears in the `AxonHub` resource.

When `spec.db` is omitted, no database environment is injected and AxonHub's
own defaults apply (SQLite with a local file — note the data is lost when the
pod's filesystem is replaced, so configure a database backed by a volume or a
remote server for anything persistent).

> **Note:** the operator does not read the referenced `Secret`, so it does not
> have to exist when the `AxonHub` resource is created; the kubelet resolves
> it when starting the pod, and a missing `Secret` or key surfaces as a pod
> `CreateContainerConfigError`. Environment variables are resolved once at
> pod start, so after rotating the `Secret` you must restart the pods (for
> example `kubectl rollout restart deployment/my-axonhub`) to pick up the new
> DSN.

The operator creates a Deployment named after the `AxonHub` resource in the
same namespace, with one container (named `axonhub`) per pod that serves HTTP
on port `8090`. It also creates a ClusterIP Service with the same name that
routes port `8090` (`http`) to the pods. Both carry an owner reference and
`app.kubernetes.io/*` labels, so deleting the `AxonHub` resource garbage
collects them.

## Checking status and accessing AxonHub

Once the `Ready` condition is true, the AxonHub HTTP API is reachable inside
the cluster through the operator-created Service:

```sh
kubectl get service my-axonhub
kubectl run curl --rm -it --image=curlimages/curl -- \
  curl http://my-axonhub.default.svc.cluster.local:8090
```

The operator reports the observed state on the resource's status:

```sh
kubectl get axonhub my-axonhub -o yaml
```

- `status.observedGeneration` — the generation most recently reconciled
- `status.readyReplicas` — ready pods of the managed Deployment
- `status.conditions` — includes a `Ready` condition
  (`DeploymentReady` / `DeploymentNotReady`) following Kubernetes API
  conventions

```sh
kubectl describe axonhub my-axonhub
```

## Scaling and upgrading

- **Scale**: `kubectl patch axonhub my-axonhub --type merge -p '{"spec":{"replicas":3}}'`
- **Upgrade**: change `spec.image` to the new AxonHub version tag; the
  operator rolls the Deployment's container image.

## Uninstall

```sh
kubectl delete -k config/samples/   # delete CR instances
make undeploy                       # remove the operator manager
make uninstall                      # remove the CRDs
```

## Development workflow

```sh
make generate     # regenerate deepcopy code after API edits
make manifests    # regenerate CRD/RBAC manifests
make test         # unit + envtest tests (run `make envtest` first to fetch binaries)
make lint         # golangci-lint
make docker-build # build the manager image
```

See [architecture.md](architecture.md) for how the operator works internally,
and the repo-root [AGENTS.md](../AGENTS.md) for contribution rules.
