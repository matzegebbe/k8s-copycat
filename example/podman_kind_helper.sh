#!/bin/bash

# Determine the tag
if [ -n "$1" ]; then
  TAG=$1
elif git rev-parse --abbrev-ref HEAD >/dev/null 2>&1; then
  TAG=$(git rev-parse --abbrev-ref HEAD)
else
  TAG="latest"
fi

podman build -t ghcr.io/matzegebbe/k8s-copycat:$TAG .
podman save ghcr.io/matzegebbe/k8s-copycat:$TAG -o k8s-copycat.tar
kind load image-archive k8s-copycat.tar --name kind
