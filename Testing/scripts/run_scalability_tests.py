#!/usr/bin/env python3
import argparse
import json
import pathlib
import sys

import yaml

SCRIPT_DIR = pathlib.Path(__file__).resolve().parent
sys.path.append(str(SCRIPT_DIR))

from lib.kube import get_pods, run_kubectl


def load_config(path: str) -> dict:
    with open(path, "r", encoding="utf-8") as handle:
        return yaml.safe_load(handle)


def exec_in_daemon(pod: str, namespace: str, cmd: str, kubeconfig: str) -> str:
    result = run_kubectl(
        ["exec", "-n", namespace, pod, "--", "sh", "-c", cmd],
        kubeconfig=kubeconfig,
        check=False,
    )
    if result.returncode != 0:
        return result.stderr.decode("utf-8")
    return result.stdout.decode("utf-8")


def count_lines(text: str) -> int:
    return len([line for line in text.splitlines() if line.strip()])


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", required=True)
    args = parser.parse_args()

    config = load_config(args.config)
    kubeconfig = config.get("kubeconfig")
    namespace = config.get("namespace", "default")

    daemon_pods = get_pods("app=daemon", namespace, kubeconfig)
    if not daemon_pods:
        print("No daemon pods found. Is the daemon DaemonSet running?", file=sys.stderr)
        return 1

    results = []
    for pod in daemon_pods:
        name = pod.get("metadata", {}).get("name")
        if not name:
            continue
        links = exec_in_daemon(name, namespace, "ip -o link show || true", kubeconfig)
        routes = exec_in_daemon(name, namespace, "ip -o route show || true", kubeconfig)
        fdb = exec_in_daemon(name, namespace, "bridge fdb show || true", kubeconfig)
        arp = exec_in_daemon(name, namespace, "ip neigh show || true", kubeconfig)

        results.append(
            {
                "pod": name,
                "links": count_lines(links),
                "routes": count_lines(routes),
                "fdb": count_lines(fdb),
                "arp": count_lines(arp),
            }
        )

    output_dir = pathlib.Path("Testing/results")
    output_dir.mkdir(parents=True, exist_ok=True)
    output_path = output_dir / "scalability_results.json"
    output_path.write_text(json.dumps(results, indent=2), encoding="utf-8")

    print(json.dumps(results, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
