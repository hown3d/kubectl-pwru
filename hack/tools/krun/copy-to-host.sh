#!/usr/bin/env bash

echo "> spin up krun container"
container=$(docker run -q -d --rm --entrypoint=bash krun -c 'sleep infinity')
rootfs="/var/lib/docker/rootfs/overlayfs/${container}"

function copy() {
  local path=$1
  local dst=$path
  if [[ -n $2 ]]; then
    dst=$2
  fi
  local full_path="${rootfs%/}/${path#/}"

  if [ ! -f ${path} ]; then
    echo "> copy krun container $path to $dst on host"
    docker run -it --rm --privileged --pid=host alpine nsenter -t 1 -m -u -n -i \
      cp --remove-destination -L -R "$full_path" "$dst"
  fi
}

function copy_if_missing() {
  local path=$1
  local full_path="${rootfs%/}/${path#/}"

  echo "> check if $path is already on host"
  if ! docker run -it --rm --privileged --pid=host alpine nsenter -t 1 -m -u -n -i test -f "$path"; then
    copy "$@"
  else
    echo "> skip copy $path"
  fi
}

function cleanup() {
  echo "> cleanup krun container"
  docker rm -f "$container"
}

trap cleanup EXIT

echo "> copy krun files to host"
copy /usr/local/bin/krun
copy /usr/local/lib64/libkrun.so /usr/lib/aarch64-linux-gnu/libkrun.so.1
copy /usr/local/lib64/libkrunfw.so /usr/lib/aarch64-linux-gnu/libkrunfw.so.5
copy_if_missing /usr/lib/aarch64-linux-gnu/libyajl.so.2
copy_if_missing /usr/lib/aarch64-linux-gnu/libcap.so.2
