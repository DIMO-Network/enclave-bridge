#!/bin/bash -e

set -xe
readonly APP_NAME=${APP_NAME:-"enclave-app"}
readonly EIF_PATH="/$APP_NAME.eif"

ENCLAVE_CPU_COUNT=${ENCLAVE_CPU_COUNT:-1}
ENCLAVE_MEMORY_SIZE=${ENCLAVE_MEMORY_SIZE:-1000}
ENCLAVE_CID=${ENCLAVE_CID:-16}

term_handler() {
  echo 'Shutting down enclave'
  nitro-cli terminate-enclave --enclave-name $APP_NAME
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
      --eif-path $EIF_PATH --enclave-cid $ENCLAVE_CID &
    echo 'Enclave running.'
  else
    echo 'Starting development enclave.'
    nitro-cli run-enclave --cpu-count $ENCLAVE_CPU_COUNT --memory $ENCLAVE_MEMORY_SIZE \
      --eif-path $EIF_PATH --enclave-cid $ENCLAVE_CID --attach-console &
    echo 'Enclave started in debug mode.'
  fi

  # wait forever
  while true
  do
    tail -f /dev/null & wait ${!}
  done
}

healthcheck() {
  cmd="nitro-cli describe-enclaves | jq -e '"'[ .[] | select( .EnclaveName == "'$APP_NAME'" and .State == "RUNNING") ] | length == 1 '"'"
  bash -c "$cmd"
}

"$@" 