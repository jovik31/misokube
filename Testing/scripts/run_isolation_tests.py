#!/usr/bin/env python3
import argparse
import json
import pathlib
import sys
import time
from typing import Dict, List

import yaml

SCRIPT_DIR = pathlib.Path(__file__).resolve().parent
sys.path.append(str(SCRIPT_DIR))

from lib.kube import apply_yaml, get_nodes, get_pods, run_kubectl, wait_for_pods


def load_config(path: str) -> dict:
    with open(path, "r", encoding="utf-8") as handle:
        return yaml.safe_load(handle)


def nodes_for_tests(nodes: List[dict], include_control_plane: bool) -> List[str]:
    selected = []
    for node in nodes:
        labels = node.get("metadata", {}).get("labels", {})
        is_cp = "node-role.kubernetes.io/control-plane" in labels or "node-role.kubernetes.io/master" in labels
        if is_cp and not include_control_plane:
            continue
        selected.append(node["metadata"]["name"])
    return selected


def create_tenants(tenants: List[dict], kubeconfig: str, zones_override: int) -> None:
    manifests = []
    for tenant in tenants:
        zones = zones_override if zones_override > 0 else int(tenant["zones"])
        manifests.append(
            {
                "apiVersion": "setera.com/v1",
                "kind": "Tenant",
                "metadata": {"name": tenant["name"]},
                "spec": {"name": tenant["name"], "zones": zones},
            }
        )

    docs = "\n---\n".join([yaml.safe_dump(item, sort_keys=False) for item in manifests])
    apply_yaml(docs, kubeconfig)


def create_pods(tenants: List[dict], nodes: List[str], pods_per_node: int, namespace: str,
                pod_image: str, kubeconfig: str) -> List[str]:
    created = []
    for tenant in tenants:
        for node in nodes:
            node_short = node.rsplit("-", 1)[-1]
            for index in range(1, pods_per_node + 1):
                name = f"pod{index}-{tenant['name']}-{node_short}".replace("_", "-")
                created.append(name)
                pod_manifest = {
                    "apiVersion": "v1",
                    "kind": "Pod",
                    "metadata": {
                        "name": name,
                        "namespace": namespace,
                        "labels": {
                            "test-suite": "isolation",
                            "tenant": tenant["name"],
                            "node": node_short,
                        },
                        "annotations": {
                            "setera.com/tenant": tenant["name"],
                        },
                    },
                    "spec": {
                        "nodeName": node,
                        "containers": [
                            {
                                "name": "main",
                                "image": pod_image,
                                "command": ["sh", "-c", "sleep 36000"],
                            }
                        ],
                        "restartPolicy": "Never",
                    },
                }
                apply_yaml(yaml.safe_dump(pod_manifest, sort_keys=False), kubeconfig)
    return created


def pod_ip_map(namespace: str, kubeconfig: str) -> Dict[str, str]:
    pods = get_pods("test-suite=isolation", namespace, kubeconfig)
    mapping = {}
    for pod in pods:
        name = pod.get("metadata", {}).get("name")
        ip = pod.get("status", {}).get("podIP")
        if name and ip:
            mapping[name] = ip
    return mapping


def exec_ping(src: str, dest_ip: str, namespace: str, kubeconfig: str) -> bool:
    result = run_kubectl(
        ["exec", "-n", namespace, src, "--", "sh", "-c", f"ping -c 2 -W 1 {dest_ip}"],
        kubeconfig=kubeconfig,
        check=False,
    )
    return result.returncode == 0


def exec_cmd(src: str, cmd: str, namespace: str, kubeconfig: str) -> bool:
    result = run_kubectl(
        ["exec", "-n", namespace, src, "--", "sh", "-c", cmd],
        kubeconfig=kubeconfig,
        check=False,
    )
    return result.returncode == 0


def record_result(results: List[dict], test: str, src: str, dst: str, expected: str, success: bool) -> None:
    observed = "allow" if success else "drop"
    passed = success if expected == "allow" else not success
    results.append({
        "test": test,
        "src": src,
        "dst": dst,
        "expected": expected,
        "observed": observed,
        "ok": passed,
    })


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", required=True)
    args = parser.parse_args()

    config = load_config(args.config)
    kubeconfig = config.get("kubeconfig")
    namespace = config.get("namespace", "default")
    include_cp = bool(config.get("include_control_plane", False))
    pod_image = config.get("pod_image", "busybox:1.36")
    pods_per_node = int(config.get("pods_per_node", 2))
    external_ip = config.get("external_ip", "8.8.8.8")

    tenants = config.get("tenants", [])
    if not tenants:
        tenants = [{"name": "t1", "zones": 3}, {"name": "t2", "zones": 3}, {"name": "t3", "zones": 3}]

    nodes = nodes_for_tests(get_nodes(kubeconfig), include_cp)
    if len(nodes) < 2:
        print("Need at least 2 nodes for isolation tests", file=sys.stderr)
        return 1
    node_shorts = [node.rsplit("-", 1)[-1] for node in nodes]

    create_tenants(tenants, kubeconfig, zones_override=len(nodes))
    create_pods(tenants, nodes, pods_per_node, namespace, pod_image, kubeconfig)
    wait_for_pods("test-suite=isolation", namespace, kubeconfig, timeout="600s")

    time.sleep(3)
    ip_map = pod_ip_map(namespace, kubeconfig)

    results = []

    tenant_a = tenants[0]["name"]
    src = f"pod1-{tenant_a}-{node_shorts[0]}"
    if src not in ip_map:
        print(f"Source pod {src} not ready", file=sys.stderr)
        return 2

    # Same-tenant, same-node
    same_node_dst = f"pod2-{tenant_a}-{node_shorts[0]}"
    if same_node_dst in ip_map:
        ping_ok = exec_ping(src, ip_map[same_node_dst], namespace, kubeconfig)
        record_result(results, "same-tenant-same-node", src, same_node_dst, "allow", ping_ok)

    # Same-tenant, different-node
    for node_short in node_shorts[1:]:
        dst = f"pod1-{tenant_a}-{node_short}"
        if dst not in ip_map:
            continue
        ping_ok = exec_ping(src, ip_map[dst], namespace, kubeconfig)
        record_result(results, "same-tenant-different-node", src, dst, "allow", ping_ok)

    # Different-tenant, same-node
    for tenant in tenants[1:]:
        dst = f"pod1-{tenant['name']}-{node_shorts[0]}"
        if dst not in ip_map:
            continue
        ping_ok = exec_ping(src, ip_map[dst], namespace, kubeconfig)
        record_result(results, "different-tenant-same-node", src, dst, "drop", ping_ok)

    # Different-tenant, different-node
    for tenant in tenants[1:]:
        for node_short in node_shorts[1:]:
            dst = f"pod1-{tenant['name']}-{node_short}"
            if dst not in ip_map:
                continue
            ping_ok = exec_ping(src, ip_map[dst], namespace, kubeconfig)
            record_result(results, "different-tenant-different-node", src, dst, "drop", ping_ok)

    # DNS, API, and external
    dns_ok = exec_cmd(src, "nslookup kubernetes.default.svc.cluster.local", namespace, kubeconfig)
    record_result(results, "dns", src, "nslookup kubernetes.default.svc.cluster.local", "allow", dns_ok)

    api_ok = exec_cmd(src, "wget -qO- --timeout=2 https://kubernetes.default.svc.cluster.local >/dev/null", namespace, kubeconfig)
    record_result(results, "kube-api", src, "wget -qO- --timeout=2 https://kubernetes.default.svc.cluster.local >/dev/null", "allow", api_ok)

    ext_ok = exec_ping(src, external_ip, namespace, kubeconfig)
    record_result(results, "external-ip", src, external_ip, "allow", ext_ok)

    output_dir = pathlib.Path("Testing/results")
    output_dir.mkdir(parents=True, exist_ok=True)
    output_path = output_dir / "isolation_results.json"
    output_path.write_text(json.dumps(results, indent=2), encoding="utf-8")

    failures = [r for r in results if not r["ok"]]
    print(json.dumps(results, indent=2))
    if failures:
        print(f"Failures: {len(failures)}", file=sys.stderr)
        return 2
    print("All isolation checks passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
