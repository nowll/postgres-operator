# PostgreSQL Operator for Kubernetes

[![Kubernetes](https://img.shields.io/badge/Kubernetes-1.24+-blue.svg)](https://kubernetes.io/)
[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8.svg)](https://golang.org/)

A production-grade Kubernetes operator that automates PostgreSQL database provisioning, management, and lifecycle operations using custom resource definitions (CRDs).

## 🎯 Overview

The PostgreSQL Operator enables you to manage PostgreSQL databases on Kubernetes using simple, declarative YAML configurations. It automates complex operational tasks including provisioning, backup scheduling, high availability setup, and database/user management.

**Perfect for:** DevOps engineers, platform teams, and organizations running cloud-native applications requiring automated database management.

## ✨ Features

### Core Capabilities
- 🚀 **Declarative Database Management** - Define PostgreSQL instances as Kubernetes resources
- 🔄 **Automated Provisioning** - One-click deployment of production-ready PostgreSQL clusters
- 💾 **Scheduled Backups** - Automated backups with retention policies and S3 support
- 🔐 **Security Built-in** - Automatic password generation, RBAC, and non-root containers
- 📊 **Resource Management** - Fine-grained CPU/memory control with requests and limits
- 🎛️ **High Availability** - Multi-replica configurations with StatefulSet orchestration
- 🗃️ **Database Automation** - Automatic database and user creation with permissions
- 📈 **Observability** - Health checks, status reporting, and connection endpoints
- ♻️ **GitOps Ready** - Full declarative configuration for ArgoCD/Flux workflows

### Technical Highlights
- Built with **Go** and **Kubebuilder** framework
- Implements **Kubernetes Operator Pattern** with reconciliation loops
- **StatefulSet-based** for data persistence and pod identity
- **Custom Resource Definitions (CRDs)** for PostgreSQL management
- **Controller-runtime** for efficient Kubernetes API interaction
- Production-ready with **health probes** and **graceful handling**

## 🚀 Quick Start

### Prerequisites
- Kubernetes cluster (1.24+)
- `kubectl` configured
- (Optional) `kind` or `minikube` for local testing

### Installation

1. **Install the CRD:**
```bash
kubectl apply -f https://raw.githubusercontent.com/nowll/postgres-operator/main/config/crd/bases/database.example.com_postgresinstances.yaml
```

2. **Deploy the Operator:**
```bash
kubectl apply -f https://raw.githubusercontent.com/nowll/postgres-operator/main/config/manager/manager.yaml
```

3. **Create a PostgreSQL Instance:**
```yaml
apiVersion: database.example.com/v1alpha1
kind: PostgresInstance
metadata:
  name: my-postgres
spec:
  version: "16"
  replicas: 1
  storage:
    size: 10Gi
    storageClass: standard
  resources:
    requests:
      cpu: 500m
      memory: 1Gi
    limits:
      cpu: 2000m
      memory: 4Gi
```
```bash
kubectl apply -f postgres-instance.yaml
```

4. **Check Status:**
```bash
kubectl get postgresinstances
kubectl describe postgresinstance my-postgres
```

5. **Connect to PostgreSQL:**
```bash
# Get the password
kubectl get secret my-postgres-postgres-secret -o jsonpath='{.data.POSTGRES_PASSWORD}' | base64 -d

# Connect via psql
kubectl run -it --rm psql --image=postgres:16 --restart=Never -- \
  psql -h my-postgres-postgres.default.svc.cluster.local -U postgres
```

## 📖 Usage Examples

### Basic PostgreSQL Instance
```yaml
apiVersion: database.example.com/v1alpha1
kind: PostgresInstance
metadata:
  name: simple-postgres
spec:
  version: "16"
  storage:
    size: 10Gi
```

### High Availability with Replicas
```yaml
apiVersion: database.example.com/v1alpha1
kind: PostgresInstance
metadata:
  name: ha-postgres
spec:
  version: "16"
  replicas: 3  # 1 primary + 2 replicas
  storage:
    size: 50Gi
    storageClass: fast-ssd
  resources:
    requests:
      cpu: 2000m
      memory: 4Gi
    limits:
      cpu: 8000m
      memory: 16Gi
```

### With Automated Backups
```yaml
apiVersion: database.example.com/v1alpha1
kind: PostgresInstance
metadata:
  name: backup-postgres
spec:
  version: "16"
  storage:
    size: 100Gi
  backup:
    enabled: true
    schedule: "0 2 * * *"  # Daily at 2 AM
    retention: 30           # Keep 30 days
    s3:
      bucket: postgres-backups
      endpoint: s3.amazonaws.com
      region: us-east-1
```

### With Pre-configured Databases and Users
```yaml
apiVersion: database.example.com/v1alpha1
kind: PostgresInstance
metadata:
  name: app-postgres
spec:
  version: "16"
  storage:
    size: 20Gi
  databases:
    - name: production_db
      owner: app_user
    - name: analytics_db
      owner: analytics_user
  users:
    - name: app_user
      databases:
        - production_db
    - name: analytics_user
      databases:
        - analytics_db
    - name: readonly_user
      databases:
        - production_db
        - analytics_db
```

## 🛠️ Development

### Building from Source
```bash
# Clone the repository
git clone https://github.com/nowll/postgres-operator.git
cd postgres-operator

# Download dependencies
go mod download

# Generate manifests and code
make manifests generate

# Run tests
make test

# Build the operator binary
make build
```

### Local Development with Kind
```bash
# Create a local Kubernetes cluster
make kind-create

# Install CRDs
make install

# Run the operator locally
make run

# In another terminal, apply samples
kubectl apply -f config/samples/

# Clean up
make kind-delete
```

### Building Docker Image
```bash
# Build image
make docker-build IMG=yourusername/postgres-operator:v1.0.0

# Push to registry
make docker-push IMG=yourusername/postgres-operator:v1.0.0

# Deploy to cluster
make deploy IMG=yourusername/postgres-operator:v1.0.0
```

## 🧪 Testing
```bash
# Run unit tests
make test

# Run with coverage
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out

# Lint code
make lint

# Format code
make fmt
```

## 📊 Monitoring

The operator exposes Prometheus metrics at `:8080/metrics`:

- `postgres_instances_total` - Total number of PostgreSQL instances
- `postgres_instance_status` - Current status of each instance
- `controller_runtime_reconcile_*` - Controller reconciliation metrics

### Example Prometheus Query
```promql
# Count of running PostgreSQL instances
count(postgres_instance_status{phase="Running"})

# Reconciliation error rate
rate(controller_runtime_reconcile_errors_total[5m])
```

## 🔐 Security

### Security Features
- ✅ **Non-root containers** - All containers run as non-root user (UID 999)
- ✅ **Secret management** - Passwords stored in Kubernetes Secrets
- ✅ **RBAC** - Minimal required permissions with least privilege
- ✅ **Pod Security Standards** - Enforced restricted security context
- ✅ **Read-only root filesystem** - Where possible
- ✅ **No privileged escalation** - Security contexts prevent privilege escalation

### Best Practices
- Use strong StorageClass with encryption at rest
- Enable network policies to restrict PostgreSQL access
- Regularly update PostgreSQL versions
- Store backups in encrypted S3 buckets
- Use separate service accounts per namespace

## 📚 API Reference

### PostgresInstance Spec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `version` | string | Yes | PostgreSQL version (14, 15, or 16) |
| `replicas` | int | No | Number of replicas (1-5, default: 1) |
| `storage.size` | string | Yes | Storage size (e.g., "10Gi") |
| `storage.storageClass` | string | No | StorageClass name |
| `resources.requests.cpu` | string | No | CPU request |
| `resources.requests.memory` | string | No | Memory request |
| `resources.limits.cpu` | string | No | CPU limit |
| `resources.limits.memory` | string | No | Memory limit |
| `backup.enabled` | bool | No | Enable automated backups |
| `backup.schedule` | string | No | Cron schedule for backups |
| `backup.retention` | int | No | Backup retention in days |
| `databases` | array | No | List of databases to create |
| `users` | array | No | List of users to create |

### PostgresInstance Status

| Field | Type | Description |
|-------|------|-------------|
| `phase` | string | Current phase (Pending, Creating, Running, Failed, Upgrading) |
| `ready` | bool | Whether the instance is ready |
| `message` | string | Human-readable status message |
| `endpoints.primary` | string | Primary connection endpoint |
| `endpoints.replicas` | array | Replica connection endpoints |

## 🗺️ Roadmap

### Current Version (v1.0.0)
- ✅ Basic PostgreSQL provisioning
- ✅ StatefulSet management
- ✅ Automated backups
- ✅ Database/user creation
- ✅ Multi-replica support

### Planned Features
- [ ] PostgreSQL streaming replication
- [ ] Point-in-time recovery (PITR)
- [ ] Connection pooling (PgBouncer)
- [ ] Automated failover
- [ ] Admission webhooks
- [ ] Prometheus PostgreSQL Exporter integration
- [ ] Schema migration support
- [ ] Multi-cluster federation
- [ ] Grafana dashboards
- [ ] Vault integration for secrets

## 🤝 Contributing

Contributions are welcome! Please follow these steps:

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

### Contribution Guidelines
- Follow Go best practices and conventions
- Add tests for new features
- Update documentation
- Ensure CI/CD passes
- Sign commits with DCO


## 🙏 Acknowledgments

- Built with [Kubebuilder](https://book.kubebuilder.io/)
- Inspired by [Zalando Postgres Operator](https://github.com/zalando/postgres-operator)
- PostgreSQL community for excellent documentation
- Kubernetes SIG API Machinery for controller-runtime

## 📞 Support

- **Issues:** [GitHub Issues](https://github.com/nowll/postgres-operator/issues)
- **Discussions:** [GitHub Discussions](https://github.com/nowll/postgres-operator/discussions)
- **Documentation:** [Wiki](https://github.com/nowll/postgres-operator/wiki)


---

**Made with ❤️ by [Samuel Caesar Paskalis](https://github.com/nowll)**

*⭐ Star this repo if you find it useful!*
