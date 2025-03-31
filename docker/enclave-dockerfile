FROM amazonlinux:2

# Install packages
#
RUN amazon-linux-extras install aws-nitro-enclaves-cli -y && \
    yum install procps-ng jq -y && \
    yum clean all && \
    rm -rf /var/cache/yum

COPY resources/docker/run.sh  /
COPY resources/docker/synthetic-wallet-enclave.eif /eif/

CMD ["/run.sh", "start"]
