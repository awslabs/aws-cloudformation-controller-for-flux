# Build the controller binary
FROM public.ecr.aws/docker/library/golang:1.20 as builder

ARG TARGETARCH

WORKDIR /workspace

ENV GOPROXY=https://proxy.golang.org|direct
ENV GO111MODULE=on
ENV GOARCH=$TARGETARCH
ENV GOOS=linux
ENV CGO_ENABLED=0

# Copy license and attributions
COPY LICENSE LICENSE
COPY THIRD-PARTY-LICENSES.txt THIRD-PARTY-LICENSES.txt

# Copy API submodule and module manifests
COPY api/ api/
COPY go.mod go.mod
COPY go.sum go.sum

# Cache deps
RUN go mod download

# Copy controller source code
COPY main.go main.go
COPY internal/ internal/

# Build
ARG BUILD_SHA
ARG BUILD_VERSION

RUN go build \
 	-ldflags "-X main.BuildSHA=$BUILD_SHA -X main.BuildVersion=$BUILD_VERSION" \
 	-a -o bin/cfn-controller main.go

# Build the controller image
FROM public.ecr.aws/eks-distro-build-tooling/eks-distro-minimal-base-nonroot:2021-12-01-1638322424

COPY --from=builder /workspace/bin/cfn-controller /workspace/LICENSE /workspace/THIRD-PARTY-LICENSES.txt /bin/

USER 1000

ENTRYPOINT ["/bin/cfn-controller"]
