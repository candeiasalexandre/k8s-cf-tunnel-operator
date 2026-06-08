# cf-tunnel-operator

Kubernetes operator that creates a **dedicated Cloudflare Tunnel** for each
`CloudflareTunnel` CRD (or for each `Ingress` with `ingressClassName: cf-tunnel`).

It manages the full lifecycle:
- Cloudflare Tunnel creation / deletion via API
- DNS CNAME record provisioning
- `cloudflared` Deployment (Kubernetes-native pods)
- Tunnel credentials as Secrets
- ConfigMaps with `cloudflared` ingress rules

PS: This project was almost 100% done using OpenCode, mainly Kimi K2.6 and DeepSeek V4 Flash

## Architecture

```
┌─────────────┐     ┌──────────────────┐     ┌──────────────────────┐
│   Ingress   │────→│  Ingress Adapter │────→│  CloudflareTunnel CRD│
│  (standard) │     │   (optional)       │     │                      │
└─────────────┘     └──────────────────┘     └──────────┬───────────┘
                                                          │
                            ┌─────────────────────────────┘
                            │
                            ▼
                   ┌────────────────────┐
                   │ Tunnel Controller  │
                   │ • Create CF tunnel │
                   │ • Create DNS CNAME │
                   │ • Secret + CM + Deployment
                   └────────────────────┘
```

## Quickstart (Minikube)

### 1. Start minikube

```bash
minikube start
```

### 2. Deploy CRD + operator

```bash
make deploy
```

### 3. Create a Sample Ingress

```bash
make sample-ingress
```


### 4. Watch

```bash
kubectl get cloudflaretunnels -A
kubectl get ingress -A
kubectl logs -n cf-tunnel-system deployment/cf-tunnel-operator -f
```

### 5. Clean up

```bash
make undeploy
```

## Configuration

The operator reads Cloudflare credentials from environment variables:

| Variable | Required | Description |
|----------|----------|-------------|
| `CF_API_TOKEN` | Yes (for real API) | Cloudflare API token with `Account:Cloudflare Tunnel:Edit` and `Zone:Edit` |
| `CF_ACCOUNT_ID` | Yes | Cloudflare Account ID |
| `CF_ZONE_ID` | Yes | Cloudflare Zone ID (for DNS records) |

If `CF_API_TOKEN` is missing, the operator falls back to a **no-op client** that mocks tunnel creation. This is useful for local development and testing without real Cloudflare credentials.

When running in-cluster, mount credentials via a Secret named `cf-operator-secrets` in `cf-tunnel-system`:

```bash
create secret generic cf-operator-secrets \
  --namespace=cf-tunnel-system \
	--from-env-file=.env \
	--dry-run=client -o yaml | kubectl apply -f -
```

## How it works

### CloudflareTunnel CRD (native experience)

Users create this directly:

```yaml
apiVersion: cloudflare.example.com/v1
kind: CloudflareTunnel
metadata:
  name: my-app
spec:
  hostname: app.example.com
  replicas: 2
  rules:
    - path: /api
      backend:
        serviceName: api
        port: 8080
    - path: /
      backend:
        serviceName: web
        port: 80
```

The controller will:
1. Create a Cloudflare Tunnel via API
2. Write tunnel credentials to a Secret
3. Write `cloudflared` config to a ConfigMap
4. Create a Deployment running `cloudflared` (2 replicas)
5. Create a (proxied) CNAME DNS record pointing to the tunnel

### Ingress adapter (standard Kubernetes UX)

Users can also write a normal `Ingress`:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: my-app
spec:
  ingressClassName: cf-tunnel
  rules:
    - host: app.example.com
      http:
        paths:
          - path: /
            backend:
              service:
                name: web
                port: { number: 80 }
```

The **Ingress Adapter** controller automatically generates a `CloudflareTunnel` CRD from it. The Tunnel Controller then provisions the actual infrastructure.

## Deletion Flow (Finalizers)

When you delete an Ingress or CloudflareTunnel, the following happens:

1. **Tunnel Controller finalizer** runs:
   - Deletes the `cloudflared` Deployment (stops active tunnel connections)
   - Waits 30 seconds for pods to terminate and connections to close
   - Deletes the Cloudflare Tunnel via API (retries if connections are still active)
   - Deletes the DNS CNAME record via API
   - Removes the finalizer
2. **Kubernetes garbage collector** removes the CloudflareTunnel and its child Secrets/ConfigMaps/Deployments

This ensures **no orphaned infrastructure** in Cloudflare.