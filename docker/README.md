## Prerequisite

### Install Docker and Docker Compose
MacOS:
```sh
# The easiest and recommended way to get Docker and
# Docker Compose is to install Docker Desktop here:
https://docs.docker.com/desktop/install/mac-install/
```

Ubuntu:
```sh
# Follow the below link to install docker on ubuntu
https://docs.docker.com/engine/install/ubuntu/#install-using-the-repository
# Follow the below link to install standalone docker compose
https://docs.docker.com/compose/install/other/
```

## Local Cluster

Detailed instruction: see the `Makefile` in the root of [the repo](https://github.com/sei-protocol/sei-chain/blob/master/Makefile) 

**To start a single local node**

```sh
make build-docker-node && make run-docker-node
```

**To start 4 node cluster**

```sh
# If this is the first time or you want to rebuild the binary:
make docker-cluster-start

# If you have run docker-cluster-start and build/seid exist, 
# you can skip the build process to quick start by:
make docker-cluster-start-skipbuild
```
All the logs and genesis files will be generated under the temporary build/generated folder. 

```sh
# To monitor logs after cluster is started
tail -f build/generated/seid-0.log
```

**To ssh into a single node**
```sh
# List all containers
docker ps -a
# SSH into a running container
docker exec -it [container_name] /bin/bash
```

****



# Build with Us!
If you are interested in building with Sei Network:
Email us at team@seinetwork.io
DM us on Twitter https://twitter.com/SeiNetwor