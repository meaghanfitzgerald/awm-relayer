### Build Stage ###
FROM golang:1.20.8-bullseye as build

WORKDIR /go/src
# Copy the code into the container
COPY . .
RUN go mod tidy
# Build awm-relayer
RUN bash ./scripts/build.sh

FROM build AS lint_env
RUN curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v1.55.2

FROM build AS e2e_test_env
ENV BASEDIR=/tmp/e2e-test
ENV AVALANCHEGO_BUILD_PATH=/tmp/e2e-test/avalanchego
RUN git clone --depth 1 --branch v0.5.6 https://github.com/ava-labs/subnet-evm.git && \
    cd subnet-evm && \
    mkdir -p /tmp/e2e-test && \
    ./scripts/install_avalanchego_release.sh && \
    ./scripts/build.sh \
        /tmp/e2e-test/avalanchego/plugins/srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy

FROM build AS test
RUN go test ./...

FROM lint_env AS lint
RUN golangci-lint run --path-prefix=. --timeout 3m

FROM e2e_test_env AS test_e2e
ENV DATA_DIR=/tmp/e2e-test/data
RUN ./scripts/e2e_test.sh

### RUN Stage ###
FROM golang:1.20.8
COPY --from=build /go/src/build/awm-relayer /usr/bin/awm-relayer
EXPOSE 8080
USER 1001
CMD ["start"]
ENTRYPOINT ["/usr/bin/awm-relayer"]
