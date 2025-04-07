FROM dimozone/nitro-enclave-builder:latest

COPY ./docker/enclave-runner/run.sh /run.sh
RUN chmod +x /run.sh
# Get Image from Environment Variable
ARG ENCLAVE_NAME
ARG ENCLAVE_TAG
ARG ENCLAVE_IMAGE
RUN echo "ENCLAVE_NAME: ${ENCLAVE_NAME}"
RUN echo "ENCLAVE_TAG: ${ENCLAVE_TAG}"
RUN echo "ENCLAVE_IMAGE: ${ENCLAVE_IMAGE}"
RUN nitro-cli build-enclave --docker-uri ${ENCLAVE_IMAGE}:${ENCLAVE_TAG} --output-file app.eif



ENV ENCLAVE_NAME=${ENCLAVE_NAME}
CMD ["/run.sh", "start"]
