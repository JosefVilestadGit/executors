# Introduction
This repo contains implementation of several Colonies executors. 

## Executors
| Executor | Purpose                                           |
| :---     | :-----------                                      |
| echo     | An executor that just echo back its input         |
| sleep    | An executor that sleeps, can be used for testings |
| kube     | Kubernetes Container Executor                     |
| hpc      | Slurm Container Executor                          |
| docker   | Docker Container Executor                         |
| backup   | An executor to backup PostgreSQL databases        |
| deployment-controller | Reconciles container deployments based on ExecutorDeployment resources |

## Functions
### Echo executor
| Function     | Description                                     |
| :----------- | :-----------                                    |
| **echo**     | Closes the process with the input set as output |

### Sleep executor
| Function     | Description  |
| :----------- | :----------- |
| **sleep**    | Sleep        |

### Backup executor
| Function     | Description      |
| :----------- | :-----------     |
| **backup**   | Trigger a backup |

### Deployment Controller executor
| Function     | Description      |
| :----------- | :-----------     |
| **reconcile** | Reconcile container deployments to match desired state |
