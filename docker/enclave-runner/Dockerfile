FROM dimozone/nitro-enclave-builder:latest

ARG ENCLAVE_NAME
COPY ./build/${ENCLAVE_NAME}.eif /${ENCLAVE_NAME}.eif
COPY ./docker/enclave-runner/run.sh /run.sh
RUN chmod +x /run.sh


ENV ENCLAVE_NAME=${ENCLAVE_NAME}
CMD ["/run.sh", "start"]
