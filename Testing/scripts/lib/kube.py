import json
import os
import subprocess
from typing import Any, Dict, List, Optional


def run_kubectl(args: List[str], kubeconfig: Optional[str] = None, input_text: Optional[str] = None,
        check: bool = True) -> subprocess.CompletedProcess:
    cmd = ["kubectl"] + args
    env = None
    if kubeconfig:
        env = dict(os.environ)
        env["KUBECONFIG"] = kubeconfig
    return subprocess.run(
        cmd,
        input=input_text.encode("utf-8") if input_text else None,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        env=env,
        check=check,
    )


def apply_yaml(yaml_text: str, kubeconfig: Optional[str]) -> None:
    run_kubectl(["apply", "-f", "-"], kubeconfig=kubeconfig, input_text=yaml_text)


def get_nodes(kubeconfig: Optional[str]) -> List[Dict[str, Any]]:
    result = run_kubectl(["get", "nodes", "-o", "json"], kubeconfig=kubeconfig)
    payload = json.loads(result.stdout.decode("utf-8"))
    return payload.get("items", [])


def get_pods(label_selector: str, namespace: str, kubeconfig: Optional[str]) -> List[Dict[str, Any]]:
    result = run_kubectl([
        "get",
        "pods",
        "-n",
        namespace,
        "-l",
        label_selector,
        "-o",
        "json",
    ], kubeconfig=kubeconfig)
    payload = json.loads(result.stdout.decode("utf-8"))
    return payload.get("items", [])


def wait_for_pods(label_selector: str, namespace: str, kubeconfig: Optional[str], timeout: str = "300s") -> None:
    run_kubectl([
        "wait",
        "--for=condition=Ready",
        "pod",
        "-n",
        namespace,
        "-l",
        label_selector,
        "--timeout",
        timeout,
    ], kubeconfig=kubeconfig)
