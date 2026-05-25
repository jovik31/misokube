# Testing Automation (kubeadm + MisoKube)

This folder contains Ansible playbooks to provision a kubeadm cluster and Python scripts to run isolation, performance, and scalability tests.

## Quick start

1) Copy the config and fill your values:

```bash
cp Testing/config.example.yaml Testing/config.yaml
```

2) Generate an inventory once you know the IPs and SSH info:

```bash
python3 Testing/scripts/generate_inventory.py --config Testing/config.yaml
```

3) Run the Ansible playbooks in order:

```bash
ansible-playbook Testing/ansible/playbooks/00_prereqs.yml
ansible-playbook Testing/ansible/playbooks/01_init_control_plane.yml
ansible-playbook Testing/ansible/playbooks/02_join_workers.yml
ansible-playbook Testing/ansible/playbooks/03_install_misokube.yml
```

4) Run the tests:

```bash
python3 Testing/scripts/run_isolation_tests.py --config Testing/config.yaml
python3 Testing/scripts/run_perf_tests.py --config Testing/config.yaml
python3 Testing/scripts/run_scalability_tests.py --config Testing/config.yaml
```

## Notes

- The playbooks assume Debian/Ubuntu hosts. Adjust for other distros.
- The image build step uses Docker on the control-plane node and loads images into containerd on all nodes.
- If you want a different Kubernetes version, set `k8s_version` and `k8s_repo_version` in the config.
- The performance script now supports scaling tenants and pods per tenant with p99 latency from ping output. Ensure the perf pod image includes `ping` (and `iperf3` if enabled).
- The isolation script uses the pod annotation `setera.com/tenant` to map pods to tenants.

## Dependencies

- Ansible 2.13+
- Python 3.9+
- Python packages: `pyyaml`

Install Python deps:

```bash
pip3 install pyyaml
```
