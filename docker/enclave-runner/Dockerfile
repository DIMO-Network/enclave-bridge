FROM dimozone/nitro-enclave-builder:latest

ARG APP_NAME
COPY ./bin/${APP_NAME}.eif /${APP_NAME}.eif
COPY ./docker/enclave-runner/run.sh /run.sh
RUN chmod +x /run.sh


ENV APP_NAME=${APP_NAME}
CMD ["/run.sh", "start"]
