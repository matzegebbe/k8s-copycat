#!/bin/bash

podman build -t ghcr.io/matzegebbe/k8s-image-doppler:1 .
podman save ghcr.io/matzegebbe/k8s-image-doppler:1 -o doppler.tar
kind load image-archive doppler.tar --name kind
