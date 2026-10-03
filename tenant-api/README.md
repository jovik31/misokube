# MIsoKube tenant API

The API creates a MIsoKube `Tenant`, waits until the Tenant is assigned,
creates the matching `misokube-NAME` namespace, installs a vCluster in that
namespace, and returns the kubeconfig stored in the `vc-NAME` Secret.

The refactored MIsoKube CRD only accepts `spec.zones`. Tenant identity is
stored in `metadata.name`. vCluster control-plane Pods use the
`misokube.com/tenant` label and the new `misokube.com/tenant.NAME=true` node
selector.

The Deployment runs on the `master` node with host networking. The Service is
also exposed through NodePort `30080`. The `/tenant` route requires a bearer
token, while `/health` is public.

NodePort requires kube-proxy (or another Service implementation). Because the
refactored MIsoKube dataplane does not implement Services, keep kube-proxy in
the kubeadm cluster. The host-networked API is also reachable directly at
`http://192.168.1.84:8080` for diagnosis if NodePort is unavailable.

Create the token before deploying:

```bash
kubectl create secret generic tenant-api-auth \
  --namespace default \
  --from-literal=token='replace-with-a-long-random-value'
```

With worker3 outside the cluster, call the API through the master node:

```bash
curl http://192.168.1.84:30080/health

curl -X POST http://192.168.1.84:30080/tenant \
  -H 'Authorization: Bearer replace-with-a-long-random-value' \
  -H 'Content-Type: application/json' \
  -d '{"name":"demo","zones":2}'
```

Because this cluster contains only worker1 and worker2 as eligible workload
nodes, requests should normally use `zones: 1` or `zones: 2`.
