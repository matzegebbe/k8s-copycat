#!/bin/bash

podman build -t ghcr.io/matzegebbe/k8s-copycat:1 .
podman save ghcr.io/matzegebbe/k8s-copycat:1 -o k8s-copycat.tar
kind load image-archive k8s-copycat.tar --name kind
