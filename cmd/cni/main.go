package main

/* cni function
- Handle CNI ADD/CHECK/DEL requests
	- Parse CNI request
	- Create CNI Request object and send to NetworkManager via Unix socket
	- Wait for response and forward it to the CNI caller - the kubelet

*/
