#!/usr/bin/env python3
import argparse
import pathlib
import sys

import yaml


def render_inventory(config: dict) -> str:
    nodes = config.get("nodes", {})
    cp_nodes = nodes.get("control_plane", [])
    worker_nodes = nodes.get("workers", [])
    ssh = config.get("ssh", {})

    if not cp_nodes or not worker_nodes:
        raise ValueError("config.yaml must include control_plane and workers with host IPs")

    lines = []
    lines.append("[control_plane]")
    for node in cp_nodes:
        lines.append(f"{node['name']} ansible_host={node['host']}")
    lines.append("")

    lines.append("[workers]")
    for node in worker_nodes:
        lines.append(f"{node['name']} ansible_host={node['host']}")
    lines.append("")

    lines.append("[all:vars]")
    if ssh.get("user"):
        lines.append(f"ansible_user={ssh['user']}")
    if ssh.get("private_key"):
        lines.append(f"ansible_ssh_private_key_file={ssh['private_key']}")

    return "\n".join(lines) + "\n"


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", required=True)
    parser.add_argument("--output", default="Testing/ansible/inventory.ini")
    args = parser.parse_args()

    with open(args.config, "r", encoding="utf-8") as handle:
        config = yaml.safe_load(handle)

    output_text = render_inventory(config)
    output_path = pathlib.Path(args.output)
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(output_text, encoding="utf-8")
    print(f"Wrote inventory to {output_path}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
