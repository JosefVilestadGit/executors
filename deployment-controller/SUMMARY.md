# Deployment Controller - Complete Summary

## What Is It?

A **reconciliation executor** for ColonyOS that manages Docker container deployments declaratively, similar to Kubernetes controllers. It watches for `ExecutorDeployment` resources and ensures containers match the desired state.

## 🎯 Key Features

- **Declarative Management**: Define desired state via JSON resources
- **Automatic Reconciliation**: Continuously syncs actual vs. desired state
- **Dynamic Scaling**: Scales containers up/down based on replica count
- **Label-Based Tracking**: Uses Docker labels to manage containers
- **Environment Variables**: Pass configuration to deployed containers
- **Zero Downtime**: Managed containers run independently of controller lifecycle

## 📁 Project Structure

```
deployment-controller/
├── cmd/main.go              # Entry point
├── pkg/
│   ├── executor/            # Process handling & executor logic
│   ├── reconciler/          # Core reconciliation engine
│   └── build/               # Version information
├── internal/cli/            # CLI commands (start, version)
├── examples/                # Example configurations
├── .env                     # Environment configuration (ready to use)
├── docker-compose.yml       # Docker Compose setup
├── Dockerfile               # Multi-stage container build
├── Makefile                 # Build automation
├── setup.sh                 # Interactive setup script
└── Documentation
    ├── README.md            # Comprehensive documentation
    ├── QUICKSTART.md        # 5-minute getting started
    ├── DOCKER-DEPLOYMENT.md # Detailed Docker guide
    ├── BUILD.md             # Build instructions
    └── SUMMARY.md           # This file
```

## 🚀 Quick Start (3 Steps)

```bash
# 1. Configure
./setup.sh   # Interactive, or manually edit .env

# 2. Start
docker-compose up -d

# 3. Verify
docker-compose logs -f
```

## 💡 How It Works

```
┌─────────────────────┐
│  ExecutorDeployment │  User creates/updates resource
│     Resource        │  (defines: image, replicas, env, etc.)
└──────────┬──────────┘
           │
           ▼
┌─────────────────────┐
│   ColonyOS Server   │  Triggers reconciliation process
└──────────┬──────────┘
           │
           ▼
┌─────────────────────┐
│ Deployment          │  Receives process with embedded resource
│ Controller          │
│ (This Executor)     │
└──────────┬──────────┘
           │
           ├─→ List existing containers (by label)
           ├─→ Compare with desired state
           ├─→ Scale up (start new containers)
           ├─→ Scale down (stop excess containers)
           └─→ Report status

┌─────────────────────┐
│   Docker Daemon     │  Containers run here (on host)
│                     │
│  nginx-deploy-0     │  ← Created & managed by controller
│  nginx-deploy-1     │  ← Labels: colonies.deployment=nginx-deploy
│  nginx-deploy-2     │  ←          colonies.managed=true
└─────────────────────┘
```

## 📝 Example Deployment

```json
{
  "kind": "ExecutorDeployment",
  "metadata": {
    "name": "web-server"
  },
  "spec": {
    "image": "nginx:latest",
    "replicas": 3,
    "env": {
      "ENVIRONMENT": "production"
    }
  }
}
```

This creates 3 nginx containers: `web-server-0`, `web-server-1`, `web-server-2`

## 🔧 Configuration

### Minimal Configuration

Only one value is **required** in `.env`:

```bash
COLONIES_COLONY_PRVKEY=your-colony-private-key-here
```

Everything else has working defaults:
- Server: `localhost:50080` (insecure)
- Colony: `dev`
- Executor: `deployment-controller-1`

### Full Configuration

See `.env.example` for all available options.

## 🐳 Docker Details

### Socket Access

The controller requires Docker socket access to manage containers:

```yaml
volumes:
  - /var/run/docker.sock:/var/run/docker.sock:rw
```

⚠️ **Security Note**: This grants significant privileges. Only use in trusted environments.

### Build Context

The container must be built from the `executors/` directory (not `deployment-controller/`) because it requires the shared `common/` package:

```bash
# Correct (using Make from deployment-controller/)
make container

# Correct (manual from executors/)
cd executors
docker build -f deployment-controller/Dockerfile -t colonyos/deployment-controller .

# Incorrect (will fail)
cd deployment-controller
docker build -t colonyos/deployment-controller .
```

The Makefile and docker-compose.yml handle this automatically.

## 📊 Container Management

### Listing Managed Containers

```bash
# All managed containers
docker ps --filter "label=colonies.managed=true"

# Specific deployment
docker ps --filter "label=colonies.deployment=web-server"
```

### Container Labels

Every managed container has:
- `colonies.deployment=<name>`: Links to deployment
- `colonies.managed=true`: Marks as ColonyOS-managed

### Container Naming

Pattern: `<deployment-name>-<index>`

Examples:
- `web-server-0`
- `web-server-1`
- `web-server-2`

## 🔍 Monitoring

```bash
# Controller logs
docker-compose logs -f

# Controller status
docker-compose ps

# Managed containers
docker ps --filter "label=colonies.managed=true"

# Specific container logs
docker logs web-server-0
```

## 🛠️ Common Operations

### Scale a Deployment

```bash
# Edit the resource to change replicas
# The controller will automatically reconcile
colonies resource update --spec deployment.json
```

### Stop the Controller

```bash
docker-compose down
```

Managed containers **continue running** - they're on the host, not inside the controller.

### Clean Up Managed Containers

```bash
# Remove all managed containers
docker rm -f $(docker ps -aq --filter "label=colonies.managed=true")

# Remove specific deployment's containers
docker rm -f $(docker ps -aq --filter "label=colonies.deployment=web-server")
```

## 📚 Documentation

Choose your path:

1. **Just want to run it?** → [QUICKSTART.md](QUICKSTART.md)
2. **Need details?** → [README.md](README.md)
3. **Production deployment?** → [DOCKER-DEPLOYMENT.md](DOCKER-DEPLOYMENT.md)
4. **Building from source?** → [BUILD.md](BUILD.md)

## ✅ Production Checklist

- [ ] Configure `.env` with real credentials
- [ ] Set `COLONIES_INSECURE=false` for production
- [ ] Use proper TLS certificates for ColonyOS server
- [ ] Restrict who can create ExecutorDeployment resources
- [ ] Set up monitoring and alerting
- [ ] Configure resource limits in docker-compose.yml
- [ ] Regular backups of `.env` file
- [ ] Review security best practices in DOCKER-DEPLOYMENT.md

## 🐛 Troubleshooting

### Controller Won't Start

Check logs: `docker-compose logs`

Common issues:
- Missing `COLONIES_COLONY_PRVKEY` in `.env`
- Can't reach ColonyOS server
- Docker daemon not running

### Can't Create Containers

Ensure Docker socket is mounted:
```bash
ls -l /var/run/docker.sock
```

### Connection Refused

On Mac/Windows, use `host.docker.internal` instead of `localhost` in `.env`:
```bash
COLONIES_SERVER_HOST=host.docker.internal
```

## 🚧 Limitations & Future Work

Current limitations:
- No volume mount support yet
- No health checks or auto-restart
- No rolling update strategy
- Single Docker daemon only (no remote)

Planned features:
- [ ] Volume mounts
- [ ] Health checks
- [ ] Resource limits enforcement
- [ ] Network configuration
- [ ] Rolling updates
- [ ] Status reporting to resource
- [ ] Multi-runtime support (Podman, containerd)

## 📄 License

See main ColonyOS repository for license information.

## 🤝 Contributing

This executor is part of the ColonyOS project. For contributions:
1. Follow the main repository's contribution guidelines
2. Test locally with `make build && make test`
3. Build container with `make container`
4. Update documentation as needed

## 📞 Support

- Documentation: All markdown files in this directory
- Issues: ColonyOS GitHub repository
- Community: ColonyOS Discord/forums

---

**Built with ❤️ for the ColonyOS ecosystem**
