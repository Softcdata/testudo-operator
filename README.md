# Testudo

[English](README.md) | [简体中文](README.zh-CN.md)

Testudo (Chinese name: 玄龟阵) is a Kubernetes application-level disaster recovery orchestration system. It protects workloads across Kubernetes clusters by synchronizing application resources and PVC data, preparing standby environments, and coordinating failover, reverse protection, disaster recovery groups, and drills through Kubernetes CRDs.

It solves a practical platform problem: teams need a repeatable way to protect Kubernetes applications, keep a target cluster recoverable, switch applications during failures, and verify recovery paths without manually assembling Velero `Backup`, `Restore`, and `Schedule` resources for every workflow.

Start here:

- Documentation site: [https://testudo.softcdata.com](https://testudo.softcdata.com)
- Install guide: [Installation](https://testudo.softcdata.com/en/docs/getting-started/install)
- Quick start: [Create the First Disaster Instance](https://testudo.softcdata.com/en/docs/getting-started/create-first-instance)
- First failover: [First Failover](https://testudo.softcdata.com/en/docs/getting-started/first-failover)
- Architecture: [System Architecture](https://testudo.softcdata.com/en/docs/overview/architecture)
- Compatibility: [Compatibility Matrix](https://testudo.softcdata.com/en/docs/reference/compatibility-matrix)
- Security: [Security Overview](https://testudo.softcdata.com/en/docs/operations/security-overview)

## Projects

This README is intended to be shared by the Testudo runtime projects. A complete source checkout usually contains these repositories:

| Project | Responsibility |
| --- | --- |
| [`testudo-operator`](https://github.com/softcdata/testudo-operator) | Kubernetes CRDs, admission webhook, controllers, workflow state machines, and reconciliation against Kubernetes and Velero resources. |
| [`testudo-server`](https://github.com/softcdata/testudo-server) | REST API, Watch API, authentication, Swagger/OpenAPI, statistics, and console-facing aggregation. |
| [`testudo-chart`](https://github.com/Softcdata/testudo-chart) | Helm Chart for installing `testudo-operator`, `testudo-server`, and the web console as one `disaster-system` release. |
| [`testudo-web`](https://github.com/Softcdata/testudo-web) | Vue/Vite web console for cluster registration, storage configuration, disaster instances, groups, drills, and operations. |

GitHub is the source of truth. Gitee repositories, when provided, are synchronization mirrors for users who need domestic access in China.

## Quick Start

Prerequisites:

- A management Kubernetes cluster.
- At least two business clusters for source and target roles.
- S3-compatible object storage reachable by both source and target clusters.
- Business clusters can pull Velero images, or Velero images have been mirrored to an internal registry. Testudo can install or align Velero during cluster registration.
- `kubectl` and Helm v3.

Install the control plane with the `testudo-chart` Helm chart:

```bash
helm repo add testudo https://softcdata.github.io/testudo-chart
helm repo update

helm upgrade --install testudo testudo/testudo-chart \
  -n disaster-system \
  --create-namespace
```

Open the console:

```text
http://<NodeIP>:30087
```

Then follow this path:

1. [Register Clusters](https://testudo.softcdata.com/en/docs/getting-started/register-clusters)
2. [Configure Storage](https://testudo.softcdata.com/en/docs/getting-started/configure-storage)
3. [Create the First Disaster Instance](https://testudo.softcdata.com/en/docs/getting-started/create-first-instance)
4. [First Failover](https://testudo.softcdata.com/en/docs/getting-started/first-failover)

Read the full [Installation Guide](https://testudo.softcdata.com/en/docs/getting-started/install) and [Compatibility Matrix](https://testudo.softcdata.com/en/docs/reference/compatibility-matrix) before deploying to a real environment. Production deployments should also review image pull secrets, webhook certificates, storage repositories, license handling, cluster permissions, and the [Security Overview](https://testudo.softcdata.com/en/docs/operations/security-overview).

## Architecture

![Testudo architecture](static/img/diagrams/architecture.png)

Testudo is organized around five layers:

- **User entry layer**: web console, CLI, automation systems, or direct API clients.
- **API aggregation layer**: `disaster-server`, which validates requests, writes CRDs, aggregates status, and serves REST/watch APIs.
- **Kubernetes API and CRD source of truth**: all long-running intent and runtime status are represented through Kubernetes resources.
- **Operator execution layer**: `disaster-operator`, which reconciles CRDs and drives backup, restore, sync, failover, reprotect, undo, cancel, and drill workflows.
- **Runtime dependency layer**: remote Kubernetes clusters, Velero, object storage, and backup storage locations.

See [System Architecture](https://testudo.softcdata.com/en/docs/overview/architecture) for details.

## Features

- **Application-level protection**: `DisasterInstance` describes the protected application, cluster roles, namespace scope, sync policies, restore policies, and runtime state.
- **Data synchronization**: `DataSync` protects PVC data through `AppBackup` and `AppRestore`, backed by Velero.
- **Resource synchronization**: `ResourceSync` keeps Kubernetes resources available in standby form on the target cluster.
- **Failover orchestration**: `DisasterOperation` runs explicit steps such as pre-check, schedule pause, final sync, source scale-down, target scale-up, replica check, and role switch.
- **Reverse protection**: `reprotect` establishes protection in the new direction after failover.
- **Disaster recovery groups**: `DisasterGroup` orchestrates multiple protected instances by level, parallelism, timeout, fail policy, and retry policy.
- **Disaster recovery drills**: `DisasterDrill` verifies recoverability without taking over production traffic.
- **Observability**: status, conditions, history, Kubernetes events, watch APIs, and statistics APIs expose workflow state.

See [Core Capabilities](https://testudo.softcdata.com/en/docs/overview/core-capabilities) and [CRD Model](https://testudo.softcdata.com/en/docs/concepts/crd-model).

## Use Cases

Testudo is designed for:

- Application-level disaster recovery between two Kubernetes clusters.
- Protecting namespace-scoped workloads, services, ingress resources, secrets, config maps, and PVC data.
- Keeping a target cluster ready with standby resources and restorable data.
- Executing controlled failover for individual applications or groups of applications.
- Running disaster recovery drills to verify that backups can actually restore.
- Integrating disaster recovery workflows into platform consoles and automation systems through APIs.

## Limitations

Testudo does not replace:

- Storage-array replication.
- Database-native replication.
- DNS switching, global load balancing, or business traffic routing.
- Application-level consistency mechanisms such as database quiesce, distributed transaction handling, or custom write fencing.
- Velero compatibility boundaries for the underlying Kubernetes resources and storage plugins.

Production traffic routing, application consistency, dependency readiness, and final operational approval should be handled by the surrounding platform runbook.

## Documentation

Documentation site:

- [https://testudo.softcdata.com](https://testudo.softcdata.com)

Recommended reading order:

1. [What Is Testudo](https://testudo.softcdata.com/en/docs/overview/what-is-testudo)
2. [System Architecture](https://testudo.softcdata.com/en/docs/overview/architecture)
3. [Prerequisites](https://testudo.softcdata.com/en/docs/getting-started/prerequisites)
4. [Installation](https://testudo.softcdata.com/en/docs/getting-started/install)
5. [Create the First Disaster Instance](https://testudo.softcdata.com/en/docs/getting-started/create-first-instance)
6. [First Failover](https://testudo.softcdata.com/en/docs/getting-started/first-failover)
7. [Compatibility Matrix](https://testudo.softcdata.com/en/docs/reference/compatibility-matrix)
8. [Security Overview](https://testudo.softcdata.com/en/docs/operations/security-overview)

Tutorials:

- [Backup and Restore Quickstart](https://testudo.softcdata.com/en/docs/tutorials/backup-restore/quickstart)
- [Disaster Recovery Quickstart](https://testudo.softcdata.com/en/docs/tutorials/disaster-recovery/quickstart)
- [Run Instance Failover](https://testudo.softcdata.com/en/docs/tutorials/disaster-recovery/instance-failover)
- [Undo, Reprotect, and Cancel](https://testudo.softcdata.com/en/docs/tutorials/disaster-recovery/undo-reprotect-cancel)
- [Disaster Recovery Troubleshooting](https://testudo.softcdata.com/en/docs/tutorials/disaster-recovery/troubleshooting)

Reference:

- [Compatibility Matrix](https://testudo.softcdata.com/en/docs/reference/compatibility-matrix)
- [REST API Reference](https://testudo.softcdata.com/en/docs/api/rest-api-reference)
- [Watch API Reference](https://testudo.softcdata.com/en/docs/api/websocket-api-reference)
- [API Authentication](https://testudo.softcdata.com/en/docs/api/authentication)
- [Error Codes](https://testudo.softcdata.com/en/docs/api/error-codes)
- [CRD Reference](https://testudo.softcdata.com/en/docs/reference/crd-reference)
- [Operation Step Catalog](https://testudo.softcdata.com/en/docs/reference/operation-step-catalog)
- [Status and Conditions](https://testudo.softcdata.com/en/docs/reference/status-and-conditions)

Operations:

- [Security Overview](https://testudo.softcdata.com/en/docs/operations/security-overview)
- [Upgrade](https://testudo.softcdata.com/en/docs/operations/upgrade)
- [Rollback](https://testudo.softcdata.com/en/docs/operations/rollback)
- [Uninstall](https://testudo.softcdata.com/en/docs/operations/uninstall)
- [Install Troubleshooting](https://testudo.softcdata.com/en/docs/operations/install-troubleshooting)
- [Monitoring](https://testudo.softcdata.com/en/docs/operations/monitoring)
- [Capacity Planning](https://testudo.softcdata.com/en/docs/operations/capacity-planning)

## Local Development

The commands below assume the four runtime projects are checked out as sibling directories. Adjust the directory names if your local checkout uses different names.

Prerequisites:

- Go 1.24.5 or newer.
- `kubectl` connected to a management Kubernetes cluster.
- Helm v3.
- Docker or another compatible container tool.
- Node.js 20.19.0 or newer.
- pnpm 10.17.1 or newer.
- S3-compatible object storage reachable from the source and target business clusters for full disaster recovery workflows.

Recommended local startup order:

1. Install CRDs from `testudo-operator`.
2. Run `testudo-operator` against the management cluster.
3. Run `testudo-server` against the same kubeconfig.
4. Run `testudo-web` and point its dev proxy to `testudo-server`.

### Operator

Install or update CRDs in the current kubeconfig cluster:

```bash
cd disaster-operator
make install
```

Run the operator locally:

```bash
make run
```

`make run` generates local webhook certificates under `/tmp/k8s-webhook-server/serving-certs` when they do not already exist, then runs the manager from `cmd/main.go`.

Build and test:

```bash
make build
make test
```

Build or deploy an operator image:

```bash
make docker-build IMG=<registry>/<namespace>/disaster-operator:<tag>
make docker-buildx IMG=<registry>/<namespace>/disaster-operator:<tag>
make deploy IMG=<registry>/<namespace>/disaster-operator:<tag>
```

### Server

Run the server locally:

```bash
cd disaster-server
go run . server --config configs/config.yaml
```

The server uses the current Kubernetes client configuration and expects the Testudo CRDs to exist in the management cluster. The sample config listens on `0.0.0.0:8080`; set a strong JWT secret before using it outside local development.

When Swagger is enabled, the API documents are available from the running server:

```text
http://127.0.0.1:8080/swagger/
http://127.0.0.1:8080/openapi.yaml
http://127.0.0.1:8080/openapi.json
```

Build and test:

```bash
go build -o bin/disaster .
go test ./...
```

Build a server image:

```bash
docker build -t <registry>/<namespace>/disaster-server:<tag> .
```

### Web Console

Console capabilities include:

- Application backup and recovery workflows
- Disaster instance and group configuration and operations
- Disaster drill planning and execution
- Cluster, backup repository, and policy management
- Real-time status via WebSocket
- Internationalization (Chinese / English)

#### Tech stack

| Layer | Technology |
| --- | --- |
| Framework | Vue 3, TypeScript |
| Build | Vite 7 |
| UI | Naive UI, Tailwind CSS |
| State | Pinia, TanStack Vue Query |
| Router | Vue Router (hash / history) |
| Visualization | ECharts, AntV G6 |

In addition to the shared [Local Development](#local-development) prerequisites, install [Node.js](https://nodejs.org/) ≥ 20.19 or ≥ 22.12 and [pnpm](https://pnpm.io/) ≥ 10.17 (recommended).

#### Install and run

```bash
cd testudo-web
pnpm install

cp .env.example .env.dev
# Edit .env.dev: set VITE_BASE_URL and VITE_URL_PROXYS to your backend

pnpm dev
```

Open `http://localhost:9009` (or the port configured in `.env`).

**Network model**: the browser always calls same-origin paths (`/apis/*`, `/login`, `/refresh_token`) with an empty axios `baseURL`. Dev uses the Vite proxy (`build/proxy.ts`); production uses Nginx (`nginx.conf.template`). Configure one entry in `.env.dev`:

```json
[["/apis","http://127.0.0.1:8080"]]
```

`/login` and `/refresh_token` are proxied to the same backend automatically.

#### Scripts

| Command | Description |
| --- | --- |
| `pnpm dev` | Development server (`dev` mode) |
| `pnpm dev:test` | Development server (`test` mode) |
| `pnpm build:dev` | Build with dev profile |
| `pnpm build:test` | Build with test profile |
| `pnpm build:staging` | Build with staging profile |
| `pnpm build:prod` | Production build |
| `pnpm preview` | Preview production build |
| `pnpm type:check` | TypeScript / Vue type check |

#### Configuration

Copy `.env.example` to `.env.dev`, `.env.test`, `.env.staging`, or `.env.prod` as needed. **Do not commit** real `.env*` files.

| Variable | Description |
| --- | --- |
| `VITE_HTTP_PROXY` | Enable Vite dev proxy |
| `VITE_URL_PROXYS` | Dev proxy targets, e.g. `[["/apis","http://127.0.0.1:8080"]]` |
| `VITE_BASE_URL` | Optional; leave empty for same-origin proxy. Absolute URL only for direct backend debugging |
| `VITE_API_PREFIX` | Gateway prefix (default `/apis`) |
| `VITE_ROUTER_HISTORY_MODE` | `hash` or `history` |
| `VITE_OUTPUT_DIR` | Build output directory (`dist-prod` for `build:prod`) |


#### Project structure

```
src/
  api/           API clients
  views/         Feature pages
  components/    Shared UI
  layout/        App shell
  router/        Routes
  locales/       i18n messages
  store/         Pinia stores
mock/            Local mock APIs (development)
public/          Static assets and platform-config.json
```

Build and preview a production bundle:

```bash
pnpm build:prod
pnpm preview
```

Build a web image after generating the production bundle:

```bash
pnpm build:prod
docker build -t <registry>/<namespace>/disaster-web:<tag> .
```

### Helm Chart

Package the chart from the chart repository root:

```bash
cd testudo-chart
helm lint .
helm package .
```

This produces a package such as:

```text
testudo-chart-1.0.0.tgz
```

Install or upgrade the full control plane:

```bash
helm upgrade --install testudo ./testudo-chart-1.0.0.tgz \
  -n disaster-system \
  --create-namespace
```

Use an existing `kubernetes.io/dockerconfigjson` Secret for private image registries, or create one explicitly:

```bash
kubectl -n disaster-system create secret docker-registry default-secret \
  --docker-server=<registry> \
  --docker-username=<username> \
  --docker-password=<password>
```

If you install into a namespace other than `disaster-system`, also set the chart namespace value:

```bash
helm upgrade --install testudo ./testudo-chart-1.0.0.tgz \
  -n <namespace> \
  --create-namespace \
  --set global.namespace=<namespace> \
  --set imagePullSecret.existingSecret=<pull-secret-name>
```

Do not commit real registry usernames, passwords, tokens, kubeconfigs, or license files into chart values.

## Contributing

Contributions should be submitted to the primary GitHub repository. When changing user-facing behavior or API contracts in the runtime repositories, update the relevant documentation at the same time:

- CRD and controller behavior: update concept, reference, and operation docs.
- REST or watch APIs: update API reference and OpenAPI-related docs.
- Console workflows: update tutorials and screenshots.
- Architecture changes: update diagrams and architecture documentation.

For local development guidance, see [Development Setup](https://testudo.softcdata.com/en/docs/contributing/development-setup).

## Security

Report security vulnerabilities through GitHub Security Advisories on the primary GitHub repository. Do not disclose suspected vulnerabilities in public issues.

- Operator advisories: [Report a vulnerability](https://github.com/softcdata/testudo-operator/security/advisories/new)
- Server advisories: [Report a vulnerability](https://github.com/softcdata/testudo-server/security/advisories/new)

## License

Testudo source code and documentation are released under the [Apache License 2.0](LICENSE) unless a specific repository or file states otherwise.

## GitHub And Gitee Mirrors

GitHub is the source of truth for this project. Issues, pull requests, releases, security advisories, and project governance should be handled on GitHub.

Gitee repositories, when provided, are synchronization mirrors for users who need domestic access in China. They should be treated as downstream mirrors unless a repository explicitly states otherwise.
