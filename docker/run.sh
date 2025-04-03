#!/bin/bash -e

set -xe
readonly ENCLAVE_NAME="sample-enclave-app"
readonly EIF_PATH="/eif/$ENCLAVE_NAME.eif"

ENCLAVE_CPU_COUNT=${ENCLAVE_CPU_COUNT:-1}
ENCLAVE_MEMORY_SIZE=${ENCLAVE_MEMORY_SIZE:-1000}
ENCLAVE_CID=${ENCLAVE_CID:-16}

# Configure DNS resolution
echo "nameserver 8.8.8.8" > /etc/resolv.conf
echo "nameserver 8.8.4.4" >> /etc/resolv.conf

# Configure network interface
ip link set dev eth0 up
ip addr add 169.254.0.2/16 dev eth0
ip route add default via 169.254.0.1 dev eth0

term_handler() {
  echo 'Shutting down enclave'
  nitro-cli terminate-enclave --enclave-name $ENCLAVE_NAME
  kill -0 $(pgrep nitro-cli)
  echo 'Shutdown complete'
  exit 0;
}

# run application
start() {
  trap 'kill ${!}; term_handler' SIGTERM

  if [[ -z "${ENCLAVE_DEBUG_MODE}" ]]; then
    echo 'Starting production enclave.'
    nitro-cli run-enclave --cpu-count $ENCLAVE_CPU_COUNT --memory $ENCLAVE_MEMORY_SIZE \
      --eif-path $EIF_PATH --enclave-cid $ENCLAVE_CID \
      --network-interface eth0 &
    echo 'Enclave running.'
  else
    echo 'Starting development enclave.'
    nitro-cli run-enclave --cpu-count $ENCLAVE_CPU_COUNT --memory $ENCLAVE_MEMORY_SIZE \
      --eif-path $EIF_PATH --enclave-cid $ENCLAVE_CID --attach-console \
      --network-interface eth0 &
    echo 'Enclave started in debug mode.'
  fi

  # wait forever
  while true
  do
    tail -f /dev/null & wait ${!}
  done
}

healthcheck() {
  cmd="nitro-cli describe-enclaves | jq -e '"'[ .[] | select( .EnclaveName == "'$ENCLAVE_NAME'" and .State == "RUNNING") ] | length == 1 '"'"
  bash -c "$cmd"
}

"$@" 