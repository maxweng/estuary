FROM golang:1.18 AS builder
ARG BRANCH=dev
RUN apt-get update && \
    apt-get install -y wget jq hwloc ocl-icd-opencl-dev git libhwloc-dev pkg-config make && \
    apt-get install -y cargo
WORKDIR /app/
RUN curl https://sh.rustup.rs -sSf | bash -s -- -y
ENV PATH="/root/.cargo/bin:${PATH}"
RUN cargo --help
RUN git clone  -b ${BRANCH} --single-branch https://github.com/maxweng/estuary . && \
    RUSTFLAGS="-C target-cpu=native -g" FFI_BUILD_FROM_SOURCE=1 FFI_USE_BLST_PORTABLE=1 make all
RUN cp ./estuary ./estuary-shuttle ./benchest ./bsget /usr/local/bin

FROM golang:1.18
RUN apt-get update && \
    apt-get install -y hwloc libhwloc-dev ocl-icd-opencl-dev

COPY --from=builder /app/estuary /usr/local/bin
COPY --from=builder /app/estuary-shuttle /usr/local/bin
COPY --from=builder /app/benchest /usr/local/bin
COPY --from=builder /app/bsget /usr/local/bin
