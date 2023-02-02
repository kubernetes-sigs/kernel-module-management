# Troubleshooting

## Reading operator logs

| Component | Command                                                                              |
|-----------|--------------------------------------------------------------------------------------|
| KMM       | `kubectl logs -fn openshift-kmm deployments/kmm-operator-controller-manager`         |
| KMM-Hub   | `kubectl logs -fn openshift-kmm-hub deployments/kmm-operator-hub-controller-manager` |
