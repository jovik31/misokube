#!/usr/bin/env python3
import argparse
import json
import math
import pathlib
import sys
import time
from statistics import mean
from typing import Dict, List, Optional

import yaml

SCRIPT_DIR = pathlib.Path(__file__).resolve().parent
sys.path.append(str(SCRIPT_DIR))

from lib.kube import apply_yaml, get_nodes, get_pods, run_kubectl, wait_for_pods


def log(message: str) -> None:
    print(message, flush=True)


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


def normalize_int_list(value: Optional[object], fallback: List[int]) -> List[int]:
    if value is None:
        return fallback
    if isinstance(value, list):
        return [int(item) for item in value]
    return [int(value)]


def scenario_id(tenant_count: int, pods_per_tenant: int) -> str:
    return f"t{tenant_count}-p{pods_per_tenant}"


def create_tenants(tenant_names: List[str], kubeconfig: str, zones_override: int, scenario: str) -> None:
    manifests = []
    for name in tenant_names:
        manifests.append(
            {
                "apiVersion": "setera.com/v1",
                "kind": "Tenant",
                "metadata": {
                    "name": name,
                    "labels": {
                        "test-suite": "perf",
                        "scenario": scenario,
                    },
                },
                "spec": {
                    "name": name,
                    "zones": zones_override,
                },
            }
        )

    docs = "\n---\n".join([yaml.safe_dump(item, sort_keys=False) for item in manifests])
    apply_yaml(docs, kubeconfig)


def create_perf_pods(
    tenant_names: List[str],
    nodes: List[str],
    pods_per_tenant: int,
    namespace: str,
    image: str,
    kubeconfig: str,
    scenario: str,
) -> Dict[str, List[str]]:
    manifests = []
    pods_by_tenant: Dict[str, List[str]] = {}

    for tenant in tenant_names:
        pods_by_tenant[tenant] = []
        for index in range(1, pods_per_tenant + 1):
            name = f"perf-{scenario}-{tenant}-{index}".replace("_", "-")
            node = nodes[(index - 1) % len(nodes)]
            pods_by_tenant[tenant].append(name)
            manifests.append(
                {
                    "apiVersion": "v1",
                    "kind": "Pod",
                    "metadata": {
                        "name": name,
                        "namespace": namespace,
                        "labels": {
                            "test-suite": "perf",
                            "tenant": tenant,
                            "scenario": scenario,
                        },
                        "annotations": {
                            "setera.com/tenant": tenant,
                        },
                    },
                    "spec": {
                        "nodeName": node,
                        "containers": [
                            {
                                "name": "main",
                                "image": image,
                                "command": ["sh", "-c", "sleep 36000"],
                            }
                        ],
                        "restartPolicy": "Never",
                    },
                }
            )

    docs = "\n---\n".join([yaml.safe_dump(item, sort_keys=False) for item in manifests])
    apply_yaml(docs, kubeconfig)
    return pods_by_tenant


def cleanup_scenario(namespace: str, scenario: str, kubeconfig: str) -> None:
    run_kubectl(
        [
            "delete",
            "pod",
            "-n",
            namespace,
            "-l",
            f"test-suite=perf,scenario={scenario}",
            "--ignore-not-found=true",
            "--wait=false",
        ],
        kubeconfig=kubeconfig,
        check=False,
    )
    run_kubectl(
        [
            "delete",
            "tenant",
            "-l",
            f"test-suite=perf,scenario={scenario}",
            "--ignore-not-found=true",
            "--wait=false",
        ],
        kubeconfig=kubeconfig,
        check=False,
    )


def pod_ip_map(label_selector: str, namespace: str, kubeconfig: str) -> Dict[str, str]:
    pods = get_pods(label_selector, namespace, kubeconfig)
    mapping = {}
    for pod in pods:
        name = pod.get("metadata", {}).get("name")
        ip = pod.get("status", {}).get("podIP")
        if name and ip:
            mapping[name] = ip
    return mapping


def parse_ping_latencies(output_text: str) -> List[float]:
    latencies = []
    for line in output_text.splitlines():
        if "time=" not in line:
            continue
        token = line.split("time=", 1)[-1].strip()
        value = token.split(" ", 1)[0].strip()
        try:
            latencies.append(float(value))
        except ValueError:
            continue
    return latencies


def percentile(values: List[float], pct: float) -> Optional[float]:
    if not values:
        return None
    ordered = sorted(values)
    index = max(0, min(len(ordered) - 1, math.ceil(pct * len(ordered)) - 1))
    return ordered[index]


def ensure_tool(pod: str, namespace: str, tool: str, kubeconfig: str) -> bool:
    checks = [
        f"command -v {tool}",
        f"command -v /bin/{tool}",
        f"command -v /usr/bin/{tool}",
    ]
    cmd = " || ".join(checks)
    result = run_kubectl(
        ["exec", "-n", namespace, pod, "--", "sh", "-c", cmd],
        kubeconfig=kubeconfig,
        check=False,
    )
    if result.returncode != 0:
        err = result.stderr.decode("utf-8").strip()
        if err:
            log(f"Tool check failed in {pod}: {err}")
    return result.returncode == 0


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", required=True)
    args = parser.parse_args()

    config = load_config(args.config)
    log(f"Loaded config from {args.config}")
    kubeconfig = config.get("kubeconfig")
    namespace = config.get("namespace", "default")
    include_cp = bool(config.get("include_control_plane", False))
    pod_image = config.get("pod_image", "busybox:1.36")
    iperf_image = config.get("iperf_image", "networkstatic/iperf3")
    tenants = config.get("tenants", [])

    perf_cfg = config.get("perf", {})
    tenant_counts = normalize_int_list(
        perf_cfg.get("tenant_counts"),
        fallback=[len(tenants) if tenants else 1],
    )
    pods_per_tenant_list = normalize_int_list(
        perf_cfg.get("pods_per_tenant"),
        fallback=[int(config.get("pods_per_node", 2))],
    )
    zones_override = int(perf_cfg.get("zones_override", 0))
    tenant_prefix = perf_cfg.get("tenant_name_prefix", "perf")
    perf_tenant = perf_cfg.get("perf_tenant", config.get("perf_tenant"))
    latency_samples = int(perf_cfg.get("latency_samples", 200))
    ping_interval = str(perf_cfg.get("ping_interval_seconds", 0.02))
    ping_timeout = str(perf_cfg.get("ping_timeout_seconds", 1))
    warmup_seconds = int(perf_cfg.get("warmup_seconds", 2))
    cleanup_between_runs = bool(perf_cfg.get("cleanup_between_runs", True))
    wait_timeout_seconds = int(perf_cfg.get("wait_timeout_seconds", 600))
    use_iperf = bool(perf_cfg.get("use_iperf", True))
    iperf_seconds = int(perf_cfg.get("iperf_seconds", 10))
    iperf_protocol = perf_cfg.get("iperf_protocol", "udp")
    iperf_udp_bandwidth = perf_cfg.get("iperf_udp_bandwidth", "100M")

    log("Fetching nodes")
    nodes = nodes_for_tests(get_nodes(kubeconfig), include_cp)
    if not nodes:
        print("No nodes found for performance tests", file=sys.stderr)
        return 1

    log(f"Using nodes: {', '.join(nodes)}")

    results = []

    for tenant_count in tenant_counts:
        for pods_per_tenant in pods_per_tenant_list:
            if pods_per_tenant < 2:
                print("pods_per_tenant must be at least 2 for latency tests", file=sys.stderr)
                return 2

            scenario = scenario_id(tenant_count, pods_per_tenant)
            if cleanup_between_runs:
                log(f"Scenario {scenario}: cleanup before run")
                cleanup_scenario(namespace, scenario, kubeconfig)

            if tenants and tenant_count <= len(tenants):
                tenant_names = [t["name"] for t in tenants[:tenant_count]]
            else:
                tenant_names = [f"{tenant_prefix}{index + 1}" for index in range(tenant_count)]

            zones = zones_override if zones_override > 0 else len(nodes)
            log(f"Scenario {scenario}: creating tenants")
            create_tenants(tenant_names, kubeconfig, zones, scenario)
            log(f"Scenario {scenario}: creating perf pods")
            pods_by_tenant = create_perf_pods(
                tenant_names,
                nodes,
                pods_per_tenant,
                namespace,
                pod_image,
                kubeconfig,
                scenario,
            )

            log(f"Scenario {scenario}: waiting for perf pods to be Ready")
            pods_snapshot = get_pods(f"test-suite=perf,scenario={scenario}", namespace, kubeconfig)
            if not pods_snapshot:
                print("No perf pods found after creation. Check namespace, RBAC, and image pulls.", file=sys.stderr)
                return 6

            wait_for_pods(
                f"test-suite=perf,scenario={scenario}",
                namespace,
                kubeconfig,
                timeout=f"{wait_timeout_seconds}s",
            )

            time.sleep(warmup_seconds)

            target_tenant = perf_tenant if perf_tenant in pods_by_tenant else tenant_names[0]
            client_pod = pods_by_tenant[target_tenant][0]
            server_pod = pods_by_tenant[target_tenant][1]

            if not ensure_tool(client_pod, namespace, "ping", kubeconfig):
                print("Perf image missing ping. Update pod_image or perf config.", file=sys.stderr)
                return 3

            ip_map = pod_ip_map(f"test-suite=perf,scenario={scenario}", namespace, kubeconfig)
            server_ip = ip_map.get(server_pod)
            if not server_ip:
                print("Could not find server pod IP", file=sys.stderr)
                return 4

            log(f"Scenario {scenario}: running ping latency probes")
            ping_result = run_kubectl(
                [
                    "exec",
                    "-n",
                    namespace,
                    client_pod,
                    "--",
                    "sh",
                    "-c",
                    f"ping -c {latency_samples} -i {ping_interval} -W {ping_timeout} {server_ip}",
                ],
                kubeconfig=kubeconfig,
                check=False,
            )

            ping_output = ping_result.stdout.decode("utf-8")
            latencies = parse_ping_latencies(ping_output)

            latency_summary = {
                "samples": len(latencies),
                "min_ms": min(latencies) if latencies else None,
                "max_ms": max(latencies) if latencies else None,
                "avg_ms": mean(latencies) if latencies else None,
                "p50_ms": percentile(latencies, 0.50),
                "p90_ms": percentile(latencies, 0.90),
                "p99_ms": percentile(latencies, 0.99),
            }

            iperf_summary = None
            if use_iperf:
                log(f"Scenario {scenario}: running iperf3")
                if not ensure_tool(client_pod, namespace, "iperf3", kubeconfig):
                    print("Perf image missing iperf3. Update pod_image or perf config.", file=sys.stderr)
                    return 5

                iperf_cmd = ["iperf3", "-c", server_ip, "-t", str(iperf_seconds), "-J"]
                if iperf_protocol == "udp":
                    iperf_cmd.extend(["-u", "-b", iperf_udp_bandwidth])

                result = run_kubectl(
                    ["exec", "-n", namespace, client_pod, "--"] + iperf_cmd,
                    kubeconfig=kubeconfig,
                    check=False,
                )
                if result.returncode == 0:
                    payload = json.loads(result.stdout.decode("utf-8"))
                    summary_bucket = payload.get("end", {}).get("sum", {})
                    iperf_summary = {
                        "throughput_mbps": summary_bucket.get("bits_per_second", 0) / 1_000_000,
                        "jitter_ms": summary_bucket.get("jitter_ms"),
                        "lost_percent": summary_bucket.get("lost_percent"),
                    }
                else:
                    iperf_summary = {"error": result.stderr.decode("utf-8").strip()}

            results.append(
                {
                    "scenario": {
                        "tenant_count": tenant_count,
                        "pods_per_tenant": pods_per_tenant,
                    },
                    "latency_ms": latency_summary,
                    "iperf": iperf_summary,
                }
            )

            if cleanup_between_runs:
                log(f"Scenario {scenario}: cleanup after run")
                cleanup_scenario(namespace, scenario, kubeconfig)

    output_dir = pathlib.Path("Testing/results")
    output_dir.mkdir(parents=True, exist_ok=True)
    output_path = output_dir / "perf_results.json"
    output_path.write_text(json.dumps(results, indent=2), encoding="utf-8")

    log(json.dumps(results, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
